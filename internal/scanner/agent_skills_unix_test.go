//go:build unix

package scanner

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/atheory-ai/skillex/internal/config"
)

func TestScanProjectAgentSkillsFollowsInRepoSymlink(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "shared", "land-it", "SKILL.md"), agentSkillMarkdown("land-it", "Ship the branch."))
	link := filepath.Join(root, ".cursor", "skills", "land-it")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "shared", "land-it"), link); err != nil {
		t.Fatal(err)
	}

	byPath := indexSkills(scanAgentSkills(t, root))
	got, ok := byPath[".cursor/skills/land-it/SKILL.md"]
	if !ok {
		t.Fatal("in-repo symlink was not indexed")
	}
	if got.Frontmatter.Name != "land-it" {
		t.Fatalf("name = %q", got.Frontmatter.Name)
	}
	if _, ok := byPath["shared/land-it/SKILL.md"]; ok {
		t.Fatal("indexed the symlink target outside a skill directory")
	}
}

func TestScanProjectAgentSkillsSkipsSymlinkOutsideRepo(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	writeFile(t, filepath.Join(outside, "escaped", "SKILL.md"), agentSkillMarkdown("escaped", "Outside."))
	link := filepath.Join(root, ".cursor", "skills", "escaped")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "escaped"), link); err != nil {
		t.Fatal(err)
	}
	fileLink := filepath.Join(root, ".claude", "skills", "leak", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(fileLink), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "escaped", "SKILL.md"), fileLink); err != nil {
		t.Fatal(err)
	}

	sc := NewWithResolvers(root, &config.Config{Version: 4}, true, nil)
	result, err := sc.Scan()
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	byPath := indexSkills(result.RepoSkills)
	if _, ok := byPath[".cursor/skills/escaped/SKILL.md"]; ok {
		t.Fatal("indexed a skill directory outside the repository")
	}
	if _, ok := byPath[".claude/skills/leak/SKILL.md"]; ok {
		t.Fatal("indexed a skill file outside the repository")
	}
	if len(result.Errors) == 0 {
		t.Fatal("expected an error for the skill file that escapes the repository")
	}
}
