package agents

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/atheory-ai/skillex/internal/registry"
)

const (
	markerStart = "<!-- skillex:start -->"
	markerEnd   = "<!-- skillex:end -->"
)

// GenerateSection creates the AGENTS.md section content from registry data.
func GenerateSection(reg *registry.Registry) (string, error) {
	topics, err := reg.AllTopics()
	if err != nil {
		return "", fmt.Errorf("fetching topics: %w", err)
	}

	tags, err := reg.AllTags()
	if err != nil {
		return "", fmt.Errorf("fetching tags: %w", err)
	}

	scopes, err := reg.AllScopes()
	if err != nil {
		return "", fmt.Errorf("fetching scopes: %w", err)
	}

	packages, err := reg.AllPackages()
	if err != nil {
		return "", fmt.Errorf("fetching packages: %w", err)
	}

	var sb strings.Builder

	sb.WriteString(markerStart + "\n")
	sb.WriteString("## Skillex\n\n")
	sb.WriteString("This project uses Skillex for skill management. Use the skillex MCP server\n")
	sb.WriteString("if available (preferred), otherwise use the CLI commands below.\n\n")

	sb.WriteString("### MCP (preferred)\n\n")
	sb.WriteString("If the `skillex` MCP server is connected, use it directly:\n\n")
	sb.WriteString("- Use the `skillex_query` tool with parameters: path, topic, tags, package, format.\n")
	sb.WriteString("- Browse available skills through MCP resource discovery.\n\n")

	sb.WriteString("### CLI (fallback)\n\n")
	sb.WriteString("If MCP is not available, query skills via the command line:\n\n")
	sb.WriteString("```\n")
	sb.WriteString("  skillex query --path <filepath>\n")
	sb.WriteString("  skillex query --topic <topic> --tags <tags>\n")
	sb.WriteString("  skillex query --package <package>\n")
	sb.WriteString("  skillex query --path <glob> --topic <topic> --format content\n")
	sb.WriteString("```\n\n")

	if len(scopes) > 0 {
		sb.WriteString("### Available scopes\n\n")
		for _, scope := range scopes {
			sb.WriteString(fmt.Sprintf("  - %s\n", scope))
		}
		sb.WriteString("\n")
	}

	if len(topics) > 0 {
		sb.WriteString("### Available topics\n\n")
		sb.WriteString("  ")
		sb.WriteString(strings.Join(topics, ", "))
		sb.WriteString("\n\n")
	}

	if len(tags) > 0 {
		sb.WriteString("### Available tags\n\n")
		sb.WriteString("  ")
		sb.WriteString(strings.Join(tags, ", "))
		sb.WriteString("\n\n")
	}

	if len(packages) > 0 {
		sb.WriteString("### Packages with skills\n\n")
		for _, p := range packages {
			version := p.Version
			if version == "" {
				version = "unknown"
			}
			sb.WriteString(fmt.Sprintf("  %s (%s) — %d public, %d private\n",
				p.Name, version, p.Public, p.Private))
		}
		sb.WriteString("\n")
	}

	sb.WriteString(markerEnd + "\n")

	return sb.String(), nil
}

// UpdateFile writes (or updates) the skillex section in the AGENTS.md file.
// If the file does not exist, it creates it.
// If it does exist, it replaces the content between markers.
func UpdateFile(agentsPath string, section string) error {
	var existing string
	data, err := os.ReadFile(agentsPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("reading AGENTS.md: %w", err)
	}
	if err == nil {
		existing = string(data)
	}

	updated := replaceSection(existing, section)

	if err := os.MkdirAll(filepath.Dir(agentsPath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(agentsPath, []byte(updated), 0o644)
}

// replaceSection replaces the content between markers, or appends if not found.
func replaceSection(existing, section string) string {
	startIdx := strings.Index(existing, markerStart)
	endIdx := strings.Index(existing, markerEnd)

	if startIdx == -1 || endIdx == -1 {
		// Markers not found — append
		if existing != "" && !strings.HasSuffix(existing, "\n") {
			existing += "\n"
		}
		return existing + "\n" + section
	}

	before := existing[:startIdx]
	after := existing[endIdx+len(markerEnd):]
	if strings.HasPrefix(after, "\n") {
		after = after[1:]
	}

	return before + section + after
}

// DefaultContent returns the initial AGENTS.md content for a new repo.
func DefaultContent() string {
	return "# AGENTS\n\nThis file documents how to work in this repository.\n\n"
}
