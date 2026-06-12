package scanner

import (
	"path/filepath"
	"testing"

	"github.com/atheory-ai/skillex/internal/config"
)

func TestScannerUsesResolverExports(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "skills", "repo.md"), "# Repo\n")
	writeFile(t, filepath.Join(root, "deps", "example", "skillex", "public", "usage.md"), `---
name: Usage
description: Use the example package.
topics: [example]
tags: []
---

# Usage
`)

	cfg := &config.Config{
		Version: 4,
		Rules: []config.Rule{
			{Scope: "**", Skills: []string{"skills/repo.md"}},
			{Scope: "app/**", DependencyBoundary: "app"},
		},
	}

	resolver := &fakeResolver{
		boundary: Boundary{
			RootRel:     "app",
			RootAbs:     filepath.Join(root, "app"),
			RepoRootAbs: root,
		},
		deps: []Dependency{
			{
				Source:      "fake-package",
				Name:        "example",
				Version:     "1.0.0",
				Direct:      true,
				BoundaryRel: "app",
			},
		},
		roots: []PackageRoot{
			{
				RootRel: filepath.ToSlash(filepath.Join("deps", "example")),
				RootAbs: filepath.Join(root, "deps", "example"),
				Dependency: Dependency{
					Source:      "fake-package",
					Name:        "example",
					Version:     "1.0.0",
					Direct:      true,
					BoundaryRel: "app",
				},
			},
		},
		exports: []SkillExport{
			{
				Path:   filepath.Join(root, "deps", "example", "skillex"),
				Format: SkillExportFormatLegacyDir,
			},
		},
	}

	sc := NewWithResolvers(root, cfg, true, []Resolver{resolver})
	result, err := sc.Scan()
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if len(result.Errors) > 0 {
		t.Fatalf("Scan() result errors = %v", result.Errors)
	}
	if len(result.RepoSkills) != 1 {
		t.Fatalf("repo skills = %d, want 1", len(result.RepoSkills))
	}
	if len(result.DepSkills) != 1 {
		t.Fatalf("dep skills = %d, want 1", len(result.DepSkills))
	}

	got := result.DepSkills[0]
	if got.PackageName != "example" {
		t.Fatalf("PackageName = %q, want example", got.PackageName)
	}
	if got.PackageVersion != "1.0.0" {
		t.Fatalf("PackageVersion = %q, want 1.0.0", got.PackageVersion)
	}
	if got.DependencyBoundary != "app" {
		t.Fatalf("DependencyBoundary = %q, want app", got.DependencyBoundary)
	}
	if got.PackageRoot != filepath.ToSlash(filepath.Join("deps", "example")) {
		t.Fatalf("PackageRoot = %q", got.PackageRoot)
	}
	if got.Visibility != "public" {
		t.Fatalf("Visibility = %q, want public", got.Visibility)
	}
}

type fakeResolver struct {
	boundary Boundary
	deps     []Dependency
	roots    []PackageRoot
	exports  []SkillExport
}

func (r *fakeResolver) Name() string {
	return "fake"
}

func (r *fakeResolver) DetectBoundary(root string, boundaryRel string) (*Boundary, bool, []error) {
	return &r.boundary, true, nil
}

func (r *fakeResolver) Dependencies(boundary Boundary, mode DependencyMode) ([]Dependency, []error) {
	return r.deps, nil
}

func (r *fakeResolver) ResolvePackageRoots(boundary Boundary, deps []Dependency) ([]PackageRoot, []error) {
	return r.roots, nil
}

func (r *fakeResolver) Exports(pkg PackageRoot) ([]SkillExport, []error) {
	return r.exports, nil
}
