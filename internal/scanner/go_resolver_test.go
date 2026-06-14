package scanner

import (
	"path/filepath"
	"testing"

	"github.com/atheory-ai/skillex/internal/config"
)

func TestGoResolverDependenciesParseRequireDirectives(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), `module example.com/app

go 1.22

require (
	github.com/gin-gonic/gin v1.10.0
	golang.org/x/text v0.14.0 // indirect
)
`)

	resolver := NewGoResolver()
	boundary, ok, errs := resolver.DetectBoundary(root, ".")
	if !ok || len(errs) > 0 {
		t.Fatalf("DetectBoundary() ok=%v errs=%v", ok, errs)
	}

	deps, errs := resolver.Dependencies(*boundary, DependencyModeDev)
	if len(errs) > 0 {
		t.Fatalf("Dependencies() errs=%v", errs)
	}
	if len(deps) != 2 {
		t.Fatalf("deps = %d, want 2", len(deps))
	}
	if got := depVersion(deps, "github.com/gin-gonic/gin"); got != "v1.10.0" {
		t.Fatalf("gin version = %q, want v1.10.0", got)
	}
	if !deps[0].Direct {
		t.Fatalf("first dep Direct = false, want true")
	}
	if deps[1].Direct {
		t.Fatalf("second dep Direct = true, want false for indirect")
	}
	if deps[0].Source != DependencySourceGo {
		t.Fatalf("Source = %q, want %q", deps[0].Source, DependencySourceGo)
	}
}

func TestGoResolverResolvePackageRootsLocalReplaceAndVendor(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), `module example.com/app

require (
	example.com/local v1.0.0
	example.com/vendor v1.0.0
	example.com/missing v1.0.0
)

replace example.com/local => ./local-module
`)
	writeFile(t, filepath.Join(root, "local-module", "go.mod"), "module example.com/local\n")
	writeFile(t, filepath.Join(root, "vendor", "example.com", "vendor", "go.mod"), "module example.com/vendor\n")

	resolver := NewGoResolver()
	boundary, ok, errs := resolver.DetectBoundary(root, ".")
	if !ok || len(errs) > 0 {
		t.Fatalf("DetectBoundary() ok=%v errs=%v", ok, errs)
	}
	deps, errs := resolver.Dependencies(*boundary, DependencyModeDev)
	if len(errs) > 0 {
		t.Fatalf("Dependencies() errs=%v", errs)
	}

	roots, errs := resolver.ResolvePackageRoots(*boundary, deps)
	if len(errs) > 0 {
		t.Fatalf("ResolvePackageRoots() errs=%v", errs)
	}
	if len(roots) != 2 {
		t.Fatalf("roots = %d, want 2", len(roots))
	}
	if roots[0].RootRel != "local-module" {
		t.Fatalf("first RootRel = %q, want local-module", roots[0].RootRel)
	}
	if roots[1].RootRel != filepath.ToSlash(filepath.Join("vendor", "example.com", "vendor")) {
		t.Fatalf("second RootRel = %q, want vendor module", roots[1].RootRel)
	}
}

func TestGoResolverExportsPackManifest(t *testing.T) {
	root := t.TempDir()
	pkgRoot := filepath.Join(root, "local-module")
	writeFile(t, filepath.Join(pkgRoot, "skillex", "pack.yaml"), `name: go-module
skills:
  - file: usage.md
    activate-when:
      dependency-declared:
        - source: go-module
          name: example.com/local
    scope: boundary
`)

	exports, errs := NewGoResolver().Exports(PackageRoot{RootAbs: pkgRoot})
	if len(errs) > 0 {
		t.Fatalf("Exports() errs=%v", errs)
	}
	if len(exports) != 1 {
		t.Fatalf("exports = %#v, want one pack manifest", exports)
	}
	if exports[0].Format != SkillExportFormatPackManifest {
		t.Fatalf("Format = %q, want pack manifest", exports[0].Format)
	}
}

func TestGoResolverMissingGoModDoesNotApply(t *testing.T) {
	root := t.TempDir()
	boundary, ok, errs := NewGoResolver().DetectBoundary(root, ".")
	if ok || boundary != nil || len(errs) > 0 {
		t.Fatalf("DetectBoundary() boundary=%#v ok=%v errs=%v, want no match", boundary, ok, errs)
	}
}

func TestGoResolverMalformedGoModReportsError(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), `module example.com/app
require broken
`)

	boundary, ok, errs := NewGoResolver().DetectBoundary(root, ".")
	if ok {
		t.Fatal("DetectBoundary() ok=true, want false")
	}
	if boundary != nil {
		t.Fatalf("boundary = %#v, want nil", boundary)
	}
	if len(errs) != 1 {
		t.Fatalf("errs = %d, want 1", len(errs))
	}
}

func TestScannerIndexesGoModuleShippedPack(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), `module example.com/app

require example.com/with-skillex v1.0.0

replace example.com/with-skillex => ./with-skillex
`)
	writeFile(t, filepath.Join(root, "with-skillex", "go.mod"), "module example.com/with-skillex\n")
	writeFile(t, filepath.Join(root, "with-skillex", "skillex", "usage.md"), `---
name: Go Usage
description: Use the Go module.
topics: [go-module]
tags: []
---

# Go Usage
`)
	writeFile(t, filepath.Join(root, "with-skillex", "skillex", "pack.yaml"), `name: example-go-module
detectors:
  with-skillex:
    matches:
      - dependency:
          source: go-module
          name: example.com/with-skillex
skills:
  - file: usage.md
    activate-when:
      detector: with-skillex
    scope: boundary
`)

	cfg := &config.Config{
		Version: 4,
		Rules: []config.Rule{
			{Scope: "**", DependencyBoundary: "."},
		},
	}
	sc := NewWithResolvers(root, cfg, true, []Resolver{NewGoResolver()})
	result, err := sc.Scan()
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if len(result.Errors) > 0 {
		t.Fatalf("Scan() result errors = %v", result.Errors)
	}
	if len(result.DepSkills) != 1 {
		t.Fatalf("DepSkills = %d, want 1", len(result.DepSkills))
	}
	got := result.DepSkills[0]
	if got.PackageName != "example.com/with-skillex" {
		t.Fatalf("PackageName = %q, want example.com/with-skillex", got.PackageName)
	}
	if got.PackageVersion != "v1.0.0" {
		t.Fatalf("PackageVersion = %q, want v1.0.0", got.PackageVersion)
	}
	if got.SourceType != "pack" {
		t.Fatalf("SourceType = %q, want pack", got.SourceType)
	}
	if len(got.ExplicitScopes) != 1 || got.ExplicitScopes[0] != "**" {
		t.Fatalf("ExplicitScopes = %v, want [**]", got.ExplicitScopes)
	}
}
