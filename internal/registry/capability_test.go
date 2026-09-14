package registry

import (
	"encoding/json"
	"fmt"
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

func TestQueryCapabilityPageFiltersCountsFacetsAndPagesInSQL(t *testing.T) {
	reg := newTestRegistry(t)
	for i := 0; i < 30; i++ {
		scope := "packages/other/**"
		server := "io.example/other"
		if i < 20 {
			scope = "packages/app/**"
			server = "io.example/app"
		}
		kind := capability.CapabilityTool
		if i%2 == 1 {
			kind = capability.CapabilityPrompt
		}
		availability := capability.AvailabilityReady
		if i%2 == 1 {
			availability = capability.AvailabilitySetupRequired
		}
		_, err := reg.InsertCapability(CapabilityRecord{
			Capability: capability.Capability{
				Server: capability.ServerVersion{Identity: capability.ServerIdentity{CanonicalName: server}, Version: "1"},
				Kind:   kind, Name: fmt.Sprintf("common.%02d", i), Description: "common benchmark capability",
				InputSchemaJSON: json.RawMessage(`{"type":"object"}`), Availability: availability,
			},
			SourceType: "test", SourceRef: "generated",
			Bindings: []CapabilityBinding{{Scope: scope, Relationship: "available"}},
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	page, err := reg.QueryCapabilityPage(CapabilityQuery{
		Search: "common", Path: "packages/app/src/main.go", View: "public", Limit: 5, Offset: 5,
	})
	if err != nil {
		t.Fatal(err)
	}
	if page.MatchCount != 20 || len(page.Records) != 5 {
		t.Fatalf("page count = %d, records = %d", page.MatchCount, len(page.Records))
	}
	if page.Records[0].Capability.Name != "common.11" || page.Records[4].Capability.Name != "common.19" {
		t.Fatalf("page records = %s..%s", page.Records[0].Capability.Name, page.Records[4].Capability.Name)
	}
	if len(page.Facets.Servers) != 1 || page.Facets.Servers[0] != (CapabilityFacet{Value: "io.example/app", Count: 20}) {
		t.Fatalf("server facets = %#v", page.Facets.Servers)
	}
	if len(page.Facets.Kinds) != 2 || page.Facets.Kinds[0].Count != 10 || page.Facets.Kinds[1].Count != 10 {
		t.Fatalf("kind facets = %#v", page.Facets.Kinds)
	}
	if len(page.Facets.Availability) != 2 || page.Facets.Availability[0].Count != 10 || page.Facets.Availability[1].Count != 10 {
		t.Fatalf("availability facets = %#v", page.Facets.Availability)
	}
}

func TestCapabilityFTSRowIDMigrationPreservesSearch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "index.db")
	reg, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	id, err := reg.InsertCapability(CapabilityRecord{
		Capability: capability.Capability{
			Server: capability.ServerVersion{Identity: capability.ServerIdentity{CanonicalName: "io.example/migrate"}, Version: "1"},
			Kind:   capability.CapabilityTool, Name: "migration.search", Description: "migration sentinel",
			InputSchemaJSON: json.RawMessage(`{"type":"object"}`), Availability: capability.AvailabilityReady,
		}, SourceType: "test", SourceRef: "migration",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reg.db.Exec(`UPDATE mcp_capability_search SET rowid = ? WHERE rowid = ?`, id+100, id); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.db.Exec(`PRAGMA user_version = 5`); err != nil {
		t.Fatal(err)
	}
	if err := reg.Close(); err != nil {
		t.Fatal(err)
	}

	reg, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reg.Close()
	page, err := reg.QueryCapabilityPage(CapabilityQuery{Search: "sentinel", View: "public", Limit: 8})
	if err != nil {
		t.Fatal(err)
	}
	if page.MatchCount != 1 || len(page.Records) != 1 || page.Records[0].ID != id {
		t.Fatalf("migrated search page = %#v", page)
	}
	var rowID int64
	if err := reg.db.QueryRow(`SELECT rowid FROM mcp_capability_search WHERE capability_id = ?`, id).Scan(&rowID); err != nil {
		t.Fatal(err)
	}
	if rowID != id {
		t.Fatalf("migrated FTS rowid = %d, want %d", rowID, id)
	}
}

func BenchmarkCapabilitySearch50000(b *testing.B) {
	reg, err := Open(filepath.Join(b.TempDir(), "index.db"))
	if err != nil {
		b.Fatal(err)
	}
	defer reg.Close()
	b.StopTimer()
	if _, err := reg.db.Exec(`PRAGMA synchronous = OFF; PRAGMA journal_mode = MEMORY;`); err != nil {
		b.Fatal(err)
	}
	tx, err := reg.db.Begin()
	if err != nil {
		b.Fatal(err)
	}
	defer tx.Rollback() //nolint:errcheck
	now := "2026-01-01T00:00:00Z"
	serverResult, err := tx.Exec(`INSERT INTO mcp_servers
		(canonical_name, publisher, source_type, source_ref, indexed_at) VALUES (?, ?, ?, ?, ?)`,
		"io.example/benchmark", "example", "benchmark", "generated", now)
	if err != nil {
		b.Fatal(err)
	}
	serverID, _ := serverResult.LastInsertId()
	versionResult, err := tx.Exec(`INSERT INTO mcp_server_versions
		(server_id, version, package_digest, status, raw_metadata, indexed_at) VALUES (?, ?, ?, ?, ?, ?)`,
		serverID, "1.0.0", "sha256:benchmark", "active", []byte(`{}`), now)
	if err != nil {
		b.Fatal(err)
	}
	serverVersionID, _ := versionResult.LastInsertId()
	viewResult, err := tx.Exec(`INSERT INTO mcp_capability_views
		(server_version_id, visibility, auth_partition_hash, cache_scope, provenance) VALUES (?, ?, ?, ?, ?)`,
		serverVersionID, "public", "public", "public", []byte(`{}`))
	if err != nil {
		b.Fatal(err)
	}
	viewID, _ := viewResult.LastInsertId()
	insertCapability, err := tx.Prepare(`INSERT INTO mcp_capabilities
		(server_version_id, view_id, kind, name, title, description, input_schema, output_schema,
		 schema_digest, risk, availability, raw_definition, indexed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		b.Fatal(err)
	}
	defer insertCapability.Close()
	insertBinding, err := tx.Prepare(`INSERT INTO mcp_capability_bindings
		(capability_id, scope, path_prefix, pattern_type, relationship, auth_profile) VALUES (?, ?, ?, ?, ?, ?)`)
	if err != nil {
		b.Fatal(err)
	}
	defer insertBinding.Close()
	insertSearch, err := tx.Prepare(`INSERT INTO mcp_capability_search
		(rowid, capability_id, server, name, title, description, schema_summary, binding) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		b.Fatal(err)
	}
	defer insertSearch.Close()
	for i := 0; i < 50_000; i++ {
		name := fmt.Sprintf("tool.%05d", i)
		description := fmt.Sprintf("benchmark capability number %05d", i)
		result, err := insertCapability.Exec(serverVersionID, viewID, capability.CapabilityTool, name,
			"Capability search benchmark", description, `{"type":"object"}`, "", "sha256:benchmark", "",
			capability.AvailabilityReady, []byte(`{}`), now)
		if err != nil {
			b.Fatal(err)
		}
		capabilityID, _ := result.LastInsertId()
		if _, err := insertBinding.Exec(capabilityID, "packages/app/**", "packages/app", "prefix", "available", ""); err != nil {
			b.Fatal(err)
		}
		if _, err := insertSearch.Exec(capabilityID, capabilityID, "io.example/benchmark", name, "Capability search benchmark",
			description, `{"type":"object"}`, "available packages/app/**"); err != nil {
			b.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		b.Fatal(err)
	}
	b.Run("Exact", func(b *testing.B) {
		b.ReportAllocs()
		b.ReportMetric(50_000, "capabilities")
		for range b.N {
			page, err := reg.QueryCapabilityPage(CapabilityQuery{Search: "49999", View: "public", Limit: 20})
			if err != nil || page.MatchCount != 1 || len(page.Records) != 1 {
				b.Fatalf("exact search = %d/%d results, %v", len(page.Records), page.MatchCount, err)
			}
		}
	})
	b.Run("BroadPage", func(b *testing.B) {
		b.ReportAllocs()
		b.ReportMetric(50_000, "capabilities")
		for range b.N {
			page, err := reg.QueryCapabilityPage(CapabilityQuery{Search: "benchmark", View: "public", Limit: 20})
			if err != nil || page.MatchCount != 50_000 || len(page.Records) != 20 {
				b.Fatalf("broad search = %d/%d results, %v", len(page.Records), page.MatchCount, err)
			}
		}
	})
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

func TestRefreshImportsOnlyProjectSelectedTrustedCatalogCache(t *testing.T) {
	root := t.TempDir()
	trustPath := filepath.Join(t.TempDir(), "mcp-trust.yaml")
	if err := os.WriteFile(trustPath, []byte(`Version: 1
CatalogSources:
  - Name: official
    Type: registry-api
    BaseURL: https://registry.example
`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SKILLEX_MCP_TRUST_CONFIG", trustPath)
	cache := filepath.Join(root, ".skillex", "mcp", "catalogs", "official.json")
	if err := os.MkdirAll(filepath.Dir(cache), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cache, []byte(`{"capabilities":[{"server":{"identity":{"canonical_name":"io.example/issues"},"version":"1.0.0"},"kind":"server","name":"server.discover","description":"Issue service","schema_digest":"","availability":"discovered"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{Version: config.MCPConfigVersion, MCP: &config.MCPConfig{
		Enabled:  true,
		Catalogs: []config.MCPCatalog{{Type: "trusted", Name: "official"}},
		Bindings: []config.MCPBinding{{Server: "io.example/issues", Version: "1.0.0", Scope: "**"}},
	}}
	reg := newTestRegistry(t)
	result, err := Refresh(reg, cfg, RefreshOptions{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	if result.CapabilitiesAdded != 1 {
		t.Fatalf("capabilities added = %d", result.CapabilitiesAdded)
	}
	records, err := reg.AllCapabilities()
	if err != nil || len(records) != 1 || records[0].SourceType != "registry-api" || records[0].SourceRef != "official" {
		t.Fatalf("trusted records = %#v, %v", records, err)
	}
}
