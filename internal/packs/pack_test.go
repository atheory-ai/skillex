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

func TestLoadMCPOnlyPackAndActivateSuggestion(t *testing.T) {
	root := t.TempDir()
	writePackTestFile(t, filepath.Join(root, "go.mod"), "module example.com/app\n")
	writePackTestFile(t, filepath.Join(root, "skillex", Filename), `name: issue-tools
version: 1.0.0
mcp-servers:
  - ref: io.example/issues
    version: 2.1.0
    relationship: suggested
    activate-when:
      detector: go
    scope: repo
    capabilities:
      prefer:
        - issues.create
`)

	pack, err := Load(filepath.Join(root, "skillex", Filename))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(pack.Manifest.MCPServers) != 1 {
		t.Fatalf("MCP servers = %d, want 1", len(pack.Manifest.MCPServers))
	}
	activated, errs := ActivateProjectMCPServers(root)
	if len(errs) > 0 {
		t.Fatalf("ActivateProjectMCPServers() errors = %v", errs)
	}
	if len(activated) != 1 || activated[0].Server.Ref != "io.example/issues" {
		t.Fatalf("activated = %#v", activated)
	}
	if got, want := activated[0].Scopes, []string{"**"}; !sameStrings(got, want) {
		t.Fatalf("scopes = %v, want %v", got, want)
	}
}

func TestPackMCPServerCannotContainExecutionOrAuthConfiguration(t *testing.T) {
	dir := t.TempDir()
	writePackTestFile(t, filepath.Join(dir, Filename), `name: unsafe
mcp-servers:
  - ref: io.example/issues
    version: 1.0.0
    relationship: suggested
    command: node
    auth-profile: secret
    activate-when:
      detector: go
`)

	_, err := Load(filepath.Join(dir, Filename))
	if err == nil {
		t.Fatal("Load() error = nil, want unknown execution/auth fields rejected")
	}
	if !strings.Contains(err.Error(), "field command not found") && !strings.Contains(err.Error(), "field auth-profile not found") {
		t.Fatalf("Load() error = %v, want forbidden field rejection", err)
	}
}

func TestPackMCPServerRequiresExactSuggestedVersion(t *testing.T) {
	dir := t.TempDir()
	writePackTestFile(t, filepath.Join(dir, Filename), `name: invalid
mcp-servers:
  - ref: io.example/issues
    version: ^1.0.0
    relationship: required
    activate-when:
      detector: go
`)

	_, err := Load(filepath.Join(dir, Filename))
	if err == nil || !strings.Contains(err.Error(), "exact version") || !strings.Contains(err.Error(), "must be suggested") {
		t.Fatalf("Load() error = %v", err)
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

func TestActivateProjectWithPackDefinedDetector(t *testing.T) {
	root := t.TempDir()
	writePackTestFile(t, filepath.Join(root, "go.mod"), "module example.com/app\n")
	writePackTestFile(t, filepath.Join(root, "cmd", "app", "main.go"), "package main\n")
	writePackTestFile(t, filepath.Join(root, "skillex", "go.md"), "# Go\n")
	writePackTestFile(t, filepath.Join(root, "skillex", Filename), `name: go-pack
detectors:
  go-app:
    matches:
      - file:
          path: go.mod
skills:
  - file: go.md
    activate-when:
      detector: go-app
    scope: matching-files
    files:
      - "**/*.go"
`)

	activated, errs := ActivateProject(root)
	if len(errs) > 0 {
		t.Fatalf("ActivateProject() errors = %v", errs)
	}
	if len(activated) != 1 {
		t.Fatalf("ActivateProject() activated = %d, want 1", len(activated))
	}
	if got, want := activated[0].Scopes, []string{"cmd/app/main.go"}; !sameStrings(got, want) {
		t.Fatalf("scopes = %v, want %v", got, want)
	}
}

func TestActivateProjectCanUseDetectorFromAnotherLoadedPack(t *testing.T) {
	root := t.TempDir()
	writePackTestFile(t, filepath.Join(root, "go.mod"), "module example.com/app\n")
	writePackTestFile(t, filepath.Join(root, "skillex", "packs", "detectors", "detector.md"), "# Detector\n")
	writePackTestFile(t, filepath.Join(root, "skillex", "packs", "detectors", Filename), `name: detectors
detectors:
  go-app:
    matches:
      - file:
          path: go.mod
skills:
  - file: detector.md
    activate-when:
      files-present:
        - never-matches
`)
	writePackTestFile(t, filepath.Join(root, "skillex", "packs", "skills", "go.md"), "# Go\n")
	writePackTestFile(t, filepath.Join(root, "skillex", "packs", "skills", Filename), `name: skills
skills:
  - file: go.md
    activate-when:
      detector: go-app
    scope: repo
`)

	activated, errs := ActivateProject(root)
	if len(errs) > 0 {
		t.Fatalf("ActivateProject() errors = %v", errs)
	}
	if len(activated) != 1 {
		t.Fatalf("ActivateProject() activated = %d, want 1", len(activated))
	}
	if activated[0].Pack.Manifest.Name != "skills" {
		t.Fatalf("activated pack = %q, want skills", activated[0].Pack.Manifest.Name)
	}
}

func TestActivateProjectReportsUnknownDetector(t *testing.T) {
	root := t.TempDir()
	writePackTestFile(t, filepath.Join(root, "skillex", "go.md"), "# Go\n")
	writePackTestFile(t, filepath.Join(root, "skillex", Filename), `name: go-pack
skills:
  - file: go.md
    activate-when:
      detector: missing
    scope: repo
`)

	_, errs := ActivateProject(root)
	if len(errs) == 0 {
		t.Fatal("ActivateProject() errors = nil, want unknown detector error")
	}
	if !strings.Contains(errs[0].Error(), `unknown detector "missing"`) {
		t.Fatalf("error = %v, want unknown detector", errs[0])
	}
}

func TestDetectorRegistryConflicts(t *testing.T) {
	registry, err := NewDetectorRegistry()
	if err != nil {
		t.Fatalf("NewDetectorRegistry() error = %v", err)
	}
	conflictingGo := Detectors{
		"go": {Matches: []DetectorMatch{{File: &FileCondition{Path: "other.mod"}}}},
	}
	if err := registry.RegisterAll(conflictingGo, "go-pack", false); err == nil {
		t.Fatal("RegisterAll() error = nil, want built-in conflict")
	}

	registry = &DetectorRegistry{}
	first := Detectors{
		"gin": {Matches: []DetectorMatch{{Dependency: &DependencyCondition{Source: "go-module", Name: "github.com/gin-gonic/gin"}}}},
	}
	second := Detectors{
		"gin": {Matches: []DetectorMatch{{Dependency: &DependencyCondition{Source: "go-module", Name: "example.com/other/gin"}}}},
	}
	if err := registry.RegisterAll(first, "gin-pack-a", false); err != nil {
		t.Fatalf("RegisterAll(first) error = %v", err)
	}
	if err := registry.RegisterAll(first, "gin-pack-b", false); err != nil {
		t.Fatalf("identical RegisterAll() error = %v", err)
	}
	if err := registry.RegisterAll(second, "gin-pack-c", false); err == nil {
		t.Fatal("RegisterAll(second) error = nil, want pack conflict")
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

func TestActivateSkillWithDependencyBackedDetector(t *testing.T) {
	root := t.TempDir()
	pack := &Pack{
		Manifest: Manifest{
			Name: "gin-pack",
			Detectors: Detectors{
				"gin": {Matches: []DetectorMatch{{Dependency: &DependencyCondition{Source: "go-module", Name: "github.com/gin-gonic/gin"}}}},
			},
		},
	}
	ctx, errs := ContextForPack(root, pack, ActivationContext{
		BoundaryRel: "services/api",
		Dependency: DependencyFact{
			Source:  "go-module",
			Name:    "github.com/gin-gonic/gin",
			Version: "v1.10.0",
		},
	})
	if len(errs) > 0 {
		t.Fatalf("ContextForPack() errors = %v", errs)
	}

	scopes, err := ActivateSkillWithContext(root, SkillRef{
		ActivateWhen: ActivateWhen{Detector: "gin"},
		Scope:        "boundary",
	}, ctx)
	if err != nil {
		t.Fatalf("ActivateSkillWithContext() error = %v", err)
	}
	if got, want := scopes, []string{"services/api/**"}; !sameStrings(got, want) {
		t.Fatalf("scopes = %v, want %v", got, want)
	}
}

func TestActivateSkillWithDependencyDeclaredScopesToBoundary(t *testing.T) {
	root := t.TempDir()

	scopes, err := ActivateSkillWithContext(root, SkillRef{
		ActivateWhen: ActivateWhen{
			DependencyDeclared: []DependencyCondition{
				{Source: "npm-package", Name: "next", Version: "15.0.0"},
			},
		},
		Scope: "boundary",
	}, ActivationContext{
		BoundaryRel: "apps/web",
		Dependency: DependencyFact{
			Source:  "npm-package",
			Name:    "next",
			Version: "15.0.0",
		},
	})
	if err != nil {
		t.Fatalf("ActivateSkillWithContext() error = %v", err)
	}
	want := []string{"apps/web/**"}
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
		{name: "boundary cannot map file match", match: "services/api/Dockerfile", scope: "boundary", want: nil},
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

func TestActivationAllRequiresEveryGateAndPreservesFileScopes(t *testing.T) {
	root := t.TempDir()
	writePackTestFile(t, filepath.Join(root, "services", "api", "Dockerfile"), "FROM scratch\n")
	when := ActivateWhen{All: []ActivateWhen{
		{FilesPresent: []string{"services/api/Dockerfile"}},
		{All: []ActivateWhen{{Detector: "go"}, {DependencyDeclared: []DependencyCondition{{Source: "go-module", Name: "example.com/api"}}}}},
	}}
	ctx := ActivationContext{DetectorKnown: map[string]bool{"go": true}, DetectorActive: map[string]bool{"go": true}, Dependency: DependencyFact{Source: "go-module", Name: "example.com/api"}, BoundaryRel: "services/api"}
	for _, server := range []bool{false, true} {
		activate := func(ctx ActivationContext) ([]string, error) {
			if server {
				return ActivateMCPServerWithContext(root, MCPServerRef{ActivateWhen: when, Scope: "subtree"}, ctx)
			}
			return ActivateSkillWithContext(root, SkillRef{ActivateWhen: when, Scope: "subtree"}, ctx)
		}
		scopes, err := activate(ctx)
		if err != nil || !sameStrings(scopes, []string{"services/api/**"}) {
			t.Fatalf("server=%v: scopes=%v err=%v", server, scopes, err)
		}
		absentDetector := ctx
		absentDetector.DetectorActive = map[string]bool{"go": false}
		scopes, err = activate(absentDetector)
		if err != nil || len(scopes) != 0 {
			t.Fatalf("false detector: scopes=%v err=%v", scopes, err)
		}
		absentDependency := ctx
		absentDependency.Dependency = DependencyFact{}
		scopes, err = activate(absentDependency)
		if err != nil || len(scopes) != 0 {
			t.Fatalf("missing dependency: scopes=%v err=%v", scopes, err)
		}
	}
	if err := os.Remove(filepath.Join(root, "services", "api", "Dockerfile")); err != nil {
		t.Fatal(err)
	}
	scopes, err := ActivateSkillWithContext(root, SkillRef{ActivateWhen: when}, ctx)
	if err != nil || len(scopes) != 0 {
		t.Fatalf("missing file: scopes=%v err=%v", scopes, err)
	}
}

func TestActivationCompositionValidation(t *testing.T) {
	cases := []struct {
		name string
		when ActivateWhen
		want string
	}{
		{"empty all", ActivateWhen{All: []ActivateWhen{}}, "at least one"},
		{"mixed", ActivateWhen{All: []ActivateWhen{{Detector: "go"}}, Detector: "go"}, "cannot be combined"},
		{"empty child", ActivateWhen{All: []ActivateWhen{{}}}, "all[0]"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ActivateSkill(t.TempDir(), SkillRef{ActivateWhen: tc.when})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error=%v, want %s", err, tc.want)
			}
		})
	}
	when := ActivateWhen{Detector: "go"}
	for i := 0; i < 34; i++ {
		when = ActivateWhen{All: []ActivateWhen{when}}
	}
	if err := validateActivation(when, "activate-when", 0); err == nil || !strings.Contains(err.Error(), "nesting depth") {
		t.Fatalf("error=%v", err)
	}
}

func TestActivationLegacyAlternativesRemainAdditive(t *testing.T) {
	root := t.TempDir()
	writePackTestFile(t, filepath.Join(root, "Dockerfile"), "FROM scratch\n")
	scopes, err := ActivateSkillWithContext(root, SkillRef{ActivateWhen: ActivateWhen{FilesPresent: []string{"Dockerfile"}, Detector: "go"}, Scope: "repo"}, ActivationContext{DetectorKnown: map[string]bool{"go": true}})
	if err != nil || !sameStrings(scopes, []string{"**"}) {
		t.Fatalf("scopes=%v err=%v", scopes, err)
	}
}
