package scanner

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNodeResolverDependenciesRespectMode(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "package.json"), `{
		"dependencies": {
			"prod-only": "1.0.0",
			"both": "1.0.0"
		},
		"devDependencies": {
			"dev-only": "2.0.0",
			"both": "2.0.0"
		}
	}`)

	resolver := NewNodeResolver()
	boundary, ok, errs := resolver.DetectBoundary(root, ".")
	if !ok || len(errs) > 0 {
		t.Fatalf("DetectBoundary() ok=%v errs=%v", ok, errs)
	}

	prodDeps, errs := resolver.Dependencies(*boundary, DependencyModeProd)
	if len(errs) > 0 {
		t.Fatalf("prod Dependencies() errs=%v", errs)
	}
	if got := depVersion(prodDeps, "prod-only"); got != "1.0.0" {
		t.Fatalf("prod-only version = %q, want 1.0.0", got)
	}
	if got := depVersion(prodDeps, "dev-only"); got != "" {
		t.Fatalf("dev-only present in prod deps with version %q", got)
	}
	if got := depVersion(prodDeps, "both"); got != "1.0.0" {
		t.Fatalf("both prod version = %q, want 1.0.0", got)
	}

	devDeps, errs := resolver.Dependencies(*boundary, DependencyModeDev)
	if len(errs) > 0 {
		t.Fatalf("dev Dependencies() errs=%v", errs)
	}
	if got := depVersion(devDeps, "dev-only"); got != "2.0.0" {
		t.Fatalf("dev-only version = %q, want 2.0.0", got)
	}
	if got := depVersion(devDeps, "both"); got != "2.0.0" {
		t.Fatalf("both dev version = %q, want 2.0.0", got)
	}
}

func TestNodeResolverResolvePackageRootsSkipsMissingAndHandlesScopedPackages(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "package.json"), `{
		"dependencies": {
			"missing": "1.0.0",
			"@scope/pkg": "2.0.0"
		}
	}`)
	writeFile(t, filepath.Join(root, "node_modules", "@scope", "pkg", "package.json"), `{
		"name": "@scope/pkg",
		"version": "2.1.0"
	}`)

	resolver := NewNodeResolver()
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
	if len(roots) != 1 {
		t.Fatalf("resolved roots = %d, want 1", len(roots))
	}
	if roots[0].Dependency.Name != "@scope/pkg" {
		t.Fatalf("resolved package name = %q, want @scope/pkg", roots[0].Dependency.Name)
	}
	if roots[0].Dependency.Version != "2.1.0" {
		t.Fatalf("resolved package version = %q, want 2.1.0", roots[0].Dependency.Version)
	}
	if roots[0].RootRel != filepath.ToSlash(filepath.Join("node_modules", "@scope", "pkg")) {
		t.Fatalf("RootRel = %q", roots[0].RootRel)
	}
}

func TestNodeResolverExportsSkillexTrueAndCustomPath(t *testing.T) {
	root := t.TempDir()
	resolver := NewNodeResolver()

	defaultRoot := filepath.Join(root, "node_modules", "default")
	writeFile(t, filepath.Join(defaultRoot, "package.json"), `{
		"name": "default",
		"version": "1.0.0",
		"skillex": true
	}`)

	customRoot := filepath.Join(root, "node_modules", "custom")
	writeFile(t, filepath.Join(customRoot, "package.json"), `{
		"name": "custom",
		"version": "1.0.0",
		"skillex": { "path": "docs/skillex" }
	}`)

	defaultExports, errs := resolver.Exports(PackageRoot{RootAbs: defaultRoot})
	if len(errs) > 0 {
		t.Fatalf("default Exports() errs=%v", errs)
	}
	if len(defaultExports) != 1 || defaultExports[0].Path != filepath.Join(defaultRoot, "skillex") {
		t.Fatalf("default exports = %#v", defaultExports)
	}

	customExports, errs := resolver.Exports(PackageRoot{RootAbs: customRoot})
	if len(errs) > 0 {
		t.Fatalf("custom Exports() errs=%v", errs)
	}
	if len(customExports) != 1 || customExports[0].Path != filepath.Join(customRoot, "docs", "skillex") {
		t.Fatalf("custom exports = %#v", customExports)
	}
}

func TestNodeResolverExportsSkipsMissingFalseNullAndInvalidConfig(t *testing.T) {
	root := t.TempDir()
	resolver := NewNodeResolver()

	cases := map[string]string{
		"missing": `{
			"name": "missing",
			"version": "1.0.0"
		}`,
		"false": `{
			"name": "false",
			"version": "1.0.0",
			"skillex": false
		}`,
		"null": `{
			"name": "null",
			"version": "1.0.0",
			"skillex": null
		}`,
		"invalid": `{
			"name": "invalid",
			"version": "1.0.0",
			"skillex": { "enabled": true }
		}`,
	}

	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			pkgRoot := filepath.Join(root, "node_modules", name)
			writeFile(t, filepath.Join(pkgRoot, "package.json"), content)

			exports, errs := resolver.Exports(PackageRoot{RootAbs: pkgRoot})
			if len(errs) > 0 {
				t.Fatalf("Exports() errs=%v", errs)
			}
			if len(exports) != 0 {
				t.Fatalf("exports = %#v, want none", exports)
			}
		})
	}
}

func TestNodeResolverFindsAncestorNodeModules(t *testing.T) {
	root := t.TempDir()
	boundaryRel := filepath.Join("packages", "app")
	writeFile(t, filepath.Join(root, boundaryRel, "package.json"), `{
		"dependencies": {
			"shared": "1.0.0"
		}
	}`)
	writeFile(t, filepath.Join(root, "node_modules", "shared", "package.json"), `{
		"name": "shared",
		"version": "1.2.3"
	}`)

	resolver := NewNodeResolver()
	boundary, ok, errs := resolver.DetectBoundary(root, boundaryRel)
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
	if len(roots) != 1 {
		t.Fatalf("resolved roots = %d, want 1", len(roots))
	}
	if roots[0].RootRel != filepath.ToSlash(filepath.Join("node_modules", "shared")) {
		t.Fatalf("RootRel = %q", roots[0].RootRel)
	}
}

func TestNodeResolverMalformedBoundaryPackageJSONReportsError(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "package.json"), `{"dependencies":`)

	resolver := NewNodeResolver()
	boundary, ok, errs := resolver.DetectBoundary(root, ".")
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

func depVersion(deps []Dependency, name string) string {
	for _, dep := range deps {
		if dep.Name == name {
			return dep.Version
		}
	}
	return ""
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%s): %v", path, err)
	}
}
