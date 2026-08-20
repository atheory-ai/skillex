package streamhttp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/atheory-ai/skillex/internal/capability"
)

func TestFactoryCallsStatelessHTTPWithRoutingHeadersAndExactAuth(t *testing.T) {
	var methods []string
	transport := roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		methods = append(methods, r.Header.Get("Mcp-Method"))
		if got := r.Header.Get("MCP-Protocol-Version"); got != ProtocolVersion {
			t.Errorf("protocol header = %q", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer mapped" {
			t.Errorf("authorization = %q", got)
		}
		var request rpcRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		var result any
		switch request.Method {
		case "server/discover":
			result = map[string]any{"supportedVersions": []string{ProtocolVersion}}
		case "tools/list":
			result = map[string]any{"tools": []any{map[string]any{"name": "issues.create", "inputSchema": map[string]any{"type": "object"}}}}
		case "tools/call":
			if r.Header.Get("Mcp-Name") != "issues.create" {
				t.Errorf("Mcp-Name = %q", r.Header.Get("Mcp-Name"))
			}
			result = map[string]any{"resultType": "complete", "structuredContent": map[string]any{"ok": true}}
		}
		data, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": result})
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(string(data))), Header: http.Header{}}, nil
	})

	selected, err := (capability.Capability{
		Server: capability.ServerVersion{Identity: capability.ServerIdentity{CanonicalName: "io.example/issues"}, Version: "1.0.0"},
		Kind:   capability.CapabilityTool, Name: "issues.create", InputSchemaJSON: json.RawMessage(`{"type":"object"}`),
	}).WithComputedSchemaDigest()
	if err != nil {
		t.Fatal(err)
	}
	factory, err := NewFactory([]ServerConfig{{
		CanonicalName: "io.example/issues", Version: "1.0.0", Endpoint: "http://127.0.0.1/mcp",
		Headers:    map[string]string{"Authorization": "Bearer mapped"},
		HTTPClient: &http.Client{Transport: transport},
	}})
	if err != nil {
		t.Fatal(err)
	}
	client, err := factory.Open(context.Background(), selected)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if _, err := client.CallTool(context.Background(), "issues.create", map[string]any{"title": "test"}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(methods, []string{"server/discover", "tools/list", "tools/call"}) {
		t.Fatalf("methods = %v", methods)
	}
}

func TestFactoryAllowsOptionalDiscoverAndRefusesRedirect(t *testing.T) {
	targetCalled := false
	transport := roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host == "target.example" {
			targetCalled = true
			t.Errorf("redirect target received Authorization=%q", r.Header.Get("Authorization"))
		}
		return &http.Response{
			StatusCode: http.StatusTemporaryRedirect, Body: io.NopCloser(strings.NewReader("")),
			Header: http.Header{"Location": []string{"https://target.example/mcp"}}, Request: r,
		}, nil
	})
	factory, err := NewFactory([]ServerConfig{{
		CanonicalName: "io.example/test", Version: "1", Endpoint: "https://source.example/mcp",
		Headers: map[string]string{"Authorization": "Bearer secret"}, HTTPClient: &http.Client{Transport: transport},
	}})
	if err != nil {
		t.Fatal(err)
	}
	selected, _ := (capability.Capability{Server: capability.ServerVersion{Identity: capability.ServerIdentity{CanonicalName: "io.example/test"}, Version: "1"}, Kind: capability.CapabilityTool, Name: "tool"}).WithComputedSchemaDigest()
	_, err = factory.Open(context.Background(), selected)
	if err == nil || !errors.Is(err, errMethodNotFound) && err.Error() != "downstream MCP HTTP redirect refused" {
		t.Fatalf("Open() error = %v", err)
	}
	if targetCalled {
		t.Fatal("redirect was followed")
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }
