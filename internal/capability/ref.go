package capability

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const referencePrefix = "mcp-tool:v1:"

var (
	ErrInvalidReference = errors.New("invalid capability reference")
	ErrExpiredReference = errors.New("expired capability reference")
)

// ReferenceClaims are integrity-protected routing claims. They intentionally
// contain neither credentials nor raw principal identifiers.
type ReferenceClaims struct {
	Server        string         `json:"server"`
	Version       string         `json:"version"`
	Capability    string         `json:"capability"`
	Kind          CapabilityKind `json:"kind"`
	SchemaDigest  string         `json:"schema_digest"`
	RoutingScope  string         `json:"routing_scope,omitempty"`
	AuthProfile   string         `json:"auth_profile,omitempty"`
	View          string         `json:"view"`
	ContextDigest string         `json:"context"`
	IssuedAt      int64          `json:"issued_at"`
	ExpiresAt     int64          `json:"expires_at"`
	Nonce         string         `json:"nonce"`
}

// ReferenceSigner issues and validates short-lived opaque capability refs.
// Key persistence and rotation are deployment concerns supplied by the caller.
type ReferenceSigner struct {
	key   []byte
	ttl   time.Duration
	clock func() time.Time
}

// SignerOption customizes a ReferenceSigner.
type SignerOption func(*ReferenceSigner)

// WithClock supplies a clock, primarily for deterministic tests.
func WithClock(clock func() time.Time) SignerOption {
	return func(s *ReferenceSigner) {
		if clock != nil {
			s.clock = clock
		}
	}
}

// NewReferenceSigner constructs an HMAC-SHA256 signer. Local and hosted key
// storage remain outside this protocol-neutral package.
func NewReferenceSigner(key []byte, ttl time.Duration, opts ...SignerOption) (*ReferenceSigner, error) {
	if len(key) < 32 {
		return nil, errors.New("capability reference signing key must be at least 32 bytes")
	}
	if ttl <= 0 {
		return nil, errors.New("capability reference TTL must be positive")
	}
	signer := &ReferenceSigner{
		key:   append([]byte(nil), key...),
		ttl:   ttl,
		clock: time.Now,
	}
	for _, opt := range opts {
		opt(signer)
	}
	return signer, nil
}

// Issue creates a signed reference for one capability in one view and context.
func (s *ReferenceSigner) Issue(capability Capability, view, contextDigest string) (string, error) {
	capability, err := capability.WithComputedSchemaDigest()
	if err != nil {
		return "", err
	}
	if view == "" || contextDigest == "" {
		return "", errors.New("capability reference view and context are required")
	}

	nonceBytes := make([]byte, 16)
	if _, err := rand.Read(nonceBytes); err != nil {
		return "", fmt.Errorf("generating capability reference nonce: %w", err)
	}
	now := s.clock().UTC()
	claims := ReferenceClaims{
		Server:        capability.Server.Identity.CanonicalName,
		Version:       capability.Server.Version,
		Capability:    capability.Name,
		Kind:          capability.Kind,
		SchemaDigest:  capability.SchemaDigest,
		RoutingScope:  capability.RoutingScope,
		AuthProfile:   capability.AuthProfile,
		View:          view,
		ContextDigest: contextDigest,
		IssuedAt:      now.Unix(),
		ExpiresAt:     now.Add(s.ttl).Unix(),
		Nonce:         base64.RawURLEncoding.EncodeToString(nonceBytes),
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", fmt.Errorf("encoding capability reference: %w", err)
	}
	mac := s.sign(payload)
	return referencePrefix + base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(mac), nil
}

// Verify validates a reference's shape, MAC, required claims, and expiry.
func (s *ReferenceSigner) Verify(ref string) (ReferenceClaims, error) {
	if !strings.HasPrefix(ref, referencePrefix) {
		return ReferenceClaims{}, ErrInvalidReference
	}
	parts := strings.Split(strings.TrimPrefix(ref, referencePrefix), ".")
	if len(parts) != 2 {
		return ReferenceClaims{}, ErrInvalidReference
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil || base64.RawURLEncoding.EncodeToString(payload) != parts[0] {
		return ReferenceClaims{}, ErrInvalidReference
	}
	providedMAC, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || base64.RawURLEncoding.EncodeToString(providedMAC) != parts[1] || !hmac.Equal(providedMAC, s.sign(payload)) {
		return ReferenceClaims{}, ErrInvalidReference
	}

	var claims ReferenceClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return ReferenceClaims{}, ErrInvalidReference
	}
	if claims.Server == "" || claims.Version == "" || claims.Capability == "" ||
		claims.Kind == "" || claims.SchemaDigest == "" || claims.View == "" ||
		claims.ContextDigest == "" || claims.IssuedAt <= 0 || claims.ExpiresAt <= claims.IssuedAt ||
		claims.Nonce == "" {
		return ReferenceClaims{}, ErrInvalidReference
	}
	if !s.clock().Before(time.Unix(claims.ExpiresAt, 0)) {
		return ReferenceClaims{}, ErrExpiredReference
	}
	return claims, nil
}

func (s *ReferenceSigner) sign(payload []byte) []byte {
	mac := hmac.New(sha256.New, s.key)
	_, _ = mac.Write(payload)
	return mac.Sum(nil)
}
