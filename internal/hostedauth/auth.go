// Package hostedauth provides inbound authentication primitives for a hosted
// Skillex broker. It deliberately stops at verified principals; tenant policy,
// reference signing, catalogs, credentials, and telemetry consume that identity
// through separate partitioned services.
package hostedauth

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/atheory-ai/skillex/internal/tenant"
)

var ErrUnauthenticated = errors.New("hosted MCP request is unauthenticated")

type Authenticator interface {
	Authenticate(context.Context, *http.Request) (tenant.Principal, error)
}

type JWTConfig struct {
	Issuer      string
	Audience    string
	TenantClaim string
	KindClaim   string
	Keys        map[string]*rsa.PublicKey
	Clock       func() time.Time
}

// JWTAuthenticator verifies resource-bound RS256 bearer tokens, including
// IAP identity tokens when configured with the expected Google issuer/audience.
type JWTAuthenticator struct{ Config JWTConfig }

func NewJWTAuthenticator(config JWTConfig, publicKeys map[string][]byte) (*JWTAuthenticator, error) {
	if config.Issuer == "" || config.Audience == "" || config.TenantClaim == "" || len(publicKeys) == 0 {
		return nil, errors.New("hosted JWT issuer, audience, tenant claim, and keys are required")
	}
	config.Keys = map[string]*rsa.PublicKey{}
	for kid, encoded := range publicKeys {
		block, _ := pem.Decode(encoded)
		if kid == "" || block == nil {
			return nil, errors.New("hosted JWT key is invalid")
		}
		parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
		if err != nil {
			return nil, errors.New("hosted JWT key is invalid")
		}
		key, ok := parsed.(*rsa.PublicKey)
		if !ok {
			return nil, errors.New("hosted JWT key must be RSA")
		}
		config.Keys[kid] = key
	}
	if config.Clock == nil {
		config.Clock = time.Now
	}
	return &JWTAuthenticator{Config: config}, nil
}

func (a *JWTAuthenticator) Authenticate(_ context.Context, request *http.Request) (tenant.Principal, error) {
	header := request.Header.Get("Authorization")
	if !strings.HasPrefix(header, "Bearer ") {
		return tenant.Principal{}, ErrUnauthenticated
	}
	parts := strings.Split(strings.TrimPrefix(header, "Bearer "), ".")
	if len(parts) != 3 {
		return tenant.Principal{}, ErrUnauthenticated
	}
	var jwtHeader struct {
		Algorithm string `json:"alg"`
		KeyID     string `json:"kid"`
	}
	if !decodeJWTPart(parts[0], &jwtHeader) || jwtHeader.Algorithm != "RS256" {
		return tenant.Principal{}, ErrUnauthenticated
	}
	key := a.Config.Keys[jwtHeader.KeyID]
	if key == nil {
		return tenant.Principal{}, ErrUnauthenticated
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return tenant.Principal{}, ErrUnauthenticated
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if rsa.VerifyPKCS1v15(key, crypto.SHA256, digest[:], signature) != nil {
		return tenant.Principal{}, ErrUnauthenticated
	}
	var claims map[string]any
	if !decodeJWTPart(parts[1], &claims) {
		return tenant.Principal{}, ErrUnauthenticated
	}
	now := a.Config.Clock().Unix()
	issuer, _ := claims["iss"].(string)
	subject, _ := claims["sub"].(string)
	tenantID, _ := claims[a.Config.TenantClaim].(string)
	if issuer != a.Config.Issuer || subject == "" || tenantID == "" || !audienceContains(claims["aud"], a.Config.Audience) || numberClaim(claims["exp"]) <= now || numberClaim(claims["nbf"]) > now {
		return tenant.Principal{}, ErrUnauthenticated
	}
	kind := tenant.PrincipalUser
	if a.Config.KindClaim != "" {
		if value, _ := claims[a.Config.KindClaim].(string); value == string(tenant.PrincipalWorkload) {
			kind = tenant.PrincipalWorkload
		}
	}
	principal := tenant.Principal{TenantID: tenantID, Subject: subject, Kind: kind}
	if err := principal.Validate(); err != nil {
		return tenant.Principal{}, ErrUnauthenticated
	}
	return principal, nil
}

// MTLSAuthenticator maps exact certificate fingerprints to principals. It
// never trusts an arbitrary subject or SAN asserted by an unknown certificate.
type MTLSAuthenticator struct{ Principals map[string]tenant.Principal }

func (a MTLSAuthenticator) Authenticate(_ context.Context, request *http.Request) (tenant.Principal, error) {
	if request.TLS == nil || len(request.TLS.VerifiedChains) == 0 || len(request.TLS.PeerCertificates) == 0 {
		return tenant.Principal{}, ErrUnauthenticated
	}
	digest := sha256.Sum256(request.TLS.PeerCertificates[0].Raw)
	principal, ok := a.Principals[hex.EncodeToString(digest[:])]
	if !ok || principal.Validate() != nil {
		return tenant.Principal{}, ErrUnauthenticated
	}
	return principal, nil
}

func decodeJWTPart(value string, target any) bool {
	data, err := base64.RawURLEncoding.DecodeString(value)
	return err == nil && json.Unmarshal(data, target) == nil
}

func audienceContains(value any, expected string) bool {
	switch audience := value.(type) {
	case string:
		return audience == expected
	case []any:
		for _, item := range audience {
			if item == expected {
				return true
			}
		}
	}
	return false
}

func numberClaim(value any) int64 {
	if value == nil {
		return 0
	}
	if number, ok := value.(float64); ok {
		return int64(number)
	}
	return 0
}
