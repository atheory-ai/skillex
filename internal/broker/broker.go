// Package broker resolves and invokes contextually selected MCP capabilities.
// It contains no SDK, transport, credential-store, or persistence dependencies;
// those concerns are supplied through adapters.
package broker

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/atheory-ai/skillex/internal/capability"
	"github.com/atheory-ai/skillex/internal/config"
	tooljsonschema "github.com/atheory-ai/skillex/internal/jsonschema"
)

var (
	ErrContextMismatch       = errors.New("capability reference context mismatch")
	ErrViewMismatch          = errors.New("capability reference view mismatch")
	ErrCapabilityChanged     = errors.New("capability changed after reference issuance")
	ErrPolicyDenied          = errors.New("capability invocation denied by policy")
	ErrApprovalRequired      = errors.New("capability invocation requires approval")
	ErrCapabilityNotReady    = errors.New("capability is not ready")
	ErrMCPDisabled           = errors.New("MCP capability brokering is not enabled for this project")
	ErrToolArgumentInvalid   = errors.New("tool arguments do not satisfy the capability input schema")
	ErrCapabilityNotCallable = errors.New("selected capability is discoverable but not directly callable")
)

// Query describes the context and intent used to retrieve capabilities.
type Query struct {
	Path          string
	Search        string
	Server        string
	Kind          capability.CapabilityKind
	Availability  capability.AvailabilityStatus
	ContextDigest string
	View          string
	Limit         int
	Offset        int
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

// Facet is a candidate-scoped capability filter value.
type Facet struct {
	Value string `json:"value"`
	Count int    `json:"count"`
}

// DiscoveryFacets contains narrowing values across the full matched set.
type DiscoveryFacets struct {
	Servers      []Facet
	Kinds        []Facet
	Availability []Facet
}

// CatalogPage is one bounded catalog page plus full-set metadata.
type CatalogPage struct {
	Capabilities []capability.Capability
	MatchCount   int
	Facets       DiscoveryFacets
}

// DiscoveryPage is one bounded signed-summary page plus full-set metadata.
type DiscoveryPage struct {
	Summaries  []Summary
	MatchCount int
	Facets     DiscoveryFacets
}

// RequestContext binds a selected reference to the current workspace/request
// view. It is rechecked at describe and invocation time.
type RequestContext struct {
	ContextDigest string
	View          string
}

// Catalog is the indexed, offline source used for discovery and resolution.
type Catalog interface {
	Search(ctx context.Context, query Query) (CatalogPage, error)
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

type PromptConnector interface {
	GetPrompt(ctx context.Context, name string, arguments map[string]any) (any, error)
}

type ResourceConnector interface {
	ReadResource(ctx context.Context, uri string) (any, error)
}

type ToolRoundConnector interface {
	CallToolRound(ctx context.Context, name string, arguments, inputResponses map[string]any, requestState string) (any, error)
}

type PromptRoundConnector interface {
	GetPromptRound(ctx context.Context, name string, arguments, inputResponses map[string]any, requestState string) (any, error)
}

type ResourceRoundConnector interface {
	ReadResourceRound(ctx context.Context, uri string, inputResponses map[string]any, requestState string) (any, error)
}

// ConnectorFactory opens the configured downstream transport only after a
// reference, context, schema, readiness, and policy have been validated.
type ConnectorFactory interface {
	Open(ctx context.Context, selected capability.Capability) (Connector, error)
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

// UsageEvent is the privacy-safe broker telemetry contract. It intentionally
// excludes arguments, results, paths, credential identities, and header values.
type UsageEvent struct {
	Operation       string
	Server          string
	Version         string
	Kind            capability.CapabilityKind
	Capability      string
	Availability    capability.AvailabilityStatus
	Outcome         string
	Duration        time.Duration
	TenantPartition string
	PrincipalKind   string
}

// Observer receives privacy-safe broker events. Recording failures must never
// change discovery or invocation behavior.
type Observer interface {
	Record(ctx context.Context, event UsageEvent)
}

type Option func(*Broker)

func WithObserver(observer Observer) Option {
	return func(b *Broker) { b.observer = observer }
}

// WithAttribution attaches opaque hosted attribution to telemetry. It must be
// a derived partition, never a raw tenant or subject identifier.
func WithAttribution(tenantPartition, principalKind string) Option {
	return func(b *Broker) {
		b.tenantPartition = tenantPartition
		b.principalKind = principalKind
	}
}

// Broker coordinates bounded discovery and lazy downstream invocation.
type Broker struct {
	catalog         Catalog
	signer          *capability.ReferenceSigner
	policy          Policy
	connectors      ConnectorFactory
	observer        Observer
	tenantPartition string
	principalKind   string
}

// New constructs a broker from protocol-neutral core interfaces.
func New(catalog Catalog, signer *capability.ReferenceSigner, policy Policy, connectors ConnectorFactory, options ...Option) (*Broker, error) {
	if catalog == nil || signer == nil || policy == nil || connectors == nil {
		return nil, errors.New("broker catalog, signer, policy, and connector factory are required")
	}
	created := &Broker{catalog: catalog, signer: signer, policy: policy, connectors: connectors}
	for _, option := range options {
		option(created)
	}
	return created, nil
}

// NewConfigured constructs the project-facing broker only after configuration
// has explicitly enabled MCP capability brokering. Low-level tests and adapters
// may use New directly; application entry points must use this gate.
func NewConfigured(cfg *config.Config, catalog Catalog, signer *capability.ReferenceSigner, policy Policy, connectors ConnectorFactory, options ...Option) (*Broker, error) {
	if cfg == nil || !cfg.MCPEnabled() {
		return nil, ErrMCPDisabled
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid MCP configuration: %w", err)
	}
	return New(catalog, signer, policy, connectors, options...)
}

// Query searches the offline catalog and issues short-lived references. It does
// not open a downstream connection.
func (b *Broker) Query(ctx context.Context, query Query) ([]Summary, error) {
	page, err := b.QueryPage(ctx, query)
	return page.Summaries, err
}

// QueryPage searches one bounded catalog page and preserves full-set metadata.
func (b *Broker) QueryPage(ctx context.Context, query Query) (DiscoveryPage, error) {
	started := time.Now()
	if query.ContextDigest == "" || query.View == "" {
		return DiscoveryPage{}, errors.New("query context digest and view are required")
	}
	page, err := b.catalog.Search(ctx, query)
	if err != nil {
		return DiscoveryPage{}, err
	}
	if page.MatchCount == 0 && len(page.Capabilities) > 0 {
		page.MatchCount = len(page.Capabilities)
	}
	capabilities := page.Capabilities
	if query.Limit > 0 && len(capabilities) > query.Limit {
		capabilities = capabilities[:query.Limit]
	}

	results := make([]Summary, 0, len(capabilities))
	for _, selected := range capabilities {
		selected, err = selected.WithComputedSchemaDigest()
		if err != nil {
			return DiscoveryPage{}, fmt.Errorf("preparing capability %s: %w", selected.Name, err)
		}
		ref, err := b.signer.Issue(selected, query.View, query.ContextDigest)
		if err != nil {
			return DiscoveryPage{}, fmt.Errorf("issuing capability reference: %w", err)
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
		b.record(ctx, UsageEvent{Operation: "discover", Server: selected.Server.Identity.CanonicalName,
			Version: selected.Server.Version, Kind: selected.Kind, Capability: selected.Name,
			Availability: selected.Availability, Outcome: "returned", Duration: time.Since(started)})
	}
	return DiscoveryPage{Summaries: results, MatchCount: page.MatchCount, Facets: page.Facets}, nil
}

// Describe revalidates and resolves one selected capability without connecting
// to its downstream server.
func (b *Broker) Describe(ctx context.Context, ref string, request RequestContext) (capability.Capability, error) {
	started := time.Now()
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
	if claims.RoutingScope != selected.RoutingScope || claims.AuthProfile != selected.AuthProfile {
		return capability.Capability{}, ErrCapabilityChanged
	}
	b.record(ctx, UsageEvent{Operation: "describe", Server: selected.Server.Identity.CanonicalName,
		Version: selected.Server.Version, Kind: selected.Kind, Capability: selected.Name,
		Availability: selected.Availability, Outcome: "success", Duration: time.Since(started)})
	return selected, nil
}

// Call validates a selected capability and invokes only its downstream server.
func (b *Broker) Call(ctx context.Context, ref string, arguments map[string]any, request RequestContext) (CallResult, error) {
	return b.CallWithInput(ctx, ref, arguments, nil, "", request)
}

// CallWithInput retries an MRTR-capable operation with host-supplied responses
// and the byte-exact opaque request state returned by the downstream server.
func (b *Broker) CallWithInput(ctx context.Context, ref string, arguments, inputResponses map[string]any, requestState string, request RequestContext) (CallResult, error) {
	started := time.Now()
	selected, err := b.Describe(ctx, ref, request)
	if err != nil {
		b.record(ctx, UsageEvent{Operation: "call", Outcome: "reference-rejected", Duration: time.Since(started)})
		return CallResult{}, err
	}
	record := func(outcome string) {
		b.record(ctx, UsageEvent{Operation: "call", Server: selected.Server.Identity.CanonicalName,
			Version: selected.Server.Version, Kind: selected.Kind, Capability: selected.Name,
			Availability: selected.Availability, Outcome: outcome, Duration: time.Since(started)})
	}
	if selected.Availability != capability.AvailabilityReady {
		record("not-ready")
		return CallResult{}, fmt.Errorf("%w: %s", ErrCapabilityNotReady, selected.Availability)
	}
	if err := tooljsonschema.Validate(selected.InputSchemaJSON, arguments); err != nil {
		record("arguments-invalid")
		return CallResult{}, ErrToolArgumentInvalid
	}
	effect, err := b.policy.Evaluate(ctx, selected, arguments)
	if err != nil {
		record("policy-error")
		return CallResult{}, err
	}
	switch effect {
	case PolicyAllow:
	case PolicyDeny:
		record("policy-denied")
		return CallResult{}, ErrPolicyDenied
	case PolicyApprovalRequired:
		record("approval-required")
		return CallResult{}, ErrApprovalRequired
	default:
		record("policy-error")
		return CallResult{}, fmt.Errorf("unknown policy effect %q", effect)
	}

	connector, err := b.connectors.Open(ctx, selected)
	if err != nil {
		record("connector-error")
		return CallResult{}, fmt.Errorf("opening downstream MCP server %s: %w", selected.Server.Identity.CanonicalName, err)
	}
	defer connector.Close()
	var result any
	switch selected.Kind {
	case capability.CapabilityTool:
		if round, ok := connector.(ToolRoundConnector); ok {
			result, err = round.CallToolRound(ctx, selected.Name, arguments, inputResponses, requestState)
		} else {
			result, err = connector.CallTool(ctx, selected.Name, arguments)
		}
	case capability.CapabilityPrompt:
		promptConnector, ok := connector.(PromptConnector)
		if !ok {
			err = ErrCapabilityNotCallable
		} else if round, ok := connector.(PromptRoundConnector); ok {
			result, err = round.GetPromptRound(ctx, selected.Name, arguments, inputResponses, requestState)
		} else {
			result, err = promptConnector.GetPrompt(ctx, selected.Name, arguments)
		}
	case capability.CapabilityResourceTemplate:
		resourceConnector, ok := connector.(ResourceConnector)
		uri, uriOK := arguments["uri"].(string)
		if !ok || !uriOK || uri == "" {
			err = ErrCapabilityNotCallable
		} else if round, ok := connector.(ResourceRoundConnector); ok {
			result, err = round.ReadResourceRound(ctx, uri, inputResponses, requestState)
		} else {
			result, err = resourceConnector.ReadResource(ctx, uri)
		}
	default:
		err = ErrCapabilityNotCallable
	}
	if err != nil {
		record("tool-error")
		return CallResult{}, fmt.Errorf("calling %s on %s: %w", selected.Name, selected.Server.Identity.CanonicalName, err)
	}

	attributed := CallResult{Server: selected.Server, Result: result}
	attributed.Capability.Kind = selected.Kind
	attributed.Capability.Name = selected.Name
	record("success")
	return attributed, nil
}

func (b *Broker) record(ctx context.Context, event UsageEvent) {
	if b.observer != nil {
		event.TenantPartition = b.tenantPartition
		event.PrincipalKind = b.principalKind
		b.observer.Record(ctx, event)
	}
}
