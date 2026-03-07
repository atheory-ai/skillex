package scanner

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ladyhunterbear/skillex/internal/config"
	"github.com/ladyhunterbear/skillex/internal/frontmatter"
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
}

// PackageJSON represents the relevant fields from a package.json file.
type PackageJSON struct {
	Name            string          `json:"name"`
	Version         string          `json:"version"`
	Dependencies    map[string]string `json:"dependencies"`
	DevDependencies map[string]string `json:"devDependencies"`
	Skillex         json.RawMessage `json:"skillex"`
}

// SkilexExport holds the skillex config extracted from a package.json.
type SkilexExport struct {
	Enabled bool
	Path    string // custom path, defaults to "skillex"
}

// Scanner discovers skill files within a repository.
type Scanner struct {
	root string
	cfg  *config.Config
	// devMode includes devDependencies
	devMode bool
}

// New creates a new Scanner.
func New(root string, cfg *config.Config, devMode bool) *Scanner {
	return &Scanner{root: root, cfg: cfg, devMode: devMode}
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
			sf, err := s.readSkillFile(abs, skillPath, "", "", "repo", "repo")
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

		depSkills, errs := s.scanBoundary(boundaryPath)
		result.DepSkills = append(result.DepSkills, depSkills...)
		result.Errors = append(result.Errors, errs...)
	}

	return result, nil
}

// scanBoundary resolves dependencies at a boundary package.json and scans for skillex exports.
func (s *Scanner) scanBoundary(boundaryPath string) ([]SkillFile, []error) {
	var skills []SkillFile
	var errs []error

	pkgJSON, err := readPackageJSON(filepath.Join(boundaryPath, "package.json"))
	if err != nil {
		return nil, []error{fmt.Errorf("reading boundary package.json at %s: %w", boundaryPath, err)}
	}

	deps := pkgJSON.Dependencies
	if s.devMode {
		for k, v := range pkgJSON.DevDependencies {
			deps[k] = v
		}
	}

	// Find node_modules root (walk up from boundary)
	nmRoot := findNodeModules(boundaryPath)

	for pkgName := range deps {
		pkgRoot := filepath.Join(nmRoot, pkgName)
		// Handle scoped packages like @acme/foo -> node_modules/@acme/foo
		depPkgJSON, err := readPackageJSON(filepath.Join(pkgRoot, "package.json"))
		if err != nil {
			// Not installed or doesn't have package.json — skip silently
			continue
		}

		export := parseSkilexExport(depPkgJSON.Skillex)
		if !export.Enabled {
			continue
		}

		skilexDir := filepath.Join(pkgRoot, export.Path)
		depSkills, depErrs := s.scanSkilexDir(skilexDir, depPkgJSON.Name, depPkgJSON.Version)
		skills = append(skills, depSkills...)
		errs = append(errs, depErrs...)
	}

	return skills, errs
}

// scanSkilexDir reads public/ and private/ directories in a skillex export directory.
func (s *Scanner) scanSkilexDir(skilexDir, pkgName, pkgVersion string) ([]SkillFile, []error) {
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
			sfs, err := s.readSkillFile(path, relToRoot, pkgName, pkgVersion, vis, "dependency")
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
func (s *Scanner) readSkillFile(absPath, relPath, pkgName, pkgVersion, visibility, sourceType string) ([]SkillFile, error) {
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
		AbsPath:        absPath,
		RelPath:        relPath,
		PackageName:    pkgName,
		PackageVersion: pkgVersion,
		Visibility:     visibility,
		SourceType:     sourceType,
		Frontmatter:    fm,
		Body:           body,
		IsTest:         isTest,
		TestFor:        testFor,
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

// readPackageJSON parses a package.json file.
func readPackageJSON(path string) (*PackageJSON, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var pkg PackageJSON
	if err := json.Unmarshal(data, &pkg); err != nil {
		return nil, err
	}
	if pkg.Dependencies == nil {
		pkg.Dependencies = map[string]string{}
	}
	if pkg.DevDependencies == nil {
		pkg.DevDependencies = map[string]string{}
	}
	return &pkg, nil
}

// parseSkilexExport extracts the skillex export config from a package.json skillex field.
func parseSkilexExport(raw json.RawMessage) SkilexExport {
	if raw == nil {
		return SkilexExport{}
	}
	s := strings.TrimSpace(string(raw))
	if s == "true" {
		return SkilexExport{Enabled: true, Path: "skillex"}
	}
	if s == "false" || s == "null" {
		return SkilexExport{}
	}
	// Try object form: {"path": "docs/skillex"}
	var obj struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(raw, &obj); err == nil && obj.Path != "" {
		return SkilexExport{Enabled: true, Path: obj.Path}
	}
	return SkilexExport{}
}

// findNodeModules walks up from the given directory to find node_modules.
func findNodeModules(start string) string {
	dir := start
	for {
		nm := filepath.Join(dir, "node_modules")
		if info, err := os.Stat(nm); err == nil && info.IsDir() {
			return nm
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return filepath.Join(start, "node_modules")
}
