package hostedauth

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"testing"
	"time"

	"github.com/atheory-ai/skillex/internal/tenant"
)

func TestJWTAuthenticatorBindsIssuerAudienceTenantAndPrincipalKind(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	public, _ := x509.MarshalPKIXPublicKey(&key.PublicKey)
	now := time.Unix(1_800_000_000, 0)
	authenticator, err := NewJWTAuthenticator(JWTConfig{Issuer: "https://iap.example", Audience: "skillex-service", TenantClaim: "tenant", KindClaim: "principal_kind", Clock: func() time.Time { return now }}, map[string][]byte{
		"iap-key": pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: public}),
	})
	if err != nil {
		t.Fatal(err)
	}
	token := signJWT(t, key, "iap-key", map[string]any{"iss": "https://iap.example", "aud": "skillex-service", "sub": "service-account", "tenant": "company-a", "principal_kind": "workload", "exp": now.Add(time.Minute).Unix()})
	request, _ := http.NewRequest(http.MethodPost, "https://skillex.example/mcp", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	principal, err := authenticator.Authenticate(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if principal != (tenant.Principal{TenantID: "company-a", Subject: "service-account", Kind: tenant.PrincipalWorkload}) {
		t.Fatalf("principal = %#v", principal)
	}

	request.Header.Set("Authorization", "Bearer "+signJWT(t, key, "iap-key", map[string]any{"iss": "https://iap.example", "aud": "different-service", "sub": "service-account", "tenant": "company-a", "exp": now.Add(time.Minute).Unix()}))
	if _, err := authenticator.Authenticate(context.Background(), request); err == nil {
		t.Fatal("wrong-audience identity token was accepted")
	}
}

func signJWT(t *testing.T, key *rsa.PrivateKey, kid string, claims map[string]any) string {
	t.Helper()
	header, _ := json.Marshal(map[string]any{"alg": "RS256", "kid": kid})
	payload, _ := json.Marshal(claims)
	unsigned := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(payload)
	digest := sha256.Sum256([]byte(unsigned))
	signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	return unsigned + "." + base64.RawURLEncoding.EncodeToString(signature)
}
