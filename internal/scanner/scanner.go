package scanner

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/atheory-ai/skillex/internal/config"
	"github.com/atheory-ai/skillex/internal/frontmatter"
)

// SkillFile represents a discovered skill file and its parsed metadata.
type SkillFile struct {
	// AbsPath is the absolute filesystem path.
	AbsPath string
	// RelPath is the path relative to the repo root.
	RelPath string
	// PackageName is the npm package name, empty for repo-level skills.
	PackageName string
	// PackageVersion is the resolved version, empty for repo-level skills.
	PackageVersion string
	// Visibility is "public", "private", or "repo".
	Visibility string
	// SourceType is "repo" or "dependency".
	SourceType string
	// Frontmatter holds the parsed metadata.
	Frontmatter frontmatter.Frontmatter
	// Body is the skill content without frontmatter.
	Body string
	// IsTest indicates this is a .test.md file.
	IsTest bool
	// TestFor is the RelPath of the skill this test belongs to (when IsTest == true).
	TestFor string
	// DependencyBoundary is the relPath of the package boundary that resolved this skill.
	DependencyBoundary string
	// PackageRoot is the relPath of the installed package root that owns this skill.
	PackageRoot string
}

// Scanner discovers skill files within a repository.
type Scanner struct {
	root      string
	cfg       *config.Config
	resolvers []Resolver
	// devMode includes devDependencies
	devMode bool
}

// New creates a new Scanner.
func New(root string, cfg *config.Config, devMode bool) *Scanner {
	return NewWithResolvers(root, cfg, devMode, DefaultResolvers())
}

// NewWithResolvers creates a scanner with an explicit resolver set.
func NewWithResolvers(root string, cfg *config.Config, devMode bool, resolvers []Resolver) *Scanner {
	return &Scanner{
		root:      root,
		cfg:       cfg,
		devMode:   devMode,
		resolvers: resolvers,
	}
}

// ScanResult holds the complete output of a scan.
type ScanResult struct {
	RepoSkills []SkillFile
	DepSkills  []SkillFile
	Errors     []error
}

// Scan performs a full discovery scan.
func (s *Scanner) Scan() (*ScanResult, error) {
	result := &ScanResult{}

	// 1. Collect repo-level skills from Rules
	for _, rule := range s.cfg.Rules {
		for _, skillPath := range rule.Skills {
			abs := filepath.Join(s.root, skillPath)
			sf, err := s.readSkillFile(abs, skillPath, "", "", "repo", "repo", "", "")
			if err != nil {
				result.Errors = append(result.Errors, fmt.Errorf("repo skill %s: %w", skillPath, err))
				continue
			}
			result.RepoSkills = append(result.RepoSkills, sf...)
		}
	}

	// 2. For each dependency boundary, resolve and scan dependencies
	seen := map[string]bool{}
	for _, rule := range s.cfg.Rules {
		if rule.DependencyBoundary == "" {
			continue
		}
		boundaryPath := filepath.Join(s.root, rule.DependencyBoundary)
		if seen[boundaryPath] {
			continue
		}
		seen[boundaryPath] = true

		depSkills, errs := s.scanDependencyBoundary(boundaryPath, rule.DependencyBoundary)
		result.DepSkills = append(result.DepSkills, depSkills...)
		result.Errors = append(result.Errors, errs...)
	}

	return result, nil
}

// scanDependencyBoundary resolves dependencies at a configured boundary and scans exported skills.
func (s *Scanner) scanDependencyBoundary(boundaryPath, boundaryRel string) ([]SkillFile, []error) {
	var skills []SkillFile
	var errs []error

	mode := DependencyModeProd
	if s.devMode {
		mode = DependencyModeDev
	}

	for _, resolver := range s.resolvers {
		boundary, ok, detectErrs := resolver.DetectBoundary(s.root, boundaryRel)
		errs = append(errs, detectErrs...)
		if !ok {
			continue
		}
		if boundary.RootAbs == "" {
			boundary.RootAbs = boundaryPath
		}
		if boundary.RootRel == "" {
			boundary.RootRel = filepath.ToSlash(boundaryRel)
		}
		if boundary.RepoRootAbs == "" {
			boundary.RepoRootAbs = s.root
		}

		deps, depErrs := resolver.Dependencies(*boundary, mode)
		errs = append(errs, depErrs...)
		if len(depErrs) > 0 {
			continue
		}

		roots, rootErrs := resolver.ResolvePackageRoots(*boundary, deps)
		errs = append(errs, rootErrs...)

		for _, pkgRoot := range roots {
			exports, exportErrs := resolver.Exports(pkgRoot)
			errs = append(errs, exportErrs...)

			for _, export := range exports {
				if export.Format != SkillExportFormatLegacyDir {
					continue
				}

				depSkills, depErrs := s.scanSkilexDir(
					export.Path,
					pkgRoot.Dependency.Name,
					pkgRoot.Dependency.Version,
					filepath.ToSlash(boundary.RootRel),
					filepath.ToSlash(pkgRoot.RootRel),
				)
				skills = append(skills, depSkills...)
				errs = append(errs, depErrs...)
			}
		}
	}

	return skills, errs
}

// scanSkilexDir reads public/ and private/ directories in a skillex export directory.
func (s *Scanner) scanSkilexDir(skilexDir, pkgName, pkgVersion, boundaryRel, pkgRootRel string) ([]SkillFile, []error) {
	var skills []SkillFile
	var errs []error

	for _, vis := range []string{"public", "private"} {
		dir := filepath.Join(skilexDir, vis)
		info, err := os.Stat(dir)
		if err != nil || !info.IsDir() {
			continue
		}
		err = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			if !strings.HasSuffix(path, ".md") {
				return nil
			}

			relToRoot, _ := filepath.Rel(s.root, path)
			sfs, err := s.readSkillFile(path, relToRoot, pkgName, pkgVersion, vis, "dependency", boundaryRel, pkgRootRel)
			if err != nil {
				errs = append(errs, fmt.Errorf("reading %s: %w", path, err))
				return nil
			}
			skills = append(skills, sfs...)
			return nil
		})
		if err != nil {
			errs = append(errs, err)
		}
	}
	return skills, errs
}

// readSkillFile reads a skill (or test) file and returns SkillFile(s).
// For repo-level skills, the path may not exist (gracefully skip).
func (s *Scanner) readSkillFile(absPath, relPath, pkgName, pkgVersion, visibility, sourceType, boundaryRel, pkgRootRel string) ([]SkillFile, error) {
	data, err := os.ReadFile(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // silently skip missing optional files
		}
		return nil, err
	}

	fm, body, err := frontmatter.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("parsing frontmatter: %w", err)
	}

	isTest := strings.HasSuffix(relPath, ".test.md")
	testFor := ""
	if isTest {
		testFor = strings.TrimSuffix(relPath, ".test.md") + ".md"
	}

	sf := SkillFile{
		AbsPath:            absPath,
		RelPath:            relPath,
		PackageName:        pkgName,
		PackageVersion:     pkgVersion,
		Visibility:         visibility,
		SourceType:         sourceType,
		Frontmatter:        fm,
		Body:               body,
		IsTest:             isTest,
		TestFor:            testFor,
		DependencyBoundary: boundaryRel,
		PackageRoot:        pkgRootRel,
	}

	return []SkillFile{sf}, nil
}

// ScanDirectory scans a specific directory for skill files (used by init --package).
func ScanDirectory(dir, relBase, pkgName, pkgVersion, visibility, sourceType string) ([]SkillFile, error) {
	var skills []SkillFile

	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".md") {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		fm, body, err := frontmatter.Parse(data)
		if err != nil {
			return err
		}

		relPath, _ := filepath.Rel(relBase, path)
		isTest := strings.HasSuffix(relPath, ".test.md")
		testFor := ""
		if isTest {
			testFor = strings.TrimSuffix(relPath, ".test.md") + ".md"
		}

		skills = append(skills, SkillFile{
			AbsPath:        path,
			RelPath:        relPath,
			PackageName:    pkgName,
			PackageVersion: pkgVersion,
			Visibility:     visibility,
			SourceType:     sourceType,
			Frontmatter:    fm,
			Body:           body,
			IsTest:         isTest,
			TestFor:        testFor,
		})
		return nil
	})

	return skills, err
}
