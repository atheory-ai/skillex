package registry

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/atheory-ai/skillex/internal/capability"
	"github.com/atheory-ai/skillex/internal/config"
)

func TestCapabilityRegistryInsertSearchAndResolve(t *testing.T) {
	reg := newTestRegistry(t)
	selected := capability.Capability{
		Server: capability.ServerVersion{
			Identity: capability.ServerIdentity{CanonicalName: "io.example/issues", Publisher: "example"},
			Version:  "1.0.0",
		},
		Kind:            capability.CapabilityTool,
		Name:            "issues.create",
		Title:           "Create issue",
		Description:     "Create an issue in the configured tracker.",
		InputSchemaJSON: json.RawMessage(`{"type":"object","properties":{"title":{"type":"string"}}}`),
		Availability:    capability.AvailabilitySetupRequired,
	}
	id, err := reg.InsertCapability(CapabilityRecord{
		Capability: selected,
		SourceType: "static",
		SourceRef:  "catalog.json",
		Bindings: []CapabilityBinding{{
			Scope:        "packages/app/**",
			Relationship: "available",
			AuthProfile:  "issues-work",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if id == 0 {
		t.Fatal("InsertCapability() returned zero id")
	}

	results, err := reg.QueryCapabilitiesBySearch("tracker title")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Capability.Name != "issues.create" {
		t.Fatalf("capability search results = %#v", results)
	}
	if len(results[0].Bindings) != 1 || results[0].Bindings[0].AuthProfile != "issues-work" {
		t.Fatalf("capability bindings = %#v", results[0].Bindings)
	}

	resolved, err := reg.ResolveCapability("io.example/issues", "1.0.0", capability.CapabilityTool, "issues.create")
	if err != nil {
		t.Fatal(err)
	}
	if resolved == nil || resolved.Capability.SchemaDigest == "" {
		t.Fatalf("resolved capability = %#v", resolved)
	}
}

func TestLoadStaticCapabilityCatalogAppliesOnlyExactBindings(t *testing.T) {
	root := t.TempDir()
	data := `{
  "capabilities": [{
    "server": {"identity": {"canonical_name": "io.example/issues", "publisher": "example"}, "version": "1.0.0"},
    "kind": "tool",
    "name": "issues.create",
    "description": "Create an issue.",
    "input_schema": {"type": "object"},
    "schema_digest": "",
    "availability": "ready"
  }]
}`
	if err := os.WriteFile(filepath.Join(root, "catalog.json"), []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	records, err := LoadStaticCapabilityCatalog(root, config.MCPCatalog{Type: "static", Path: "catalog.json"}, []config.MCPBinding{
		{Server: "io.example/issues", Version: "2.0.0", Scope: "wrong/**"},
		{Server: "io.example/issues", Version: "1.0.0", Scope: "packages/app/**", AuthProfile: "issues-work"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || len(records[0].Bindings) != 1 {
		t.Fatalf("records = %#v", records)
	}
	if got := records[0].Bindings[0].Scope; got != "packages/app/**" {
		t.Fatalf("binding scope = %q", got)
	}
	if got := records[0].Capability.Availability; got != capability.AvailabilitySetupRequired {
		t.Fatalf("availability = %q, want setup-required", got)
	}
}

func TestLoadStaticCapabilityCatalogRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "catalog.json")
	if err := os.WriteFile(outside, []byte(`{"capabilities":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "catalog.json")); err != nil {
		t.Fatal(err)
	}
	_, err := LoadStaticCapabilityCatalog(root, config.MCPCatalog{Type: "static", Path: "catalog.json"}, nil)
	if err == nil {
		t.Fatal("expected symlink escape rejection")
	}
}

func TestMigrateFreshDBHasCapabilitySchema(t *testing.T) {
	reg := newTestRegistry(t)
	for _, table := range []string{
		"mcp_servers", "mcp_server_versions", "mcp_transports",
		"mcp_capability_views", "mcp_capabilities", "mcp_capability_bindings",
		"mcp_capability_search",
	} {
		var name string
		if err := reg.db.QueryRow(`SELECT name FROM sqlite_master WHERE name = ?`, table).Scan(&name); err != nil {
			t.Fatalf("missing capability table %s: %v", table, err)
		}
	}
}
