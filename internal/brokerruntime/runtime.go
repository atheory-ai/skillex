// Package brokerruntime assembles the local capability broker from trusted
// project configuration and the offline registry.
package brokerruntime

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/atheory-ai/skillex/internal/broker"
	"github.com/atheory-ai/skillex/internal/capability"
	"github.com/atheory-ai/skillex/internal/catalog"
	"github.com/atheory-ai/skillex/internal/config"
	"github.com/atheory-ai/skillex/internal/connector/stdio"
	"github.com/atheory-ai/skillex/internal/connector/streamhttp"
	"github.com/atheory-ai/skillex/internal/registry"
	"github.com/atheory-ai/skillex/internal/telemetry"
	"github.com/atheory-ai/skillex/internal/trust"
)

const localView = "local:public"

// Runtime contains the broker and reference-binding context shared by CLI and
// MCP interface layers.
type Runtime struct {
	Broker          *broker.Broker
	ContextDigest   string
	View            string
	TrustConfigPath string
}

// NewDiscovery constructs the offline discovery/describe runtime. Invocation
// remains unavailable until trusted policy and connector configuration are
// supplied; static catalog metadata alone can never execute a process.
func NewDiscovery(root string, cfg *config.Config, reg *registry.Registry) (*Runtime, error) {
	if cfg == nil || !cfg.MCPEnabled() {
		return nil, broker.ErrMCPDisabled
	}
	registryCatalog, err := catalog.NewRegistry(reg)
	if err != nil {
		return nil, err
	}
	trusted, trustPath, err := trust.LoadConfigured()
	if err != nil {
		return nil, fmt.Errorf("loading trusted MCP configuration: %w", err)
	}
	catalogAdapter := readinessCatalog{Catalog: registryCatalog, trusted: trusted, projectRoot: root}
	signer, err := capability.LoadOrCreateReferenceSigner(
		filepath.Join(root, ".skillex", "mcp-signing.key"), 5*time.Minute,
	)
	if err != nil {
		return nil, err
	}
	signature, err := reg.Signature()
	if err != nil {
		return nil, fmt.Errorf("computing capability context: %w", err)
	}
	var options []broker.Option
	if trusted.Telemetry != nil && trusted.Telemetry.Enabled {
		options = append(options, broker.WithObserver(telemetry.NewLocal(trusted.Telemetry.Path)))
	}
	engine, err := broker.NewConfigured(cfg, catalogAdapter, signer,
		trustedPolicy{cfg: cfg, trusted: trusted, projectRoot: root},
		&connectorFactory{trusted: trusted, projectRoot: root, httpCache: &streamhttp.DefinitionCache{}}, options...)
	if err != nil {
		return nil, err
	}
	return &Runtime{Broker: engine, ContextDigest: "sha256:" + signature, View: localView, TrustConfigPath: trustPath}, nil
}

type readinessCatalog struct {
	broker.Catalog
	trusted     *trust.Config
	projectRoot string
}

func (c readinessCatalog) Search(ctx context.Context, query broker.Query) ([]capability.Capability, error) {
	results, err := c.Catalog.Search(ctx, query)
	if err != nil {
		return nil, err
	}
	for i := range results {
		if results[i].Availability != capability.AvailabilityStale {
			results[i].Availability = c.trusted.Ready(results[i], c.projectRoot)
		}
	}
	return results, nil
}

func (c readinessCatalog) Resolve(ctx context.Context, claims capability.ReferenceClaims) (capability.Capability, error) {
	selected, err := c.Catalog.Resolve(ctx, claims)
	if err != nil {
		return capability.Capability{}, err
	}
	if selected.Availability != capability.AvailabilityStale {
		selected.Availability = c.trusted.Ready(selected, c.projectRoot)
	}
	return selected, nil
}

type trustedPolicy struct {
	cfg         *config.Config
	trusted     *trust.Config
	projectRoot string
}

func (p trustedPolicy) Evaluate(_ context.Context, selected capability.Capability, _ map[string]any) (broker.PolicyEffect, error) {
	foundBinding := false
	for _, binding := range p.cfg.MCP.Bindings {
		if binding.Server == selected.Server.Identity.CanonicalName && binding.Version == selected.Server.Version &&
			binding.Scope == selected.RoutingScope && binding.AuthProfile == selected.AuthProfile {
			foundBinding = true
			break
		}
	}
	if !foundBinding || p.trusted.Ready(selected, p.projectRoot) != capability.AvailabilityReady {
		return broker.PolicyDeny, nil
	}
	return broker.PolicyAllow, nil
}

type connectorFactory struct {
	trusted     *trust.Config
	projectRoot string
	httpCache   *streamhttp.DefinitionCache
}

func (f connectorFactory) Open(ctx context.Context, selected capability.Capability) (broker.Connector, error) {
	server, ok := f.trusted.FindServer(selected.Server.Identity.CanonicalName, selected.Server.Version)
	if !ok {
		return nil, trust.ErrServerUntrusted
	}
	if server.HTTP != nil {
		configured, err := f.trusted.StreamableHTTPConfig(ctx, selected, f.projectRoot)
		if err != nil {
			return nil, err
		}
		configured.DefinitionCache = f.httpCache
		configured.CachePartition = selected.AuthProfile
		factory, err := streamhttp.NewFactory([]streamhttp.ServerConfig{configured})
		if err != nil {
			return nil, err
		}
		return factory.Open(ctx, selected)
	}
	configured, err := f.trusted.StdioConfig(selected, f.projectRoot)
	if err != nil {
		return nil, err
	}
	factory, err := stdio.NewFactory([]stdio.ServerConfig{configured})
	if err != nil {
		return nil, err
	}
	return factory.Open(ctx, selected)
}
