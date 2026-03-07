package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Config represents the root skillex.yaml configuration.
type Config struct {
	Version int    `yaml:"Version"`
	Rules   []Rule `yaml:"Rules"`
}

// Rule defines a scope-to-skills mapping, with optional dependency boundary.
type Rule struct {
	Scope              string   `yaml:"Scope"`
	Skills             []string `yaml:"Skills"`
	DependencyBoundary string   `yaml:"DependencyBoundary"`
}

// Load reads and parses skillex.yaml from the given root directory.
func Load(root string) (*Config, error) {
	path := filepath.Join(root, "skillex.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("skillex.yaml not found at %s; run 'skillex init' to initialize", root)
		}
		return nil, fmt.Errorf("reading skillex.yaml: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing skillex.yaml: %w", err)
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

// Marshal serializes the config to YAML bytes.
func Marshal(cfg *Config) ([]byte, error) {
	return yaml.Marshal(cfg)
}
