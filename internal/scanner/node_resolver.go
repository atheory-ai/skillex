package scanner

import (
	"fmt"
	"path/filepath"
)

// NodeResolver resolves npm-package dependencies from package.json boundaries.
type NodeResolver struct{}

// NewNodeResolver creates the built-in Node resolver.
func NewNodeResolver() *NodeResolver {
	return &NodeResolver{}
}

func (r *NodeResolver) Name() string {
	return "node"
}

func (r *NodeResolver) DetectBoundary(root string, boundaryRel string) (*Boundary, bool, []error) {
	boundaryPath := filepath.Join(root, boundaryRel)
	manifestPath := filepath.Join(boundaryPath, "package.json")
	if _, err := readPackageJSON(manifestPath); err != nil {
		return nil, false, []error{fmt.Errorf("reading boundary package.json at %s: %w", boundaryPath, err)}
	}

	return &Boundary{
		RootRel:     filepath.ToSlash(boundaryRel),
		RootAbs:     boundaryPath,
		RepoRootAbs: root,
		Manifests: []Manifest{
			{
				PathRel: filepath.ToSlash(filepath.Join(boundaryRel, "package.json")),
				Kind:    ManifestKindPackageJSON,
			},
		},
	}, true, nil
}

func (r *NodeResolver) Dependencies(boundary Boundary, mode DependencyMode) ([]Dependency, []error) {
	pkgJSON, err := readPackageJSON(filepath.Join(boundary.RootAbs, "package.json"))
	if err != nil {
		return nil, []error{fmt.Errorf("reading boundary package.json at %s: %w", boundary.RootAbs, err)}
	}

	declared := make(map[string]string, len(pkgJSON.Dependencies)+len(pkgJSON.DevDependencies))
	for name, version := range pkgJSON.Dependencies {
		declared[name] = version
	}
	if mode == DependencyModeDev {
		for name, version := range pkgJSON.DevDependencies {
			declared[name] = version
		}
	}

	deps := make([]Dependency, 0, len(declared))
	for name, version := range declared {
		deps = append(deps, Dependency{
			Source:      DependencySourceNPM,
			Name:        name,
			Version:     version,
			Direct:      true,
			BoundaryRel: boundary.RootRel,
		})
	}
	return deps, nil
}

func (r *NodeResolver) ResolvePackageRoots(boundary Boundary, deps []Dependency) ([]PackageRoot, []error) {
	var roots []PackageRoot
	nmRoot := findNodeModules(boundary.RootAbs)

	for _, dep := range deps {
		pkgRoot := filepath.Join(nmRoot, dep.Name)
		depPkgJSON, err := readPackageJSON(filepath.Join(pkgRoot, "package.json"))
		if err != nil {
			// Preserve existing behavior: not installed or missing package.json is skipped.
			continue
		}

		resolvedDep := dep
		resolvedDep.Name = depPkgJSON.Name
		resolvedDep.Version = depPkgJSON.Version

		rootRel, _ := filepath.Rel(boundary.RepoRootAbs, pkgRoot)
		roots = append(roots, PackageRoot{
			RootAbs:    pkgRoot,
			RootRel:    filepath.ToSlash(rootRel),
			Dependency: resolvedDep,
		})
	}
	return roots, nil
}

func (r *NodeResolver) Exports(pkg PackageRoot) ([]SkillExport, []error) {
	pkgJSON, err := readPackageJSON(filepath.Join(pkg.RootAbs, "package.json"))
	if err != nil {
		return nil, nil
	}

	export := parseSkilexExport(pkgJSON.Skillex)
	if !export.Enabled {
		return nil, nil
	}

	return []SkillExport{
		{
			Path:   filepath.Join(pkg.RootAbs, export.Path),
			Format: SkillExportFormatLegacyDir,
		},
	}, nil
}
