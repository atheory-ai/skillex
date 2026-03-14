package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

const (
	JSONFilename = "skillex.json"
	YAMLFilename = "skillex.yaml"
)

type Format string

const (
	FormatJSON Format = "json"
	FormatYAML Format = "yaml"
)

// Config represents the root skillex configuration.
type Config struct {
	Version int    `yaml:"Version" json:"Version"`
	Rules   []Rule `yaml:"Rules" json:"Rules"`
}

// Rule defines a scope-to-skills mapping, with optional dependency boundary.
type Rule struct {
	Scope              string   `yaml:"Scope" json:"Scope"`
	Skills             []string `yaml:"Skills" json:"Skills"`
	DependencyBoundary string   `yaml:"DependencyBoundary" json:"DependencyBoundary"`
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
	switch format {
	case FormatJSON:
		if err := json.Unmarshal(data, &cfg); err != nil {
			return nil, fmt.Errorf("parsing %s: %w", filepath.Base(path), err)
		}
	case FormatYAML:
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			return nil, fmt.Errorf("parsing %s: %w", filepath.Base(path), err)
		}
	default:
		return nil, fmt.Errorf("unsupported config format %q", format)
	}

	return &cfg, nil
}

// DefaultConfig returns a minimal default configuration.
func DefaultConfig() *Config {
	return &Config{
		Version: 4,
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
