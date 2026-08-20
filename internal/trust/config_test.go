package trust

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/atheory-ai/skillex/internal/capability"
)

func TestStdioConfigResolvesOnlyMappedEnvironmentKey(t *testing.T) {
	root := t.TempDir()
	t.Setenv("MAPPED_TOKEN", "mapped-value")
	t.Setenv("UNMAPPED_SECRET", "must-not-be-inherited")
	cfg := testConfig(root, []CredentialSource{{Env: &EnvSource{Key: "MAPPED_TOKEN"}}})
	selected := testCapability("profile")

	got, err := cfg.StdioConfig(selected, root)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Environment, []string{"DOWNSTREAM_TOKEN=mapped-value"}) {
		t.Fatalf("environment = %#v", got.Environment)
	}
	for _, entry := range got.Environment {
		if strings.Contains(entry, "UNMAPPED_SECRET") || strings.Contains(entry, "must-not-be-inherited") {
			t.Fatalf("unmapped secret leaked: %q", entry)
		}
	}
}

func TestCredentialHelperReceivesNoParentEnvironmentAndReturnsOnlyStdout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is Unix-only")
	}
	root := t.TempDir()
	helper := filepath.Join(root, "credential-helper")
	if err := os.WriteFile(helper, []byte("#!/bin/sh\nif [ -n \"$UNMAPPED_SECRET\" ]; then exit 7; fi\nprintf helper-value\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("UNMAPPED_SECRET", "must-not-reach-helper")
	cfg := testConfig(root, []CredentialSource{{Helper: &HelperSource{Command: helper}}})
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	configured, err := cfg.StdioConfig(testCapability("profile"), root)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(configured.Environment, []string{"DOWNSTREAM_TOKEN=helper-value"}) {
		t.Fatalf("helper environment = %#v", configured.Environment)
	}
}

func TestMTLSCredentialPresenceAffectsReadiness(t *testing.T) {
	root := t.TempDir()
	cfg := &Config{
		Version: ConfigVersion,
		Servers: []Server{{Server: "io.example/service", Version: "1.0.0", AllowedProjects: []string{root}, HTTP: &HTTPConfig{
			Endpoint: "https://mcp.example/mcp", MTLS: &MTLSConfig{
				CertificateSources: []CredentialSource{{Env: &EnvSource{Key: "MISSING_CERT"}}},
				PrivateKeySources:  []CredentialSource{{Env: &EnvSource{Key: "MISSING_KEY"}}},
			},
		}}},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	if got := cfg.Ready(testCapability(""), root); got != capability.AvailabilityCredentialMissing {
		t.Fatalf("mTLS readiness = %s", got)
	}
}

func TestAuthorizationCodeProfileTransitionsFromLoginRequiredToReady(t *testing.T) {
	root := t.TempDir()
	storeRoot := t.TempDir()
	transport := roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		body := ""
		switch request.URL.String() {
		case "https://mcp.example/.well-known/oauth-protected-resource":
			body = `{"resource":"https://mcp.example","authorization_servers":["https://auth.example"]}`
		case "https://auth.example/.well-known/oauth-authorization-server":
			body = `{"issuer":"https://auth.example","authorization_endpoint":"https://auth.example/authorize","token_endpoint":"https://auth.example/token","code_challenge_methods_supported":["S256"],"authorization_response_iss_parameter_supported":true}`
		case "https://client.example/client.json":
			body = `{"client_id":"https://client.example/client.json","redirect_uris":["http://127.0.0.1/callback"],"grant_types":["authorization_code"],"response_types":["code"],"token_endpoint_auth_method":"none"}`
		case "https://auth.example/token":
			body = `{"access_token":"user-access","refresh_token":"refresh","token_type":"Bearer","expires_in":300}`
		default:
			t.Fatalf("unexpected URL %s", request.URL)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	cfg := &Config{
		Version:    ConfigVersion,
		OAuthStore: &OAuthStoreConfig{KeyPath: filepath.Join(storeRoot, "key"), Directory: filepath.Join(storeRoot, "tokens")},
		Servers:    []Server{{Server: "io.example/service", Version: "1.0.0", AllowedProjects: []string{root}, AuthProfiles: []string{"personal"}, HTTP: &HTTPConfig{Endpoint: "https://mcp.example/mcp"}}},
		CredentialProfiles: []CredentialProfile{{Name: "personal", Service: "io.example/service", OAuth: &OAuthProfile{
			Type: "authorization-code", ProtectedResourceMetadataURL: "https://mcp.example/.well-known/oauth-protected-resource",
			Resource: "https://mcp.example", ClientID: "https://client.example/client.json", RedirectURI: "http://127.0.0.1/callback",
		}}},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	cfg.SetHTTPClient(&http.Client{Transport: transport})
	selected := testCapability("personal")
	if got := cfg.Ready(selected, root); got != capability.AvailabilityLoginRequired {
		t.Fatalf("initial readiness = %s", got)
	}
	loginURL, err := cfg.StartOAuthLogin(context.Background(), "personal")
	if err != nil {
		t.Fatal(err)
	}
	parsed, _ := url.Parse(loginURL)
	callback := "http://127.0.0.1/callback?code=code&state=" + url.QueryEscape(parsed.Query().Get("state")) + "&iss=" + url.QueryEscape("https://auth.example")
	if err := cfg.CompleteOAuthLogin(context.Background(), "personal", callback); err != nil {
		t.Fatal(err)
	}
	if got := cfg.Ready(selected, root); got != capability.AvailabilityReady {
		t.Fatalf("completed readiness = %s", got)
	}
	configured, err := cfg.StreamableHTTPConfig(context.Background(), selected, root)
	if err != nil || configured.Headers["Authorization"] != "Bearer user-access" {
		t.Fatalf("configured = %#v, %v", configured, err)
	}
}

func TestStreamableHTTPConfigAcquiresAndCachesResourceBoundClientCredential(t *testing.T) {
	root := t.TempDir()
	t.Setenv("OAUTH_CLIENT_SECRET", "exact-secret")
	tokenRequests := 0
	transport := roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		var body string
		switch request.URL.String() {
		case "https://mcp.example/.well-known/oauth-protected-resource":
			body = `{"resource":"https://mcp.example","authorization_servers":["https://auth.example"]}`
		case "https://auth.example/.well-known/oauth-authorization-server":
			body = `{"issuer":"https://auth.example","token_endpoint":"https://auth.example/token"}`
		case "https://auth.example/token":
			tokenRequests++
			encoded, _ := io.ReadAll(request.Body)
			if !strings.Contains(string(encoded), "resource=https%3A%2F%2Fmcp.example") || !strings.Contains(string(encoded), "client_secret=exact-secret") {
				t.Errorf("token request = %s", encoded)
			}
			body = `{"access_token":"access","token_type":"Bearer","expires_in":300}`
		default:
			t.Fatalf("unexpected auth URL %s", request.URL)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	cfg := &Config{
		Version: ConfigVersion,
		Servers: []Server{{Server: "io.example/service", Version: "1.0.0", AllowedProjects: []string{root}, AuthProfiles: []string{"oauth"}, HTTP: &HTTPConfig{Endpoint: "https://mcp.example/mcp"}}},
		CredentialProfiles: []CredentialProfile{{
			Name: "oauth", Service: "io.example/service",
			OAuth: &OAuthProfile{
				Type: "client-credentials", ProtectedResourceMetadataURL: "https://mcp.example/.well-known/oauth-protected-resource",
				Resource: "https://mcp.example", ClientID: "client",
				ClientSecretSources: []CredentialSource{{Env: &EnvSource{Key: "OAUTH_CLIENT_SECRET"}}},
			},
		}},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	cfg.SetHTTPClient(&http.Client{Transport: transport})
	selected := testCapability("oauth")
	for range 2 {
		configured, err := cfg.StreamableHTTPConfig(context.Background(), selected, root)
		if err != nil {
			t.Fatal(err)
		}
		if configured.Headers["Authorization"] != "Bearer access" {
			t.Fatalf("authorization header = %q", configured.Headers["Authorization"])
		}
	}
	if tokenRequests != 1 {
		t.Fatalf("token requests = %d, want cached single request", tokenRequests)
	}
}

func TestStdioConfigReadsExactDotenvKeyAndConfinesProjectTemplate(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".env.mcp"), []byte("OTHER=hidden\nMAPPED=selected\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := testConfig(root, []CredentialSource{{Dotenv: &DotenvSource{Path: "${projectRoot}/.env.mcp", Key: "MAPPED"}}})
	got, err := cfg.StdioConfig(testCapability("profile"), root)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Environment, []string{"DOWNSTREAM_TOKEN=selected"}) {
		t.Fatalf("environment = %#v", got.Environment)
	}

	outside := filepath.Join(t.TempDir(), "outside.env")
	if err := os.WriteFile(outside, []byte("MAPPED=escaped\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape.env")); err != nil {
		t.Fatal(err)
	}
	cfg.CredentialProfiles[0].Credentials[0].Sources[0].Dotenv.Path = "${projectRoot}/escape.env"
	_, err = cfg.StdioConfig(testCapability("profile"), root)
	if err == nil || !strings.Contains(err.Error(), "outside project root") {
		t.Fatalf("symlink escape error = %v", err)
	}
}

func TestReadinessDistinguishesMissingCredentialAndPolicy(t *testing.T) {
	root := t.TempDir()
	cfg := testConfig(root, []CredentialSource{{Env: &EnvSource{Key: "ABSENT_TEST_TOKEN"}}})
	if got := cfg.Ready(testCapability("profile"), root); got != capability.AvailabilityCredentialMissing {
		t.Fatalf("missing credential availability = %s", got)
	}
	if got := cfg.Ready(testCapability("other-profile"), root); got != capability.AvailabilityPolicyDenied {
		t.Fatalf("untrusted profile availability = %s", got)
	}
	if got := cfg.Ready(testCapability("profile"), t.TempDir()); got != capability.AvailabilitySetupRequired {
		t.Fatalf("unapproved project availability = %s", got)
	}
	_, err := cfg.StdioConfig(testCapability("profile"), t.TempDir())
	if !errors.Is(err, ErrProjectUntrusted) {
		t.Fatalf("unapproved project error = %v", err)
	}
}

func testConfig(root string, sources []CredentialSource) *Config {
	return &Config{
		Version: ConfigVersion,
		Servers: []Server{{
			Server: "io.example/service", Version: "1.0.0", AllowedProjects: []string{root},
			AuthProfiles: []string{"profile"}, Stdio: &StdioConfig{Command: "/bin/echo"},
		}},
		CredentialProfiles: []CredentialProfile{{
			Name: "profile", Service: "io.example/service",
			Credentials: []Credential{{
				Slot: "token", Sources: sources, Inject: Injection{StdioEnv: "DOWNSTREAM_TOKEN"},
			}},
		}},
	}
}

func testCapability(profile string) capability.Capability {
	return capability.Capability{
		Server: capability.ServerVersion{Identity: capability.ServerIdentity{CanonicalName: "io.example/service"}, Version: "1.0.0"},
		Kind:   capability.CapabilityTool, Name: "tool", AuthProfile: profile,
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }
