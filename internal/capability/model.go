package capability

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"
)

// CapabilityKind identifies a downstream MCP capability family.
type CapabilityKind string //nolint:revive // Explicit domain name avoids ambiguity at connector boundaries.

const (
	CapabilityTool             CapabilityKind = "tool"
	CapabilityPrompt           CapabilityKind = "prompt"
	CapabilityResourceTemplate CapabilityKind = "resource-template"
	CapabilityServer           CapabilityKind = "server"
)

// AvailabilityStatus describes whether a capability can currently be used.
// It is an evaluated overlay, not a permanent property of the downstream tool.
type AvailabilityStatus string

const (
	AvailabilityDiscovered        AvailabilityStatus = "discovered"
	AvailabilitySuggested         AvailabilityStatus = "suggested"
	AvailabilitySetupRequired     AvailabilityStatus = "setup-required"
	AvailabilityCredentialMissing AvailabilityStatus = "credential-missing"
	AvailabilityLoginRequired     AvailabilityStatus = "login-required"
	AvailabilityScopeRequired     AvailabilityStatus = "scope-required"
	AvailabilityReady             AvailabilityStatus = "ready"
	AvailabilityUnsupportedAuth   AvailabilityStatus = "unsupported-authentication"
	AvailabilityPolicyDenied      AvailabilityStatus = "policy-denied"
	AvailabilityUnreachable       AvailabilityStatus = "unreachable"
	AvailabilityStale             AvailabilityStatus = "stale"
)

// ServerIdentity is the security identity of an MCP server. Display names and
// self-reported server information must not be substituted for it.
type ServerIdentity struct {
	CanonicalName string `json:"canonical_name"`
	Publisher     string `json:"publisher,omitempty"`
}

// ServerVersion binds a canonical server identity to an exact release.
type ServerVersion struct {
	Identity      ServerIdentity `json:"identity"`
	Version       string         `json:"version"`
	PackageDigest string         `json:"package_digest,omitempty"`
	Status        string         `json:"status,omitempty"`
}

// Capability is the protocol-neutral representation indexed and invoked by
// the broker. Transport and SDK-specific types belong in adapters.
type Capability struct {
	Server           ServerVersion      `json:"server"`
	Kind             CapabilityKind     `json:"kind"`
	Name             string             `json:"name"`
	Title            string             `json:"title,omitempty"`
	Description      string             `json:"description,omitempty"`
	InputSchemaJSON  json.RawMessage    `json:"input_schema,omitempty"`
	OutputSchemaJSON json.RawMessage    `json:"output_schema,omitempty"`
	SchemaDigest     string             `json:"schema_digest"`
	Availability     AvailabilityStatus `json:"availability"`
	ObservedAt       time.Time          `json:"observed_at,omitempty"`
	ExpiresAt        time.Time          `json:"expires_at,omitempty"`
	// RoutingScope and AuthProfile are integrity-protected broker routing
	// metadata. They are never serialized as capability definitions.
	RoutingScope string `json:"-"`
	AuthProfile  string `json:"-"`
}

// Validate checks the identity and schema invariants needed before a
// capability can be referenced or invoked.
func (c Capability) Validate() error {
	if c.Server.Identity.CanonicalName == "" {
		return errors.New("capability server canonical name is required")
	}
	if c.Server.Version == "" {
		return errors.New("capability server version is required")
	}
	if c.Kind == "" {
		return errors.New("capability kind is required")
	}
	if c.Name == "" {
		return errors.New("capability name is required")
	}

	digest, err := ComputeSchemaDigest(c.Kind, c.Name, c.InputSchemaJSON, c.OutputSchemaJSON)
	if err != nil {
		return err
	}
	if c.SchemaDigest != "" && c.SchemaDigest != digest {
		return fmt.Errorf("capability schema digest mismatch: have %q, computed %q", c.SchemaDigest, digest)
	}
	return nil
}

// WithComputedSchemaDigest returns a copy with its canonical schema digest set.
func (c Capability) WithComputedSchemaDigest() (Capability, error) {
	digest, err := ComputeSchemaDigest(c.Kind, c.Name, c.InputSchemaJSON, c.OutputSchemaJSON)
	if err != nil {
		return Capability{}, err
	}
	c.SchemaDigest = digest
	if err := c.Validate(); err != nil {
		return Capability{}, err
	}
	return c, nil
}

// ComputeSchemaDigest produces a stable digest over the invocation identity and
// canonicalized input/output schemas. JSON object key order does not affect it.
func ComputeSchemaDigest(kind CapabilityKind, name string, input, output json.RawMessage) (string, error) {
	canonicalInput, err := canonicalJSON(input)
	if err != nil {
		return "", fmt.Errorf("canonicalizing input schema: %w", err)
	}
	canonicalOutput, err := canonicalJSON(output)
	if err != nil {
		return "", fmt.Errorf("canonicalizing output schema: %w", err)
	}

	payload, err := json.Marshal(struct {
		Kind   CapabilityKind  `json:"kind"`
		Name   string          `json:"name"`
		Input  json.RawMessage `json:"input"`
		Output json.RawMessage `json:"output"`
	}{
		Kind:   kind,
		Name:   name,
		Input:  canonicalInput,
		Output: canonicalOutput,
	})
	if err != nil {
		return "", fmt.Errorf("encoding schema digest payload: %w", err)
	}

	sum := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func canonicalJSON(raw json.RawMessage) (json.RawMessage, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return json.RawMessage("null"), nil
	}

	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, errors.New("multiple JSON values")
		}
		return nil, err
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return canonical, nil
}
