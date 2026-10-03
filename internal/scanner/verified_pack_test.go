package scanner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/atheory-ai/skillex/internal/packs"
)

func TestVerifiedPackReadsFrozenArchiveSnapshot(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "usage.md")
	trusted := []byte("---\nname: Authenticated skill\ntopics: [verified]\n---\nOriginal signed guidance\n")
	pack := &packs.Pack{Dir: dir, VerifiedFiles: map[string][]byte{"usage.md": trusted, "usage.test.md": []byte("# Tests: usage.md\n\n## Validation: authentic\n\nPrompt: Original signed prompt\nSuccess criteria:\n  - Original criterion\n")}}
	if err := os.WriteFile(file, []byte("Unsigned replacement"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := &Scanner{}
	files, err := s.readPackSkillWithScopes(pack, file, ".skillex/packs/example@1.0.0/usage.md", []string{"**"})
	if err != nil || len(files) != 1 || !strings.Contains(files[0].Body, "Original signed guidance") || strings.Contains(files[0].Body, "Unsigned") {
		t.Fatalf("scanner reopened mutable skill: %v; %#v", err, files)
	}
	tests, err := s.readPackSkillWithScopes(pack, filepath.Join(dir, "usage.test.md"), ".skillex/packs/example@1.0.0/usage.test.md", nil)
	if err != nil || len(tests) != 1 || !strings.Contains(string(tests[0].VerifiedContent), "Original signed prompt") {
		t.Fatalf("tests must retain verified bytes without reopening source: %v; %#v", err, tests)
	}
}

func TestRuleCannotReadUnverifiedInstalledGuidance(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".skillex", "packs", "example@1.0.0")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "usage.md"), []byte("Unsigned guidance"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	s := &Scanner{}
	for _, file := range []string{".skillex/packs/example@1.0.0/usage.md", ".skillex/other/../packs/example@1.0.0/usage.md"} {
		if _, err := s.readSkillFileWithScopes(file, file, "", "", "repo", "repo", "", "", nil); err == nil {
			t.Fatalf("rule read unverified installed source: %s", file)
		}
	}
}
