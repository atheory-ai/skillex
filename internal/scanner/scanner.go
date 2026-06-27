package scanner

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/atheory-ai/skillex/internal/config"
	"github.com/atheory-ai/skillex/internal/frontmatter"
	"github.com/atheory-ai/skillex/internal/packs"
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
	// ExplicitScopes are precomputed scopes for activated skills such as pack skills.
	ExplicitScopes []string
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

			// Discover the sibling .test.md, consistent with pack scanning (scanProjectPacks /
			// scanDependencyPack). Without this, test scenarios for config-listed repo skills were
			// never scanned, so `refresh` always reported "0 test scenarios". A missing test file
			// is skipped silently (readSkillFile returns nil for a non-existent optional file).
			testRel := strings.TrimSuffix(skillPath, ".md") + ".test.md"
			testAbs := filepath.Join(s.root, testRel)
			testSf, err := s.readSkillFile(testAbs, testRel, "", "", "repo", "repo", "", "")
			if err != nil {
				result.Errors = append(result.Errors, fmt.Errorf("repo test %s: %w", testRel, err))
			} else {
				result.RepoSkills = append(result.RepoSkills, testSf...)
			}
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

	packSkills, errs := s.scanProjectPacks()
	result.RepoSkills = append(result.RepoSkills, packSkills...)
	result.Errors = append(result.Errors, errs...)

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
				switch export.Format {
				case SkillExportFormatLegacyDir:
					depSkills, depErrs := s.scanSkilexDir(
						export.Path,
						pkgRoot.Dependency.Name,
						pkgRoot.Dependency.Version,
						filepath.ToSlash(boundary.RootRel),
						filepath.ToSlash(pkgRoot.RootRel),
					)
					skills = append(skills, depSkills...)
					errs = append(errs, depErrs...)
				case SkillExportFormatPackManifest:
					depSkills, depErrs := s.scanDependencyPack(export.Path, *boundary, pkgRoot)
					skills = append(skills, depSkills...)
					errs = append(errs, depErrs...)
				default:
					continue
				}
			}
		}
	}

	return skills, errs
}

func (s *Scanner) scanDependencyPack(manifestPath string, boundary Boundary, pkgRoot PackageRoot) ([]SkillFile, []error) {
	var skills []SkillFile
	var errs []error

	pack, err := packs.Load(manifestPath)
	if err != nil {
		return nil, []error{err}
	}

	ctx := packs.ActivationContext{
		BoundaryRel: filepath.ToSlash(boundary.RootRel),
		Dependency: packs.DependencyFact{
			Source:  pkgRoot.Dependency.Source,
			Name:    pkgRoot.Dependency.Name,
			Version: pkgRoot.Dependency.Version,
		},
	}
	ctx, detectorErrs := packs.ContextForPack(s.root, pack, ctx)
	errs = append(errs, detectorErrs...)

	for _, skill := range pack.Manifest.Skills {
		scopes, err := packs.ActivateSkillWithContext(s.root, skill, ctx)
		if err != nil {
			errs = append(errs, fmt.Errorf("activating pack %s skill %s: %w", pack.Manifest.Name, skill.File, err))
			continue
		}
		if len(scopes) == 0 {
			continue
		}

		absPath := filepath.Join(pack.Dir, skill.File)
		relPath, _ := filepath.Rel(s.root, absPath)
		sfs, err := s.readSkillFileWithScopes(
			absPath,
			filepath.ToSlash(relPath),
			pkgRoot.Dependency.Name,
			pkgRoot.Dependency.Version,
			"public",
			"pack",
			filepath.ToSlash(boundary.RootRel),
			filepath.ToSlash(pkgRoot.RootRel),
			scopes,
		)
		if err != nil {
			errs = append(errs, fmt.Errorf("pack skill %s: %w", relPath, err))
			continue
		}
		skills = append(skills, sfs...)

		testAbsPath := strings.TrimSuffix(absPath, ".md") + ".test.md"
		testRelPath, _ := filepath.Rel(s.root, testAbsPath)
		testSfs, err := s.readSkillFileWithScopes(
			testAbsPath,
			filepath.ToSlash(testRelPath),
			pkgRoot.Dependency.Name,
			pkgRoot.Dependency.Version,
			"public",
			"pack",
			filepath.ToSlash(boundary.RootRel),
			filepath.ToSlash(pkgRoot.RootRel),
			nil,
		)
		if err != nil {
			errs = append(errs, fmt.Errorf("pack test %s: %w", testRelPath, err))
			continue
		}
		skills = append(skills, testSfs...)
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
	return s.readSkillFileWithScopes(absPath, relPath, pkgName, pkgVersion, visibility, sourceType, boundaryRel, pkgRootRel, nil)
}

func (s *Scanner) readSkillFileWithScopes(absPath, relPath, pkgName, pkgVersion, visibility, sourceType, boundaryRel, pkgRootRel string, explicitScopes []string) ([]SkillFile, error) {
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
		ExplicitScopes:     explicitScopes,
	}

	return []SkillFile{sf}, nil
}

func (s *Scanner) scanProjectPacks() ([]SkillFile, []error) {
	var skills []SkillFile

	activated, errs := packs.ActivateProject(s.root)
	for _, activation := range activated {
		absPath := filepath.Join(activation.Pack.Dir, activation.Skill.File)
		relPath, _ := filepath.Rel(s.root, absPath)
		sfs, err := s.readSkillFileWithScopes(
			absPath,
			filepath.ToSlash(relPath),
			"",
			"",
			"repo",
			"pack",
			"",
			"",
			activation.Scopes,
		)
		if err != nil {
			errs = append(errs, fmt.Errorf("pack skill %s: %w", relPath, err))
			continue
		}
		skills = append(skills, sfs...)

		testAbsPath := strings.TrimSuffix(absPath, ".md") + ".test.md"
		testRelPath, _ := filepath.Rel(s.root, testAbsPath)
		testSfs, err := s.readSkillFileWithScopes(
			testAbsPath,
			filepath.ToSlash(testRelPath),
			"",
			"",
			"repo",
			"pack",
			"",
			"",
			nil,
		)
		if err != nil {
			errs = append(errs, fmt.Errorf("pack test %s: %w", testRelPath, err))
			continue
		}
		skills = append(skills, testSfs...)
	}

	return skills, errs
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

		data, err := os.ReadFile(path) //nolint:gosec // G122: walking a known-trusted tree under the project root; TOCTOU not a meaningful concern here
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
