package acceptance

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

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
