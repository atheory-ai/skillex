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
func (c *Registry) Search(_ context.Context, query broker.Query) ([]capability.Capability, error) {
	records, err := c.registry.QueryCapabilitiesBySearch(query.Search)
	if err != nil {
		return nil, err
	}
	results := make([]capability.Capability, 0, len(records))
	for _, record := range records {
		binding, ok := matchingBinding(record, query.Path)
		if query.Path != "" && !ok {
			continue
		}
		selected := record.Capability
		if ok {
			selected.RoutingScope = binding.Scope
			selected.AuthProfile = binding.AuthProfile
		}
		results = append(results, selected)
	}
	return results, nil
}

// Resolve retrieves the exact current definition named by a selected ref.
func (c *Registry) Resolve(_ context.Context, claims capability.ReferenceClaims) (capability.Capability, error) {
	record, err := c.registry.ResolveCapability(claims.Server, claims.Version, claims.Kind, claims.Capability)
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
