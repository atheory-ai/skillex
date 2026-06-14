package packs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadValidPack(t *testing.T) {
	dir := t.TempDir()
	writePackTestFile(t, filepath.Join(dir, "docker.md"), "# Docker\n")
	writePackTestFile(t, filepath.Join(dir, Filename), `name: docker
version: 1.0.0
description: Docker guidance.
skills:
  - file: docker.md
    activate-when:
      files-present:
        - Dockerfile
    scope: subtree
`)

	pack, err := Load(filepath.Join(dir, Filename))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if pack.Manifest.Name != "docker" {
		t.Fatalf("pack name = %q, want docker", pack.Manifest.Name)
	}
	if len(pack.Manifest.Skills) != 1 {
		t.Fatalf("skills = %d, want 1", len(pack.Manifest.Skills))
	}
}

func TestLoadInvalidPackReportsIssues(t *testing.T) {
	dir := t.TempDir()
	writePackTestFile(t, filepath.Join(dir, Filename), `name: ""
skills:
  - file: ../outside.md
    activate-when: {}
    scope: ghost
`)

	_, err := Load(filepath.Join(dir, Filename))
	if err == nil {
		t.Fatal("Load() error = nil, want validation error")
	}
	msg := err.Error()
	for _, want := range []string{"name is required", "file must be a relative path", "files-present", "scope"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("validation error %q missing %q", msg, want)
		}
	}
}

func writePackTestFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%s): %v", path, err)
	}
}
