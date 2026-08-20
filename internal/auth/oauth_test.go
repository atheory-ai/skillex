package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestPrivateKeyJWTClientAuthentication(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: mustPKCS8(t, key)})
	var form url.Values
	client := &http.Client{Transport: roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(request.Body)
		form, _ = url.ParseQuery(string(body))
		return response(http.StatusOK, `{"access_token":"access","token_type":"Bearer","expires_in":300}`), nil
	})}
	_, err = AcquireClientCredentials(context.Background(), client, ClientCredentialsRequest{
		Metadata: Metadata{Resource: "https://mcp.example", TokenEndpoint: "https://auth.example/token"},
		ClientID: "service-client", AuthMethod: "private_key_jwt", PrivateKeyPEM: keyPEM, KeyID: "workload-key",
	})
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(form.Get("client_assertion"), ".")
	if len(parts) != 3 || form.Get("client_assertion_type") != "urn:ietf:params:oauth:client-assertion-type:jwt-bearer" {
		t.Fatalf("private_key_jwt form = %#v", form)
	}
	headerBytes, _ := base64.RawURLEncoding.DecodeString(parts[0])
	claimsBytes, _ := base64.RawURLEncoding.DecodeString(parts[1])
	var header, claims map[string]any
	_ = json.Unmarshal(headerBytes, &header)
	_ = json.Unmarshal(claimsBytes, &claims)
	if header["alg"] != "RS256" || header["kid"] != "workload-key" || claims["aud"] != "https://auth.example/token" || claims["iss"] != "service-client" {
		t.Fatalf("header/claims = %#v %#v", header, claims)
	}
}

func TestWorkloadTokenExchangeIsResourceBound(t *testing.T) {
	var form url.Values
	client := &http.Client{Transport: roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(request.Body)
		form, _ = url.ParseQuery(string(body))
		return response(http.StatusOK, `{"access_token":"workload-access","token_type":"Bearer","expires_in":300}`), nil
	})}
	_, err := AcquireTokenExchange(context.Background(), client, TokenExchangeRequest{
		Metadata:     Metadata{Resource: "https://mcp.example", TokenEndpoint: "https://auth.example/token"},
		SubjectToken: "oidc-token", SubjectTokenType: "urn:ietf:params:oauth:token-type:id_token", ClientID: "workload-client",
	})
	if err != nil {
		t.Fatal(err)
	}
	if form.Get("resource") != "https://mcp.example" || form.Get("subject_token") != "oidc-token" || form.Get("requested_token_type") != "urn:ietf:params:oauth:token-type:access_token" {
		t.Fatalf("workload exchange form = %#v", form)
	}
}

func mustPKCS8(t *testing.T, key *rsa.PrivateKey) []byte {
	t.Helper()
	encoded, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func TestDiscoverAndClientCredentialsAreResourceBound(t *testing.T) {
	var tokenForm url.Values
	transport := roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		switch request.URL.String() {
		case "https://mcp.example/.well-known/oauth-protected-resource":
			return response(http.StatusOK, `{"resource":"https://mcp.example","authorization_servers":["https://auth.example/tenant"],"scopes_supported":["issues:write"]}`), nil
		case "https://auth.example/.well-known/oauth-authorization-server/tenant":
			return response(http.StatusOK, `{"issuer":"https://auth.example/tenant","token_endpoint":"https://auth.example/token","code_challenge_methods_supported":["S256"]}`), nil
		case "https://auth.example/token":
			body, _ := io.ReadAll(request.Body)
			tokenForm, _ = url.ParseQuery(string(body))
			return response(http.StatusOK, `{"access_token":"bound-token","token_type":"Bearer","expires_in":300,"scope":"issues:write"}`), nil
		default:
			t.Fatalf("unexpected URL %s", request.URL)
			return nil, nil
		}
	})
	client := &http.Client{Transport: transport}
	metadata, err := Discover(context.Background(), client, DiscoveryOptions{
		ProtectedResourceMetadataURL: "https://mcp.example/.well-known/oauth-protected-resource",
		ExpectedResource:             "https://mcp.example",
	})
	if err != nil {
		t.Fatal(err)
	}
	token, err := AcquireClientCredentials(context.Background(), client, ClientCredentialsRequest{
		Metadata: metadata, ClientID: "client", ClientSecret: "secret", Scopes: []string{"issues:write"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if token.AuthorizationHeader() != "Bearer bound-token" || tokenForm.Get("resource") != "https://mcp.example" || tokenForm.Get("grant_type") != "client_credentials" {
		t.Fatalf("token/form = %q %#v", token.AuthorizationHeader(), tokenForm)
	}
}

func TestEnterpriseManagedPerformsIDJAGThenJWTGrant(t *testing.T) {
	var forms []url.Values
	transport := roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(request.Body)
		form, _ := url.ParseQuery(string(body))
		forms = append(forms, form)
		if request.URL.String() == "https://idp.example/token" {
			return response(http.StatusOK, `{"access_token":"id-jag","token_type":"N_A","issued_token_type":"urn:ietf:params:oauth:token-type:id-jag","expires_in":60}`), nil
		}
		return response(http.StatusOK, `{"access_token":"mcp-access","token_type":"Bearer","expires_in":300}`), nil
	})
	metadata := Metadata{
		Resource: "https://mcp.example", AuthorizationServer: "https://auth.example",
		TokenEndpoint:          "https://auth.example/token",
		GrantProfilesSupported: []string{"urn:ietf:params:oauth:grant-profile:id-jag"},
	}
	token, err := AcquireEnterpriseManaged(context.Background(), &http.Client{Transport: transport}, EnterpriseManagedRequest{
		Metadata: metadata, IDPTokenEndpoint: "https://idp.example/token", IdentityAssertion: "id-token",
		IDPClientID: "client", IDPClientSecret: "secret", ResourceClientID: "https://client.example/client.json",
	})
	if err != nil {
		t.Fatal(err)
	}
	if token.AuthorizationHeader() != "Bearer mcp-access" || len(forms) != 2 {
		t.Fatalf("token/forms = %q %#v", token.AuthorizationHeader(), forms)
	}
	if forms[0].Get("requested_token_type") != "urn:ietf:params:oauth:token-type:id-jag" || forms[0].Get("audience") != "https://auth.example" || forms[0].Get("resource") != "https://mcp.example" {
		t.Fatalf("ID-JAG exchange form = %#v", forms[0])
	}
	if forms[1].Get("grant_type") != "urn:ietf:params:oauth:grant-type:jwt-bearer" || forms[1].Get("assertion") != "id-jag" || forms[1].Get("resource") != "https://mcp.example" {
		t.Fatalf("resource grant form = %#v", forms[1])
	}
}

func TestTokenErrorsNeverIncludeCredentialValues(t *testing.T) {
	client := &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
		return response(http.StatusUnauthorized, `{"error_description":"reflected super-secret"}`), nil
	})}
	_, err := AcquireClientCredentials(context.Background(), client, ClientCredentialsRequest{
		Metadata: Metadata{Resource: "https://mcp.example", TokenEndpoint: "https://auth.example/token"},
		ClientID: "client", ClientSecret: "super-secret",
	})
	if err == nil || strings.Contains(err.Error(), "super-secret") {
		t.Fatalf("unsafe token error = %v", err)
	}
}

func response(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }
