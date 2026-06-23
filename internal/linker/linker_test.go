package linker

import (
	"testing"

	"github.com/atheory-ai/skillex/internal/config"
	"github.com/atheory-ai/skillex/internal/scanner"
)

// A skill listed under several scope rules is scanned once per rule, so it appears in
// ScanResult.RepoSkills multiple times. Link must emit it once with all scopes merged.
func TestLink_DeduplicatesRepoSkillsAcrossRules(t *testing.T) {
	cfg := &config.Config{
		Rules: []config.Rule{
			{Scope: "components/**", Skills: []string{"skills/a.md"}},
			{Scope: "pages/**", Skills: []string{"skills/a.md"}},
		},
	}
	lnk := New("/root", cfg)

	skillA := scanner.SkillFile{RelPath: "skills/a.md", Visibility: "public", SourceType: "repo"}
	result := &scanner.ScanResult{
		// Scanner produces one entry per rule reference.
		RepoSkills: []scanner.SkillFile{skillA, skillA},
	}

	linked := lnk.Link(result)

	var skills []LinkedSkill
	for _, ls := range linked {
		if !ls.IsTest {
			skills = append(skills, ls)
		}
	}
	if len(skills) != 1 {
		t.Fatalf("expected 1 linked skill, got %d (duplicate entries inflate SkillsAdded)", len(skills))
	}

	wantScopes := map[string]bool{"components/**": true, "pages/**": true}
	if len(skills[0].Scopes) != len(wantScopes) {
		t.Fatalf("expected %d scopes, got %v", len(wantScopes), skills[0].Scopes)
	}
	for _, s := range skills[0].Scopes {
		if !wantScopes[s] {
			t.Errorf("unexpected scope %q", s)
		}
	}
}

func TestLink_DeduplicatesRepoTestFiles(t *testing.T) {
	cfg := &config.Config{
		Rules: []config.Rule{
			{Scope: "components/**", Skills: []string{"skills/a.md"}},
			{Scope: "pages/**", Skills: []string{"skills/a.md"}},
		},
	}
	lnk := New("/root", cfg)

	skillA := scanner.SkillFile{RelPath: "skills/a.md", Visibility: "public", SourceType: "repo"}
	testA := scanner.SkillFile{RelPath: "skills/a.test.md", IsTest: true, TestFor: "skills/a.md", SourceType: "repo"}
	result := &scanner.ScanResult{
		RepoSkills: []scanner.SkillFile{skillA, testA, skillA, testA},
	}

	linked := lnk.Link(result)

	var tests []LinkedSkill
	for _, ls := range linked {
		if ls.IsTest {
			tests = append(tests, ls)
		}
	}
	if len(tests) != 1 {
		t.Fatalf("expected 1 linked test file, got %d (duplicates cause repeated scenario insertion)", len(tests))
	}
}
