// Package registryapi synchronizes trusted MCP Registry API metadata into a
// bounded offline catalog snapshot. Synchronization is explicit; queries never
// call a registry service.
package registryapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/atheory-ai/skillex/internal/capability"
	"github.com/atheory-ai/skillex/internal/registry"
	"github.com/atheory-ai/skillex/internal/trust"
)

const (
	maxPageBytes = 16 << 20
	maxPages     = 1000
)

type SyncResult struct {
	Source  string `json:"source"`
	Servers int    `json:"servers"`
	Pages   int    `json:"pages"`
	Path    string `json:"path"`
}

type Client struct {
	HTTPClient *http.Client
}

// Sync follows opaque Registry API cursors, filters trusted namespaces, and
// atomically replaces one offline metadata snapshot.
func (c Client) Sync(ctx context.Context, source trust.CatalogSource, destination string) (SyncResult, error) {
	base, err := validateBaseURL(source.BaseURL)
	if err != nil {
		return SyncResult{}, err
	}
	httpClient := http.Client{Timeout: 30 * time.Second}
	if c.HTTPClient != nil {
		httpClient = *c.HTTPClient
	}
	httpClient.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }

	seenCursor := map[string]bool{}
	seenServer := map[string]bool{}
	var capabilities []capability.Capability
	var transports []registry.TransportRecord
	cursor := ""
	pages := 0
	for ; pages < maxPages; pages++ {
		endpoint := *base
		endpoint.Path = strings.TrimRight(endpoint.Path, "/") + "/v0.1/servers"
		query := endpoint.Query()
		query.Set("limit", "100")
		if cursor != "" {
			query.Set("cursor", cursor)
		}
		endpoint.RawQuery = query.Encode()
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
		if err != nil {
			return SyncResult{}, err
		}
		request.Header.Set("Accept", "application/json")
		response, err := httpClient.Do(request)
		if err != nil {
			return SyncResult{}, fmt.Errorf("synchronizing MCP registry %s: %w", source.Name, err)
		}
		if response.StatusCode >= 300 && response.StatusCode < 400 {
			response.Body.Close()
			return SyncResult{}, errors.New("MCP registry redirect refused")
		}
		if response.StatusCode != http.StatusOK {
			response.Body.Close()
			return SyncResult{}, fmt.Errorf("MCP registry returned HTTP %d", response.StatusCode)
		}
		data, err := io.ReadAll(io.LimitReader(response.Body, maxPageBytes+1))
		response.Body.Close()
		if err != nil {
			return SyncResult{}, err
		}
		if len(data) > maxPageBytes {
			return SyncResult{}, errors.New("MCP registry page exceeds size limit")
		}
		var page serverList
		if err := json.Unmarshal(data, &page); err != nil {
			return SyncResult{}, fmt.Errorf("decoding MCP Registry API response: %w", err)
		}
		for _, envelope := range page.Servers {
			server := envelope.Server
			if server.Name == "" {
				server = envelope.serverDetail
			}
			if !namespaceAllowed(server.Name, source.AllowedNamespaces) || server.Version == "" || server.Description == "" {
				continue
			}
			key := server.Name + "\x00" + server.Version
			if seenServer[key] {
				continue
			}
			seenServer[key] = true
			publisher, _, _ := strings.Cut(server.Name, "/")
			selected, err := (capability.Capability{
				Server: capability.ServerVersion{
					Identity: capability.ServerIdentity{CanonicalName: server.Name, Publisher: publisher},
					Version:  server.Version, Status: envelope.Meta.Official.Status,
				},
				Kind: capability.CapabilityServer, Name: "server.discover", Title: server.Title,
				Description: server.Description, Availability: capability.AvailabilityDiscovered,
			}).WithComputedSchemaDigest()
			if err != nil {
				return SyncResult{}, err
			}
			capabilities = append(capabilities, selected)
			for _, remote := range server.Remotes {
				transports = append(transports, registry.TransportRecord{Server: server.Name, Version: server.Version,
					Kind: remote.Type, Endpoint: remote.URL, Origin: source.Name})
			}
			for _, pkg := range server.Packages {
				transports = append(transports, registry.TransportRecord{Server: server.Name, Version: server.Version,
					Kind: pkg.Transport.Type, PackageRegistry: pkg.RegistryType, PackageID: pkg.Identifier,
					PackageVersion: pkg.Version, PackageDigest: pkg.FileSHA256, Origin: source.Name})
			}
		}
		next := page.Metadata.NextCursor
		if next == "" {
			pages++
			break
		}
		if seenCursor[next] {
			return SyncResult{}, errors.New("MCP registry repeated a pagination cursor")
		}
		seenCursor[next] = true
		cursor = next
	}
	if pages >= maxPages && cursor != "" {
		return SyncResult{}, fmt.Errorf("MCP registry exceeded %d pages", maxPages)
	}
	document, err := json.MarshalIndent(struct {
		Capabilities []capability.Capability    `json:"capabilities"`
		Transports   []registry.TransportRecord `json:"transports,omitempty"`
	}{Capabilities: capabilities, Transports: transports}, "", "  ")
	if err != nil {
		return SyncResult{}, err
	}
	if err := atomicWrite(destination, document); err != nil {
		return SyncResult{}, err
	}
	return SyncResult{Source: source.Name, Servers: len(capabilities), Pages: pages, Path: destination}, nil
}

type serverList struct {
	Servers  []serverEnvelope `json:"servers"`
	Metadata struct {
		NextCursor string `json:"nextCursor"`
	} `json:"metadata"`
}

type serverEnvelope struct {
	Server serverDetail `json:"server"`
	serverDetail
	Meta struct {
		Official struct {
			Status string `json:"status"`
		} `json:"io.modelcontextprotocol.registry/official"`
	} `json:"_meta"`
}

type serverDetail struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Remotes     []struct {
		Type string `json:"type"`
		URL  string `json:"url"`
	} `json:"remotes,omitempty"`
	Packages []struct {
		RegistryType string `json:"registryType"`
		Identifier   string `json:"identifier"`
		Version      string `json:"version"`
		FileSHA256   string `json:"fileSha256"`
		Transport    struct {
			Type string `json:"type"`
		} `json:"transport"`
	} `json:"packages,omitempty"`
}

func validateBaseURL(value string) (*url.URL, error) {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errors.New("trusted MCP registry BaseURL is invalid")
	}
	if parsed.Scheme != "https" && (parsed.Scheme != "http" || !isLoopback(parsed.Hostname())) {
		return nil, errors.New("trusted MCP registry BaseURL must use HTTPS (HTTP is allowed only for loopback)")
	}
	return parsed, nil
}

func isLoopback(host string) bool {
	host = strings.ToLower(host)
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

func namespaceAllowed(name string, allowed []string) bool {
	if len(allowed) == 0 {
		return true
	}
	for _, prefix := range allowed {
		if name == prefix || strings.HasPrefix(name, strings.TrimRight(prefix, "/")+"/") {
			return true
		}
	}
	return false
}

func atomicWrite(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".catalog-*.tmp")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(0o600); err != nil {
		temp.Close()
		return err
	}
	if _, err := io.Copy(temp, bytes.NewReader(data)); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(tempPath, path)
}
