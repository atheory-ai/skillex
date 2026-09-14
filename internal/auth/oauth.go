// Package auth implements outbound OAuth credential acquisition for trusted
// Streamable HTTP MCP servers. It keeps token values behind an injection-only
// handle and refuses redirects or non-TLS non-loopback endpoints.
package auth

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
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const maxAuthResponseBytes = 1 << 20

var (
	ErrDiscoveryInvalid = errors.New("OAuth discovery metadata is invalid")
	ErrTokenExchange    = errors.New("OAuth token exchange failed")
	ErrScopeRequired    = errors.New("OAuth token does not include required scopes")
)

type Metadata struct {
	Resource                    string
	AuthorizationServer         string
	AuthorizationEndpoint       string
	TokenEndpoint               string
	ScopesSupported             []string
	CodeChallengeMethods        []string
	GrantProfilesSupported      []string
	ClientMetadataDocument      bool
	DynamicRegistrationEndpoint string
	AuthorizationResponseIssuer bool
}

type Token struct {
	value     string
	expiresAt time.Time
	scopes    string
	refresh   string
}

func (t Token) AuthorizationHeader() string { return "Bearer " + t.value }
func (t Token) ExpiresAt() time.Time        { return t.expiresAt }
func (t Token) Scopes() string              { return t.scopes }
func (t Token) ValidFor(duration time.Duration) bool {
	return t.value != "" && time.Until(t.expiresAt) > duration
}
func (t Token) CanRefresh() bool { return t.refresh != "" }

type DiscoveryOptions struct {
	ProtectedResourceMetadataURL string
	ExpectedResource             string
	ExpectedIssuer               string
}

func Discover(ctx context.Context, client *http.Client, options DiscoveryOptions) (Metadata, error) {
	client = secureClient(client)
	if err := validateHTTPSURL(options.ProtectedResourceMetadataURL); err != nil {
		return Metadata{}, err
	}
	var resource struct {
		Resource             string   `json:"resource"`
		AuthorizationServers []string `json:"authorization_servers"`
		ScopesSupported      []string `json:"scopes_supported"`
	}
	if err := getJSON(ctx, client, options.ProtectedResourceMetadataURL, &resource); err != nil {
		return Metadata{}, err
	}
	if resource.Resource == "" || len(resource.AuthorizationServers) == 0 || options.ExpectedResource != "" && resource.Resource != options.ExpectedResource {
		return Metadata{}, ErrDiscoveryInvalid
	}
	issuer := resource.AuthorizationServers[0]
	if options.ExpectedIssuer != "" {
		found := false
		for _, candidate := range resource.AuthorizationServers {
			if candidate == options.ExpectedIssuer {
				issuer, found = candidate, true
				break
			}
		}
		if !found {
			return Metadata{}, ErrDiscoveryInvalid
		}
	}
	if err := validateHTTPSURL(issuer); err != nil {
		return Metadata{}, err
	}
	var server struct {
		Issuer                               string   `json:"issuer"`
		AuthorizationEndpoint                string   `json:"authorization_endpoint"`
		TokenEndpoint                        string   `json:"token_endpoint"`
		ScopesSupported                      []string `json:"scopes_supported"`
		CodeChallengeMethodsSupported        []string `json:"code_challenge_methods_supported"`
		AuthorizationGrantProfilesSupported  []string `json:"authorization_grant_profiles_supported"`
		ClientIDMetadataDocumentSupported    bool     `json:"client_id_metadata_document_supported"`
		RegistrationEndpoint                 string   `json:"registration_endpoint"`
		AuthorizationResponseIssuerSupported bool     `json:"authorization_response_iss_parameter_supported"`
	}
	metadataURL, err := authorizationMetadataURL(issuer)
	if err != nil {
		return Metadata{}, err
	}
	if err := getJSON(ctx, client, metadataURL, &server); err != nil {
		// MCP clients must support OIDC discovery as a fallback.
		oidcURL := strings.TrimRight(issuer, "/") + "/.well-known/openid-configuration"
		if oidcErr := getJSON(ctx, client, oidcURL, &server); oidcErr != nil {
			return Metadata{}, err
		}
	}
	if server.Issuer != issuer || server.TokenEndpoint == "" {
		return Metadata{}, ErrDiscoveryInvalid
	}
	if err := validateHTTPSURL(server.TokenEndpoint); err != nil {
		return Metadata{}, err
	}
	return Metadata{
		Resource: resource.Resource, AuthorizationServer: issuer,
		AuthorizationEndpoint: server.AuthorizationEndpoint, TokenEndpoint: server.TokenEndpoint,
		ScopesSupported:             chooseScopes(resource.ScopesSupported, server.ScopesSupported),
		CodeChallengeMethods:        server.CodeChallengeMethodsSupported,
		GrantProfilesSupported:      server.AuthorizationGrantProfilesSupported,
		ClientMetadataDocument:      server.ClientIDMetadataDocumentSupported,
		DynamicRegistrationEndpoint: server.RegistrationEndpoint,
		AuthorizationResponseIssuer: server.AuthorizationResponseIssuerSupported,
	}, nil
}

type ClientCredentialsRequest struct {
	Metadata      Metadata
	ClientID      string
	ClientSecret  string
	AuthMethod    string
	Scopes        []string
	PrivateKeyPEM []byte
	KeyID         string
}

func AcquireClientCredentials(ctx context.Context, client *http.Client, request ClientCredentialsRequest) (Token, error) {
	values := url.Values{
		"grant_type": {"client_credentials"},
		"resource":   {request.Metadata.Resource},
	}
	if len(request.Scopes) > 0 {
		values.Set("scope", strings.Join(request.Scopes, " "))
	}
	headers := http.Header{}
	switch request.AuthMethod {
	case "private_key_jwt":
		assertion, err := signedClientAssertion(request.ClientID, request.Metadata.TokenEndpoint, request.PrivateKeyPEM, request.KeyID)
		if err != nil {
			return Token{}, err
		}
		values.Set("client_assertion_type", "urn:ietf:params:oauth:client-assertion-type:jwt-bearer")
		values.Set("client_assertion", assertion)
	case "client_secret_basic":
		headers.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(url.QueryEscape(request.ClientID)+":"+url.QueryEscape(request.ClientSecret))))
	case "client_secret_post", "":
		values.Set("client_id", request.ClientID)
		values.Set("client_secret", request.ClientSecret)
	default:
		return Token{}, errors.New("unsupported client credentials authentication method")
	}
	return tokenRequest(ctx, client, request.Metadata.TokenEndpoint, values, headers)
}

type TokenExchangeRequest struct {
	Metadata         Metadata
	SubjectToken     string
	SubjectTokenType string
	ClientID         string
	Scopes           []string
}

func AcquireTokenExchange(ctx context.Context, client *http.Client, request TokenExchangeRequest) (Token, error) {
	values := url.Values{
		"grant_type":           {"urn:ietf:params:oauth:grant-type:token-exchange"},
		"requested_token_type": {"urn:ietf:params:oauth:token-type:access_token"},
		"subject_token":        {request.SubjectToken}, "subject_token_type": {request.SubjectTokenType},
		"resource": {request.Metadata.Resource},
	}
	if request.ClientID != "" {
		values.Set("client_id", request.ClientID)
	}
	if len(request.Scopes) > 0 {
		values.Set("scope", strings.Join(request.Scopes, " "))
	}
	return tokenRequest(ctx, client, request.Metadata.TokenEndpoint, values, nil)
}

type EnterpriseManagedRequest struct {
	Metadata          Metadata
	IDPTokenEndpoint  string
	IdentityAssertion string
	SubjectTokenType  string
	IDPClientID       string
	IDPClientSecret   string
	ResourceClientID  string
	Scopes            []string
}

func AcquireEnterpriseManaged(ctx context.Context, client *http.Client, request EnterpriseManagedRequest) (Token, error) {
	if !contains(request.Metadata.GrantProfilesSupported, "urn:ietf:params:oauth:grant-profile:id-jag") {
		return Token{}, errors.New("authorization server does not advertise ID-JAG support")
	}
	if err := validateHTTPSURL(request.IDPTokenEndpoint); err != nil {
		return Token{}, err
	}
	subjectType := request.SubjectTokenType
	if subjectType == "" {
		subjectType = "urn:ietf:params:oauth:token-type:id_token"
	}
	exchange := url.Values{
		"grant_type":           {"urn:ietf:params:oauth:grant-type:token-exchange"},
		"requested_token_type": {"urn:ietf:params:oauth:token-type:id-jag"},
		"audience":             {request.Metadata.AuthorizationServer},
		"resource":             {request.Metadata.Resource},
		"subject_token":        {request.IdentityAssertion},
		"subject_token_type":   {subjectType},
	}
	if request.IDPClientID != "" {
		exchange.Set("client_id", request.IDPClientID)
	}
	if request.IDPClientSecret != "" {
		exchange.Set("client_secret", request.IDPClientSecret)
	}
	if len(request.Scopes) > 0 {
		exchange.Set("scope", strings.Join(request.Scopes, " "))
	}
	idJag, err := rawTokenRequest(ctx, client, request.IDPTokenEndpoint, exchange, nil)
	if err != nil {
		return Token{}, err
	}
	if idJag.IssuedTokenType != "urn:ietf:params:oauth:token-type:id-jag" || idJag.AccessToken == "" {
		return Token{}, ErrTokenExchange
	}
	grant := url.Values{
		"grant_type": {"urn:ietf:params:oauth:grant-type:jwt-bearer"},
		"assertion":  {idJag.AccessToken},
		"resource":   {request.Metadata.Resource},
	}
	if request.ResourceClientID != "" {
		grant.Set("client_id", request.ResourceClientID)
	}
	return tokenRequest(ctx, client, request.Metadata.TokenEndpoint, grant, nil)
}

type tokenResponse struct {
	AccessToken     string `json:"access_token"`
	TokenType       string `json:"token_type"`
	ExpiresIn       int64  `json:"expires_in"`
	Scope           string `json:"scope"`
	IssuedTokenType string `json:"issued_token_type"`
	RefreshToken    string `json:"refresh_token"`
}

func tokenRequest(ctx context.Context, client *http.Client, endpoint string, values url.Values, headers http.Header) (Token, error) {
	response, err := rawTokenRequest(ctx, client, endpoint, values, headers)
	if err != nil {
		return Token{}, err
	}
	if response.AccessToken == "" || !strings.EqualFold(response.TokenType, "Bearer") {
		return Token{}, ErrTokenExchange
	}
	if requested := strings.Fields(values.Get("scope")); len(requested) > 0 && response.Scope != "" {
		granted := strings.Fields(response.Scope)
		for _, scope := range requested {
			if !contains(granted, scope) {
				return Token{}, ErrScopeRequired
			}
		}
	}
	expires := time.Now().Add(time.Duration(response.ExpiresIn) * time.Second)
	if response.ExpiresIn <= 0 {
		expires = time.Now().Add(5 * time.Minute)
	}
	return Token{value: response.AccessToken, expiresAt: expires, scopes: response.Scope, refresh: response.RefreshToken}, nil
}

func rawTokenRequest(ctx context.Context, client *http.Client, endpoint string, values url.Values, headers http.Header) (tokenResponse, error) {
	if err := validateHTTPSURL(endpoint); err != nil {
		return tokenResponse{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(values.Encode()))
	if err != nil {
		return tokenResponse{}, err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	for name, values := range headers {
		for _, value := range values {
			request.Header.Add(name, value)
		}
	}
	response, err := secureClient(client).Do(request)
	if err != nil {
		return tokenResponse{}, fmt.Errorf("OAuth token request failed: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return tokenResponse{}, fmt.Errorf("%w: HTTP %d", ErrTokenExchange, response.StatusCode)
	}
	var decoded tokenResponse
	if err := decodeBounded(response.Body, &decoded); err != nil {
		return tokenResponse{}, ErrTokenExchange
	}
	return decoded, nil
}

func getJSON(ctx context.Context, client *http.Client, endpoint string, target any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("OAuth metadata endpoint returned HTTP %d", response.StatusCode)
	}
	return decodeBounded(response.Body, target)
}

func decodeBounded(reader io.Reader, target any) error {
	data, err := io.ReadAll(io.LimitReader(reader, maxAuthResponseBytes+1))
	if err != nil {
		return err
	}
	if len(data) > maxAuthResponseBytes {
		return errors.New("OAuth response exceeds size limit")
	}
	return json.Unmarshal(data, target)
}

func secureClient(source *http.Client) *http.Client {
	created := http.Client{Timeout: 30 * time.Second}
	if source != nil {
		created = *source
	}
	created.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	return &created
}

func authorizationMetadataURL(issuer string) (string, error) {
	parsed, err := url.Parse(issuer)
	if err != nil {
		return "", err
	}
	path := strings.TrimRight(parsed.EscapedPath(), "/")
	parsed.Path = "/.well-known/oauth-authorization-server" + path
	parsed.RawPath = ""
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), nil
}

func validateHTTPSURL(value string) error {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" {
		return ErrDiscoveryInvalid
	}
	if parsed.Scheme != "https" && (parsed.Scheme != "http" || !isLoopback(parsed.Hostname())) {
		return errors.New("OAuth endpoints must use HTTPS (HTTP is allowed only for loopback)")
	}
	return nil
}

func isLoopback(host string) bool {
	host = strings.ToLower(host)
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

func chooseScopes(resource, server []string) []string {
	if len(resource) > 0 {
		return append([]string(nil), resource...)
	}
	return append([]string(nil), server...)
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func signedClientAssertion(clientID, audience string, keyPEM []byte, keyID string) (string, error) {
	block, _ := pem.Decode(keyPEM)
	if block == nil {
		return "", errors.New("private_key_jwt key is invalid")
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		if parsed, legacyErr := x509.ParsePKCS1PrivateKey(block.Bytes); legacyErr == nil {
			key = parsed
		} else {
			return "", errors.New("private_key_jwt key is invalid")
		}
	}
	rsaKey, ok := key.(*rsa.PrivateKey)
	if !ok {
		return "", errors.New("private_key_jwt currently requires an RSA key")
	}
	header := map[string]any{"alg": "RS256", "typ": "JWT"}
	if keyID != "" {
		header["kid"] = keyID
	}
	jti, err := randomURLToken(16)
	if err != nil {
		return "", err
	}
	now := time.Now().Unix()
	claims := map[string]any{"iss": clientID, "sub": clientID, "aud": audience, "iat": now, "exp": now + 300, "jti": jti}
	headerJSON, _ := json.Marshal(header)
	claimsJSON, _ := json.Marshal(claims)
	unsigned := base64.RawURLEncoding.EncodeToString(headerJSON) + "." + base64.RawURLEncoding.EncodeToString(claimsJSON)
	digest := sha256.Sum256([]byte(unsigned))
	signature, err := rsa.SignPKCS1v15(rand.Reader, rsaKey, crypto.SHA256, digest[:])
	if err != nil {
		return "", errors.New("signing private_key_jwt assertion failed")
	}
	return unsigned + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}
