package watch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/atheory-ai/skillex/internal/registry"
)

func TestWatcherUpdatesSkillsUnderKnownRoots(t *testing.T) {
	root := t.TempDir()
	landIt := filepath.Join(root, ".cursor", "skills", "land-it", "SKILL.md")
	writeSkill(t, landIt, "land-it", "Ship the branch.")
	writeFile(t, strings.TrimSuffix(landIt, ".md")+".test.md", "# Tests: SKILL.md\n\n## Validation: Ship\nPrompt: \"Ship it\"\nSuccess criteria:\n  - Ships\n")

	reg := openRegistry(t, root)
	w := New(Config{Root: root, Registry: reg, Debounce: 40 * time.Millisecond, OnError: func(err error) {
		if strings.Contains(err.Error(), "parsing frontmatter") {
			return
		}
		t.Errorf("watch error: %v", err)
	}})
	if err := w.Arm(); err != nil {
		t.Fatalf("Arm: %v", err)
	}
	t.Cleanup(w.Close)

	got := waitSkill(t, reg, ".cursor/skills/land-it/SKILL.md", "Ship the branch.")
	if len(got.Scopes) != 1 || got.Scopes[0] != "**" {
		t.Fatalf("scopes = %v", got.Scopes)
	}
	scenarios, err := reg.GetTestScenarios(got.ID)
	if err != nil || len(scenarios) != 1 || scenarios[0].Name != "Ship" {
		t.Fatalf("scenarios = %#v, %v", scenarios, err)
	}

	writeSkill(t, landIt, "land-it", "Ship it twice.")
	waitSkill(t, reg, ".cursor/skills/land-it/SKILL.md", "Ship it twice.")

	review := filepath.Join(root, ".cursor", "skills", "review", "SKILL.md")
	writeSkill(t, review, "review", "Review the pull request.")
	waitSkill(t, reg, ".cursor/skills/review/SKILL.md", "Review the pull request.")

	if err := os.Remove(review); err != nil {
		t.Fatal(err)
	}
	waitAbsent(t, reg, ".cursor/skills/review/SKILL.md")

	writeFile(t, landIt, "---\nname: [\n---\n\n# broken\n")
	time.Sleep(300 * time.Millisecond)
	if skill, err := reg.GetSkillByPath(".cursor/skills/land-it/SKILL.md"); err != nil || skill == nil || !strings.Contains(skill.Content, "Ship it twice.") {
		t.Fatalf("malformed write replaced skill: %#v, %v", skill, err)
	}

	deploy := filepath.Join(root, "apps", "web", ".claude", "skills", "deploy-web", "SKILL.md")
	writeSkill(t, deploy, "deploy-web", "Deploy the web app.")
	time.Sleep(500 * time.Millisecond)
	if skill, err := reg.GetSkillByPath("apps/web/.claude/skills/deploy-web/SKILL.md"); err != nil || skill != nil {
		t.Fatalf("new root indexed without refresh: %#v, %v", skill, err)
	}

	if _, err := reg.InsertSkill(registry.Skill{
		Path:       "skills/poke.md",
		Content:    "# poke\n",
		Visibility: "repo",
		SourceType: "repo",
	}); err != nil {
		t.Fatal(err)
	}
	w.BeforeRequest()
	waitSkill(t, reg, "apps/web/.claude/skills/deploy-web/SKILL.md", "Deploy the web app.")
	if skill, err := reg.GetSkillByPath("skills/poke.md"); err != nil || skill == nil {
		t.Fatalf("poke skill = %#v, %v", skill, err)
	}

	writeSkill(t, deploy, "deploy-web", "Deploy the web app again.")
	waitSkill(t, reg, "apps/web/.claude/skills/deploy-web/SKILL.md", "Deploy the web app again.")
}

func TestWatcherRearmsAfterIdle(t *testing.T) {
	root := t.TempDir()
	landIt := filepath.Join(root, ".cursor", "skills", "land-it", "SKILL.md")
	writeSkill(t, landIt, "land-it", "Ship the branch.")

	reg := openRegistry(t, root)
	w := New(Config{
		Root:        root,
		Registry:    reg,
		IdleTimeout: 150 * time.Millisecond,
		Debounce:    40 * time.Millisecond,
		OnError:     failOnError(t),
	})
	if err := w.Arm(); err != nil {
		t.Fatalf("Arm: %v", err)
	}
	t.Cleanup(w.Close)
	waitSkill(t, reg, ".cursor/skills/land-it/SKILL.md", "Ship the branch.")

	deadline := time.Now().Add(2 * time.Second)
	for w.Armed() && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if w.Armed() {
		t.Fatal("watches stayed armed after the idle timeout")
	}

	writeSkill(t, landIt, "land-it", "Edited while idle.")
	time.Sleep(300 * time.Millisecond)
	skill, err := reg.GetSkillByPath(".cursor/skills/land-it/SKILL.md")
	if err != nil || skill == nil || !strings.Contains(skill.Content, "Ship the branch.") {
		t.Fatalf("idle edit reached the index: %#v, %v", skill, err)
	}

	w.BeforeRequest()
	waitSkill(t, reg, ".cursor/skills/land-it/SKILL.md", "Edited while idle.")
}

func openRegistry(t *testing.T, root string) *registry.Registry {
	t.Helper()
	reg, err := registry.Open(filepath.Join(root, ".skillex", "index.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { reg.Close() })
	return reg
}

func writeSkill(t *testing.T, path, name, description string) {
	t.Helper()
	writeFile(t, path, "---\nname: "+name+"\ndescription: "+description+"\n---\n\n"+description+"\n")
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func waitSkill(t *testing.T, reg *registry.Registry, path, content string) *registry.Skill {
	t.Helper()
	var last *registry.Skill
	waitUntil(t, path, func() bool {
		skill, err := reg.GetSkillByPath(path)
		if err != nil || skill == nil {
			return false
		}
		last = skill
		return strings.Contains(skill.Content, content)
	})
	return last
}

func waitAbsent(t *testing.T, reg *registry.Registry, path string) {
	t.Helper()
	waitUntil(t, path, func() bool {
		skill, err := reg.GetSkillByPath(path)
		return err == nil && skill == nil
	})
}

func waitUntil(t *testing.T, label string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", label)
}

func failOnError(t *testing.T) func(error) {
	t.Helper()
	return func(err error) {
		t.Errorf("watch error: %v", err)
	}
}
