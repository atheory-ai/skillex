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

func TestFactoryContinuesWhenOptionalDiscoverIsNotImplemented(t *testing.T) {
	var methods []string
	transport := roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		var request rpcRequest
		_ = json.NewDecoder(r.Body).Decode(&request)
		methods = append(methods, request.Method)
		if request.Method == "server/discover" {
			data, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": request.ID, "error": map[string]any{"code": -32601}})
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(string(data))), Header: http.Header{}}, nil
		}
		data, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": map[string]any{
			"tools": []any{map[string]any{"name": "tool", "inputSchema": map[string]any{}}},
		}})
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(string(data))), Header: http.Header{}}, nil
	})
	selected, _ := (capability.Capability{Server: capability.ServerVersion{Identity: capability.ServerIdentity{CanonicalName: "io.example/test"}, Version: "1"}, Kind: capability.CapabilityTool, Name: "tool", InputSchemaJSON: json.RawMessage(`{}`)}).WithComputedSchemaDigest()
	factory, _ := NewFactory([]ServerConfig{{CanonicalName: "io.example/test", Version: "1", Endpoint: "https://source.example/mcp", HTTPClient: &http.Client{Transport: transport}}})
	connector, err := factory.Open(context.Background(), selected)
	if err != nil {
		t.Fatal(err)
	}
	_ = connector.Close()
	if !reflect.DeepEqual(methods, []string{"server/discover", "tools/list"}) {
		t.Fatalf("methods = %#v", methods)
	}
}

func TestPublicDefinitionCacheAvoidsRepeatedDiscoveryAndListing(t *testing.T) {
	requests := 0
	transport := roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		requests++
		var request rpcRequest
		_ = json.NewDecoder(r.Body).Decode(&request)
		result := map[string]any{"supportedVersions": []string{ProtocolVersion}}
		if request.Method == "tools/list" {
			result = map[string]any{"tools": []any{map[string]any{"name": "tool", "inputSchema": map[string]any{}}}, "ttlMs": 60000, "cacheScope": "public"}
		}
		data, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": result})
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(string(data))), Header: http.Header{}}, nil
	})
	selected, _ := (capability.Capability{Server: capability.ServerVersion{Identity: capability.ServerIdentity{CanonicalName: "io.example/cache"}, Version: "1"}, Kind: capability.CapabilityTool, Name: "tool", InputSchemaJSON: json.RawMessage(`{}`)}).WithComputedSchemaDigest()
	cache := &DefinitionCache{}
	for range 2 {
		factory, _ := NewFactory([]ServerConfig{{CanonicalName: "io.example/cache", Version: "1", Endpoint: "https://source.example/mcp", HTTPClient: &http.Client{Transport: transport}, DefinitionCache: cache, CachePartition: "profile"}})
		connector, err := factory.Open(context.Background(), selected)
		if err != nil {
			t.Fatal(err)
		}
		_ = connector.Close()
	}
	if requests != 2 {
		t.Fatalf("requests = %d, want one discovery and one list", requests)
	}
}

func TestInspectIndexesToolsPromptsAndResourceTemplates(t *testing.T) {
	transport := roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		var request rpcRequest
		_ = json.NewDecoder(r.Body).Decode(&request)
		var result any
		switch request.Method {
		case "server/discover":
			result = map[string]any{"supportedVersions": []string{ProtocolVersion}}
		case "tools/list":
			result = map[string]any{"tools": []any{map[string]any{"name": "issues.create", "description": "Create issue", "inputSchema": map[string]any{"type": "object"}}}}
		case "prompts/list":
			result = map[string]any{"prompts": []any{map[string]any{"name": "issue.plan", "description": "Plan issue", "arguments": []any{map[string]any{"name": "topic", "required": true}}}}}
		case "resources/templates/list":
			result = map[string]any{"resourceTemplates": []any{map[string]any{"uriTemplate": "issues://{id}", "name": "Issue"}}}
		default:
			t.Fatalf("unexpected method %s", request.Method)
		}
		data, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": result})
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(string(data))), Header: http.Header{}}, nil
	})
	capabilities, err := Inspect(context.Background(), ServerConfig{CanonicalName: "io.example/issues", Version: "1", Endpoint: "https://mcp.example/mcp", HTTPClient: &http.Client{Transport: transport}})
	if err != nil {
		t.Fatal(err)
	}
	var kinds []capability.CapabilityKind
	for _, selected := range capabilities {
		kinds = append(kinds, selected.Kind)
		if selected.SchemaDigest == "" {
			t.Fatalf("capability has no schema digest: %#v", selected)
		}
	}
	if !reflect.DeepEqual(kinds, []capability.CapabilityKind{capability.CapabilityPrompt, capability.CapabilityResourceTemplate, capability.CapabilityTool}) {
		t.Fatalf("indexed kinds = %#v", kinds)
	}
}

func TestToolMultiRoundTripEchoesResponsesAndOpaqueRequestState(t *testing.T) {
	callCount := 0
	transport := roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		var request rpcRequest
		_ = json.NewDecoder(r.Body).Decode(&request)
		var result any
		switch request.Method {
		case "server/discover":
			result = map[string]any{"supportedVersions": []string{ProtocolVersion}}
		case "tools/list":
			result = map[string]any{"tools": []any{map[string]any{"name": "deploy", "inputSchema": map[string]any{"type": "object"}}}}
		case "tools/call":
			callCount++
			if callCount == 1 {
				result = map[string]any{"resultType": "input_required", "inputRequests": map[string]any{"confirm": map[string]any{"method": "elicitation/create", "params": map[string]any{"message": "Deploy?"}}}, "requestState": "opaque.state"}
			} else {
				if request.Params["requestState"] != "opaque.state" {
					t.Errorf("requestState = %#v", request.Params["requestState"])
				}
				responses, _ := request.Params["inputResponses"].(map[string]any)
				if _, ok := responses["confirm"]; !ok {
					t.Errorf("inputResponses = %#v", responses)
				}
				result = map[string]any{"resultType": "complete", "structuredContent": map[string]any{"deployed": true}}
			}
		}
		data, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": result})
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(string(data))), Header: http.Header{}}, nil
	})
	selected, _ := (capability.Capability{Server: capability.ServerVersion{Identity: capability.ServerIdentity{CanonicalName: "io.example/deploy"}, Version: "1"}, Kind: capability.CapabilityTool, Name: "deploy", InputSchemaJSON: json.RawMessage(`{"type":"object"}`)}).WithComputedSchemaDigest()
	factory, _ := NewFactory([]ServerConfig{{CanonicalName: "io.example/deploy", Version: "1", Endpoint: "https://mcp.example/mcp", HTTPClient: &http.Client{Transport: transport}}})
	opened, err := factory.Open(context.Background(), selected)
	if err != nil {
		t.Fatal(err)
	}
	client := opened.(*client)
	first, err := client.CallTool(context.Background(), "deploy", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	interim := first.(ToolResult)
	if interim.ResultType != "input_required" || interim.RequestState != "opaque.state" || len(interim.InputRequests) == 0 {
		t.Fatalf("interim result = %#v", interim)
	}
	if _, err := client.CallToolRound(context.Background(), "deploy", map[string]any{}, map[string]any{"confirm": map[string]any{"action": "accept"}}, interim.RequestState); err != nil {
		t.Fatal(err)
	}
}

func TestFactoryContainsHTTPTransportFailures(t *testing.T) {
	tests := []struct {
		name      string
		transport roundTripperFunc
		wantError string
	}{
		{
			name: "timeout",
			transport: func(*http.Request) (*http.Response, error) {
				return nil, context.DeadlineExceeded
			},
			wantError: "context deadline exceeded",
		},
		{
			name: "malformed JSON-RPC",
			transport: func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("{")), Header: http.Header{}}, nil
			},
			wantError: "invalid JSON-RPC",
		},
		{
			name: "oversized response",
			transport: func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(strings.Repeat("x", maxMessageBytes+1))), Header: http.Header{}}, nil
			},
			wantError: "response exceeds message limit",
		},
		{
			name: "downstream outage",
			transport: func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusServiceUnavailable, Body: io.NopCloser(strings.NewReader("unavailable")), Header: http.Header{}}, nil
			},
			wantError: "HTTP status 503",
		},
	}
	selected, err := (capability.Capability{
		Server: capability.ServerVersion{Identity: capability.ServerIdentity{CanonicalName: "io.example/failure"}, Version: "1"},
		Kind:   capability.CapabilityTool, Name: "tool", InputSchemaJSON: json.RawMessage(`{}`),
	}).WithComputedSchemaDigest()
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			factory, err := NewFactory([]ServerConfig{{
				CanonicalName: "io.example/failure", Version: "1", Endpoint: "https://source.example/mcp",
				HTTPClient: &http.Client{Transport: test.transport},
			}})
			if err != nil {
				t.Fatal(err)
			}
			_, err = factory.Open(context.Background(), selected)
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("Open() error = %v, want substring %q", err, test.wantError)
			}
		})
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }
