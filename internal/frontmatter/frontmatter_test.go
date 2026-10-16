package frontmatter

import "testing"

func TestParseAgentSkillPaths(t *testing.T) {
	content := []byte(`---
name: react-patterns
description: React conventions.
paths:
  - "**/*.tsx"
  - "packages/ui/**/*.ts"
globs: "**/*.jsx"
---

# React
`)
	fm, body, err := Parse(content)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if body != "\n# React\n" {
		t.Fatalf("body = %q", body)
	}
	got := fm.PathGlobs()
	if len(got) != 2 || got[0] != "**/*.tsx" || got[1] != "packages/ui/**/*.ts" {
		t.Fatalf("PathGlobs() = %v", got)
	}
}

func TestParseAgentSkillPathsCommaString(t *testing.T) {
	content := []byte("---\nname: python-style\ndescription: Style.\npaths: \"**/*.py, scripts/**/*.py\"\n---\n\n# Style\n")
	fm, _, err := Parse(content)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	got := fm.PathGlobs()
	if len(got) != 2 || got[0] != "**/*.py" || got[1] != "scripts/**/*.py" {
		t.Fatalf("PathGlobs() = %v", got)
	}
}

func TestParseLegacyGlobsWhenPathsUnset(t *testing.T) {
	content := []byte("---\nname: old\ndescription: Old.\nglobs: \"*.md\"\n---\n\n# Old\n")
	fm, _, err := Parse(content)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	got := fm.PathGlobs()
	if len(got) != 1 || got[0] != "*.md" {
		t.Fatalf("PathGlobs() = %v", got)
	}
}
