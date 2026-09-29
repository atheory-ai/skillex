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
		"### CLI (fallback)",
		"skillex query --search",
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
