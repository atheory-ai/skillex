package agents

import (
	"strings"
	"testing"
)

func TestGenerateSectionIsStableBootstrapOnly(t *testing.T) {
	section := GenerateSection()

	for _, required := range []string{
		"skillex_query",
		"skillex_read",
		MCPSetupReminder,
		"before each occurrence",
		"### CLI (fallback)",
		"skillex query --search",
		"Treat update notices as advisory.",
	} {
		if !strings.Contains(section, required) {
			t.Errorf("generated section missing %q:\n%s", required, section)
		}
	}

	for _, legacyHeading := range []string{
		"### Available scopes",
		"### Available topics",
		"### Available tags",
		"### Packages with skills",
	} {
		if strings.Contains(section, legacyHeading) {
			t.Errorf("generated section must not contain legacy inventory %q:\n%s", legacyHeading, section)
		}
	}
}

func TestMCPInstructionsContainRetrievalGuidance(t *testing.T) {
	for _, required := range []string{MCPSetupReminder, "configured tasks", "core ref and revision", "narrow_with", "reconnect MCP"} {
		if !strings.Contains(MCPInstructions, required) {
			t.Errorf("MCP instructions missing %q", required)
		}
	}
}
