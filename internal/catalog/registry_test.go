package catalog

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/atheory-ai/skillex/internal/broker"
	"github.com/atheory-ai/skillex/internal/capability"
	"github.com/atheory-ai/skillex/internal/registry"
)

func TestRegistrySearchFiltersByCapabilityBindingScope(t *testing.T) {
	reg, err := registry.Open(filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer reg.Close()
	_, err = reg.InsertCapability(registry.CapabilityRecord{
		Capability: capability.Capability{
			Server: capability.ServerVersion{Identity: capability.ServerIdentity{CanonicalName: "io.example/issues"}, Version: "1.0.0"},
			Kind:   capability.CapabilityTool, Name: "issues.create", Description: "Create an issue",
			InputSchemaJSON: json.RawMessage(`{"type":"object"}`), Availability: capability.AvailabilitySetupRequired,
		},
		SourceType: "static", SourceRef: "catalog.json",
		Bindings: []registry.CapabilityBinding{{Scope: "packages/app/**", Relationship: "available"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := NewRegistry(reg)
	if err != nil {
		t.Fatal(err)
	}
	matched, err := catalog.Search(context.Background(), broker.Query{Path: "packages/app/src/main.go", Search: "issue"})
	if err != nil || len(matched) != 1 {
		t.Fatalf("matching search = %#v, %v", matched, err)
	}
	unmatched, err := catalog.Search(context.Background(), broker.Query{Path: "packages/other/main.go", Search: "issue"})
	if err != nil || len(unmatched) != 0 {
		t.Fatalf("out-of-scope search = %#v, %v", unmatched, err)
	}
}
