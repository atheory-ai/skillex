package scanner

// DependencyMode controls which dependency classes a resolver includes.
type DependencyMode string

const (
	DependencyModeProd DependencyMode = "prod"
	DependencyModeDev  DependencyMode = "dev"
)

// Boundary is a dependency-resolution area in a repository.
type Boundary struct {
	RootRel     string
	RootAbs     string
	RepoRootAbs string
	Scope       string
	Manifests   []Manifest
}

// Manifest describes a dependency or project manifest discovered at a boundary.
type Manifest struct {
	PathRel string
	Kind    string
}

// Dependency is a declared package/module dependency within a boundary.
type Dependency struct {
	Source      string
	Name        string
	Version     string
	Direct      bool
	BoundaryRel string
}

// PackageRoot is an installed/source dependency root resolved from a boundary.
type PackageRoot struct {
	RootRel    string
	RootAbs    string
	Dependency Dependency
}

// SkillExport describes a Skillex export discovered in a package root.
type SkillExport struct {
	Path   string
	Format string
}

const (
	ManifestKindPackageJSON = "package-json"

	DependencySourceNPM = "npm-package"

	SkillExportFormatLegacyDir    = "legacy-skillex-dir"
	SkillExportFormatPackManifest = "pack-manifest"
)

// Resolver maps one dependency ecosystem into Skillex's scanner model.
type Resolver interface {
	Name() string
	DetectBoundary(root string, boundaryRel string) (*Boundary, bool, []error)
	Dependencies(boundary Boundary, mode DependencyMode) ([]Dependency, []error)
	ResolvePackageRoots(boundary Boundary, deps []Dependency) ([]PackageRoot, []error)
	Exports(pkg PackageRoot) ([]SkillExport, []error)
}

// DefaultResolvers returns the built-in resolvers used by refresh.
func DefaultResolvers() []Resolver {
	return []Resolver{
		NewNodeResolver(),
	}
}
