package scanner

import (
	"path/filepath"
	"testing"

	"github.com/atheory-ai/skillex/internal/config"
)

func TestScannerProjectPackActivatesWithFilesPresent(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "Dockerfile"), "FROM scratch\n")
	writeFile(t, filepath.Join(root, "skillex", "pack.yaml"), `name: docker
version: 1.0.0
skills:
  - file: docker.md
    activate-when:
      files-present:
        - Dockerfile
    scope: repo
`)
	writeFile(t, filepath.Join(root, "skillex", "docker.md"), `---
name: Docker
description: Docker guidance.
topics: [docker]
tags: []
---

# Docker
`)
	writeFile(t, filepath.Join(root, "skillex", "docker.test.md"), `# Tests: docker.md

## Validation: Basic
Prompt: "How should I build this Dockerfile?"
Success criteria:
  - Mentions Docker guidance
`)

	sc := NewWithResolvers(root, &config.Config{Version: 4}, true, nil)
	result, err := sc.Scan()
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if len(result.Errors) > 0 {
		t.Fatalf("Scan() result errors = %v", result.Errors)
	}
	if len(result.RepoSkills) != 2 {
		t.Fatalf("RepoSkills = %d, want 2", len(result.RepoSkills))
	}

	got := result.RepoSkills[0]
	if got.SourceType != "pack" {
		t.Fatalf("SourceType = %q, want pack", got.SourceType)
	}
	if len(got.ExplicitScopes) != 1 || got.ExplicitScopes[0] != "**" {
		t.Fatalf("ExplicitScopes = %v, want [**]", got.ExplicitScopes)
	}
	if !result.RepoSkills[1].IsTest {
		t.Fatalf("second pack file should be test, got %#v", result.RepoSkills[1])
	}
}

func TestScannerProjectPackDoesNotActivateWithoutMatchingFiles(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "skillex", "pack.yaml"), `name: docker
version: 1.0.0
skills:
  - file: docker.md
    activate-when:
      files-present:
        - Dockerfile
`)
	writeFile(t, filepath.Join(root, "skillex", "docker.md"), "# Docker\n")

	sc := NewWithResolvers(root, &config.Config{Version: 4}, true, nil)
	result, err := sc.Scan()
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if len(result.Errors) > 0 {
		t.Fatalf("Scan() result errors = %v", result.Errors)
	}
	if len(result.RepoSkills) != 0 {
		t.Fatalf("RepoSkills = %d, want 0", len(result.RepoSkills))
	}
}
