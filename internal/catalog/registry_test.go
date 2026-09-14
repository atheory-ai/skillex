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
	if err != nil || len(matched.Capabilities) != 1 {
		t.Fatalf("matching search = %#v, %v", matched, err)
	}
	unmatched, err := catalog.Search(context.Background(), broker.Query{Path: "packages/other/main.go", Search: "issue"})
	if err != nil || len(unmatched.Capabilities) != 0 {
		t.Fatalf("out-of-scope search = %#v, %v", unmatched, err)
	}
}

func TestRegistryNeverReturnsOrResolvesAnotherPrivateView(t *testing.T) {
	reg, err := registry.Open(filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer reg.Close()
	selected := capability.Capability{
		Server: capability.ServerVersion{Identity: capability.ServerIdentity{CanonicalName: "io.example/private"}, Version: "1"},
		Kind:   capability.CapabilityTool, Name: "private.read", Description: "Private data", Availability: capability.AvailabilityReady,
	}
	for _, view := range []string{"tenant-a", "tenant-b"} {
		if _, err := reg.InsertCapability(registry.CapabilityRecord{Capability: selected, Visibility: "private", AuthPartitionHash: view,
			CacheScope: "private", SourceType: "observed", SourceRef: view}); err != nil {
			t.Fatal(err)
		}
	}
	catalog, _ := NewRegistry(reg)
	results, err := catalog.Search(context.Background(), broker.Query{Search: "Private", View: "tenant-a"})
	if err != nil || len(results.Capabilities) != 1 {
		t.Fatalf("private search = %#v, %v", results, err)
	}
	claims := capability.ReferenceClaims{Server: "io.example/private", Version: "1", Kind: capability.CapabilityTool, Capability: "private.read", View: "tenant-a"}
	if _, err := catalog.Resolve(context.Background(), claims); err != nil {
		t.Fatal(err)
	}
	claims.View = "tenant-c"
	if _, err := catalog.Resolve(context.Background(), claims); err == nil {
		t.Fatal("unrecognized tenant resolved a private capability")
	}
}
