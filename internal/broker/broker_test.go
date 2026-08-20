package broker

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/atheory-ai/skillex/internal/capability"
	"github.com/atheory-ai/skillex/internal/config"
)

func TestNewConfiguredRequiresExplicitMCPOptIn(t *testing.T) {
	for _, cfg := range []*config.Config{
		nil,
		config.DefaultConfig(),
		{Version: config.MCPConfigVersion},
		{Version: config.MCPConfigVersion, MCP: &config.MCPConfig{Enabled: false}},
	} {
		created, err := NewConfigured(cfg, nil, nil, nil, nil)
		if !errors.Is(err, ErrMCPDisabled) {
			t.Fatalf("NewConfigured(%#v) error = %v, want %v", cfg, err, ErrMCPDisabled)
		}
		if created != nil {
			t.Fatalf("NewConfigured(%#v) returned a broker while disabled", cfg)
		}
	}
}

func TestNewConfiguredAllowsValidatedOptIn(t *testing.T) {
	cfg := &config.Config{
		Version: config.MCPConfigVersion,
		MCP: &config.MCPConfig{
			Enabled: true,
			Bindings: []config.MCPBinding{{
				Server:  "io.example/issues",
				Version: "1.0.0",
				Scope:   "**",
			}},
		},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	signer, err := capability.NewReferenceSigner([]byte("0123456789abcdef0123456789abcdef"), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	created, err := NewConfigured(cfg, &fakeCatalog{}, signer, allowPolicy{}, &fakeConnectorFactory{})
	if err != nil {
		t.Fatalf("NewConfigured(enabled) error = %v", err)
	}
	if created == nil {
		t.Fatal("NewConfigured(enabled) returned nil broker")
	}
}

func TestBrokerQueriesOfflineAndInvokesOnlySelectedServer(t *testing.T) {
	ctx := context.Background()
	weather := newTestCapability(t, "io.example/weather", "weather.lookup")
	issues := newTestCapability(t, "io.example/issues", "issues.create")
	catalog := &fakeCatalog{capabilities: []capability.Capability{weather, issues}}
	factory := &fakeConnectorFactory{}
	broker := newTestBroker(t, catalog, allowPolicy{}, factory)

	results, err := broker.Query(ctx, Query{
		Search:        "create an issue",
		ContextDigest: "sha256:repo-context",
		View:          "private:test-user",
		Limit:         8,
	})
	if err != nil {
		t.Fatal(err)
	}
	if catalog.resolveCalls != 0 || len(factory.opened) != 0 {
		t.Fatal("query performed runtime resolution or opened a downstream connector")
	}
	if len(results) != 2 {
		t.Fatalf("Query() returned %d results, want 2", len(results))
	}

	call, err := broker.Call(ctx, results[1].Ref, map[string]any{"title": "Broken build"}, RequestContext{
		ContextDigest: "sha256:repo-context",
		View:          "private:test-user",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(factory.opened) != 1 || factory.opened[0] != issues.Server.Identity.CanonicalName {
		t.Fatalf("opened servers = %v, want only %q", factory.opened, issues.Server.Identity.CanonicalName)
	}
	if call.Server.Identity.CanonicalName != issues.Server.Identity.CanonicalName || call.Capability.Name != issues.Name {
		t.Fatalf("call result lacked downstream attribution: %#v", call)
	}
}

func TestBrokerRejectsContextAndSchemaChangesBeforeConnecting(t *testing.T) {
	ctx := context.Background()
	selected := newTestCapability(t, "io.example/issues", "issues.create")
	catalog := &fakeCatalog{capabilities: []capability.Capability{selected}}
	factory := &fakeConnectorFactory{}
	broker := newTestBroker(t, catalog, allowPolicy{}, factory)
	results, err := broker.Query(ctx, Query{ContextDigest: "sha256:first", View: "public"})
	if err != nil {
		t.Fatal(err)
	}

	_, err = broker.Call(ctx, results[0].Ref, nil, RequestContext{ContextDigest: "sha256:other", View: "public"})
	if !errors.Is(err, ErrContextMismatch) {
		t.Fatalf("Call(context mismatch) error = %v, want %v", err, ErrContextMismatch)
	}

	catalog.capabilities[0].InputSchemaJSON = []byte(`{"type":"object","required":["title"]}`)
	_, err = broker.Call(ctx, results[0].Ref, nil, RequestContext{ContextDigest: "sha256:first", View: "public"})
	if !errors.Is(err, ErrCapabilityChanged) {
		t.Fatalf("Call(schema changed) error = %v, want %v", err, ErrCapabilityChanged)
	}
	if len(factory.opened) != 0 {
		t.Fatalf("opened connectors after validation failure: %v", factory.opened)
	}
}

func TestBrokerEnforcesPolicyBeforeConnecting(t *testing.T) {
	ctx := context.Background()
	selected := newTestCapability(t, "io.example/issues", "issues.delete")
	catalog := &fakeCatalog{capabilities: []capability.Capability{selected}}
	factory := &fakeConnectorFactory{}
	broker := newTestBroker(t, catalog, staticPolicy{effect: PolicyApprovalRequired}, factory)
	results, err := broker.Query(ctx, Query{ContextDigest: "sha256:repo", View: "public"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = broker.Call(ctx, results[0].Ref, map[string]any{"id": "123"}, RequestContext{ContextDigest: "sha256:repo", View: "public"})
	if !errors.Is(err, ErrApprovalRequired) {
		t.Fatalf("Call() error = %v, want %v", err, ErrApprovalRequired)
	}
	if len(factory.opened) != 0 {
		t.Fatalf("opened connectors before approval: %v", factory.opened)
	}
}

func newTestBroker(t *testing.T, catalog Catalog, policy Policy, factory ConnectorFactory) *Broker {
	t.Helper()
	signer, err := capability.NewReferenceSigner(
		[]byte("0123456789abcdef0123456789abcdef"),
		time.Minute,
		capability.WithClock(func() time.Time { return time.Unix(1_800_000_000, 0) }),
	)
	if err != nil {
		t.Fatal(err)
	}
	broker, err := New(catalog, signer, policy, factory)
	if err != nil {
		t.Fatal(err)
	}
	return broker
}

func newTestCapability(t *testing.T, server, name string) capability.Capability {
	t.Helper()
	selected, err := (capability.Capability{
		Server: capability.ServerVersion{
			Identity: capability.ServerIdentity{CanonicalName: server, Publisher: "example"},
			Version:  "1.0.0",
		},
		Kind:            capability.CapabilityTool,
		Name:            name,
		Description:     "Fake downstream capability",
		InputSchemaJSON: []byte(`{"type":"object"}`),
		Availability:    capability.AvailabilityReady,
	}).WithComputedSchemaDigest()
	if err != nil {
		t.Fatal(err)
	}
	return selected
}

type fakeCatalog struct {
	capabilities []capability.Capability
	resolveCalls int
}

func (f *fakeCatalog) Search(context.Context, Query) ([]capability.Capability, error) {
	return append([]capability.Capability(nil), f.capabilities...), nil
}

func (f *fakeCatalog) Resolve(_ context.Context, claims capability.ReferenceClaims) (capability.Capability, error) {
	f.resolveCalls++
	for _, selected := range f.capabilities {
		if selected.Server.Identity.CanonicalName == claims.Server && selected.Server.Version == claims.Version &&
			selected.Kind == claims.Kind && selected.Name == claims.Capability {
			return selected, nil
		}
	}
	return capability.Capability{}, fmt.Errorf("capability not found")
}

type allowPolicy struct{}

func (allowPolicy) Evaluate(context.Context, capability.Capability, map[string]any) (PolicyEffect, error) {
	return PolicyAllow, nil
}

type staticPolicy struct {
	effect PolicyEffect
}

func (p staticPolicy) Evaluate(context.Context, capability.Capability, map[string]any) (PolicyEffect, error) {
	return p.effect, nil
}

type fakeConnectorFactory struct {
	opened []string
}

func (f *fakeConnectorFactory) Open(_ context.Context, selected capability.Capability) (Connector, error) {
	f.opened = append(f.opened, selected.Server.Identity.CanonicalName)
	return &fakeConnector{server: selected.Server.Identity.CanonicalName}, nil
}

type fakeConnector struct {
	server string
}

func (f *fakeConnector) CallTool(_ context.Context, name string, arguments map[string]any) (any, error) {
	return map[string]any{"server": f.server, "tool": name, "arguments": arguments}, nil
}

func (*fakeConnector) Close() error { return nil }
