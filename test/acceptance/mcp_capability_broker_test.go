package acceptance

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/atheory-ai/skillex/internal/broker"
	"github.com/atheory-ai/skillex/internal/capability"
	"github.com/atheory-ai/skillex/internal/config"
	stdioConnector "github.com/atheory-ai/skillex/internal/connector/stdio"
	"github.com/atheory-ai/skillex/internal/registry"
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

func (c *goldenCapabilityCatalog) Search(context.Context, broker.Query) ([]capability.Capability, error) {
	return append([]capability.Capability(nil), c.Capabilities...), nil
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
