package acceptance

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/atheory-ai/skillex/internal/broker"
	"github.com/atheory-ai/skillex/internal/capability"
	"github.com/atheory-ai/skillex/internal/config"
	stdioConnector "github.com/atheory-ai/skillex/internal/connector/stdio"
	"github.com/atheory-ai/skillex/internal/registry"
	"github.com/atheory-ai/skillex/internal/tenant"
	"github.com/atheory-ai/skillex/test/helpers"
)

func TestMCPBroker_StaticCatalogRefreshIsOffline(t *testing.T) {
	fixtureDir := helpers.CopyGoldenFixture(t, "mcp-capability-broker")
	cfg, err := config.Load(fixtureDir)
	if err != nil {
		t.Fatal(err)
	}
	hostConfigPath := filepath.Join(fixtureDir, ".cursor", "mcp.json")
	hostConfigBefore, err := os.ReadFile(hostConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	reg, err := registry.Open(filepath.Join(fixtureDir, ".skillex", "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer reg.Close()
	result, err := registry.Refresh(reg, cfg, registry.RefreshOptions{Root: fixtureDir, DevMode: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.CapabilitiesAdded != 2 {
		t.Fatalf("capabilities added = %d, want 2", result.CapabilitiesAdded)
	}
	capabilities, err := reg.QueryCapabilitiesBySearch("create issue")
	if err != nil {
		t.Fatal(err)
	}
	if len(capabilities) != 1 || capabilities[0].Capability.Name != "issues.create" {
		t.Fatalf("offline capability search = %#v", capabilities)
	}
	hostConfigAfter, err := os.ReadFile(hostConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(hostConfigBefore, hostConfigAfter) {
		t.Fatal("static capability refresh modified host MCP configuration")
	}
}

func TestMCPBroker_ExplicitTrustedInspectionBuildsOfflineCapabilityIndex(t *testing.T) {
	fixtureDir := helpers.CopyGoldenFixture(t, "mcp-capability-broker")
	fakeServer := helpers.BuildFakeMCPServer(t)
	events := filepath.Join(t.TempDir(), "inspect.jsonl")
	trustPath := filepath.Join(t.TempDir(), "mcp-trust.yaml")
	trustDocument := fmt.Sprintf(`Version: 1
Servers:
  - Server: io.example/issues
    Version: 1.0.0
    AllowedProjects: [%q]
    AuthProfiles: [issues-test]
    Stdio:
      Command: %q
      Args: ["--fixture", %q, "--events", %q]
CredentialProfiles:
  - Name: issues-test
    Service: io.example/issues
    Credentials:
      - Slot: token
        Sources:
          - Env:
              Key: SKILLEX_ISSUES_TOKEN
        Inject:
          StdioEnv: ISSUES_TOKEN
`, fixtureDir, fakeServer, filepath.Join(fixtureDir, "servers", "issues.json"), events)
	if err := os.WriteFile(trustPath, []byte(trustDocument), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SKILLEX_MCP_TRUST_CONFIG", trustPath)
	t.Setenv("SKILLEX_ISSUES_TOKEN", "exact-token")
	hostConfig := filepath.Join(fixtureDir, ".cursor", "mcp.json")
	before, _ := os.ReadFile(hostConfig)
	result := helpers.Run(t, fixtureDir, "catalog", "inspect", "--server", "io.example/issues", "--json")
	if result.ExitCode != 0 {
		t.Fatalf("catalog inspect failed: %s", result.Stderr)
	}
	var summary struct {
		Servers      int `json:"servers"`
		Capabilities int `json:"capabilities"`
	}
	if err := json.Unmarshal([]byte(result.Stdout), &summary); err != nil {
		t.Fatal(err)
	}
	if summary.Servers != 1 || summary.Capabilities != 1 {
		t.Fatalf("inspection summary = %#v", summary)
	}
	observed, err := os.ReadFile(filepath.Join(fixtureDir, ".skillex", "mcp", "observed.json"))
	if err != nil || !bytes.Contains(observed, []byte(`"name": "issues.create"`)) || !bytes.Contains(observed, []byte(`"observed_at"`)) {
		t.Fatalf("observed catalog = %s, %v", observed, err)
	}
	assertEvents(t, events, []protocolEvent{{Method: "server/discover"}, {Method: "tools/list"}, {Method: "prompts/list"}, {Method: "resources/templates/list"}})
	after, _ := os.ReadFile(hostConfig)
	if !bytes.Equal(before, after) {
		t.Fatal("trusted inspection modified host MCP configuration")
	}
}

func TestMCPBroker_HostDiscoversAndDescribesCapabilityThroughSkillexOnly(t *testing.T) {
	fixtureDir := helpers.CopyGoldenFixture(t, "mcp-capability-broker")
	hostConfigPath := filepath.Join(fixtureDir, ".cursor", "mcp.json")
	hostConfigBefore, err := os.ReadFile(hostConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	refresh := helpers.Run(t, fixtureDir, "refresh")
	if refresh.ExitCode != 0 {
		t.Fatalf("refresh failed: %s", refresh.Stderr)
	}
	client := helpers.StartMCPServer(t, fixtureDir)
	defer client.Close()
	text, err := client.CallToolText("skillex_query", map[string]interface{}{
		"path": "packages/app/src/issues.ts", "search": "create issue", "limit": 8,
	})
	if err != nil {
		t.Fatal(err)
	}
	var response struct {
		Capabilities []broker.Summary `json:"capabilities"`
	}
	if err := json.Unmarshal([]byte(text), &response); err != nil {
		t.Fatalf("decoding capability query: %v\n%s", err, text)
	}
	issues := findCapabilitySummary(t, response.Capabilities, "io.example/issues", "issues.create")
	if issues.Ref == "" {
		t.Fatal("host-facing capability result omitted signed ref")
	}
	described, err := client.CallToolText("skillex_mcp_describe", map[string]interface{}{"ref": issues.Ref})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(described, `"name": "issues.create"`) || !strings.Contains(described, `"input_schema"`) {
		t.Fatalf("unexpected capability description: %s", described)
	}
	if _, err := client.CallToolText("skillex_mcp_call", map[string]interface{}{
		"ref": issues.Ref, "arguments": map[string]any{"title": "Not ready"},
	}); err == nil {
		t.Fatal("setup-required static capability was invokable")
	}
	hostConfigAfter, err := os.ReadFile(hostConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(hostConfigBefore, hostConfigAfter) {
		t.Fatal("host-facing dynamic discovery modified host MCP configuration")
	}
}

func TestMCPBroker_GoldenCLIAndMCPQueryParity(t *testing.T) {
	fixtureDir := helpers.CopyGoldenFixture(t, "mcp-capability-broker")
	refresh := helpers.Run(t, fixtureDir, "refresh")
	if refresh.ExitCode != 0 {
		t.Fatalf("refresh failed: %s", refresh.Stderr)
	}

	args := []string{"query", "--path", "packages/app/src/issues.ts", "--search", "create issue", "--json"}
	cli := helpers.Run(t, fixtureDir, args...)
	if cli.ExitCode != 0 {
		t.Fatalf("CLI query failed: %s", cli.Stderr)
	}

	client := helpers.StartMCPServer(t, fixtureDir)
	defer client.Close()
	mcpText, err := client.CallToolText("skillex_query", map[string]interface{}{
		"path": "packages/app/src/issues.ts", "search": "create issue",
	})
	if err != nil {
		t.Fatal(err)
	}
	assertNormalizedJSONEqual(t, []byte(cli.Stdout), []byte(mcpText))

	human := helpers.Run(t, fixtureDir, "query", "--path", "packages/app/src/issues.ts", "--search", "create issue")
	if human.ExitCode != 0 {
		t.Fatalf("human query failed: %s", human.Stderr)
	}
	assertGoldenBytes(t, filepath.Join(fixtureDir, "expected", "query-human.txt"), []byte(human.Stdout))
}

func TestMCPBroker_GoldenBroadAndNoMatchContracts(t *testing.T) {
	fixtureDir := helpers.CopyGoldenFixture(t, "mcp-capability-broker")
	refresh := helpers.Run(t, fixtureDir, "refresh")
	if refresh.ExitCode != 0 {
		t.Fatalf("refresh failed: %s", refresh.Stderr)
	}

	broad := helpers.Run(t, fixtureDir, "query", "--path", "packages/app/src/issues.ts", "--limit", "1", "--json")
	if broad.ExitCode != 0 {
		t.Fatalf("broad query failed: %s", broad.Stderr)
	}
	assertNormalizedJSONGolden(t, filepath.Join(fixtureDir, "expected", "query-too-broad.json"), []byte(broad.Stdout))

	noMatch := helpers.Run(t, fixtureDir, "query", "--mcp-server", "io.example/missing", "--json")
	if noMatch.ExitCode != 0 {
		t.Fatalf("no-match query failed: %s", noMatch.Stderr)
	}
	assertNormalizedJSONGolden(t, filepath.Join(fixtureDir, "expected", "query-no-match.json"), []byte(noMatch.Stdout))
}

func TestMCPBroker_GoldenDescribeAndTypedError(t *testing.T) {
	fixtureDir := helpers.CopyGoldenFixture(t, "mcp-capability-broker")
	refresh := helpers.Run(t, fixtureDir, "refresh")
	if refresh.ExitCode != 0 {
		t.Fatalf("refresh failed: %s", refresh.Stderr)
	}

	client := helpers.StartMCPServer(t, fixtureDir)
	defer client.Close()
	queryText, err := client.CallToolText("skillex_query", map[string]interface{}{"mcp_server": "io.example/issues"})
	if err != nil {
		t.Fatal(err)
	}
	var discovered struct {
		Capabilities []broker.Summary `json:"capabilities"`
	}
	if err := json.Unmarshal([]byte(queryText), &discovered); err != nil || len(discovered.Capabilities) != 1 {
		t.Fatalf("decoding discovered capability: %v\n%s", err, queryText)
	}
	described, err := client.CallToolText("skillex_mcp_describe", map[string]interface{}{"ref": discovered.Capabilities[0].Ref})
	if err != nil {
		t.Fatal(err)
	}
	assertNormalizedJSONGolden(t, filepath.Join(fixtureDir, "expected", "describe.json"), []byte(described))
	tooLarge := mcpToolErrorText(t, client, "skillex_mcp_describe", map[string]interface{}{
		"ref": discovered.Capabilities[0].Ref, "max_bytes": 32,
	})
	assertNormalizedJSONGolden(t, filepath.Join(fixtureDir, "expected", "error-description-too-large.json"), []byte(tooLarge))
	invalidBudget := mcpToolErrorText(t, client, "skillex_mcp_describe", map[string]interface{}{
		"ref": discovered.Capabilities[0].Ref, "max_bytes": 65537,
	})
	assertNormalizedJSONGolden(t, filepath.Join(fixtureDir, "expected", "error-description-budget-invalid.json"), []byte(invalidBudget))

	invalidRef := mcpToolErrorText(t, client, "skillex_mcp_describe", map[string]interface{}{"ref": "mcp-tool:v1:tampered"})
	assertNormalizedJSONGolden(t, filepath.Join(fixtureDir, "expected", "error-invalid-ref.json"), []byte(invalidRef))
}

func mcpToolErrorText(t *testing.T, client *helpers.MCPClient, name string, arguments map[string]interface{}) string {
	t.Helper()
	raw, err := client.CallTool(name, arguments)
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	if !result.IsError || len(result.Content) != 1 {
		t.Fatalf("typed MCP error envelope = %s", raw)
	}
	return result.Content[0].Text
}

func TestMCPBroker_GoldenHTTPInvocation(t *testing.T) {
	fixtureDir := helpers.CopyGoldenFixture(t, "mcp-capability-broker")
	var methods []string
	var methodsMu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		defer request.Body.Close()
		var rpc struct {
			ID     int64          `json:"id"`
			Method string         `json:"method"`
			Params map[string]any `json:"params"`
		}
		if err := json.NewDecoder(request.Body).Decode(&rpc); err != nil {
			t.Errorf("decoding HTTP MCP request: %v", err)
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		methodsMu.Lock()
		methods = append(methods, rpc.Method)
		methodsMu.Unlock()
		if request.Header.Get("Authorization") != "Bearer exact-http-token" {
			t.Errorf("Authorization = %q", request.Header.Get("Authorization"))
		}
		var result any
		switch rpc.Method {
		case "server/discover":
			result = map[string]any{"supportedVersions": []string{"2026-07-28"}}
		case "tools/list":
			result = map[string]any{"tools": []any{map[string]any{
				"name": "issues.create", "inputSchema": map[string]any{
					"type": "object", "properties": map[string]any{"title": map[string]any{"type": "string"}},
					"required": []string{"title"}, "additionalProperties": false,
				},
			}}}
		case "tools/call":
			result = map[string]any{"resultType": "complete", "structuredContent": map[string]any{
				"authorized": true, "transport": "http",
			}}
		default:
			t.Errorf("unexpected HTTP MCP method %q", rpc.Method)
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(writer).Encode(map[string]any{"jsonrpc": "2.0", "id": rpc.ID, "result": result}); err != nil {
			t.Errorf("encoding HTTP MCP response: %v", err)
		}
	}))
	defer server.Close()

	trustPath := filepath.Join(t.TempDir(), "mcp-trust.yaml")
	trustDocument := fmt.Sprintf(`Version: 1
Servers:
  - Server: io.example/issues
    Version: 1.0.0
    AllowedProjects: [%q]
    AuthProfiles: [issues-test]
    HTTP:
      Endpoint: %q
CredentialProfiles:
  - Name: issues-test
    Service: io.example/issues
    Credentials:
      - Slot: token
        Sources:
          - Env:
              Key: SKILLEX_HTTP_TOKEN
        Inject:
          HTTPHeader:
            Name: Authorization
            Format: "Bearer ${value}"
`, fixtureDir, server.URL)
	if err := os.WriteFile(trustPath, []byte(trustDocument), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SKILLEX_MCP_TRUST_CONFIG", trustPath)
	t.Setenv("SKILLEX_HTTP_TOKEN", "exact-http-token")
	refresh := helpers.Run(t, fixtureDir, "refresh")
	if refresh.ExitCode != 0 {
		t.Fatalf("refresh failed: %s", refresh.Stderr)
	}
	client := helpers.StartMCPServer(t, fixtureDir)
	defer client.Close()
	queryText, err := client.CallToolText("skillex_query", map[string]interface{}{
		"path": "packages/app/src/issues.ts", "mcp_server": "io.example/issues",
	})
	if err != nil {
		t.Fatal(err)
	}
	var discovered struct {
		Capabilities []broker.Summary `json:"capabilities"`
	}
	if err := json.Unmarshal([]byte(queryText), &discovered); err != nil || len(discovered.Capabilities) != 1 {
		t.Fatalf("decoding query: %v\n%s", err, queryText)
	}
	called, err := client.CallToolText("skillex_mcp_call", map[string]interface{}{
		"ref": discovered.Capabilities[0].Ref, "arguments": map[string]any{"title": "HTTP journey"},
	})
	if err != nil {
		t.Fatal(err)
	}
	assertNormalizedJSONGolden(t, filepath.Join(fixtureDir, "expected", "http-call.json"), []byte(called))
	methodsMu.Lock()
	defer methodsMu.Unlock()
	if !reflect.DeepEqual(methods, []string{"server/discover", "tools/list", "tools/call"}) {
		t.Fatalf("HTTP MCP methods = %#v", methods)
	}
}

func TestMCPBroker_GoldenOAuthLoginAndReadinessRetry(t *testing.T) {
	fixtureDir := helpers.CopyGoldenFixture(t, "mcp-capability-broker")
	var tokenForm url.Values
	var tokenFormMu sync.Mutex
	var serverURL string
	authServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/resource-metadata":
			_ = json.NewEncoder(writer).Encode(map[string]any{
				"resource": serverURL, "authorization_servers": []string{serverURL}, "scopes_supported": []string{"issues:write"},
			})
		case "/.well-known/oauth-authorization-server":
			_ = json.NewEncoder(writer).Encode(map[string]any{
				"issuer": serverURL, "authorization_endpoint": serverURL + "/authorize", "token_endpoint": serverURL + "/token",
				"code_challenge_methods_supported": []string{"S256"}, "authorization_response_iss_parameter_supported": true,
			})
		case "/token":
			if err := request.ParseForm(); err != nil {
				t.Errorf("parsing token form: %v", err)
			}
			tokenFormMu.Lock()
			tokenForm = request.PostForm
			tokenFormMu.Unlock()
			_ = json.NewEncoder(writer).Encode(map[string]any{
				"access_token": "oauth-access", "refresh_token": "oauth-refresh", "token_type": "Bearer",
				"expires_in": 300, "scope": "issues:write",
			})
		default:
			t.Errorf("unexpected OAuth path %q", request.URL.Path)
			writer.WriteHeader(http.StatusNotFound)
		}
	}))
	defer authServer.Close()
	serverURL = authServer.URL

	storeDir := t.TempDir()
	trustPath := filepath.Join(t.TempDir(), "mcp-trust.yaml")
	trustDocument := fmt.Sprintf(`Version: 1
OAuthStore:
  KeyPath: %q
  Directory: %q
Servers:
  - Server: io.example/issues
    Version: 1.0.0
    AllowedProjects: [%q]
    AuthProfiles: [issues-test]
    HTTP:
      Endpoint: %q
CredentialProfiles:
  - Name: issues-test
    Service: io.example/issues
    OAuth:
      Type: authorization-code
      ProtectedResourceMetadataURL: %q
      Resource: %q
      Scopes: [issues:write]
      ClientID: skillex-test-client
      ClientAuthMethod: none
      RedirectURI: http://127.0.0.1/callback
`, filepath.Join(storeDir, "oauth.key"), filepath.Join(storeDir, "tokens"), fixtureDir, serverURL+"/mcp", serverURL+"/resource-metadata", serverURL)
	if err := os.WriteFile(trustPath, []byte(trustDocument), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SKILLEX_MCP_TRUST_CONFIG", trustPath)

	status := helpers.Run(t, fixtureDir, "auth", "status", "--profile", "issues-test", "--json")
	if status.ExitCode != 0 {
		t.Fatalf("initial auth status failed: %s", status.Stderr)
	}
	assertNormalizedJSONGolden(t, filepath.Join(fixtureDir, "expected", "auth-status-login-required.json"), []byte(status.Stdout))

	started := helpers.Run(t, fixtureDir, "auth", "login", "--profile", "issues-test", "--json")
	if started.ExitCode != 0 {
		t.Fatalf("starting OAuth login failed: %s", started.Stderr)
	}
	var login struct {
		AuthorizationURL string `json:"authorization_url"`
	}
	if err := json.Unmarshal([]byte(started.Stdout), &login); err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(login.AuthorizationURL)
	if err != nil || parsed.Query().Get("state") == "" || parsed.Query().Get("code_challenge_method") != "S256" {
		t.Fatalf("authorization URL = %q, %v", login.AuthorizationURL, err)
	}
	assertNormalizedJSONGolden(t, filepath.Join(fixtureDir, "expected", "auth-login-start.json"), []byte(started.Stdout))

	callback := "http://127.0.0.1/callback?code=accepted&state=" + url.QueryEscape(parsed.Query().Get("state")) + "&iss=" + url.QueryEscape(serverURL)
	completed := helpers.Run(t, fixtureDir, "auth", "login", "--profile", "issues-test", "--callback", callback, "--json")
	if completed.ExitCode != 0 {
		t.Fatalf("completing OAuth login failed: %s", completed.Stderr)
	}
	assertNormalizedJSONGolden(t, filepath.Join(fixtureDir, "expected", "auth-login-complete.json"), []byte(completed.Stdout))
	tokenFormMu.Lock()
	grantType := tokenForm.Get("grant_type")
	codeVerifier := tokenForm.Get("code_verifier")
	resource := tokenForm.Get("resource")
	tokenFormSnapshot := tokenForm.Encode()
	tokenFormMu.Unlock()
	if grantType != "authorization_code" || codeVerifier == "" || resource != serverURL {
		t.Fatalf("OAuth token form = %s", tokenFormSnapshot)
	}

	status = helpers.Run(t, fixtureDir, "auth", "status", "--profile", "issues-test", "--json")
	if status.ExitCode != 0 {
		t.Fatalf("ready auth status failed: %s", status.Stderr)
	}
	assertNormalizedJSONGolden(t, filepath.Join(fixtureDir, "expected", "auth-status-ready.json"), []byte(status.Stdout))
}

func TestMCPBroker_GoldenTenantPartitionAndReferenceIsolation(t *testing.T) {
	partitionKey := []byte("0123456789abcdef0123456789abcdef")
	masterKey := []byte("abcdef0123456789abcdef0123456789")
	principalA := tenant.Principal{TenantID: "tenant-a", Subject: "user-a", Kind: tenant.PrincipalUser}
	principalB := tenant.Principal{TenantID: "tenant-a", Subject: "user-b", Kind: tenant.PrincipalUser}
	principalC := tenant.Principal{TenantID: "tenant-b", Subject: "user-a", Kind: tenant.PrincipalUser}
	partitionA, err := tenant.Partition(partitionKey, principalA)
	if err != nil {
		t.Fatal(err)
	}
	partitionB, _ := tenant.Partition(partitionKey, principalB)
	partitionC, _ := tenant.Partition(partitionKey, principalC)
	keyA, err := tenant.DeriveSigningKey(masterKey, principalA.TenantID, "capability-refs")
	if err != nil {
		t.Fatal(err)
	}
	keyC, err := tenant.DeriveSigningKey(masterKey, principalC.TenantID, "capability-refs")
	if err != nil {
		t.Fatal(err)
	}
	clock := func() time.Time { return time.Unix(1_800_000_000, 0) }
	signerA, _ := capability.NewReferenceSigner(keyA, 5*time.Minute, capability.WithClock(clock))
	signerC, _ := capability.NewReferenceSigner(keyC, 5*time.Minute, capability.WithClock(clock))
	selected := loadGoldenCapabilityCatalog(t, helpers.GoldenPath("mcp-capability-broker/catalog.json")).Capabilities[0]
	ref, err := signerA.Issue(selected, partitionA, "sha256:workspace")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := signerA.Verify(ref); err != nil {
		t.Fatal(err)
	}
	_, crossTenantErr := signerC.Verify(ref)
	rawIdentityHidden := true
	for _, sensitive := range []string{principalA.TenantID, principalA.Subject, principalB.Subject, principalC.TenantID} {
		rawIdentityHidden = rawIdentityHidden && !strings.Contains(partitionA, sensitive) && !strings.Contains(ref, sensitive)
	}
	result, err := json.Marshal(map[string]any{
		"cross_subject_partition_isolated": partitionA != partitionB,
		"cross_tenant_partition_isolated":  partitionA != partitionC,
		"cross_tenant_reference_rejected":  errors.Is(crossTenantErr, capability.ErrInvalidReference),
		"partition_prefix":                 "tenant:v1:",
		"raw_identity_hidden":              rawIdentityHidden,
	})
	if err != nil {
		t.Fatal(err)
	}
	assertNormalizedJSONGolden(t, helpers.GoldenPath("mcp-capability-broker/expected/tenant-isolation.json"), result)
}

func TestMCPBroker_HostInvokesTrustedDynamicServerWithoutHostRegistration(t *testing.T) {
	fixtureDir := helpers.CopyGoldenFixture(t, "mcp-capability-broker")
	fakeServer := helpers.BuildFakeMCPServer(t)
	events := filepath.Join(t.TempDir(), "host-issues.jsonl")
	usageEvents := filepath.Join(t.TempDir(), "usage.jsonl")
	trustPath := filepath.Join(t.TempDir(), "mcp-trust.yaml")
	trustDocument := fmt.Sprintf(`Version: 1
Servers:
  - Server: io.example/issues
    Version: 1.0.0
    AllowedProjects: [%q]
    AuthProfiles: [issues-test]
    Stdio:
      Command: %q
      Args: ["--fixture", %q, "--events", %q]
      Directory: %q
CredentialProfiles:
  - Name: issues-test
    Service: io.example/issues
    Credentials:
      - Slot: access-token
        Sources:
          - Env:
              Key: SKILLEX_ISSUES_TOKEN
        Inject:
          StdioEnv: ISSUES_TOKEN
Telemetry:
  Enabled: true
  Path: %q
`, fixtureDir, fakeServer, filepath.Join(fixtureDir, "servers", "issues.json"), events, fixtureDir, usageEvents)
	if err := os.WriteFile(trustPath, []byte(trustDocument), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SKILLEX_MCP_TRUST_CONFIG", trustPath)
	t.Setenv("SKILLEX_ISSUES_TOKEN", "mapped-token")
	t.Setenv("SKILLEX_TEST_SECRET", "must-not-reach-downstream")

	hostConfigPath := filepath.Join(fixtureDir, ".cursor", "mcp.json")
	hostConfigBefore, err := os.ReadFile(hostConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	refresh := helpers.Run(t, fixtureDir, "refresh")
	if refresh.ExitCode != 0 {
		t.Fatalf("refresh failed: %s", refresh.Stderr)
	}
	client := helpers.StartMCPServer(t, fixtureDir)
	defer client.Close()
	text, err := client.CallToolText("skillex_query", map[string]interface{}{
		"path": "packages/app/src/issues.ts", "search": "create issue", "limit": 8,
	})
	if err != nil {
		t.Fatal(err)
	}
	var response struct {
		Capabilities []broker.Summary `json:"capabilities"`
	}
	if err := json.Unmarshal([]byte(text), &response); err != nil {
		t.Fatal(err)
	}
	issues := findCapabilitySummary(t, response.Capabilities, "io.example/issues", "issues.create")
	if issues.Availability != capability.AvailabilityReady {
		t.Fatalf("availability = %s, want ready", issues.Availability)
	}
	called, err := client.CallToolText("skillex_mcp_call", map[string]interface{}{
		"ref": issues.Ref, "arguments": map[string]any{"title": "Dynamic host call"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(called, `"server": "issues-fixture-server"`) || !strings.Contains(called, `"secretVisible": false`) {
		t.Fatalf("unexpected downstream result: %s", called)
	}
	assertEvents(t, events, []protocolEvent{{Method: "server/discover"}, {Method: "tools/list"}, {Method: "tools/call", Tool: "issues.create"}})
	usage, err := os.ReadFile(usageEvents)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(usage, []byte(`"operation":"call"`)) || !bytes.Contains(usage, []byte(`"outcome":"success"`)) {
		t.Fatalf("usage telemetry omitted successful call: %s", usage)
	}
	for _, secret := range []string{"mapped-token", "Dynamic host call", "SKILLEX_ISSUES_TOKEN", "ISSUES_TOKEN"} {
		if bytes.Contains(usage, []byte(secret)) {
			t.Fatalf("usage telemetry leaked %q: %s", secret, usage)
		}
	}
	hostConfigAfter, err := os.ReadFile(hostConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(hostConfigBefore, hostConfigAfter) {
		t.Fatal("dynamic invocation modified host MCP configuration")
	}
}

func TestMCPBroker_GoldenDiscoveryDescribeAndRealStdioCall(t *testing.T) {
	fixtureDir := helpers.CopyGoldenFixture(t, "mcp-capability-broker")
	cfg, err := config.Load(fixtureDir)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.MCPEnabled() {
		t.Fatal("golden capability broker fixture did not explicitly opt in")
	}
	fakeServer := helpers.BuildFakeMCPServer(t)
	catalog := loadGoldenCapabilityCatalog(t, filepath.Join(fixtureDir, "catalog.json"))
	weatherEvents := filepath.Join(t.TempDir(), "weather.jsonl")
	issuesEvents := filepath.Join(t.TempDir(), "issues.jsonl")
	factory, err := stdioConnector.NewFactory([]stdioConnector.ServerConfig{
		{
			CanonicalName: "io.example/weather",
			Version:       "1.0.0",
			Command:       fakeServer,
			Args:          []string{"--fixture", filepath.Join(fixtureDir, "servers", "weather.json"), "--events", weatherEvents},
			Directory:     fixtureDir,
		},
		{
			CanonicalName: "io.example/issues",
			Version:       "1.0.0",
			Command:       fakeServer,
			Args:          []string{"--fixture", filepath.Join(fixtureDir, "servers", "issues.json"), "--events", issuesEvents},
			Directory:     fixtureDir,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	signer, err := capability.NewReferenceSigner(
		[]byte("0123456789abcdef0123456789abcdef"),
		5*time.Minute,
		capability.WithClock(func() time.Time { return time.Unix(1_800_000_000, 0) }),
	)
	if err != nil {
		t.Fatal(err)
	}
	engine, err := broker.NewConfigured(cfg, catalog, signer, acceptanceAllowPolicy{}, factory)
	if err != nil {
		t.Fatal(err)
	}

	hostConfigPath := filepath.Join(fixtureDir, ".cursor", "mcp.json")
	hostConfigBefore, err := os.ReadFile(hostConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("SKILLEX_TEST_SECRET", "must-not-reach-downstream")
	request := broker.RequestContext{ContextDigest: "sha256:golden-workspace", View: "private:golden-user"}

	results, err := engine.Query(context.Background(), broker.Query{
		Path:          "packages/app/src/issues.ts",
		Search:        "create an issue",
		ContextDigest: request.ContextDigest,
		View:          request.View,
		Limit:         8,
	})
	if err != nil {
		t.Fatal(err)
	}
	assertNoFile(t, weatherEvents)
	assertNoFile(t, issuesEvents)
	assertCapabilityQueryGolden(t, filepath.Join(fixtureDir, "expected", "query.json"), results)

	issues := findCapabilitySummary(t, results, "io.example/issues", "issues.create")
	described, err := engine.Describe(context.Background(), issues.Ref, request)
	if err != nil {
		t.Fatal(err)
	}
	if described.Name != "issues.create" || len(described.InputSchemaJSON) == 0 {
		t.Fatalf("unexpected described capability: %#v", described)
	}
	assertNoFile(t, weatherEvents)
	assertNoFile(t, issuesEvents)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	call, err := engine.Call(ctx, issues.Ref, map[string]any{"title": "Broken release"}, request)
	if err != nil {
		t.Fatal(err)
	}
	if call.Server.Identity.CanonicalName != "io.example/issues" || call.Capability.Name != "issues.create" {
		t.Fatalf("call result is not attributed to the selected server/tool: %#v", call)
	}
	toolResult, ok := call.Result.(stdioConnector.ToolResult)
	if !ok {
		t.Fatalf("call result type = %T, want stdio.ToolResult", call.Result)
	}
	structured, ok := toolResult.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("structured content type = %T", toolResult.StructuredContent)
	}
	if structured["server"] != "issues-fixture-server" || structured["tool"] != "issues.create" || structured["secretVisible"] != false {
		t.Fatalf("unexpected structured result or leaked parent environment: %#v", structured)
	}
	arguments, ok := structured["arguments"].(map[string]any)
	if !ok || arguments["title"] != "Broken release" {
		t.Fatalf("arguments did not traverse the MCP connection: %#v", structured["arguments"])
	}
	assertNoFile(t, weatherEvents)
	assertEvents(t, issuesEvents, []protocolEvent{
		{Method: "server/discover"},
		{Method: "tools/list"},
		{Method: "tools/call", Tool: "issues.create"},
	})

	// Runtime re-listing rejects a server whose live schema drifted from the
	// indexed golden snapshot, before tools/call is sent.
	weather := findCapabilitySummary(t, results, "io.example/weather", "weather.lookup")
	weatherFixturePath := filepath.Join(fixtureDir, "servers", "weather.json")
	weatherFixture, err := os.ReadFile(weatherFixturePath)
	if err != nil {
		t.Fatal(err)
	}
	weatherFixture = bytes.Replace(weatherFixture, []byte(`"required": ["location"]`), []byte(`"required": ["location", "units"]`), 1)
	if err := os.WriteFile(weatherFixturePath, weatherFixture, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = engine.Call(ctx, weather.Ref, map[string]any{"location": "Vancouver"}, request)
	if !errors.Is(err, stdioConnector.ErrSchemaChanged) {
		t.Fatalf("Call(runtime schema drift) error = %v, want %v", err, stdioConnector.ErrSchemaChanged)
	}
	assertEvents(t, weatherEvents, []protocolEvent{{Method: "server/discover"}, {Method: "tools/list"}})

	hostConfigAfter, err := os.ReadFile(hostConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(hostConfigBefore, hostConfigAfter) {
		t.Fatal("capability broker modified the host MCP configuration")
	}
}

func TestMCPBroker_LegacyGoldenConfigsRemainSkillsOnly(t *testing.T) {
	fixtures := []string{
		"go-basic",
		"monorepo-npm",
		"monorepo-pnpm",
		"monorepo-yarn",
		"multi-version-local",
		"single-package",
	}
	for _, fixture := range fixtures {
		t.Run(fixture, func(t *testing.T) {
			fixtureDir := helpers.CopyGoldenFixture(t, fixture)
			cfg, err := config.Load(fixtureDir)
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Version != config.SkillsOnlyConfigVersion || cfg.MCPEnabled() {
				t.Fatalf("legacy config changed mode: version=%d MCPEnabled=%v", cfg.Version, cfg.MCPEnabled())
			}

			// The project-facing constructor must stop before it can initialize
			// catalogs, credentials, policy, or downstream connectors.
			created, err := broker.NewConfigured(cfg, nil, nil, nil, nil)
			if !errors.Is(err, broker.ErrMCPDisabled) {
				t.Fatalf("NewConfigured() error = %v, want %v", err, broker.ErrMCPDisabled)
			}
			if created != nil {
				t.Fatal("legacy skills-only config constructed an MCP broker")
			}
		})
	}
}

type goldenCapabilityCatalog struct {
	Capabilities []capability.Capability `json:"capabilities"`
}

func loadGoldenCapabilityCatalog(t *testing.T, path string) *goldenCapabilityCatalog {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var catalog goldenCapabilityCatalog
	if err := json.Unmarshal(data, &catalog); err != nil {
		t.Fatal(err)
	}
	for i, selected := range catalog.Capabilities {
		catalog.Capabilities[i], err = selected.WithComputedSchemaDigest()
		if err != nil {
			t.Fatalf("invalid golden capability %s: %v", selected.Name, err)
		}
	}
	return &catalog
}

func (c *goldenCapabilityCatalog) Search(_ context.Context, query broker.Query) (broker.CatalogPage, error) {
	capabilities := append([]capability.Capability(nil), c.Capabilities...)
	matchCount := len(capabilities)
	offset := query.Offset
	if offset > matchCount {
		offset = matchCount
	}
	capabilities = capabilities[offset:]
	if query.Limit > 0 && len(capabilities) > query.Limit {
		capabilities = capabilities[:query.Limit]
	}
	return broker.CatalogPage{Capabilities: capabilities, MatchCount: matchCount}, nil
}

func (c *goldenCapabilityCatalog) Resolve(_ context.Context, claims capability.ReferenceClaims) (capability.Capability, error) {
	for _, selected := range c.Capabilities {
		if selected.Server.Identity.CanonicalName == claims.Server && selected.Server.Version == claims.Version &&
			selected.Kind == claims.Kind && selected.Name == claims.Capability {
			return selected, nil
		}
	}
	return capability.Capability{}, fmt.Errorf("golden capability not found")
}

type acceptanceAllowPolicy struct{}

func (acceptanceAllowPolicy) Evaluate(context.Context, capability.Capability, map[string]any) (broker.PolicyEffect, error) {
	return broker.PolicyAllow, nil
}

func assertCapabilityQueryGolden(t *testing.T, expectedPath string, results []broker.Summary) {
	t.Helper()
	normalized := append([]broker.Summary(nil), results...)
	for i := range normalized {
		if normalized[i].Ref == "" {
			t.Fatalf("query result omitted capability reference: %#v", normalized[i])
		}
		normalized[i].Ref = "<capability-ref>"
	}
	actualJSON, err := json.Marshal(normalized)
	if err != nil {
		t.Fatal(err)
	}
	expectedJSON, err := os.ReadFile(expectedPath)
	if err != nil {
		t.Fatal(err)
	}
	var actual, expected any
	if err := json.Unmarshal(actualJSON, &actual); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(expectedJSON, &expected); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(actual, expected) {
		pretty, _ := json.MarshalIndent(actual, "", "  ")
		t.Fatalf("capability query differs from golden output\nactual:\n%s\nexpected:\n%s", pretty, expectedJSON)
	}
}

func assertNormalizedJSONGolden(t *testing.T, expectedPath string, actualJSON []byte) {
	t.Helper()
	expectedJSON, err := os.ReadFile(expectedPath)
	if err != nil {
		t.Fatal(err)
	}
	assertNormalizedJSONEqual(t, expectedJSON, actualJSON)
}

func assertNormalizedJSONEqual(t *testing.T, expectedJSON, actualJSON []byte) {
	t.Helper()
	var actual, expected any
	if err := json.Unmarshal(actualJSON, &actual); err != nil {
		t.Fatalf("invalid actual JSON: %v\n%s", err, actualJSON)
	}
	if err := json.Unmarshal(expectedJSON, &expected); err != nil {
		t.Fatalf("invalid expected JSON: %v\n%s", err, expectedJSON)
	}
	normalizeCapabilityRefs(actual)
	normalizeCapabilityRefs(expected)
	if !reflect.DeepEqual(actual, expected) {
		pretty, _ := json.MarshalIndent(actual, "", "  ")
		t.Fatalf("JSON differs from golden output\nactual:\n%s\nexpected:\n%s", pretty, expectedJSON)
	}
}

func normalizeCapabilityRefs(value any) {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if key == "message" {
				if text, ok := child.(string); ok && strings.HasPrefix(text, "capability description ") {
					typed[key] = "<description-budget-error>"
					continue
				}
			}
			if key == "authorization_url" {
				typed[key] = "<authorization-url>"
				continue
			}
			if key == "ref" {
				if text, ok := child.(string); ok && strings.HasPrefix(text, "mcp-tool:") {
					typed[key] = "<capability-ref>"
				}
				continue
			}
			normalizeCapabilityRefs(child)
		}
	case []any:
		for _, child := range typed {
			normalizeCapabilityRefs(child)
		}
	}
}

func assertGoldenBytes(t *testing.T, expectedPath string, actual []byte) {
	t.Helper()
	expected, err := os.ReadFile(expectedPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(actual, expected) {
		t.Fatalf("output differs from golden\nactual:\n%s\nexpected:\n%s", actual, expected)
	}
}

func findCapabilitySummary(t *testing.T, results []broker.Summary, server, name string) broker.Summary {
	t.Helper()
	for _, result := range results {
		if result.Server.CanonicalName == server && result.Name == name {
			return result
		}
	}
	t.Fatalf("capability %s/%s not found in %#v", server, name, results)
	return broker.Summary{}
}

type protocolEvent struct {
	Method string `json:"method"`
	Tool   string `json:"tool,omitempty"`
}

func assertEvents(t *testing.T, path string, expected []protocolEvent) {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	var actual []protocolEvent
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var value protocolEvent
		if err := json.Unmarshal(scanner.Bytes(), &value); err != nil {
			t.Fatal(err)
		}
		actual = append(actual, value)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("protocol events = %#v, want %#v", actual, expected)
	}
}

func assertNoFile(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected no downstream process event file at %s, stat error = %v", path, err)
	}
}
