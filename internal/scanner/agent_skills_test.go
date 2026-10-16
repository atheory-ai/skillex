package scanner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/atheory-ai/skillex/internal/config"
)

func TestScanProjectAgentSkillsDiscoversToolDirectories(t *testing.T) {
	root := t.TempDir()
	for _, parent := range projectAgentSkillParents {
		writeFile(t, filepath.Join(root, parent, "skills", "from-"+parent[1:], "SKILL.md"), agentSkillMarkdown("from-"+parent[1:], "Use for "+parent))
	}
	writeFile(t, filepath.Join(root, ".cursor", "skills", "shipping", "land-it", "SKILL.md"), agentSkillMarkdown("", "Ship the branch."))
	writeFile(t, filepath.Join(root, ".cursor", "skills", "shipping", "land-it", "references", "guide.md"), "# not a skill\n")
	writeFile(t, filepath.Join(root, ".cursor", "skills", "shipping", "land-it", "SKILL.test.md"), "# Tests: SKILL.md\n\n## Validation: Ship\nPrompt: \"Ship it\"\nSuccess criteria:\n  - Ships\n")
	writeFile(t, filepath.Join(root, "node_modules", "pkg", ".cursor", "skills", "hidden", "SKILL.md"), agentSkillMarkdown("hidden", "Dependency skill."))
	writeFile(t, filepath.Join(root, "docs", "notes.md"), "# notes\n")

	skills := scanAgentSkills(t, root)
	byPath := indexSkills(skills)

	for _, parent := range projectAgentSkillParents {
		rel := parent + "/skills/from-" + parent[1:] + "/SKILL.md"
		got, ok := byPath[rel]
		if !ok {
			t.Errorf("missing %s", rel)
			continue
		}
		if got.SourceType != "agent" || got.Visibility != "repo" {
			t.Errorf("%s source=%s visibility=%s", rel, got.SourceType, got.Visibility)
		}
		if len(got.ExplicitScopes) != 1 || got.ExplicitScopes[0] != "**" {
			t.Errorf("%s scopes = %v", rel, got.ExplicitScopes)
		}
	}

	landIt, ok := byPath[".cursor/skills/shipping/land-it/SKILL.md"]
	if !ok {
		t.Fatal("missing categorized skill")
	}
	if landIt.Frontmatter.Name != "land-it" {
		t.Fatalf("directory name fallback = %q", landIt.Frontmatter.Name)
	}
	if _, ok := byPath[".cursor/skills/shipping/land-it/references/guide.md"]; ok {
		t.Fatal("indexed a reference file as a skill")
	}
	if _, ok := byPath["node_modules/pkg/.cursor/skills/hidden/SKILL.md"]; ok {
		t.Fatal("indexed a dependency skill")
	}

	testFile, ok := byPath[".cursor/skills/shipping/land-it/SKILL.test.md"]
	if !ok || !testFile.IsTest || testFile.TestFor != ".cursor/skills/shipping/land-it/SKILL.md" {
		t.Fatalf("sibling test = %#v", testFile)
	}
}

func TestScanProjectAgentSkillsScopesNestedDirectoriesAndPaths(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "apps", "web", ".claude", "skills", "deploy-web", "SKILL.md"), agentSkillMarkdown("deploy-web", "Deploy the web app."))
	writeFile(t, filepath.Join(root, ".cursor", "skills", "react-patterns", "SKILL.md"), `---
name: react-patterns
description: React conventions.
paths:
  - "**/*.tsx"
  - "packages/ui/**/*.ts"
globs: "*.jsx"
---

# React
`)
	writeFile(t, filepath.Join(root, "services", "api", ".github", "skills", "handlers", "SKILL.md"), `---
name: handlers
description: HTTP handlers.
paths: "**/*.go, docs/**/*.md"
---

# Handlers
`)
	writeFile(t, filepath.Join(root, ".gemini", "skills", "legacy", "SKILL.md"), `---
name: legacy
description: Old filter.
globs: "*.md"
---

# Legacy
`)

	byPath := indexSkills(scanAgentSkills(t, root))

	deploy := byPath["apps/web/.claude/skills/deploy-web/SKILL.md"]
	if len(deploy.ExplicitScopes) != 1 || deploy.ExplicitScopes[0] != "apps/web/**" {
		t.Fatalf("nested scopes = %v", deploy.ExplicitScopes)
	}
	react := byPath[".cursor/skills/react-patterns/SKILL.md"]
	if strings.Join(react.ExplicitScopes, ",") != "**/*.tsx,*.tsx,packages/ui/**/*.ts,packages/ui/*.ts" {
		t.Fatalf("paths scopes = %v", react.ExplicitScopes)
	}
	handlers := byPath["services/api/.github/skills/handlers/SKILL.md"]
	if strings.Join(handlers.ExplicitScopes, ",") != "services/api/**/*.go,services/api/*.go" {
		t.Fatalf("nested paths scopes = %v", handlers.ExplicitScopes)
	}
	legacy := byPath[".gemini/skills/legacy/SKILL.md"]
	if len(legacy.ExplicitScopes) != 1 || legacy.ExplicitScopes[0] != "*.md" {
		t.Fatalf("legacy globs = %v", legacy.ExplicitScopes)
	}
}

func TestDiscoverAgentSkillsReportsRoots(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, ".cursor", "skills", "land-it", "SKILL.md"), agentSkillMarkdown("land-it", "Ship."))
	writeFile(t, filepath.Join(root, "apps", "web", ".claude", "skills", "deploy-web", "SKILL.md"), agentSkillMarkdown("deploy-web", "Deploy."))
	if err := os.MkdirAll(filepath.Join(root, ".github", "skills"), 0o755); err != nil {
		t.Fatal(err)
	}

	skills, roots, errs := DiscoverAgentSkills(root)
	if len(errs) > 0 {
		t.Fatalf("DiscoverAgentSkills errors = %v", errs)
	}
	if _, ok := indexSkills(skills)[".cursor/skills/land-it/SKILL.md"]; !ok {
		t.Fatal("missing land-it")
	}
	want := []string{
		filepath.Join(root, ".cursor", "skills"),
		filepath.Join(root, ".github", "skills"),
		filepath.Join(root, "apps", "web", ".claude", "skills"),
	}
	if strings.Join(roots, "\n") != strings.Join(want, "\n") {
		t.Fatalf("roots = %v, want %v", roots, want)
	}
}

func TestScanProjectAgentSkillsReflectsLaterWrites(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, ".cursor", "skills", "land-it", "SKILL.md"), agentSkillMarkdown("land-it", "Ship the branch."))

	first := indexSkills(scanAgentSkills(t, root))
	if _, ok := first[".cursor/skills/land-it/SKILL.md"]; !ok {
		t.Fatal("first scan missed land-it")
	}

	writeFile(t, filepath.Join(root, ".agents", "skills", "review", "SKILL.md"), agentSkillMarkdown("review", "Review the diff."))
	if err := os.Remove(filepath.Join(root, ".cursor", "skills", "land-it", "SKILL.md")); err != nil {
		t.Fatal(err)
	}

	second := indexSkills(scanAgentSkills(t, root))
	if _, ok := second[".cursor/skills/land-it/SKILL.md"]; ok {
		t.Fatal("removed skill still indexed")
	}
	if _, ok := second[".agents/skills/review/SKILL.md"]; !ok {
		t.Fatal("newly written skill was not indexed")
	}
}

func TestScanProjectAgentSkillsReportsMalformedFrontmatter(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, ".cursor", "skills", "broken", "SKILL.md"), "---\nname: [\n---\n\n# Broken\n")
	writeFile(t, filepath.Join(root, ".claude", "skills", "ok", "SKILL.md"), agentSkillMarkdown("ok", "Still indexed."))

	sc := NewWithResolvers(root, &config.Config{Version: 4}, true, nil)
	result, err := sc.Scan()
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if len(result.Errors) == 0 {
		t.Fatal("expected a frontmatter error")
	}
	byPath := indexSkills(result.RepoSkills)
	if _, ok := byPath[".claude/skills/ok/SKILL.md"]; !ok {
		t.Fatal("malformed skill aborted the rest of the scan")
	}
	if _, ok := byPath[".cursor/skills/broken/SKILL.md"]; ok {
		t.Fatal("indexed a skill with malformed frontmatter")
	}
}

func scanAgentSkills(t *testing.T, root string) []SkillFile {
	t.Helper()
	sc := NewWithResolvers(root, &config.Config{Version: 4}, true, nil)
	result, err := sc.Scan()
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if len(result.Errors) > 0 {
		t.Fatalf("Scan() errors = %v", result.Errors)
	}
	return result.RepoSkills
}

func indexSkills(skills []SkillFile) map[string]SkillFile {
	byPath := map[string]SkillFile{}
	for _, skill := range skills {
		byPath[skill.RelPath] = skill
	}
	return byPath
}

func agentSkillMarkdown(name, description string) string {
	return "---\nname: " + name + "\ndescription: " + description + "\n---\n\n# " + name + "\n"
}
