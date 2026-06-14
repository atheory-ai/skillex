package acceptance

import (
	"os"
	"path/filepath"
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
