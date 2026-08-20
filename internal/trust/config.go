// Package trust loads user- or enterprise-controlled MCP transport, policy,
// and credential-source configuration. Project and package files can select
// these definitions but can never define them.
package trust

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/atheory-ai/skillex/internal/auth"
	"github.com/atheory-ai/skillex/internal/capability"
	"github.com/atheory-ai/skillex/internal/connector/stdio"
	"github.com/atheory-ai/skillex/internal/connector/streamhttp"
	"gopkg.in/yaml.v3"
)

const (
	ConfigVersion = 1
	PathEnv       = "SKILLEX_MCP_TRUST_CONFIG"
	maxDotenvSize = 1 << 20
)

var (
	ErrServerUntrusted   = errors.New("downstream MCP server is not trusted")
	ErrProjectUntrusted  = errors.New("project is not approved for downstream MCP server")
	ErrProfileUntrusted  = errors.New("credential profile is not trusted for downstream MCP server")
	ErrCredentialMissing = errors.New("configured MCP credential is missing")
)

type Config struct {
	Version            int                 `yaml:"Version"`
	Servers            []Server            `yaml:"Servers"`
	CredentialProfiles []CredentialProfile `yaml:"CredentialProfiles,omitempty"`
	Telemetry          *TelemetryConfig    `yaml:"Telemetry,omitempty"`
	CatalogSources     []CatalogSource     `yaml:"CatalogSources,omitempty"`
	OAuthStore         *OAuthStoreConfig   `yaml:"OAuthStore,omitempty"`
	tokenMu            sync.Mutex
	tokens             map[string]auth.Token
	httpClient         *http.Client
}

type OAuthStoreConfig struct {
	KeyPath   string `yaml:"KeyPath"`
	Directory string `yaml:"Directory"`
}

type CatalogSource struct {
	Name              string   `yaml:"Name"`
	Type              string   `yaml:"Type"`
	BaseURL           string   `yaml:"BaseURL"`
	AllowedNamespaces []string `yaml:"AllowedNamespaces,omitempty"`
}

type TelemetryConfig struct {
	Enabled bool   `yaml:"Enabled"`
	Path    string `yaml:"Path,omitempty"`
}

type Server struct {
	Server          string       `yaml:"Server"`
	Version         string       `yaml:"Version"`
	AllowedProjects []string     `yaml:"AllowedProjects"`
	AuthProfiles    []string     `yaml:"AuthProfiles,omitempty"`
	Stdio           *StdioConfig `yaml:"Stdio,omitempty"`
	HTTP            *HTTPConfig  `yaml:"HTTP,omitempty"`
}

type StdioConfig struct {
	Command          string   `yaml:"Command"`
	Args             []string `yaml:"Args,omitempty"`
	Directory        string   `yaml:"Directory,omitempty"`
	StartupTimeoutMS int      `yaml:"StartupTimeoutMS,omitempty"`
}

type HTTPConfig struct {
	Endpoint  string      `yaml:"Endpoint"`
	TimeoutMS int         `yaml:"TimeoutMS,omitempty"`
	MTLS      *MTLSConfig `yaml:"MTLS,omitempty"`
}

type MTLSConfig struct {
	CertificateSources []CredentialSource `yaml:"CertificateSources"`
	PrivateKeySources  []CredentialSource `yaml:"PrivateKeySources"`
	ServerName         string             `yaml:"ServerName,omitempty"`
}

type CredentialProfile struct {
	Name        string        `yaml:"Name"`
	Service     string        `yaml:"Service"`
	Credentials []Credential  `yaml:"Credentials,omitempty"`
	OAuth       *OAuthProfile `yaml:"OAuth,omitempty"`
}

type OAuthProfile struct {
	Type                         string             `yaml:"Type"`
	ProtectedResourceMetadataURL string             `yaml:"ProtectedResourceMetadataURL"`
	Resource                     string             `yaml:"Resource"`
	Issuer                       string             `yaml:"Issuer,omitempty"`
	Scopes                       []string           `yaml:"Scopes,omitempty"`
	ClientID                     string             `yaml:"ClientID,omitempty"`
	ClientName                   string             `yaml:"ClientName,omitempty"`
	AllowDynamicRegistration     bool               `yaml:"AllowDynamicRegistration,omitempty"`
	ClientSecretSources          []CredentialSource `yaml:"ClientSecretSources,omitempty"`
	PrivateKeySources            []CredentialSource `yaml:"PrivateKeySources,omitempty"`
	KeyID                        string             `yaml:"KeyID,omitempty"`
	ClientAuthMethod             string             `yaml:"ClientAuthMethod,omitempty"`
	IDPTokenEndpoint             string             `yaml:"IdPTokenEndpoint,omitempty"`
	IdentityAssertionSources     []CredentialSource `yaml:"IdentityAssertionSources,omitempty"`
	SubjectTokenType             string             `yaml:"SubjectTokenType,omitempty"`
	IDPClientID                  string             `yaml:"IdPClientID,omitempty"`
	IDPClientSecretSources       []CredentialSource `yaml:"IdPClientSecretSources,omitempty"`
	ResourceClientID             string             `yaml:"ResourceClientID,omitempty"`
	RedirectURI                  string             `yaml:"RedirectURI,omitempty"`
	SubjectTokenSources          []CredentialSource `yaml:"SubjectTokenSources,omitempty"`
	WorkloadSubjectTokenType     string             `yaml:"WorkloadSubjectTokenType,omitempty"`
}

type Credential struct {
	Slot    string             `yaml:"Slot"`
	Sources []CredentialSource `yaml:"Sources"`
	Inject  Injection          `yaml:"Inject"`
}

type CredentialSource struct {
	Env      *EnvSource      `yaml:"Env,omitempty"`
	Dotenv   *DotenvSource   `yaml:"Dotenv,omitempty"`
	Keychain *KeychainSource `yaml:"Keychain,omitempty"`
	Helper   *HelperSource   `yaml:"Helper,omitempty"`
}

type EnvSource struct {
	Key string `yaml:"Key"`
}

type DotenvSource struct {
	Path string `yaml:"Path"`
	Key  string `yaml:"Key"`
}

type KeychainSource struct {
	Service string `yaml:"Service"`
	Account string `yaml:"Account"`
}

// HelperSource invokes one explicitly trusted absolute executable with an
// empty environment and treats its bounded stdout as the one credential value.
type HelperSource struct {
	Command   string   `yaml:"Command"`
	Args      []string `yaml:"Args,omitempty"`
	TimeoutMS int      `yaml:"TimeoutMS,omitempty"`
}

type Injection struct {
	StdioEnv   string            `yaml:"StdioEnv,omitempty"`
	HTTPHeader *HTTPHeaderInject `yaml:"HTTPHeader,omitempty"`
}

type HTTPHeaderInject struct {
	Name   string `yaml:"Name"`
	Format string `yaml:"Format"`
}

// LoadConfigured loads the exact path selected by the user environment, or the
// platform user config directory. A missing default file means no servers are trusted.
func LoadConfigured() (*Config, string, error) {
	path := strings.TrimSpace(os.Getenv(PathEnv))
	explicit := path != ""
	if !explicit {
		base, err := os.UserConfigDir()
		if err != nil {
			return nil, "", err
		}
		path = filepath.Join(base, "skillex", "mcp-trust.yaml")
	}
	cfg, err := Load(path)
	if err != nil {
		if !explicit && errors.Is(err, os.ErrNotExist) {
			return &Config{Version: ConfigVersion}, path, nil
		}
		return nil, path, err
	}
	return cfg, path, nil
}

func Load(path string) (*Config, error) {
	//nolint:gosec // G703: this is the exact user-selected trust config path.
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	var cfg Config
	if err := decoder.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("parsing trusted MCP config: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return nil, errors.New("trusted MCP config contains multiple documents")
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (c *Config) Validate() error {
	if c.Version != ConfigVersion {
		return fmt.Errorf("trusted MCP config Version must be %d", ConfigVersion)
	}
	if c.Telemetry != nil && c.Telemetry.Enabled && !filepath.IsAbs(c.Telemetry.Path) {
		return errors.New("Telemetry.Path must be absolute when telemetry is enabled")
	}
	if c.OAuthStore != nil && (!filepath.IsAbs(c.OAuthStore.KeyPath) || !filepath.IsAbs(c.OAuthStore.Directory)) {
		return errors.New("OAuthStore KeyPath and Directory must be absolute")
	}
	catalogNames := map[string]bool{}
	for i, source := range c.CatalogSources {
		if !safeName(source.Name) || source.Type != "registry-api" || source.BaseURL == "" {
			return fmt.Errorf("CatalogSources[%d] requires a safe Name, Type registry-api, and BaseURL", i)
		}
		if catalogNames[source.Name] {
			return fmt.Errorf("CatalogSources[%d].Name is duplicated", i)
		}
		catalogNames[source.Name] = true
	}
	servers := map[string]bool{}
	for i, server := range c.Servers {
		prefix := fmt.Sprintf("Servers[%d]", i)
		if server.Server == "" || server.Version == "" {
			return fmt.Errorf("%s requires Server and exact Version", prefix)
		}
		if strings.ContainsAny(server.Version, "*^~<>=|, \t\r\n") {
			return fmt.Errorf("%s.Version must be exact", prefix)
		}
		key := server.Server + "\x00" + server.Version
		if servers[key] {
			return fmt.Errorf("%s duplicates %s@%s", prefix, server.Server, server.Version)
		}
		servers[key] = true
		if (server.Stdio == nil) == (server.HTTP == nil) {
			return fmt.Errorf("%s requires exactly one of Stdio or HTTP", prefix)
		}
		if server.Stdio != nil && !filepath.IsAbs(server.Stdio.Command) {
			return fmt.Errorf("%s.Stdio.Command must be absolute", prefix)
		}
		if server.Stdio != nil && server.Stdio.Directory != "" && !filepath.IsAbs(server.Stdio.Directory) && !strings.HasPrefix(server.Stdio.Directory, "${projectRoot}") {
			return fmt.Errorf("%s.Stdio.Directory must be absolute or project-root relative", prefix)
		}
		if server.HTTP != nil {
			if _, err := streamhttp.NewFactory([]streamhttp.ServerConfig{{CanonicalName: server.Server, Version: server.Version, Endpoint: server.HTTP.Endpoint}}); err != nil {
				return fmt.Errorf("%s.HTTP: %w", prefix, err)
			}
			if server.HTTP.MTLS != nil {
				if len(server.HTTP.MTLS.CertificateSources) == 0 || len(server.HTTP.MTLS.PrivateKeySources) == 0 {
					return fmt.Errorf("%s.HTTP.MTLS requires CertificateSources and PrivateKeySources", prefix)
				}
				if err := validateSources(prefix+".HTTP.MTLS.CertificateSources", server.HTTP.MTLS.CertificateSources); err != nil {
					return err
				}
				if err := validateSources(prefix+".HTTP.MTLS.PrivateKeySources", server.HTTP.MTLS.PrivateKeySources); err != nil {
					return err
				}
			}
		}
		for _, project := range server.AllowedProjects {
			if project != "${projectRoot}" && !filepath.IsAbs(project) {
				return fmt.Errorf("%s.AllowedProjects entries must be absolute", prefix)
			}
		}
	}
	profiles := map[string]bool{}
	for i, profile := range c.CredentialProfiles {
		prefix := fmt.Sprintf("CredentialProfiles[%d]", i)
		if profile.Name == "" || profile.Service == "" {
			return fmt.Errorf("%s requires Name and Service", prefix)
		}
		if profiles[profile.Name] {
			return fmt.Errorf("%s.Name is duplicated", prefix)
		}
		profiles[profile.Name] = true
		if profile.OAuth != nil && len(profile.Credentials) > 0 {
			return fmt.Errorf("%s cannot combine Credentials and OAuth", prefix)
		}
		if profile.OAuth != nil {
			if err := validateOAuthProfile(prefix+".OAuth", profile.OAuth); err != nil {
				return err
			}
		}
		for j, credential := range profile.Credentials {
			cp := fmt.Sprintf("%s.Credentials[%d]", prefix, j)
			if credential.Slot == "" || len(credential.Sources) == 0 || (credential.Inject.StdioEnv == "" && credential.Inject.HTTPHeader == nil) {
				return fmt.Errorf("%s requires Slot, Sources, and an exact injection", cp)
			}
			if header := credential.Inject.HTTPHeader; header != nil {
				if header.Name == "" || strings.ContainsAny(header.Name, "\r\n:") || !strings.Contains(header.Format, "${value}") {
					return fmt.Errorf("%s.Inject.HTTPHeader requires a safe Name and Format containing ${value}", cp)
				}
			}
			if err := validateSources(cp+".Sources", credential.Sources); err != nil {
				return err
			}
		}
	}
	for i, server := range c.Servers {
		for _, profile := range server.AuthProfiles {
			if !profiles[profile] {
				return fmt.Errorf("Servers[%d].AuthProfiles references unknown profile %q", i, profile)
			}
			configured, _ := c.FindProfile(profile)
			if configured.OAuth != nil && server.HTTP == nil {
				return fmt.Errorf("servers[%d] uses OAuth profile %q without HTTP transport", i, profile)
			}
		}
	}
	return nil
}

func validateOAuthProfile(prefix string, profile *OAuthProfile) error {
	if profile.ProtectedResourceMetadataURL == "" || profile.Resource == "" {
		return fmt.Errorf("%s requires ProtectedResourceMetadataURL and Resource", prefix)
	}
	switch profile.Type {
	case "client-credentials":
		if profile.ClientID == "" {
			return fmt.Errorf("%s client-credentials requires ClientID", prefix)
		}
		if profile.ClientAuthMethod == "private_key_jwt" {
			if len(profile.PrivateKeySources) == 0 {
				return fmt.Errorf("%s private_key_jwt requires PrivateKeySources", prefix)
			}
		} else if len(profile.ClientSecretSources) == 0 {
			return fmt.Errorf("%s client-credentials requires ClientSecretSources", prefix)
		}
		if profile.ClientAuthMethod != "" && profile.ClientAuthMethod != "client_secret_post" && profile.ClientAuthMethod != "client_secret_basic" && profile.ClientAuthMethod != "private_key_jwt" {
			return fmt.Errorf("%s.ClientAuthMethod is unsupported", prefix)
		}
	case "enterprise-managed":
		if profile.IDPTokenEndpoint == "" || len(profile.IdentityAssertionSources) == 0 || profile.ResourceClientID == "" {
			return fmt.Errorf("%s enterprise-managed requires IdPTokenEndpoint, IdentityAssertionSources, and ResourceClientID", prefix)
		}
	case "authorization-code":
		if (profile.ClientID == "" && !profile.AllowDynamicRegistration) || profile.RedirectURI == "" {
			return fmt.Errorf("%s authorization-code requires ClientID (or explicit dynamic registration) and RedirectURI", prefix)
		}
	case "workload-token-exchange":
		if len(profile.SubjectTokenSources) == 0 || profile.WorkloadSubjectTokenType == "" {
			return fmt.Errorf("%s workload-token-exchange requires SubjectTokenSources and WorkloadSubjectTokenType", prefix)
		}
	default:
		return fmt.Errorf("%s.Type %q is unsupported", prefix, profile.Type)
	}
	for i, sources := range [][]CredentialSource{profile.ClientSecretSources, profile.PrivateKeySources, profile.IdentityAssertionSources, profile.IDPClientSecretSources, profile.SubjectTokenSources} {
		if len(sources) > 0 {
			if err := validateSources(fmt.Sprintf("%s source group %d", prefix, i), sources); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateSources(prefix string, sources []CredentialSource) error {
	for i, source := range sources {
		configured := 0
		if source.Env != nil {
			configured++
			if source.Env.Key == "" {
				return fmt.Errorf("%s[%d].Env.Key is required", prefix, i)
			}
		}
		if source.Dotenv != nil {
			configured++
			if source.Dotenv.Path == "" || source.Dotenv.Key == "" {
				return fmt.Errorf("%s[%d].Dotenv requires Path and Key", prefix, i)
			}
		}
		if source.Keychain != nil {
			configured++
			if source.Keychain.Service == "" || source.Keychain.Account == "" {
				return fmt.Errorf("%s[%d].Keychain requires Service and Account", prefix, i)
			}
		}
		if source.Helper != nil {
			configured++
			if !filepath.IsAbs(source.Helper.Command) {
				return fmt.Errorf("%s[%d].Helper.Command must be absolute", prefix, i)
			}
		}
		if configured != 1 {
			return fmt.Errorf("%s[%d] must contain exactly one source", prefix, i)
		}
	}
	return nil
}

func (c *Config) FindCatalogSource(name string) (CatalogSource, bool) {
	for _, source := range c.CatalogSources {
		if source.Name == name {
			return source, true
		}
	}
	return CatalogSource{}, false
}

func safeName(value string) bool {
	if value == "" || value == "." || value == ".." {
		return false
	}
	for _, r := range value {
		if !strings.ContainsRune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_.", r) {
			return false
		}
	}
	return true
}

func (c *Config) FindServer(name, version string) (Server, bool) {
	for _, server := range c.Servers {
		if server.Server == name && server.Version == version {
			return server, true
		}
	}
	return Server{}, false
}

func (c *Config) FindProfile(name string) (CredentialProfile, bool) {
	for _, profile := range c.CredentialProfiles {
		if profile.Name == name {
			return profile, true
		}
	}
	return CredentialProfile{}, false
}

// Ready evaluates exact trust and credential-source presence without starting a server.
func (c *Config) Ready(selected capability.Capability, projectRoot string) capability.AvailabilityStatus {
	server, ok := c.FindServer(selected.Server.Identity.CanonicalName, selected.Server.Version)
	if !ok || !projectAllowed(server, projectRoot) {
		return capability.AvailabilitySetupRequired
	}
	if server.HTTP != nil && server.HTTP.MTLS != nil {
		if err := mtlsSourcesPresent(server.HTTP.MTLS, projectRoot); err != nil {
			if errors.Is(err, ErrCredentialMissing) {
				return capability.AvailabilityCredentialMissing
			}
			return capability.AvailabilitySetupRequired
		}
	}
	if selected.AuthProfile == "" {
		return capability.AvailabilityReady
	}
	if !contains(server.AuthProfiles, selected.AuthProfile) {
		return capability.AvailabilityPolicyDenied
	}
	err := c.credentialsPresent(selected, projectRoot)
	if errors.Is(err, ErrCredentialMissing) {
		return capability.AvailabilityCredentialMissing
	}
	if errors.Is(err, auth.ErrLoginRequired) {
		return capability.AvailabilityLoginRequired
	}
	if err != nil {
		return capability.AvailabilitySetupRequired
	}
	return capability.AvailabilityReady
}

// StdioConfig resolves only the selected profile's exact sources and builds a
// minimal child environment. It never enumerates or inherits the parent environment.
func (c *Config) StdioConfig(selected capability.Capability, projectRoot string) (stdio.ServerConfig, error) {
	server, ok := c.FindServer(selected.Server.Identity.CanonicalName, selected.Server.Version)
	if !ok {
		return stdio.ServerConfig{}, ErrServerUntrusted
	}
	if !projectAllowed(server, projectRoot) {
		return stdio.ServerConfig{}, ErrProjectUntrusted
	}
	values, err := c.credentialValues(selected, projectRoot)
	if err != nil {
		return stdio.ServerConfig{}, err
	}
	if server.Stdio == nil {
		return stdio.ServerConfig{}, errors.New("trusted server is not configured for stdio")
	}
	environment := make([]string, 0, len(values))
	profile, _ := c.FindProfile(selected.AuthProfile)
	if profile.OAuth != nil {
		return stdio.ServerConfig{}, errors.New("OAuth credential profiles require Streamable HTTP")
	}
	for i, credential := range profile.Credentials {
		if credential.Inject.StdioEnv == "" {
			return stdio.ServerConfig{}, fmt.Errorf("credential slot %s has no stdio injection", credential.Slot)
		}
		environment = append(environment, credential.Inject.StdioEnv+"="+values[i])
	}
	directory, err := expandProjectPath(server.Stdio.Directory, projectRoot, false)
	if err != nil {
		return stdio.ServerConfig{}, err
	}
	timeout := time.Duration(server.Stdio.StartupTimeoutMS) * time.Millisecond
	return stdio.ServerConfig{
		CanonicalName:  selected.Server.Identity.CanonicalName,
		Version:        selected.Server.Version,
		Command:        server.Stdio.Command,
		Args:           append([]string(nil), server.Stdio.Args...),
		Directory:      directory,
		Environment:    environment,
		StartupTimeout: timeout,
	}, nil
}

// StreamableHTTPConfig resolves exact mapped header injections for a selected server.
func (c *Config) StreamableHTTPConfig(ctx context.Context, selected capability.Capability, projectRoot string) (streamhttp.ServerConfig, error) {
	server, ok := c.FindServer(selected.Server.Identity.CanonicalName, selected.Server.Version)
	if !ok {
		return streamhttp.ServerConfig{}, ErrServerUntrusted
	}
	if !projectAllowed(server, projectRoot) {
		return streamhttp.ServerConfig{}, ErrProjectUntrusted
	}
	if server.HTTP == nil {
		return streamhttp.ServerConfig{}, errors.New("trusted server is not configured for Streamable HTTP")
	}
	headers := map[string]string{}
	var httpClient *http.Client
	if server.HTTP.MTLS != nil {
		certificatePEM, present, err := resolveSources(server.HTTP.MTLS.CertificateSources, projectRoot)
		if err != nil || !present {
			return streamhttp.ServerConfig{}, ErrCredentialMissing
		}
		privateKeyPEM, present, err := resolveSources(server.HTTP.MTLS.PrivateKeySources, projectRoot)
		if err != nil || !present {
			return streamhttp.ServerConfig{}, ErrCredentialMissing
		}
		certificate, err := tls.X509KeyPair([]byte(certificatePEM), []byte(privateKeyPEM))
		if err != nil {
			return streamhttp.ServerConfig{}, errors.New("configured MCP mTLS certificate or private key is invalid")
		}
		httpClient = &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS12, ServerName: server.HTTP.MTLS.ServerName,
			Certificates: []tls.Certificate{certificate},
		}}}
	}
	profile, _ := c.FindProfile(selected.AuthProfile)
	if profile.OAuth != nil {
		token, err := c.acquireOAuth(ctx, profile, projectRoot)
		if err != nil {
			return streamhttp.ServerConfig{}, err
		}
		headers["Authorization"] = token.AuthorizationHeader()
	} else {
		values, err := c.credentialValues(selected, projectRoot)
		if err != nil {
			return streamhttp.ServerConfig{}, err
		}
		for i, credential := range profile.Credentials {
			injection := credential.Inject.HTTPHeader
			if injection == nil {
				return streamhttp.ServerConfig{}, fmt.Errorf("credential slot %s has no HTTP header injection", credential.Slot)
			}
			headers[injection.Name] = strings.ReplaceAll(injection.Format, "${value}", values[i])
		}
	}
	return streamhttp.ServerConfig{
		CanonicalName: selected.Server.Identity.CanonicalName, Version: selected.Server.Version,
		Endpoint: server.HTTP.Endpoint, Headers: headers, Timeout: time.Duration(server.HTTP.TimeoutMS) * time.Millisecond,
		HTTPClient: httpClient,
	}, nil
}

func mtlsSourcesPresent(config *MTLSConfig, projectRoot string) error {
	for _, sources := range [][]CredentialSource{config.CertificateSources, config.PrivateKeySources} {
		_, present, err := resolveSources(sources, projectRoot)
		if err != nil {
			return err
		}
		if !present {
			return ErrCredentialMissing
		}
	}
	return nil
}

func (c *Config) credentialsPresent(selected capability.Capability, projectRoot string) error {
	if selected.AuthProfile == "" {
		return nil
	}
	profile, ok := c.FindProfile(selected.AuthProfile)
	if !ok || profile.Service != selected.Server.Identity.CanonicalName {
		return ErrProfileUntrusted
	}
	if profile.OAuth == nil {
		_, err := c.credentialValues(selected, projectRoot)
		return err
	}
	var groups [][]CredentialSource
	switch profile.OAuth.Type {
	case "client-credentials":
		if profile.OAuth.ClientAuthMethod == "private_key_jwt" {
			groups = [][]CredentialSource{profile.OAuth.PrivateKeySources}
		} else {
			groups = [][]CredentialSource{profile.OAuth.ClientSecretSources}
		}
	case "enterprise-managed":
		groups = [][]CredentialSource{profile.OAuth.IdentityAssertionSources}
		if len(profile.OAuth.IDPClientSecretSources) > 0 {
			groups = append(groups, profile.OAuth.IDPClientSecretSources)
		}
	case "authorization-code":
		token, err := c.tokenStore().Load(profile.Name)
		if errors.Is(err, os.ErrNotExist) || (err == nil && !token.ValidFor(30*time.Second) && !token.CanRefresh()) {
			return auth.ErrLoginRequired
		}
		return err
	case "workload-token-exchange":
		groups = [][]CredentialSource{profile.OAuth.SubjectTokenSources}
	}
	for _, sources := range groups {
		_, present, err := resolveSources(sources, projectRoot)
		if err != nil {
			return err
		}
		if !present {
			return ErrCredentialMissing
		}
	}
	return nil
}

func (c *Config) acquireOAuth(ctx context.Context, profile CredentialProfile, projectRoot string) (auth.Token, error) {
	c.tokenMu.Lock()
	defer c.tokenMu.Unlock()
	if c.tokens == nil {
		c.tokens = map[string]auth.Token{}
	}
	if token, ok := c.tokens[profile.Name]; ok && time.Until(token.ExpiresAt()) > 30*time.Second {
		return token, nil
	}
	configured := profile.OAuth
	metadata, err := auth.Discover(ctx, c.httpClient, auth.DiscoveryOptions{
		ProtectedResourceMetadataURL: configured.ProtectedResourceMetadataURL,
		ExpectedResource:             configured.Resource, ExpectedIssuer: configured.Issuer,
	})
	if err != nil {
		return auth.Token{}, err
	}
	registration := auth.ClientRegistration{ClientID: configured.ClientID}
	if configured.Type == "client-credentials" || configured.Type == "authorization-code" {
		registration, err = c.resolveClientRegistration(ctx, profile, metadata)
		if err != nil {
			return auth.Token{}, err
		}
	}
	var token auth.Token
	switch configured.Type {
	case "client-credentials":
		secret := ""
		privateKey := ""
		var present bool
		if configured.ClientAuthMethod == "private_key_jwt" {
			privateKey, present, err = resolveSources(configured.PrivateKeySources, projectRoot)
		} else {
			secret, present, err = resolveSources(configured.ClientSecretSources, projectRoot)
		}
		if err != nil || !present {
			return auth.Token{}, ErrCredentialMissing
		}
		token, err = auth.AcquireClientCredentials(ctx, c.httpClient, auth.ClientCredentialsRequest{
			Metadata: metadata, ClientID: registration.ClientID, ClientSecret: secret,
			AuthMethod: configured.ClientAuthMethod, Scopes: configured.Scopes,
			PrivateKeyPEM: []byte(privateKey), KeyID: configured.KeyID,
		})
		if err != nil {
			return auth.Token{}, err
		}
	case "enterprise-managed":
		assertion, present, err := resolveSources(configured.IdentityAssertionSources, projectRoot)
		if err != nil || !present {
			return auth.Token{}, ErrCredentialMissing
		}
		clientSecret := ""
		if len(configured.IDPClientSecretSources) > 0 {
			clientSecret, present, err = resolveSources(configured.IDPClientSecretSources, projectRoot)
			if err != nil || !present {
				return auth.Token{}, ErrCredentialMissing
			}
		}
		token, err = auth.AcquireEnterpriseManaged(ctx, c.httpClient, auth.EnterpriseManagedRequest{
			Metadata: metadata, IDPTokenEndpoint: configured.IDPTokenEndpoint,
			IdentityAssertion: assertion, SubjectTokenType: configured.SubjectTokenType,
			IDPClientID: configured.IDPClientID, IDPClientSecret: clientSecret,
			ResourceClientID: configured.ResourceClientID, Scopes: configured.Scopes,
		})
		if err != nil {
			return auth.Token{}, err
		}
	case "authorization-code":
		store := c.tokenStore()
		token, err = store.Load(profile.Name)
		if errors.Is(err, os.ErrNotExist) {
			return auth.Token{}, auth.ErrLoginRequired
		}
		if err != nil {
			return auth.Token{}, err
		}
		if !token.ValidFor(30 * time.Second) {
			token, err = auth.Refresh(ctx, c.httpClient, metadata, registration.ClientID, token)
			if err != nil {
				return auth.Token{}, err
			}
			if err := store.Save(profile.Name, token); err != nil {
				return auth.Token{}, err
			}
		}
	case "workload-token-exchange":
		subjectToken, present, err := resolveSources(configured.SubjectTokenSources, projectRoot)
		if err != nil || !present {
			return auth.Token{}, ErrCredentialMissing
		}
		token, err = auth.AcquireTokenExchange(ctx, c.httpClient, auth.TokenExchangeRequest{
			Metadata: metadata, SubjectToken: subjectToken, SubjectTokenType: configured.WorkloadSubjectTokenType,
			ClientID: configured.ClientID, Scopes: configured.Scopes,
		})
		if err != nil {
			return auth.Token{}, err
		}
	}
	c.tokens[profile.Name] = token
	return token, nil
}

func (c *Config) resolveClientRegistration(ctx context.Context, profile CredentialProfile, metadata auth.Metadata) (auth.ClientRegistration, error) {
	configured := profile.OAuth
	if configured.ClientID != "" {
		if auth.IsURLClientID(configured.ClientID) {
			return auth.ValidateClientMetadataDocument(ctx, c.httpClient, configured.ClientID, configured.RedirectURI)
		}
		return auth.ClientRegistration{ClientID: configured.ClientID, AuthMethod: configured.ClientAuthMethod}, nil
	}
	if !configured.AllowDynamicRegistration {
		return auth.ClientRegistration{}, auth.ErrClientRegistration
	}
	store := c.tokenStore()
	if registered, err := store.LoadRegistration(profile.Name); err == nil {
		return registered, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return auth.ClientRegistration{}, err
	}
	registered, err := auth.RegisterDynamicClient(ctx, c.httpClient, auth.DynamicRegistrationRequest{
		Metadata: metadata, ClientName: configured.ClientName, RedirectURI: configured.RedirectURI, Scopes: configured.Scopes,
	})
	if err != nil {
		return auth.ClientRegistration{}, err
	}
	if err := store.SaveRegistration(profile.Name, registered); err != nil {
		return auth.ClientRegistration{}, err
	}
	return registered, nil
}

// SetHTTPClient supplies a transport for auth conformance tests or managed deployments.
func (c *Config) SetHTTPClient(client *http.Client) { c.httpClient = client }

func (c *Config) tokenStore() auth.TokenStore {
	if c.OAuthStore != nil {
		return auth.TokenStore{KeyPath: c.OAuthStore.KeyPath, Dir: c.OAuthStore.Directory}
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return auth.TokenStore{}
	}
	base = filepath.Join(base, "skillex")
	return auth.TokenStore{KeyPath: filepath.Join(base, "oauth.key"), Dir: filepath.Join(base, "tokens")}
}

// StartOAuthLogin creates and stores a short-lived PKCE continuation.
func (c *Config) StartOAuthLogin(ctx context.Context, profileName string) (string, error) {
	profile, ok := c.FindProfile(profileName)
	if !ok || profile.OAuth == nil || profile.OAuth.Type != "authorization-code" {
		return "", errors.New("profile is not configured for authorization-code OAuth")
	}
	metadata, err := auth.Discover(ctx, c.httpClient, auth.DiscoveryOptions{
		ProtectedResourceMetadataURL: profile.OAuth.ProtectedResourceMetadataURL,
		ExpectedResource:             profile.OAuth.Resource, ExpectedIssuer: profile.OAuth.Issuer,
	})
	if err != nil {
		return "", err
	}
	registration, err := c.resolveClientRegistration(ctx, profile, metadata)
	if err != nil {
		return "", err
	}
	if registration.AuthMethod != "" && registration.AuthMethod != "none" {
		return "", errors.New("authorization-code profile requires a public CIMD or DCR client using token_endpoint_auth_method none")
	}
	loginURL, pending, err := auth.StartAuthorization(metadata, auth.AuthorizationOptions{
		ClientID: registration.ClientID, RedirectURI: profile.OAuth.RedirectURI, Scopes: profile.OAuth.Scopes,
	})
	if err != nil {
		return "", err
	}
	if err := c.tokenStore().SavePending(profile.Name, pending); err != nil {
		return "", err
	}
	return loginURL, nil
}

// CompleteOAuthLogin validates a callback and stores the encrypted token.
func (c *Config) CompleteOAuthLogin(ctx context.Context, profileName, callbackURL string) error {
	profile, ok := c.FindProfile(profileName)
	if !ok || profile.OAuth == nil || profile.OAuth.Type != "authorization-code" {
		return errors.New("profile is not configured for authorization-code OAuth")
	}
	store := c.tokenStore()
	pending, err := store.LoadPending(profile.Name)
	if err != nil {
		return err
	}
	metadata, err := auth.Discover(ctx, c.httpClient, auth.DiscoveryOptions{
		ProtectedResourceMetadataURL: profile.OAuth.ProtectedResourceMetadataURL,
		ExpectedResource:             profile.OAuth.Resource, ExpectedIssuer: profile.OAuth.Issuer,
	})
	if err != nil {
		return err
	}
	token, err := auth.CompleteAuthorization(ctx, c.httpClient, metadata, pending, callbackURL)
	if err != nil {
		return err
	}
	if err := store.Save(profile.Name, token); err != nil {
		return err
	}
	if err := store.DeletePending(profile.Name); err != nil {
		return err
	}
	c.tokenMu.Lock()
	if c.tokens == nil {
		c.tokens = map[string]auth.Token{}
	}
	c.tokens[profile.Name] = token
	c.tokenMu.Unlock()
	return nil
}

func (c *Config) credentialValues(selected capability.Capability, projectRoot string) ([]string, error) {
	if selected.AuthProfile == "" {
		return []string{}, nil
	}
	server, ok := c.FindServer(selected.Server.Identity.CanonicalName, selected.Server.Version)
	if !ok || !contains(server.AuthProfiles, selected.AuthProfile) {
		return nil, ErrProfileUntrusted
	}
	profile, ok := c.FindProfile(selected.AuthProfile)
	if !ok || profile.Service != selected.Server.Identity.CanonicalName {
		return nil, ErrProfileUntrusted
	}
	values := make([]string, 0, len(profile.Credentials))
	for _, credential := range profile.Credentials {
		value, present, err := resolveSources(credential.Sources, projectRoot)
		if err != nil {
			return nil, err
		}
		if !present {
			return nil, fmt.Errorf("%w: slot %s", ErrCredentialMissing, credential.Slot)
		}
		values = append(values, value)
	}
	return values, nil
}

func resolveSources(sources []CredentialSource, projectRoot string) (string, bool, error) {
	for _, source := range sources {
		if source.Env != nil {
			if value, ok := os.LookupEnv(source.Env.Key); ok {
				return value, true, nil
			}
			continue
		}
		if source.Dotenv != nil {
			path, err := expandProjectPath(source.Dotenv.Path, projectRoot, true)
			if err != nil {
				return "", false, err
			}
			value, ok, err := readDotenvExact(path, source.Dotenv.Key)
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					continue
				}
				return "", false, err
			}
			if ok {
				return value, true, nil
			}
			continue
		}
		if source.Keychain != nil {
			value, present, err := resolveKeychain(*source.Keychain)
			if err != nil {
				return "", false, err
			}
			if present {
				return value, true, nil
			}
			continue
		}
		if source.Helper != nil {
			value, present, err := resolveHelper(*source.Helper)
			if err != nil {
				return "", false, err
			}
			if present {
				return value, true, nil
			}
		}
	}
	return "", false, nil
}

func resolveKeychain(source KeychainSource) (string, bool, error) {
	var helper HelperSource
	switch runtime.GOOS {
	case "darwin":
		helper = HelperSource{Command: "/usr/bin/security", Args: []string{"find-generic-password", "-w", "-s", source.Service, "-a", source.Account}}
	case "linux":
		path, err := exec.LookPath("secret-tool")
		if err != nil {
			return "", false, nil
		}
		helper = HelperSource{Command: path, Args: []string{"lookup", "service", source.Service, "account", source.Account}}
	default:
		return "", false, errors.New("OS keychain credential source is unsupported on this platform")
	}
	value, present, err := resolveHelper(helper)
	if err != nil && strings.Contains(err.Error(), "exited") {
		return "", false, nil
	}
	return value, present, err
}

func resolveHelper(source HelperSource) (string, bool, error) {
	timeout := time.Duration(source.TimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	//nolint:gosec // The executable and arguments are user/enterprise trusted config.
	command := exec.CommandContext(ctx, source.Command, source.Args...)
	command.Env = []string{}
	var stdout limitedCredentialBuffer
	command.Stdout = &stdout
	command.Stderr = io.Discard
	if err := command.Run(); err != nil {
		if ctx.Err() != nil {
			return "", false, errors.New("credential helper timed out")
		}
		return "", false, errors.New("credential helper exited unsuccessfully")
	}
	value := strings.TrimSpace(stdout.String())
	return value, value != "", nil
}

type limitedCredentialBuffer struct {
	buf bytes.Buffer
}

func (b *limitedCredentialBuffer) Write(data []byte) (int, error) {
	const limit = 64 << 10
	original := len(data)
	if b.buf.Len()+len(data) > limit {
		return 0, errors.New("credential helper output exceeds size limit")
	}
	_, err := b.buf.Write(data)
	return original, err
}

func (b *limitedCredentialBuffer) String() string { return b.buf.String() }

func readDotenvExact(path, key string) (string, bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", false, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return "", false, err
	}
	if info.Size() > maxDotenvSize {
		return "", false, errors.New("configured dotenv file exceeds size limit")
	}
	scanner := bufio.NewScanner(io.LimitReader(file, maxDotenvSize+1))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		name, value, ok := strings.Cut(line, "=")
		if !ok || strings.TrimSpace(name) != key {
			continue
		}
		value = strings.TrimSpace(value)
		if len(value) >= 2 && ((value[0] == '\'' && value[len(value)-1] == '\'') || (value[0] == '"' && value[len(value)-1] == '"')) {
			value = value[1 : len(value)-1]
		}
		return value, true, nil
	}
	return "", false, scanner.Err()
}

func projectAllowed(server Server, root string) bool {
	root, _ = filepath.Abs(root)
	root = filepath.Clean(root)
	for _, allowed := range server.AllowedProjects {
		if allowed == "${projectRoot}" {
			return true
		}
		candidate, _ := filepath.Abs(allowed)
		if filepath.Clean(candidate) == root {
			return true
		}
	}
	return false
}

func expandProjectPath(value, projectRoot string, confine bool) (string, error) {
	if value == "" {
		return "", nil
	}
	usesRoot := strings.Contains(value, "${projectRoot}")
	if strings.Contains(strings.ReplaceAll(value, "${projectRoot}", ""), "${") {
		return "", errors.New("unsupported path template variable")
	}
	expanded := strings.ReplaceAll(value, "${projectRoot}", projectRoot)
	if !filepath.IsAbs(expanded) {
		return "", errors.New("trusted MCP path must be absolute")
	}
	if !confine || !usesRoot {
		return filepath.Clean(expanded), nil
	}
	rootResolved, err := filepath.EvalSymlinks(projectRoot)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(expanded)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(rootResolved, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("configured project dotenv path resolves outside project root")
	}
	return resolved, nil
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
