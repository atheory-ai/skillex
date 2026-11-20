package agents

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	markerStart = "<!-- skillex:start -->"
	markerEnd   = "<!-- skillex:end -->"
)

// MCPSetupReminder is surfaced by MCP and in the generated agent bootstrap.
const MCPSetupReminder = "At the start of each conversation, call `skillex_query` with no filters if Skillex setup context is missing."

// MCPInstructions provides the agent-facing guidance shipped by the server.
const MCPInstructions = MCPSetupReminder + "\n\n" +
	"- The setup response includes configured tasks. Use each task's `when` cue to decide when it applies. Reconsider the list as work changes across turns. Before each matching task, use `skillex_read` with its returned ref if the skill is missing from context, and follow it for that occurrence.\n" +
	"- If a lookup returns a core ref and revision, read it with `skillex_read` when that revision's guidance is missing from context or the revision changes. Core guidance gives short cues and skill refs. Apply the cues throughout the conversation.\n" +
	"- Use `source: skillex` to discover bundled skills. Project tasks can also point to bundled skills.\n" +
	"- Query by path, topic, tags, package, or intent to discover additional skills. If `too_broad` is true, use `narrow_with` to refine the query.\n" +
	"- Read one selected ref or section at a time. MCP skill resources provide a table of contents.\n" +
	"- Treat update notices as advisory. Offer a new target once per conversation. Read its upgrade_plan_ref with skillex_read to explain the applicable steps. Keep the ref across installation, get approval before making changes, and reconnect MCP afterward. A project-pinned installation needs a dependency update."

// GenerateSection creates the stable Skillex bootstrap section for AGENTS.md.
func GenerateSection() string { return GenerateSectionWithCommand("skillex") }

// GenerateSectionWithCommand renders persisted project invocation instructions.
func GenerateSectionWithCommand(command string) string {
	var sb strings.Builder

	sb.WriteString(markerStart + "\n")
	sb.WriteString("## Skillex\n\n")
	sb.WriteString("This project uses Skillex for skill management. Use the skillex MCP server\n")
	sb.WriteString("if available (preferred), otherwise use the CLI commands below.\n\n")
	sb.WriteString("### MCP (preferred)\n\n")
	sb.WriteString(MCPSetupReminder + " Follow the setup guidance from Skillex and use `skillex_read` for returned refs.\n\n")

	sb.WriteString("### CLI (fallback)\n\n")
	sb.WriteString("If MCP is not available, query skills via the command line. If the repository documents a local development binary, use it instead of a globally installed release:\n\n")
	sb.WriteString("Run the unfiltered query at the start of each conversation if setup context is missing. If it returns a core ref, use `read --ref` when that revision's guidance is missing or the revision changes. When a core cue applies, read its linked skill if needed. Match tasks using their cues and apply the linked skill before each occurrence. Use `read --ref` when a linked skill is missing from context.\n\n")
	sb.WriteString("Use filtered queries for additional context. Refine broad results before reading a selected skill or section. Treat update notices as advisory.\n\n")
	sb.WriteString("```\n")
	sb.WriteString("  " + command + " query\n")
	sb.WriteString("  " + command + " query --search \"<concepts>\" --json\n")
	sb.WriteString("  " + command + " query --path <filepath> --limit 8 --json\n")
	sb.WriteString("  " + command + " query --topic <topic> --tags <tags> --json\n")
	sb.WriteString("  " + command + " query --source skillex --json\n")
	sb.WriteString("  " + command + " read --ref <ref-from-query> --section <optional-section-id>\n")
	sb.WriteString("```\n\n")

	sb.WriteString(markerEnd + "\n")

	return sb.String()
}

// CheckSetup compares only the managed block against the current project invocation.
func CheckSetup(path, command string) error {
	data, err := os.ReadFile(path)
	repair := fmt.Sprintf("run '%s init --yes'", command)
	if os.IsNotExist(err) {
		return fmt.Errorf("AGENTS.md not found — %s", repair)
	}
	if err != nil {
		return fmt.Errorf("reading AGENTS.md: %w", err)
	}
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	if !strings.Contains(text, markerStart) && !strings.Contains(text, markerEnd) {
		return fmt.Errorf("AGENTS.md is missing its Skillex setup block — %s", repair)
	}
	_, rest, _ := strings.Cut(text, markerStart)
	block, _, closed := strings.Cut(rest, markerEnd)
	if !closed || strings.Count(text, markerStart) != 1 || strings.Count(text, markerEnd) != 1 {
		return fmt.Errorf("AGENTS.md has malformed Skillex markers — restore a single ordered marker pair before running init")
	}
	expected := strings.TrimSuffix(GenerateSectionWithCommand(command), "\n")
	if markerStart+block+markerEnd != expected {
		return fmt.Errorf("AGENTS.md has outdated Skillex setup instructions — %s", repair)
	}
	return nil
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
