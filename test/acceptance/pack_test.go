package acceptance

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/atheory-ai/skillex/internal/filelock"
	"github.com/atheory-ai/skillex/internal/packregistry"
	"github.com/atheory-ai/skillex/test/helpers"
)

func TestPack_ProjectLocalFilesPresentActivation(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")

	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte("FROM scratch\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "skillex"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "skillex", "pack.yaml"), []byte(`name: docker
version: 1.0.0
description: Docker guidance.
skills:
  - file: docker.md
    activate-when:
      files-present:
        - Dockerfile
    scope: repo
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "skillex", "docker.md"), []byte(`---
name: Docker
description: Dockerfile guidance.
topics: [docker]
tags: [containers]
---

# Docker

Use Docker guidance from the activated pack.
`), 0o644); err != nil {
		t.Fatal(err)
	}

	res := helpers.Run(t, dir, "refresh")
	if res.ExitCode != 0 {
		t.Fatalf("refresh failed (exit %d): %s", res.ExitCode, res.Stderr)
	}

	skills := queryResults(t, dir, "--topic", "docker", "--format", "summary")
	helpers.AssertSkillPresent(t, skills, "docker.md")
	if len(skills) != 1 {
		t.Fatalf("docker topic results = %d, want 1", len(skills))
	}
	if skills[0].SourceType != "pack" {
		t.Fatalf("SourceType = %q, want pack", skills[0].SourceType)
	}
}

func TestPack_ProjectLocalPackWithoutMatchDoesNotActivate(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")

	if err := os.MkdirAll(filepath.Join(dir, "skillex"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "skillex", "pack.yaml"), []byte(`name: docker
version: 1.0.0
skills:
  - file: docker.md
    activate-when:
      files-present:
        - Dockerfile
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "skillex", "docker.md"), []byte("# Docker\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	res := helpers.Run(t, dir, "refresh")
	if res.ExitCode != 0 {
		t.Fatalf("refresh failed (exit %d): %s", res.ExitCode, res.Stderr)
	}

	resp, _ := helpers.RunQueryJSON(t, dir, "query", "--topic", "docker", "--format", "summary")
	if resp.Type != "no_match" {
		t.Fatalf("response type = %q, want no_match", resp.Type)
	}
}

func TestPack_NodeDependencyShippedPackActivation(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")

	appPkgPath := filepath.Join(dir, "packages", "app-a", "package.json")
	data, err := os.ReadFile(appPkgPath)
	if err != nil {
		t.Fatal(err)
	}
	updated := strings.Replace(string(data), `"@test/utils": "workspace:*"`, `"@test/utils": "workspace:*",
    "with-pack": "1.0.0"`, 1)
	if err := os.WriteFile(appPkgPath, []byte(updated), 0o644); err != nil {
		t.Fatal(err)
	}

	pkgRoot := filepath.Join(dir, "packages", "app-a", "node_modules", "with-pack")
	if err := os.MkdirAll(filepath.Join(pkgRoot, "skillex"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkgRoot, "package.json"), []byte(`{
  "name": "with-pack",
  "version": "1.0.0",
  "skillex": true
}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkgRoot, "skillex", "pack.yaml"), []byte(`name: with-pack
version: 1.0.0
skills:
  - file: usage.md
    activate-when:
      dependency-declared:
        - source: npm-package
          name: with-pack
    scope: boundary
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkgRoot, "skillex", "usage.md"), []byte(`---
name: With Pack
description: Guidance shipped from a dependency pack.
topics: [with-pack]
tags: [dependency-pack]
---

# With Pack

Use package-shipped pack guidance.
`), 0o644); err != nil {
		t.Fatal(err)
	}

	res := helpers.Run(t, dir, "refresh")
	if res.ExitCode != 0 {
		t.Fatalf("refresh failed (exit %d): %s", res.ExitCode, res.Stderr)
	}

	skills := queryResults(t, dir, "--path", "packages/app-a/src/index.ts", "--topic", "with-pack", "--format", "summary")
	helpers.AssertSkillPresent(t, skills, "usage.md")
	if len(skills) != 1 {
		t.Fatalf("with-pack results = %d, want 1", len(skills))
	}
	if skills[0].SourceType != "pack" {
		t.Fatalf("SourceType = %q, want pack", skills[0].SourceType)
	}
	if skills[0].PackageName != "with-pack" {
		t.Fatalf("PackageName = %q, want with-pack", skills[0].PackageName)
	}
}

func TestPack_GoFixtureActivatesProjectAndModulePacks(t *testing.T) {
	dir := helpers.CopyFixture(t, "go-basic")

	res := helpers.Run(t, dir, "refresh")
	if res.ExitCode != 0 {
		t.Fatalf("refresh failed (exit %d): %s", res.ExitCode, res.Stderr)
	}

	projectSkills := queryResults(t, dir, "--path", "main.go", "--topic", "go", "--format", "summary")
	helpers.AssertSkillPresent(t, projectSkills, "modules.md")
	if len(projectSkills) != 1 {
		t.Fatalf("go project results = %d, want 1", len(projectSkills))
	}
	if projectSkills[0].SourceType != "pack" {
		t.Fatalf("project SourceType = %q, want pack", projectSkills[0].SourceType)
	}

	moduleSkills := queryResults(t, dir, "--path", "main.go", "--package", "example.com/with-skillex", "--format", "summary")
	helpers.AssertSkillPresent(t, moduleSkills, "usage.md")
	if len(moduleSkills) != 1 {
		t.Fatalf("go module results = %d, want 1", len(moduleSkills))
	}
	if moduleSkills[0].SourceType != "pack" {
		t.Fatalf("module SourceType = %q, want pack", moduleSkills[0].SourceType)
	}
	if moduleSkills[0].PackageName != "example.com/with-skillex" {
		t.Fatalf("PackageName = %q, want example.com/with-skillex", moduleSkills[0].PackageName)
	}
}

func TestPack_AllActivationRefreshAndMCPParity(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	packDir := filepath.Join(dir, "skillex")
	if err := os.MkdirAll(packDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `name: composed
skills:
  - file: composed.md
    activate-when:
      all:
        - files-present: [Dockerfile]
        - files-present: [go.mod]
    scope: repo
`
	if err := os.WriteFile(filepath.Join(packDir, "pack.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(packDir, "composed.md"), []byte("---\nname: Composed deployment\ndescription: Guidance for composed activation.\ntopics: [composed]\n---\n# Composed Deployment\nUse the combined workflow.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte("FROM scratch\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	refresh := func() {
		t.Helper()
		res := helpers.Run(t, dir, "refresh")
		if res.ExitCode != 0 {
			t.Fatalf("refresh: %s", res.Stderr)
		}
	}
	refresh()
	resp, _ := helpers.RunQueryJSON(t, dir, "query", "--topic", "composed")
	if resp.Type != "no_match" {
		t.Fatalf("one gate response=%s", resp.Type)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/app\n\ngo 1.23\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	refresh()
	skills := queryResults(t, dir, "--topic", "composed", "--path", "src/main.go")
	helpers.AssertSkillPresent(t, skills, "composed.md")
	client := helpers.StartMCPServer(t, dir)
	defer client.Close()
	result, err := client.CallToolText("skillex_query", map[string]interface{}{"topic": "composed", "path": "src/main.go"})
	if err != nil || !strings.Contains(result, "composed.md") {
		t.Fatalf("MCP missing composed skill: result=%s err=%v", result, err)
	}
	if err := os.Remove(filepath.Join(dir, "go.mod")); err != nil {
		t.Fatal(err)
	}
	refresh()
	resp, _ = helpers.RunQueryJSON(t, dir, "query", "--topic", "composed")
	if resp.Type != "no_match" {
		t.Fatalf("removed gate response=%s", resp.Type)
	}
}

// Replay the published installation evidence so the golden project exercises
// the normal offline verifier without depending on GitHub.
func installPublishedExample(t *testing.T, dir string, legacy bool) packregistry.Entry {
	t.Helper()
	fixture := filepath.Join("..", "..", "cli", "testdata", "pack-registry")
	archivePath := filepath.Join(fixture, "archive.tar.gz")
	if legacy {
		fixture = filepath.Join("..", "..", "internal", "verify", "testdata")
		archivePath = filepath.Join("..", "..", "internal", "packregistry", "testdata", "example.tar.gz")
	}
	read := func(file string) []byte {
		t.Helper()
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	manifest := read(filepath.Join(fixture, "manifest.json"))
	var catalog packregistry.Manifest
	if err := json.Unmarshal(manifest, &catalog); err != nil {
		t.Fatal(err)
	}
	entry := catalog.Packs[0].Entry
	pack := packregistry.Prepared{Archive: read(archivePath), Lock: packregistry.Lock{
		Entry: entry, Registry: catalog.Registry, ManifestURL: packregistry.DefaultURL,
		Manifest: manifest, Bundle: read(filepath.Join(fixture, "manifest.json.bundle")),
	}}
	if err := pack.Install(dir); err != nil {
		t.Fatal(err)
	}
	return entry
}

func TestPack_RemoveVerifiedInstallation(t *testing.T) {
	dir := helpers.CopyFixture(t, "go-basic")
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"name":"fixture"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	entry := installPublishedExample(t, dir, false)
	id := entry.Name + "@" + entry.Version
	packs := filepath.Join(dir, ".skillex", "packs")
	installed := filepath.Join(packs, id)
	if res := helpers.Run(t, dir, "refresh"); res.ExitCode != 0 {
		t.Fatal(res.Stderr)
	}
	client := helpers.StartMCPServer(t, dir)
	check := func(topic, suffix string, present bool) {
		t.Helper()
		cli, _ := helpers.RunQueryJSON(t, dir, "query", "--topic", topic, "--path", "main.go")
		body, err := client.CallToolText("skillex_query", map[string]any{"topic": topic, "path": "main.go"})
		var mcp helpers.QueryResponse
		if err != nil || json.Unmarshal([]byte(body), &mcp) != nil {
			t.Fatalf("MCP lookup: %s, %v", body, err)
		}
		for _, result := range []helpers.QueryResponse{cli, mcp} {
			if present {
				helpers.AssertSkillPresent(t, result.Results, suffix)
			} else if result.Type != "no_match" || len(result.Results) != 0 {
				t.Fatalf("removed skill still discoverable: %+v", result)
			}
		}
	}
	check("example", "usage.md", true)

	// Both released versions carry authentic evidence. A bare name must never
	// silently select one; removing the older version preserves the active one.
	legacy := installPublishedExample(t, dir, true)
	if r := helpers.Run(t, dir, "pack", "remove", entry.Name); r.ExitCode == 0 || !strings.Contains(r.Stderr, "ambiguous") || !strings.Contains(r.Stderr, legacy.Version) || !strings.Contains(r.Stderr, entry.Version) {
		t.Fatalf("ambiguous selection: %+v", r)
	}
	if r := helpers.Run(t, dir, "pack", "remove", legacy.Name+"@"+legacy.Version); r.ExitCode != 0 {
		t.Fatal(r.Stderr)
	}
	check("example", "usage.md", true)

	// A retry cannot take over staged files while an earlier removal finishes.
	lease, err := os.OpenFile(filepath.Join(packs, ".removing", ".lock"), os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := filelock.TryExclusive(lease); err != nil {
		t.Fatal(err)
	}
	if r := helpers.Run(t, dir, "pack", "remove", id); r.ExitCode == 0 || !strings.Contains(r.Stderr, "another pack removal") {
		t.Fatalf("concurrent removal: %+v", r)
	}
	if err := lease.Close(); err != nil {
		t.Fatal(err)
	}

	// A local edit is never unlinked by removal.
	note := filepath.Join(installed, "my-notes.md")
	if err := os.WriteFile(note, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if r := helpers.Run(t, dir, "pack", "remove", id); r.ExitCode == 0 || !strings.Contains(r.Stderr, "unexpected installed file") {
		t.Fatalf("accepted unverified installation: %+v", r)
	}
	if err := os.Remove(note); err != nil {
		t.Fatal(err)
	}

	// Force a write failure after the old index has been cleared within the transaction.
	db, err := sql.Open("sqlite", filepath.Join(dir, ".skillex", "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec("CREATE TRIGGER reject_rebuild BEFORE INSERT ON skills BEGIN SELECT RAISE(ABORT, 'test rebuild failure'); END"); err != nil {
		t.Fatal(err)
	}
	if r := helpers.Run(t, dir, "pack", "remove", id); r.ExitCode == 0 || !strings.Contains(r.Stderr, "test rebuild failure") {
		t.Fatalf("expected rebuild failure: %+v", r)
	}
	if _, err := os.Stat(installed); err != nil {
		t.Fatal("removal failed to restore installation", err)
	}
	check("example", "usage.md", true)
	if _, err := db.Exec("DROP TRIGGER reject_rebuild"); err != nil {
		t.Fatal(err)
	}

	var result struct {
		Pack    packregistry.Entry
		Removed bool
	}
	if r := helpers.RunJSON(t, dir, &result, "pack", "remove", entry.Name); r.ExitCode != 0 || !result.Removed || result.Pack != entry {
		t.Fatalf("remove: %+v, %+v", r, result)
	}
	if _, err := os.Stat(installed); !os.IsNotExist(err) {
		t.Fatalf("installation still present: %v", err)
	}
	check("example", "usage.md", false)
	check("go", "modules.md", true)
	if r := helpers.Run(t, dir, "pack", "list", "--json"); r.ExitCode != 0 || strings.TrimSpace(r.Stdout) != "[]" {
		t.Fatalf("installed packs: %+v", r)
	}

	// A retry after interruption between staging and commit finishes normally.
	installPublishedExample(t, dir, false)
	if err := os.Rename(installed, filepath.Join(packs, ".removing", id)); err != nil {
		t.Fatal(err)
	}
	if r := helpers.Run(t, dir, "pack", "remove", id); r.ExitCode != 0 || !strings.Contains(r.Stdout, id) {
		t.Fatalf("resume: %+v", r)
	}
}
