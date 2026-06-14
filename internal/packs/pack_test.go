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

func TestProjectManifestPathsFindsSupportedLocations(t *testing.T) {
	root := t.TempDir()
	writePackTestFile(t, filepath.Join(root, "skillex", "root.md"), "# Root\n")
	writePackTestFile(t, filepath.Join(root, "skillex", Filename), `name: root
skills:
  - file: root.md
    activate-when:
      files-present:
        - Dockerfile
`)
	writePackTestFile(t, filepath.Join(root, "skillex", "packs", "docker", "docker.md"), "# Docker\n")
	writePackTestFile(t, filepath.Join(root, "skillex", "packs", "docker", Filename), `name: docker
skills:
  - file: docker.md
    activate-when:
      files-present:
        - Dockerfile
`)

	paths := ProjectManifestPaths(root)
	if len(paths) != 2 {
		t.Fatalf("ProjectManifestPaths() = %v, want 2 paths", paths)
	}
	if paths[0] != filepath.Join(root, "skillex", Filename) {
		t.Fatalf("first path = %q, want root pack first", paths[0])
	}
	if paths[1] != filepath.Join(root, "skillex", "packs", "docker", Filename) {
		t.Fatalf("second path = %q, want nested pack", paths[1])
	}
}

func TestActivateProjectReturnsMatchedSkills(t *testing.T) {
	root := t.TempDir()
	writePackTestFile(t, filepath.Join(root, "services", "api", "Dockerfile"), "FROM scratch\n")
	writePackTestFile(t, filepath.Join(root, "skillex", "docker.md"), "# Docker\n")
	writePackTestFile(t, filepath.Join(root, "skillex", Filename), `name: docker
skills:
  - file: docker.md
    activate-when:
      files-present:
        - Dockerfile
    scope: directory
`)

	activated, errs := ActivateProject(root)
	if len(errs) > 0 {
		t.Fatalf("ActivateProject() errors = %v", errs)
	}
	if len(activated) != 1 {
		t.Fatalf("ActivateProject() activated = %d, want 1", len(activated))
	}
	if activated[0].Pack.Manifest.Name != "docker" {
		t.Fatalf("pack name = %q, want docker", activated[0].Pack.Manifest.Name)
	}
	if got, want := activated[0].Scopes, []string{"services/api/*"}; !sameStrings(got, want) {
		t.Fatalf("scopes = %v, want %v", got, want)
	}
}

func TestActivateSkillSupportsFilesMatching(t *testing.T) {
	root := t.TempDir()
	writePackTestFile(t, filepath.Join(root, "services", "api", "main.ts"), "export {}\n")
	writePackTestFile(t, filepath.Join(root, "services", "worker", "main.ts"), "export {}\n")

	scopes, err := ActivateSkill(root, SkillRef{
		ActivateWhen: ActivateWhen{
			FilesMatching: []string{"**/*.ts"},
		},
		Scope: "nearest-ancestor",
	})
	if err != nil {
		t.Fatalf("ActivateSkill() error = %v", err)
	}
	want := []string{"services/api/**", "services/worker/**"}
	if !sameStrings(scopes, want) {
		t.Fatalf("scopes = %v, want %v", scopes, want)
	}
}

func TestActivateSkillMatchingFilesCanUseSeparateFilePatterns(t *testing.T) {
	root := t.TempDir()
	writePackTestFile(t, filepath.Join(root, "package.json"), `{"dependencies":{"next":"latest"}}`)
	writePackTestFile(t, filepath.Join(root, "app", "page.tsx"), "export default function Page() { return null }\n")
	writePackTestFile(t, filepath.Join(root, "app", "route.ts"), "export async function GET() {}\n")

	scopes, err := ActivateSkill(root, SkillRef{
		ActivateWhen: ActivateWhen{
			FilesPresent: []string{"package.json"},
		},
		Scope: "matching-files",
		Files: []string{"**/*.tsx"},
	})
	if err != nil {
		t.Fatalf("ActivateSkill() error = %v", err)
	}
	want := []string{"app/page.tsx"}
	if !sameStrings(scopes, want) {
		t.Fatalf("scopes = %v, want %v", scopes, want)
	}
}
func TestMatchRepoFilesSkipsGeneratedDirectories(t *testing.T) {
	root := t.TempDir()
	writePackTestFile(t, filepath.Join(root, "Dockerfile"), "FROM scratch\n")
	writePackTestFile(t, filepath.Join(root, "node_modules", "pkg", "Dockerfile"), "FROM scratch\n")
	writePackTestFile(t, filepath.Join(root, ".skillex", "Dockerfile"), "FROM scratch\n")

	matches, err := MatchRepoFiles(root, "Dockerfile")
	if err != nil {
		t.Fatalf("MatchRepoFiles() error = %v", err)
	}
	if got, want := matches, []string{"Dockerfile"}; !sameStrings(got, want) {
		t.Fatalf("matches = %v, want %v", got, want)
	}
}

func TestScopeForMatch(t *testing.T) {
	tests := []struct {
		name  string
		match string
		scope string
		want  []string
	}{
		{name: "repo", match: "services/api/Dockerfile", scope: "repo", want: []string{"**"}},
		{name: "directory", match: "services/api/Dockerfile", scope: "directory", want: []string{"services/api/*"}},
		{name: "matching files", match: "services/api/main.ts", scope: "matching-files", want: []string{"services/api/main.ts"}},
		{name: "nearest ancestor", match: "services/api/Dockerfile", scope: "nearest-ancestor", want: []string{"services/api/**"}},
		{name: "subtree", match: "services/api/Dockerfile", scope: "subtree", want: []string{"services/api/**"}},
		{name: "default root", match: "Dockerfile", scope: "", want: []string{"**"}},
		{name: "directory root", match: "Dockerfile", scope: "directory", want: []string{"*"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ScopeForMatch(tt.match, tt.scope); !sameStrings(got, tt.want) {
				t.Fatalf("ScopeForMatch() = %v, want %v", got, tt.want)
			}
		})
	}
}

func sameStrings(a []string, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
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
