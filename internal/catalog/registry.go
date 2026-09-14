// Package catalog adapts persisted capability metadata to the broker's offline
// discovery and exact-resolution contract.
package catalog

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/atheory-ai/skillex/internal/broker"
	"github.com/atheory-ai/skillex/internal/capability"
	"github.com/atheory-ai/skillex/internal/registry"
	"github.com/gobwas/glob"
)

// Registry is an offline broker catalog backed by the Skillex SQLite index.
type Registry struct {
	registry *registry.Registry
}

// NewRegistry constructs an offline capability catalog.
func NewRegistry(reg *registry.Registry) (*Registry, error) {
	if reg == nil {
		return nil, fmt.Errorf("capability registry is required")
	}
	return &Registry{registry: reg}, nil
}

// Search returns capability definitions matching intent and project scope. It
// performs no authentication, network requests, or downstream process starts.
func (c *Registry) Search(_ context.Context, query broker.Query) (broker.CatalogPage, error) {
	page, err := c.registry.QueryCapabilityPage(registry.CapabilityQuery{
		Search: query.Search, Path: query.Path, Server: query.Server, Kind: query.Kind,
		Availability: query.Availability, View: query.View, Limit: query.Limit, Offset: query.Offset,
	})
	if err != nil {
		return broker.CatalogPage{}, err
	}
	results := make([]capability.Capability, 0, len(page.Records))
	for _, record := range page.Records {
		binding, ok := matchingBinding(record, query.Path)
		selected := record.Capability
		if ok {
			selected.RoutingScope = binding.Scope
			selected.AuthProfile = binding.AuthProfile
		}
		results = append(results, selected)
	}
	return broker.CatalogPage{Capabilities: results, MatchCount: page.MatchCount, Facets: broker.DiscoveryFacets{
		Servers: capabilityFacets(page.Facets.Servers), Kinds: capabilityFacets(page.Facets.Kinds),
		Availability: capabilityFacets(page.Facets.Availability),
	}}, nil
}

func capabilityFacets(values []registry.CapabilityFacet) []broker.Facet {
	results := make([]broker.Facet, len(values))
	for i, value := range values {
		results[i] = broker.Facet{Value: value.Value, Count: value.Count}
	}
	return results
}

// Resolve retrieves the exact current definition named by a selected ref.
func (c *Registry) Resolve(_ context.Context, claims capability.ReferenceClaims) (capability.Capability, error) {
	record, err := c.registry.ResolveCapabilityView(claims.Server, claims.Version, claims.Kind, claims.Capability, claims.View)
	if err != nil {
		return capability.Capability{}, err
	}
	if record == nil {
		return capability.Capability{}, fmt.Errorf("capability %s@%s/%s not found", claims.Server, claims.Version, claims.Capability)
	}
	selected := record.Capability
	for _, binding := range record.Bindings {
		if binding.Scope == claims.RoutingScope && binding.AuthProfile == claims.AuthProfile {
			selected.RoutingScope = binding.Scope
			selected.AuthProfile = binding.AuthProfile
			return selected, nil
		}
	}
	if claims.RoutingScope != "" || claims.AuthProfile != "" {
		return capability.Capability{}, fmt.Errorf("capability binding is no longer available")
	}
	return selected, nil
}

func matchingBinding(record registry.CapabilityRecord, path string) (registry.CapabilityBinding, bool) {
	path = filepath.ToSlash(strings.TrimPrefix(path, "./"))
	for _, binding := range record.Bindings {
		pattern := filepath.ToSlash(binding.Scope)
		if pattern == "**" || pattern == "*" {
			return binding, true
		}
		compiled, err := glob.Compile(pattern, '/')
		if path != "" && err == nil && compiled.Match(path) {
			return binding, true
		}
	}
	if path == "" && len(record.Bindings) > 0 {
		return record.Bindings[0], true
	}
	return registry.CapabilityBinding{}, false
}
