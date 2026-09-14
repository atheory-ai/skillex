package brokerruntime

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/atheory-ai/skillex/internal/broker"
	"github.com/atheory-ai/skillex/internal/capability"
	"github.com/atheory-ai/skillex/internal/config"
	"github.com/atheory-ai/skillex/internal/registry"
	"github.com/atheory-ai/skillex/internal/trust"
)

func TestNewDiscoveryDoesNotCreateKeyForSkillsOnlyConfig(t *testing.T) {
	root := t.TempDir()
	reg, err := registry.Open(filepath.Join(root, ".skillex", "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer reg.Close()
	created, err := NewDiscovery(root, config.DefaultConfig(), reg)
	if !errors.Is(err, broker.ErrMCPDisabled) || created != nil {
		t.Fatalf("NewDiscovery(skills-only) = %#v, %v", created, err)
	}
}

func TestNewDiscoveryBuildsOfflineRuntimeAndDescribesCapability(t *testing.T) {
	root := t.TempDir()
	reg, err := registry.Open(filepath.Join(root, ".skillex", "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer reg.Close()
	selected := capability.Capability{
		Server:          capability.ServerVersion{Identity: capability.ServerIdentity{CanonicalName: "io.example/issues"}, Version: "1.0.0"},
		Kind:            capability.CapabilityTool,
		Name:            "issues.create",
		InputSchemaJSON: json.RawMessage(`{"type":"object"}`),
		Availability:    capability.AvailabilitySetupRequired,
	}
	if _, err := reg.InsertCapability(registry.CapabilityRecord{
		Capability: selected,
		SourceType: "static",
		SourceRef:  "catalog.json",
		Bindings:   []registry.CapabilityBinding{{Scope: "packages/app/**", AuthProfile: "issues"}},
	}); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{Version: config.MCPConfigVersion, MCP: &config.MCPConfig{
		Enabled: true,
		Bindings: []config.MCPBinding{{
			Server: "io.example/issues", Version: "1.0.0", Scope: "packages/app/**", AuthProfile: "issues",
		}},
	}}
	trustPath := filepath.Join(t.TempDir(), "mcp-trust.yaml")
	if err := os.WriteFile(trustPath, []byte("Version: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SKILLEX_MCP_TRUST_CONFIG", trustPath)

	runtime, err := NewDiscovery(root, cfg, reg)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.ContextDigest == "" || runtime.View != localView || runtime.TrustConfigPath != trustPath {
		t.Fatalf("runtime = %#v", runtime)
	}
	results, err := runtime.Broker.Query(context.Background(), broker.Query{
		Path: "packages/app/main.go", ContextDigest: runtime.ContextDigest, View: runtime.View,
	})
	if err != nil || len(results) != 1 {
		t.Fatalf("Query() = %#v, %v", results, err)
	}
	described, err := runtime.Broker.Describe(context.Background(), results[0].Ref, broker.RequestContext{
		ContextDigest: runtime.ContextDigest, View: runtime.View,
	})
	if err != nil || described.Name != selected.Name || described.Availability != capability.AvailabilitySetupRequired {
		t.Fatalf("Describe() = %#v, %v", described, err)
	}
}

func TestTrustedPolicyRequiresExactBindingAndReadyServer(t *testing.T) {
	root := t.TempDir()
	selected := capability.Capability{
		Server:       capability.ServerVersion{Identity: capability.ServerIdentity{CanonicalName: "io.example/issues"}, Version: "1.0.0"},
		Kind:         capability.CapabilityTool,
		Name:         "issues.create",
		RoutingScope: "packages/app/**",
	}
	cfg := &config.Config{Version: config.MCPConfigVersion, MCP: &config.MCPConfig{
		Enabled:  true,
		Bindings: []config.MCPBinding{{Server: "io.example/issues", Version: "1.0.0", Scope: "packages/app/**"}},
	}}
	trusted := &trust.Config{Version: trust.ConfigVersion, Servers: []trust.Server{{
		Server: "io.example/issues", Version: "1.0.0", AllowedProjects: []string{root},
		Stdio: &trust.StdioConfig{Command: filepath.Join(root, "server")},
	}}}
	policy := trustedPolicy{cfg: cfg, trusted: trusted, projectRoot: root}
	if effect, err := policy.Evaluate(context.Background(), selected, nil); err != nil || effect != broker.PolicyAllow {
		t.Fatalf("ready policy = %s, %v", effect, err)
	}
	selected.RoutingScope = "other/**"
	if effect, err := policy.Evaluate(context.Background(), selected, nil); err != nil || effect != broker.PolicyDeny {
		t.Fatalf("mismatched policy = %s, %v", effect, err)
	}
}

func TestConnectorFactoryRejectsUntrustedServer(t *testing.T) {
	factory := connectorFactory{trusted: &trust.Config{Version: trust.ConfigVersion}, projectRoot: t.TempDir()}
	_, err := factory.Open(context.Background(), capability.Capability{
		Server: capability.ServerVersion{Identity: capability.ServerIdentity{CanonicalName: "io.example/missing"}, Version: "1"},
	})
	if !errors.Is(err, trust.ErrServerUntrusted) {
		t.Fatalf("Open() error = %v", err)
	}
}
