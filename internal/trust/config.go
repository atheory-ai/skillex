// Package trust loads user- or enterprise-controlled MCP transport, policy,
// and credential-source configuration. Project and package files can select
// these definitions but can never define them.
package trust

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

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
	Endpoint  string `yaml:"Endpoint"`
	TimeoutMS int    `yaml:"TimeoutMS,omitempty"`
}

type CredentialProfile struct {
	Name        string       `yaml:"Name"`
	Service     string       `yaml:"Service"`
	Credentials []Credential `yaml:"Credentials"`
}

type Credential struct {
	Slot    string             `yaml:"Slot"`
	Sources []CredentialSource `yaml:"Sources"`
	Inject  Injection          `yaml:"Inject"`
}

type CredentialSource struct {
	Env    *EnvSource    `yaml:"Env,omitempty"`
	Dotenv *DotenvSource `yaml:"Dotenv,omitempty"`
}

type EnvSource struct {
	Key string `yaml:"Key"`
}

type DotenvSource struct {
	Path string `yaml:"Path"`
	Key  string `yaml:"Key"`
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
			for k, source := range credential.Sources {
				if (source.Env == nil) == (source.Dotenv == nil) {
					return fmt.Errorf("%s.Sources[%d] must contain exactly one of Env or Dotenv", cp, k)
				}
				if source.Env != nil && source.Env.Key == "" {
					return fmt.Errorf("%s.Sources[%d].Env.Key is required", cp, k)
				}
				if source.Dotenv != nil && (source.Dotenv.Path == "" || source.Dotenv.Key == "") {
					return fmt.Errorf("%s.Sources[%d].Dotenv requires Path and Key", cp, k)
				}
			}
		}
	}
	for i, server := range c.Servers {
		for _, profile := range server.AuthProfiles {
			if !profiles[profile] {
				return fmt.Errorf("Servers[%d].AuthProfiles references unknown profile %q", i, profile)
			}
		}
	}
	return nil
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
	if selected.AuthProfile == "" {
		return capability.AvailabilityReady
	}
	if !contains(server.AuthProfiles, selected.AuthProfile) {
		return capability.AvailabilityPolicyDenied
	}
	_, err := c.credentialValues(selected, projectRoot)
	if errors.Is(err, ErrCredentialMissing) {
		return capability.AvailabilityCredentialMissing
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
func (c *Config) StreamableHTTPConfig(selected capability.Capability, projectRoot string) (streamhttp.ServerConfig, error) {
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
	values, err := c.credentialValues(selected, projectRoot)
	if err != nil {
		return streamhttp.ServerConfig{}, err
	}
	headers := map[string]string{}
	profile, _ := c.FindProfile(selected.AuthProfile)
	for i, credential := range profile.Credentials {
		injection := credential.Inject.HTTPHeader
		if injection == nil {
			return streamhttp.ServerConfig{}, fmt.Errorf("credential slot %s has no HTTP header injection", credential.Slot)
		}
		headers[injection.Name] = strings.ReplaceAll(injection.Format, "${value}", values[i])
	}
	return streamhttp.ServerConfig{
		CanonicalName: selected.Server.Identity.CanonicalName, Version: selected.Server.Version,
		Endpoint: server.HTTP.Endpoint, Headers: headers, Timeout: time.Duration(server.HTTP.TimeoutMS) * time.Millisecond,
	}, nil
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
	}
	return "", false, nil
}

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
