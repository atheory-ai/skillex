package scanner

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/atheory-ai/skillex/internal/packs"
)

// GoResolver resolves Go module dependencies from go.mod boundaries.
type GoResolver struct{}

// NewGoResolver creates the built-in Go resolver.
func NewGoResolver() *GoResolver {
	return &GoResolver{}
}

func (r *GoResolver) Name() string {
	return "go"
}

func (r *GoResolver) DetectBoundary(root string, boundaryRel string) (*Boundary, bool, []error) {
	boundaryPath := filepath.Join(root, boundaryRel)
	manifestPath := filepath.Join(boundaryPath, "go.mod")
	if _, err := os.Stat(manifestPath); err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, []error{fmt.Errorf("reading boundary go.mod at %s: %w", boundaryPath, err)}
	}
	if _, err := readGoMod(manifestPath); err != nil {
		return nil, false, []error{fmt.Errorf("reading boundary go.mod at %s: %w", boundaryPath, err)}
	}

	return &Boundary{
		RootRel:     filepath.ToSlash(boundaryRel),
		RootAbs:     boundaryPath,
		RepoRootAbs: root,
		Manifests: []Manifest{
			{
				PathRel: filepath.ToSlash(filepath.Join(boundaryRel, "go.mod")),
				Kind:    ManifestKindGoMod,
			},
		},
	}, true, nil
}

func (r *GoResolver) Dependencies(boundary Boundary, mode DependencyMode) ([]Dependency, []error) {
	mod, err := readGoMod(filepath.Join(boundary.RootAbs, "go.mod"))
	if err != nil {
		return nil, []error{fmt.Errorf("reading boundary go.mod at %s: %w", boundary.RootAbs, err)}
	}

	deps := make([]Dependency, 0, len(mod.Requires))
	for _, req := range mod.Requires {
		deps = append(deps, Dependency{
			Source:      DependencySourceGo,
			Name:        req.Path,
			Version:     req.Version,
			Direct:      !req.Indirect,
			BoundaryRel: boundary.RootRel,
		})
	}
	return deps, nil
}

func (r *GoResolver) ResolvePackageRoots(boundary Boundary, deps []Dependency) ([]PackageRoot, []error) {
	mod, err := readGoMod(filepath.Join(boundary.RootAbs, "go.mod"))
	if err != nil {
		return nil, []error{fmt.Errorf("reading boundary go.mod at %s: %w", boundary.RootAbs, err)}
	}

	replaces := map[string]string{}
	for _, repl := range mod.Replaces {
		replaces[repl.OldPath] = repl.NewPath
	}

	var roots []PackageRoot
	for _, dep := range deps {
		var rootAbs string
		if replacement, ok := replaces[dep.Name]; ok && isLocalGoReplacement(replacement) {
			rootAbs = filepath.Clean(filepath.Join(boundary.RootAbs, replacement))
		} else {
			vendorRoot := filepath.Join(boundary.RootAbs, "vendor", filepath.FromSlash(dep.Name))
			if info, err := os.Stat(vendorRoot); err == nil && info.IsDir() {
				rootAbs = vendorRoot
			}
		}
		if rootAbs == "" {
			continue
		}

		rootRel, _ := filepath.Rel(boundary.RepoRootAbs, rootAbs)
		roots = append(roots, PackageRoot{
			RootAbs:    rootAbs,
			RootRel:    filepath.ToSlash(rootRel),
			Dependency: dep,
		})
	}
	return roots, nil
}

func (r *GoResolver) Exports(pkg PackageRoot) ([]SkillExport, []error) {
	packManifestPath := filepath.Join(pkg.RootAbs, "skillex", packs.Filename)
	if info, err := os.Stat(packManifestPath); err == nil && !info.IsDir() {
		return []SkillExport{
			{
				Path:   packManifestPath,
				Format: SkillExportFormatPackManifest,
			},
		}, nil
	}
	return nil, nil
}

type goModFile struct {
	Requires []goRequire
	Replaces []goReplace
}

type goRequire struct {
	Path     string
	Version  string
	Indirect bool
}

type goReplace struct {
	OldPath string
	NewPath string
}

func readGoMod(path string) (*goModFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	mod, err := parseGoMod(string(data))
	if err != nil {
		return nil, fmt.Errorf("parsing go.mod: %w", err)
	}
	return mod, nil
}

func parseGoMod(content string) (*goModFile, error) {
	mod := &goModFile{}
	lines := strings.Split(content, "\n")
	for i := 0; i < len(lines); i++ {
		line := trimGoModLine(lines[i])
		if line == "" {
			continue
		}
		switch {
		case line == "require (":
			for i++; i < len(lines); i++ {
				rawBlockLine := strings.TrimSpace(lines[i])
				blockLine := trimGoModLine(rawBlockLine)
				if blockLine == ")" {
					break
				}
				if blockLine == "" {
					continue
				}
				req, err := parseGoRequire(rawBlockLine)
				if err != nil {
					return nil, err
				}
				mod.Requires = append(mod.Requires, req)
			}
		case line == "replace (":
			for i++; i < len(lines); i++ {
				blockLine := trimGoModLine(lines[i])
				if blockLine == ")" {
					break
				}
				if blockLine == "" {
					continue
				}
				repl, err := parseGoReplace(blockLine)
				if err != nil {
					return nil, err
				}
				mod.Replaces = append(mod.Replaces, repl)
			}
		case strings.HasPrefix(line, "require "):
			req, err := parseGoRequire(strings.TrimSpace(strings.TrimPrefix(line, "require ")))
			if err != nil {
				return nil, err
			}
			mod.Requires = append(mod.Requires, req)
		case strings.HasPrefix(line, "replace "):
			repl, err := parseGoReplace(strings.TrimSpace(strings.TrimPrefix(line, "replace ")))
			if err != nil {
				return nil, err
			}
			mod.Replaces = append(mod.Replaces, repl)
		}
	}
	return mod, nil
}

func parseGoRequire(line string) (goRequire, error) {
	indirect := strings.Contains(line, "// indirect")
	line = trimGoModLine(line)
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return goRequire{}, fmt.Errorf("invalid require directive %q", line)
	}
	return goRequire{Path: fields[0], Version: fields[1], Indirect: indirect}, nil
}

func parseGoReplace(line string) (goReplace, error) {
	parts := strings.Split(line, "=>")
	if len(parts) != 2 {
		return goReplace{}, fmt.Errorf("invalid replace directive %q", line)
	}
	oldFields := strings.Fields(strings.TrimSpace(parts[0]))
	newFields := strings.Fields(strings.TrimSpace(parts[1]))
	if len(oldFields) == 0 || len(newFields) == 0 {
		return goReplace{}, fmt.Errorf("invalid replace directive %q", line)
	}
	return goReplace{OldPath: oldFields[0], NewPath: newFields[0]}, nil
}

func trimGoModLine(line string) string {
	if idx := strings.Index(line, "//"); idx >= 0 {
		line = line[:idx]
	}
	return strings.TrimSpace(line)
}

func isLocalGoReplacement(path string) bool {
	return strings.HasPrefix(path, ".") || filepath.IsAbs(path)
}
