package query

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/atheory-ai/skillex/internal/broker"
	"github.com/atheory-ai/skillex/internal/capability"
	"github.com/atheory-ai/skillex/internal/registry"
)

func TestExecuteReturnsAdditiveCapabilityResults(t *testing.T) {
	reg, err := registry.Open(filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer reg.Close()
	discoverer := &fakeCapabilityDiscoverer{results: []broker.Summary{{
		Ref: "mcp-tool:v1:test", Server: capability.ServerIdentity{CanonicalName: "io.example/issues"},
		Version: "1.0.0", Kind: capability.CapabilityTool, Name: "issues.create",
		Availability: capability.AvailabilitySetupRequired,
	}}}
	engine := NewWithCapabilities(reg, discoverer, "sha256:workspace", "local:public")
	response, err := engine.Execute(Params{Path: "packages/app/main.go", Search: "create issue", Limit: 8})
	if err != nil {
		t.Fatal(err)
	}
	if response.Type != ResponseTypeResults || len(response.Results) != 0 || len(response.Capabilities) != 1 {
		t.Fatalf("response = %#v", response)
	}
	if discoverer.query.ContextDigest != "sha256:workspace" || discoverer.query.View != "local:public" {
		t.Fatalf("capability query context = %#v", discoverer.query)
	}
}

func TestCapabilityOnlyResultsUseIndependentContinuationOffset(t *testing.T) {
	reg, err := registry.Open(filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer reg.Close()
	discoverer := &fakeCapabilityDiscoverer{}
	for _, name := range []string{"one", "two", "three"} {
		discoverer.results = append(discoverer.results, broker.Summary{Ref: "ref-" + name,
			Server: capability.ServerIdentity{CanonicalName: "io.example/tools"}, Version: "1", Kind: capability.CapabilityTool, Name: name})
	}
	engine := NewWithCapabilities(reg, discoverer, "sha256:workspace", "local:public")
	first, err := engine.Execute(Params{Search: "tool", Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Capabilities) != 2 || first.NextCursor == "" {
		t.Fatalf("first capability page = %#v", first)
	}
	second, err := engine.Execute(Params{Search: "tool", Limit: 2, Cursor: first.NextCursor})
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Capabilities) != 1 || second.Capabilities[0].Name != "three" || second.NextCursor != "" {
		t.Fatalf("second capability page = %#v", second)
	}
}

type fakeCapabilityDiscoverer struct {
	results []broker.Summary
	query   broker.Query
}

func (f *fakeCapabilityDiscoverer) Query(_ context.Context, query broker.Query) ([]broker.Summary, error) {
	f.query = query
	return append([]broker.Summary(nil), f.results...), nil
}
