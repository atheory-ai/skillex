// Package tenant defines hosted identity and partition primitives independently
// of any inbound HTTP framework or identity provider.
package tenant

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
)

type PrincipalKind string

const (
	PrincipalUser     PrincipalKind = "user"
	PrincipalWorkload PrincipalKind = "workload"
)

type Principal struct {
	TenantID string
	Subject  string
	Kind     PrincipalKind
}

func (p Principal) Validate() error {
	if p.TenantID == "" || p.Subject == "" || p.Kind != PrincipalUser && p.Kind != PrincipalWorkload {
		return errors.New("authenticated tenant, subject, and principal kind are required")
	}
	return nil
}

// Partition derives a non-reversible cache/reference partition. Raw tenant and
// subject identifiers are not embedded in capability refs or SQLite views.
func Partition(key []byte, principal Principal) (string, error) {
	if len(key) < 32 {
		return "", errors.New("tenant partition key must be at least 32 bytes")
	}
	if err := principal.Validate(); err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(principal.TenantID))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(principal.Kind))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(principal.Subject))
	return "tenant:v1:" + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

// DeriveSigningKey gives each hosted tenant and purpose an independent key so
// a reference minted for one tenant cannot validate in another tenant's signer.
func DeriveSigningKey(master []byte, tenantID, purpose string) ([]byte, error) {
	if len(master) < 32 || tenantID == "" || purpose == "" {
		return nil, errors.New("master key, tenant, and signing purpose are required")
	}
	mac := hmac.New(sha256.New, master)
	_, _ = mac.Write([]byte("skillex-hosted-key:v1"))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(tenantID))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(purpose))
	return mac.Sum(nil), nil
}
