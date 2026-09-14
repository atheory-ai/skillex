package registryapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/atheory-ai/skillex/internal/capability"
	"github.com/atheory-ai/skillex/internal/registry"
	"github.com/atheory-ai/skillex/internal/trust"
)

func TestSyncUsesOpaqueCursorsFiltersNamespacesAndWritesOfflineCatalog(t *testing.T) {
	var cursors []string
	transport := roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		cursors = append(cursors, request.URL.Query().Get("cursor"))
		var body string
		if len(cursors) == 1 {
			body = `{"servers":[{"server":{"name":"io.example/issues","version":"1.0.0","title":"Issues","description":"Manage issues","remotes":[{"type":"streamable-http","url":"https://mcp.example/issues"}],"packages":[{"registryType":"npm","identifier":"@example/issues-mcp","version":"1.0.0","transport":{"type":"stdio"}}]},"_meta":{"io.modelcontextprotocol.registry/official":{"status":"active"}}},{"server":{"name":"com.other/ignored","version":"1","description":"Ignore"}}],"metadata":{"nextCursor":"opaque+/="}}`
		} else {
			body = `{"servers":[{"server":{"name":"io.example/weather","version":"2.0.0","description":"Weather forecasts"}}],"metadata":{}}`
		}
		return jsonResponse(body), nil
	})
	destination := filepath.Join(t.TempDir(), "catalog.json")
	result, err := (Client{HTTPClient: &http.Client{Transport: transport}}).Sync(context.Background(), trust.CatalogSource{
		Name: "official", Type: "registry-api", BaseURL: "https://registry.example", AllowedNamespaces: []string{"io.example"},
	}, destination)
	if err != nil {
		t.Fatal(err)
	}
	if result.Servers != 2 || result.Pages != 2 || len(cursors) != 2 || cursors[1] != "opaque+/=" {
		t.Fatalf("result=%#v cursors=%#v", result, cursors)
	}
	data, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Capabilities []capability.Capability    `json:"capabilities"`
		Transports   []registry.TransportRecord `json:"transports"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	if len(document.Capabilities) != 2 || document.Capabilities[0].Kind != capability.CapabilityServer {
		t.Fatalf("offline catalog = %#v", document.Capabilities)
	}
	if len(document.Transports) != 2 || document.Transports[0].Origin != "official" {
		t.Fatalf("transport provenance = %#v", document.Transports)
	}
	if strings.Contains(string(data), "com.other") {
		t.Fatalf("namespace filter failed: %s", data)
	}
}

func jsonResponse(body string) *http.Response {
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }
