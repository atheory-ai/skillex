package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/atheory-ai/skillex/internal/capability"
	"github.com/atheory-ai/skillex/internal/config"
	"github.com/atheory-ai/skillex/internal/connector/stdio"
	"github.com/atheory-ai/skillex/internal/connector/streamhttp"
	"github.com/atheory-ai/skillex/internal/registry"
	"github.com/atheory-ai/skillex/internal/registryapi"
	"github.com/atheory-ai/skillex/internal/trust"
	"github.com/spf13/cobra"
)

func newCatalogCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "catalog", Short: "Synchronize trusted MCP discovery catalogs"}
	cmd.AddCommand(newCatalogSyncCmd(), newCatalogInspectCmd())
	return cmd
}

func newCatalogInspectCmd() *cobra.Command {
	var serverFilter string
	cmd := &cobra.Command{
		Use: "inspect", Short: "Explicitly inspect trusted bound servers and update the offline capability index",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			root := repoRoot()
			cfg, err := config.Load(root)
			if err != nil {
				return err
			}
			if !cfg.MCPEnabled() {
				return fmt.Errorf("MCP capability brokering is not enabled for this project")
			}
			trusted, _, err := trust.LoadConfigured()
			if err != nil {
				return err
			}
			seen := map[string]bool{}
			var capabilities []capability.Capability
			for _, binding := range cfg.MCP.Bindings {
				if serverFilter != "" && binding.Server != serverFilter {
					continue
				}
				key := binding.Server + "\x00" + binding.Version + "\x00" + binding.AuthProfile
				if seen[key] {
					continue
				}
				seen[key] = true
				selected := capability.Capability{Server: capability.ServerVersion{Identity: capability.ServerIdentity{CanonicalName: binding.Server}, Version: binding.Version}, AuthProfile: binding.AuthProfile}
				server, ok := trusted.FindServer(binding.Server, binding.Version)
				if !ok {
					return fmt.Errorf("trusted server %s@%s is not configured", binding.Server, binding.Version)
				}
				var inspected []capability.Capability
				if server.HTTP != nil {
					configured, err := trusted.StreamableHTTPConfig(cmd.Context(), selected, root)
					if err != nil {
						return err
					}
					inspected, err = streamhttp.Inspect(cmd.Context(), configured)
					if err != nil {
						return err
					}
				} else {
					configured, err := trusted.StdioConfig(selected, root)
					if err != nil {
						return err
					}
					inspected, err = stdio.Inspect(cmd.Context(), configured)
					if err != nil {
						return err
					}
				}
				capabilities = append(capabilities, inspected...)
			}
			if len(seen) == 0 {
				return fmt.Errorf("no matching bound trusted MCP server is configured")
			}
			observedAt := time.Now().UTC()
			for i := range capabilities {
				capabilities[i].ObservedAt = observedAt
				capabilities[i].ExpiresAt = observedAt.Add(5 * time.Minute)
			}
			path := filepath.Join(root, ".skillex", "mcp", "observed.json")
			if serverFilter != "" {
				existing, err := readObservedCapabilities(path)
				if err != nil && !os.IsNotExist(err) {
					return err
				}
				for _, selected := range existing {
					if selected.Server.Identity.CanonicalName != serverFilter {
						capabilities = append(capabilities, selected)
					}
				}
			}
			deduplicated := map[string]capability.Capability{}
			for _, selected := range capabilities {
				key := selected.Server.Identity.CanonicalName + "\x00" + selected.Server.Version + "\x00" + string(selected.Kind) + "\x00" + selected.Name
				deduplicated[key] = selected
			}
			capabilities = capabilities[:0]
			for _, selected := range deduplicated {
				capabilities = append(capabilities, selected)
			}
			sort.Slice(capabilities, func(i, j int) bool {
				a := capabilities[i].Server.Identity.CanonicalName + "\x00" + capabilities[i].Server.Version + "\x00" + string(capabilities[i].Kind) + "\x00" + capabilities[i].Name
				b := capabilities[j].Server.Identity.CanonicalName + "\x00" + capabilities[j].Server.Version + "\x00" + string(capabilities[j].Kind) + "\x00" + capabilities[j].Name
				return a < b
			})
			encoded, err := json.MarshalIndent(map[string]any{"capabilities": capabilities}, "", "  ")
			if err != nil {
				return err
			}
			if err := writeCatalogAtomic(path, encoded); err != nil {
				return err
			}
			reg, err := registry.Open(filepath.Join(root, ".skillex", "index.db"))
			if err != nil {
				return err
			}
			defer reg.Close()
			if _, err := registry.Refresh(reg, cfg, registry.RefreshOptions{Root: root, DevMode: true}); err != nil {
				return err
			}
			if flagJSON {
				return json.NewEncoder(os.Stdout).Encode(map[string]any{"servers": len(seen), "capabilities": len(capabilities), "path": path})
			}
			fmt.Printf("Inspected %d trusted servers and indexed %d capabilities\n", len(seen), len(capabilities))
			return nil
		},
	}
	cmd.Flags().StringVar(&serverFilter, "server", "", "Inspect one exact bound canonical server")
	return cmd
}

func readObservedCapabilities(path string) ([]capability.Capability, error) {
	//nolint:gosec // G304: path is the fixed project-local generated catalog.
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(data) > 16<<20 {
		return nil, fmt.Errorf("observed MCP catalog exceeds size limit")
	}
	var document struct {
		Capabilities []capability.Capability `json:"capabilities"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&document); err != nil {
		return nil, fmt.Errorf("parsing observed MCP catalog: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return nil, errors.New("observed MCP catalog contains multiple JSON values")
	}
	return document.Capabilities, nil
}

func writeCatalogAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".observed-*.tmp")
	if err != nil {
		return err
	}
	temporary := file.Name()
	defer os.Remove(temporary)
	if err := file.Chmod(0o600); err != nil {
		file.Close()
		return err
	}
	if _, err := io.Copy(file, bytes.NewReader(data)); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(temporary, path)
}

func newCatalogSyncCmd() *cobra.Command {
	var selectedSource string
	cmd := &cobra.Command{
		Use: "sync", Short: "Synchronize configured MCP Registry API sources into the offline project index",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			root := repoRoot()
			cfg, err := config.Load(root)
			if err != nil {
				return err
			}
			if !cfg.MCPEnabled() {
				return fmt.Errorf("MCP capability brokering is not enabled for this project")
			}
			trusted, _, err := trust.LoadConfigured()
			if err != nil {
				return err
			}
			var results []registryapi.SyncResult
			for _, configured := range cfg.MCP.Catalogs {
				if configured.Type != "trusted" || selectedSource != "" && configured.Name != selectedSource {
					continue
				}
				source, ok := trusted.FindCatalogSource(configured.Name)
				if !ok {
					return fmt.Errorf("trusted MCP catalog %q is not defined", configured.Name)
				}
				destination := filepath.Join(root, ".skillex", "mcp", "catalogs", source.Name+".json")
				result, err := (registryapi.Client{}).Sync(context.Background(), source, destination)
				if err != nil {
					return err
				}
				results = append(results, result)
			}
			if len(results) == 0 {
				return fmt.Errorf("no matching trusted catalog source is configured by this project")
			}
			reg, err := registry.Open(filepath.Join(root, ".skillex", "index.db"))
			if err != nil {
				return err
			}
			defer reg.Close()
			if _, err := registry.Refresh(reg, cfg, registry.RefreshOptions{Root: root, DevMode: true}); err != nil {
				return err
			}
			if flagJSON {
				encoder := json.NewEncoder(os.Stdout)
				encoder.SetIndent("", "  ")
				return encoder.Encode(map[string]any{"sources": results})
			}
			for _, result := range results {
				fmt.Printf("%s: %d servers across %d pages\n", result.Source, result.Servers, result.Pages)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&selectedSource, "source", "", "Synchronize one configured trusted source by name")
	return cmd
}
