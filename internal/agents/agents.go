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
	sb.WriteString("- Start with `skillex_query` (path, topic, tags, package, search, limit, cursor). It returns bounded discovery summaries, not whole skill files.\n")
	sb.WriteString("- If `too_broad` is true, use its candidate-scoped `narrow_with` facets and suggestions to refine the query.\n")
	sb.WriteString("- Use `skillex_read` with a selected result `ref` and optional section id only after discovery; keep reads bounded.\n")
	sb.WriteString("- MCP resources provide a skill table of contents. Do not bulk-load skill content.\n\n")

	sb.WriteString("### CLI (fallback)\n\n")
	sb.WriteString("If MCP is not available, query skills via the command line. If the repository documents a local development binary, use it instead of a globally installed release:\n\n")
	sb.WriteString("```\n")
	sb.WriteString("  skillex query --search \"<concepts>\"\n")
	sb.WriteString("  skillex query --path <filepath> --limit 8\n")
	sb.WriteString("  skillex query --topic <topic> --tags <tags>\n")
	sb.WriteString("  skillex read --ref <ref-from-query> --section <optional-section-id>\n")
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
	return os.WriteFile(agentsPath, []byte(updated), 0o644) //nolint:gosec // G306: AGENTS.md is user-edited project doc
}

// replaceSection replaces the content between markers, or appends if not found.
func replaceSection(existing, section string) string {
	return replaceMarkedSection(existing, section, markerStart, markerEnd)
}

func replaceMarkedSection(existing, section string, startMarker string, endMarker string) string {
	startIdx := strings.Index(existing, startMarker)
	endIdx := strings.Index(existing, endMarker)

	if startIdx == -1 || endIdx == -1 {
		// Markers not found — append
		if existing != "" && !strings.HasSuffix(existing, "\n") {
			existing += "\n"
		}
		return existing + "\n" + section
	}

	before := existing[:startIdx]
	after := strings.TrimPrefix(existing[endIdx+len(endMarker):], "\n")

	return before + section + after
}

// DefaultContent returns the initial AGENTS.md content for a new repo.
func DefaultContent() string {
	return "# AGENTS\n\nThis file documents how to work in this repository.\n\n"
}
