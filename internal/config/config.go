package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	JSONFilename = "skillex.json"
	YAMLFilename = "skillex.yaml"

	// SkillsOnlyConfigVersion is the existing skills-only configuration format.
	SkillsOnlyConfigVersion = 4
	// MCPConfigVersion introduces the explicit, default-off MCP broker gate.
	MCPConfigVersion = 5
)

type Format string

const (
	FormatJSON Format = "json"
	FormatYAML Format = "yaml"
)

// Config represents the root skillex configuration.
type Config struct {
	Install *InstallConfig `yaml:"Install,omitempty" json:"Install,omitempty"`
	Version int            `yaml:"Version" json:"Version"`
	Rules   []Rule         `yaml:"Rules" json:"Rules"`
	MCP     *MCPConfig     `yaml:"MCP,omitempty" json:"MCP,omitempty"`
}

// Rule defines a scope-to-skills mapping, with optional dependency boundary.
type Rule struct {
	Scope              string   `yaml:"Scope" json:"Scope"`
	Skills             []string `yaml:"Skills" json:"Skills"`
	DependencyBoundary string   `yaml:"DependencyBoundary" json:"DependencyBoundary"`
}

// MCPConfig is the explicit project-level gate for MCP capability brokering.
// Credential definitions remain outside project configuration; bindings may
// only name a trusted profile supplied by a user or enterprise configuration.
type MCPConfig struct {
	Enabled  bool         `yaml:"Enabled" json:"Enabled"`
	Catalogs []MCPCatalog `yaml:"Catalogs,omitempty" json:"Catalogs,omitempty"`
	Bindings []MCPBinding `yaml:"Bindings,omitempty" json:"Bindings,omitempty"`
}

// MCPCatalog identifies a metadata-only capability source. Static catalog
// paths are resolved within the project root and are never executed.
type MCPCatalog struct {
	Type string `yaml:"Type" json:"Type"`
	Path string `yaml:"Path,omitempty" json:"Path,omitempty"`
	Name string `yaml:"Name,omitempty" json:"Name,omitempty"`
}

// MCPBinding selects one exact downstream server version and, optionally, a
// previously trusted authentication profile for a project scope.
type MCPBinding struct {
	Server      string `yaml:"Server" json:"Server"`
	Version     string `yaml:"Version" json:"Version"`
	AuthProfile string `yaml:"AuthProfile,omitempty" json:"AuthProfile,omitempty"`
	Scope       string `yaml:"Scope" json:"Scope"`
}

// MCPEnabled reports whether this project explicitly opted into capability
// brokering. Version 4 and version 5 configurations without MCP.Enabled remain
// skills-only.
func (c *Config) MCPEnabled() bool {
	return c != nil && c.Version == MCPConfigVersion && c.MCP != nil && c.MCP.Enabled
}

// ResolvePath returns the config file path and format for the given repo root.
func ResolvePath(root string) (string, Format, error) {
	jsonPath := filepath.Join(root, JSONFilename)
	yamlPath := filepath.Join(root, YAMLFilename)

	jsonExists := fileExists(jsonPath)
	yamlExists := fileExists(yamlPath)

	switch {
	case jsonExists && yamlExists:
		return "", "", fmt.Errorf("both %s and %s exist at %s; keep only one config file", JSONFilename, YAMLFilename, root)
	case jsonExists:
		return jsonPath, FormatJSON, nil
	case yamlExists:
		return yamlPath, FormatYAML, nil
	default:
		return "", "", fmt.Errorf("%s or %s not found at %s; run 'skillex init' to initialize", JSONFilename, YAMLFilename, root)
	}
}

// Load reads and parses skillex.json or skillex.yaml from the given root directory.
func Load(root string) (*Config, error) {
	path, format, err := ResolvePath(root)
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", filepath.Base(path), err)
	}

	var cfg Config
	if err := decodeConfig(data, format, false, &cfg); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", filepath.Base(path), err)
	}
	// Version 5 contains security-sensitive server and auth-profile selectors.
	// Decode it again with unknown-field rejection so a typo cannot silently
	// widen or disable a policy boundary. Version 4 keeps its historical
	// permissive decoding behavior for compatibility.
	if cfg.Version == MCPConfigVersion {
		cfg = Config{}
		if err := decodeConfig(data, format, true, &cfg); err != nil {
			return nil, fmt.Errorf("parsing %s: %w", filepath.Base(path), err)
		}
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid %s: %w", filepath.Base(path), err)
	}

	return &cfg, nil
}

// Validate enforces the configuration-version and explicit MCP opt-in
// boundary without changing the behavior of existing version 4 projects.
func (c *Config) Validate() error {
	if err := c.Install.Validate(); err != nil {
		return err
	}
	switch c.Version {
	case SkillsOnlyConfigVersion:
		if c.MCP != nil {
			return fmt.Errorf("MCP configuration requires Version %d", MCPConfigVersion)
		}
		return nil
	case MCPConfigVersion:
	default:
		return fmt.Errorf("unsupported Version %d (supported: %d and %d)", c.Version, SkillsOnlyConfigVersion, MCPConfigVersion)
	}

	if c.MCP == nil {
		return nil
	}
	if !c.MCP.Enabled {
		if len(c.MCP.Bindings) > 0 {
			return errorsForDisabledBindings()
		}
		return nil
	}
	if len(c.MCP.Bindings) == 0 {
		return errorsForMissingBindings()
	}
	for i, catalog := range c.MCP.Catalogs {
		prefix := fmt.Sprintf("MCP.Catalogs[%d]", i)
		switch catalog.Type {
		case "static":
			path := filepath.Clean(strings.TrimSpace(catalog.Path))
			if path == "." || filepath.IsAbs(path) || path == ".." || strings.HasPrefix(path, ".."+string(filepath.Separator)) {
				return fmt.Errorf("%s.Path must be a relative path inside the project", prefix)
			}
			if catalog.Name != "" {
				return fmt.Errorf("%s.Name is not valid for a static catalog", prefix)
			}
		case "trusted":
			if !validCatalogName(catalog.Name) || catalog.Path != "" {
				return fmt.Errorf("%s requires a safe Name and no Path", prefix)
			}
		default:
			return fmt.Errorf("%s.Type %q is unsupported (supported: static, trusted)", prefix, catalog.Type)
		}
	}

	seen := make(map[string]struct{}, len(c.MCP.Bindings))
	for i, binding := range c.MCP.Bindings {
		prefix := fmt.Sprintf("MCP.Bindings[%d]", i)
		if strings.TrimSpace(binding.Server) == "" {
			return fmt.Errorf("%s.Server is required", prefix)
		}
		if strings.TrimSpace(binding.Version) == "" {
			return fmt.Errorf("%s.Version is required and must be exact", prefix)
		}
		if strings.ContainsAny(binding.Version, "*^~<>=|, \t\r\n") {
			return fmt.Errorf("%s.Version %q must be exact, not a range", prefix, binding.Version)
		}
		if strings.TrimSpace(binding.Scope) == "" {
			return fmt.Errorf("%s.Scope is required", prefix)
		}
		key := binding.Server + "\x00" + binding.Version + "\x00" + binding.Scope
		if _, ok := seen[key]; ok {
			return fmt.Errorf("%s duplicates server %q version %q scope %q", prefix, binding.Server, binding.Version, binding.Scope)
		}
		seen[key] = struct{}{}
	}
	return nil
}

func validCatalogName(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if !strings.ContainsRune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_.", r) {
			return false
		}
	}
	return value != "." && value != ".."
}

func errorsForDisabledBindings() error {
	return fmt.Errorf("MCP.Bindings requires MCP.Enabled: true")
}

func errorsForMissingBindings() error {
	return fmt.Errorf("MCP.Enabled: true requires at least one MCP.Binding")
}

func decodeConfig(data []byte, format Format, strict bool, cfg *Config) error {
	switch format {
	case FormatJSON:
		decoder := json.NewDecoder(bytes.NewReader(data))
		if strict {
			decoder.DisallowUnknownFields()
		}
		if err := decoder.Decode(cfg); err != nil {
			return err
		}
		if err := decoder.Decode(&struct{}{}); err != io.EOF {
			if err == nil {
				return errorsForTrailingDocument()
			}
			return err
		}
		return nil
	case FormatYAML:
		decoder := yaml.NewDecoder(bytes.NewReader(data))
		decoder.KnownFields(strict)
		if err := decoder.Decode(cfg); err != nil {
			return err
		}
		if err := decoder.Decode(&struct{}{}); err != io.EOF {
			if err == nil {
				return errorsForTrailingDocument()
			}
			return err
		}
		return nil
	default:
		return fmt.Errorf("unsupported config format %q", format)
	}
}

func errorsForTrailingDocument() error {
	return fmt.Errorf("multiple configuration documents are not supported")
}

// DefaultConfig returns a minimal default configuration.
func DefaultConfig() *Config {
	return &Config{
		Version: SkillsOnlyConfigVersion,
		Rules: []Rule{
			{
				Scope:  "**",
				Skills: []string{"skills/repo.md"},
			},
		},
	}
}

// Marshal serializes the config to the requested format.
func Marshal(cfg *Config, format Format) ([]byte, error) {
	switch format {
	case FormatJSON:
		return json.MarshalIndent(cfg, "", "  ")
	case FormatYAML:
		return yaml.Marshal(cfg)
	default:
		return nil, fmt.Errorf("unsupported config format %q", format)
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
