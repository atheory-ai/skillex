// Package broker resolves and invokes contextually selected MCP capabilities.
// It contains no SDK, transport, credential-store, or persistence dependencies;
// those concerns are supplied through adapters.
package broker

import (
	"context"
	"errors"
	"fmt"

	"github.com/atheory-ai/skillex/internal/capability"
)

var (
	ErrContextMismatch    = errors.New("capability reference context mismatch")
	ErrViewMismatch       = errors.New("capability reference view mismatch")
	ErrCapabilityChanged  = errors.New("capability changed after reference issuance")
	ErrPolicyDenied       = errors.New("capability invocation denied by policy")
	ErrApprovalRequired   = errors.New("capability invocation requires approval")
	ErrCapabilityNotReady = errors.New("capability is not ready")
)

// Query describes the context and intent used to retrieve capabilities.
type Query struct {
	Path          string
	Search        string
	ContextDigest string
	View          string
	Limit         int
}

// Summary is a bounded capability discovery result.
type Summary struct {
	Ref          string                        `json:"ref"`
	Server       capability.ServerIdentity     `json:"server"`
	Version      string                        `json:"version"`
	Kind         capability.CapabilityKind     `json:"kind"`
	Name         string                        `json:"name"`
	Title        string                        `json:"title,omitempty"`
	Description  string                        `json:"description,omitempty"`
	Availability capability.AvailabilityStatus `json:"availability"`
}

// RequestContext binds a selected reference to the current workspace/request
// view. It is rechecked at describe and invocation time.
type RequestContext struct {
	ContextDigest string
	View          string
}

// Catalog is the indexed, offline source used for discovery and resolution.
type Catalog interface {
	Search(ctx context.Context, query Query) ([]capability.Capability, error)
	Resolve(ctx context.Context, claims capability.ReferenceClaims) (capability.Capability, error)
}

// PolicyEffect is the broker's invocation decision.
type PolicyEffect string

const (
	PolicyAllow            PolicyEffect = "allow"
	PolicyDeny             PolicyEffect = "deny"
	PolicyApprovalRequired PolicyEffect = "approval-required"
)

// Policy evaluates the actual selected server and capability immediately before
// invocation. A discovery result is never an authorization grant.
type Policy interface {
	Evaluate(ctx context.Context, selected capability.Capability, arguments map[string]any) (PolicyEffect, error)
}

// Connector is a lazily opened protocol adapter for one downstream server.
type Connector interface {
	CallTool(ctx context.Context, name string, arguments map[string]any) (any, error)
	Close() error
}

// ConnectorFactory opens the configured downstream transport only after a
// reference, context, schema, readiness, and policy have been validated.
type ConnectorFactory interface {
	Open(ctx context.Context, server capability.ServerVersion) (Connector, error)
}

// CallResult attributes a downstream result to the real server and tool.
type CallResult struct {
	Server     capability.ServerVersion `json:"server"`
	Capability struct {
		Kind capability.CapabilityKind `json:"kind"`
		Name string                    `json:"name"`
	} `json:"capability"`
	Result any `json:"result"`
}

// Broker coordinates bounded discovery and lazy downstream invocation.
type Broker struct {
	catalog    Catalog
	signer     *capability.ReferenceSigner
	policy     Policy
	connectors ConnectorFactory
}

// New constructs a broker from protocol-neutral core interfaces.
func New(catalog Catalog, signer *capability.ReferenceSigner, policy Policy, connectors ConnectorFactory) (*Broker, error) {
	if catalog == nil || signer == nil || policy == nil || connectors == nil {
		return nil, errors.New("broker catalog, signer, policy, and connector factory are required")
	}
	return &Broker{catalog: catalog, signer: signer, policy: policy, connectors: connectors}, nil
}

// Query searches the offline catalog and issues short-lived references. It does
// not open a downstream connection.
func (b *Broker) Query(ctx context.Context, query Query) ([]Summary, error) {
	if query.ContextDigest == "" || query.View == "" {
		return nil, errors.New("query context digest and view are required")
	}
	capabilities, err := b.catalog.Search(ctx, query)
	if err != nil {
		return nil, err
	}
	if query.Limit > 0 && len(capabilities) > query.Limit {
		capabilities = capabilities[:query.Limit]
	}

	results := make([]Summary, 0, len(capabilities))
	for _, selected := range capabilities {
		selected, err = selected.WithComputedSchemaDigest()
		if err != nil {
			return nil, fmt.Errorf("preparing capability %s: %w", selected.Name, err)
		}
		ref, err := b.signer.Issue(selected, query.View, query.ContextDigest)
		if err != nil {
			return nil, fmt.Errorf("issuing capability reference: %w", err)
		}
		results = append(results, Summary{
			Ref:          ref,
			Server:       selected.Server.Identity,
			Version:      selected.Server.Version,
			Kind:         selected.Kind,
			Name:         selected.Name,
			Title:        selected.Title,
			Description:  selected.Description,
			Availability: selected.Availability,
		})
	}
	return results, nil
}

// Describe revalidates and resolves one selected capability without connecting
// to its downstream server.
func (b *Broker) Describe(ctx context.Context, ref string, request RequestContext) (capability.Capability, error) {
	claims, err := b.signer.Verify(ref)
	if err != nil {
		return capability.Capability{}, err
	}
	if claims.ContextDigest != request.ContextDigest {
		return capability.Capability{}, ErrContextMismatch
	}
	if claims.View != request.View {
		return capability.Capability{}, ErrViewMismatch
	}
	selected, err := b.catalog.Resolve(ctx, claims)
	if err != nil {
		return capability.Capability{}, err
	}
	selected, err = selected.WithComputedSchemaDigest()
	if err != nil {
		return capability.Capability{}, err
	}
	if claims.Server != selected.Server.Identity.CanonicalName ||
		claims.Version != selected.Server.Version ||
		claims.Kind != selected.Kind ||
		claims.Capability != selected.Name ||
		claims.SchemaDigest != selected.SchemaDigest {
		return capability.Capability{}, ErrCapabilityChanged
	}
	return selected, nil
}

// Call validates a selected capability and invokes only its downstream server.
func (b *Broker) Call(ctx context.Context, ref string, arguments map[string]any, request RequestContext) (CallResult, error) {
	selected, err := b.Describe(ctx, ref, request)
	if err != nil {
		return CallResult{}, err
	}
	if selected.Availability != capability.AvailabilityReady {
		return CallResult{}, fmt.Errorf("%w: %s", ErrCapabilityNotReady, selected.Availability)
	}
	effect, err := b.policy.Evaluate(ctx, selected, arguments)
	if err != nil {
		return CallResult{}, err
	}
	switch effect {
	case PolicyAllow:
	case PolicyDeny:
		return CallResult{}, ErrPolicyDenied
	case PolicyApprovalRequired:
		return CallResult{}, ErrApprovalRequired
	default:
		return CallResult{}, fmt.Errorf("unknown policy effect %q", effect)
	}

	connector, err := b.connectors.Open(ctx, selected.Server)
	if err != nil {
		return CallResult{}, fmt.Errorf("opening downstream MCP server %s: %w", selected.Server.Identity.CanonicalName, err)
	}
	defer connector.Close()
	result, err := connector.CallTool(ctx, selected.Name, arguments)
	if err != nil {
		return CallResult{}, fmt.Errorf("calling %s on %s: %w", selected.Name, selected.Server.Identity.CanonicalName, err)
	}

	attributed := CallResult{Server: selected.Server, Result: result}
	attributed.Capability.Kind = selected.Kind
	attributed.Capability.Name = selected.Name
	return attributed, nil
}
