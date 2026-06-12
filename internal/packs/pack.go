package packs

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gobwas/glob"
	"gopkg.in/yaml.v3"
)

const Filename = "pack.yaml"

// Manifest describes a Skillex pack.
type Manifest struct {
	Name        string     `yaml:"name"`
	Version     string     `yaml:"version"`
	Description string     `yaml:"description"`
	Source      string     `yaml:"source"`
	Skills      []SkillRef `yaml:"skills"`
}

// SkillRef describes one skill file and its activation rules.
type SkillRef struct {
	File         string       `yaml:"file"`
	ActivateWhen ActivateWhen `yaml:"activate-when"`
	Scope        string       `yaml:"scope"`
}

// ActivateWhen contains refresh-time activation conditions.
type ActivateWhen struct {
	FilesPresent []string `yaml:"files-present"`
}

// Pack is a parsed manifest with its filesystem location.
type Pack struct {
	Path     string
	Dir      string
	Manifest Manifest
}

// ActivatedSkill is a manifest skill whose activation rules matched the repo.
type ActivatedSkill struct {
	Pack   *Pack
	Skill  SkillRef
	Scopes []string
}

// Load reads and validates a pack manifest.
func Load(path string) (*Pack, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var manifest Manifest
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}

	pack := &Pack{
		Path:     path,
		Dir:      filepath.Dir(path),
		Manifest: manifest,
	}
	if err := pack.Validate(); err != nil {
		return nil, err
	}
	return pack, nil
}

// Validate checks manifest structure and referenced skill files.
func (p *Pack) Validate() error {
	var errs []string
	if strings.TrimSpace(p.Manifest.Name) == "" {
		errs = append(errs, "name is required")
	}
	if len(p.Manifest.Skills) == 0 {
		errs = append(errs, "skills must contain at least one entry")
	}

	for i, skill := range p.Manifest.Skills {
		prefix := fmt.Sprintf("skills[%d]", i)
		if strings.TrimSpace(skill.File) == "" {
			errs = append(errs, prefix+".file is required")
		} else if !isSafeRelativePath(skill.File) {
			errs = append(errs, prefix+".file must be a relative path inside the pack")
		} else if _, err := os.Stat(filepath.Join(p.Dir, skill.File)); err != nil {
			errs = append(errs, fmt.Sprintf("%s.file %q not found", prefix, skill.File))
		}

		if len(skill.ActivateWhen.FilesPresent) == 0 {
			errs = append(errs, prefix+".activate-when.files-present must contain at least one pattern")
		}

		switch skill.Scope {
		case "", "subtree", "repo", "directory":
		default:
			errs = append(errs, prefix+".scope must be one of: repo, subtree, directory")
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("invalid pack %s: %s", p.Path, strings.Join(errs, "; "))
	}
	return nil
}

func isSafeRelativePath(path string) bool {
	if filepath.IsAbs(path) {
		return false
	}
	clean := filepath.Clean(path)
	return clean != ".." && !strings.HasPrefix(clean, ".."+string(filepath.Separator))
}

// ActivateProject discovers and activates project-local packs for a repo root.
func ActivateProject(root string) ([]ActivatedSkill, []error) {
	var activated []ActivatedSkill
	var errs []error

	for _, manifestPath := range ProjectManifestPaths(root) {
		pack, err := Load(manifestPath)
		if err != nil {
			errs = append(errs, err)
			continue
		}

		for _, skill := range pack.Manifest.Skills {
			scopes, err := ActivateSkill(root, skill)
			if err != nil {
				errs = append(errs, fmt.Errorf("activating pack %s skill %s: %w", pack.Manifest.Name, skill.File, err))
				continue
			}
			if len(scopes) == 0 {
				continue
			}

			activated = append(activated, ActivatedSkill{
				Pack:   pack,
				Skill:  skill,
				Scopes: scopes,
			})
		}
	}

	return activated, errs
}

// ProjectManifestPaths returns supported project-local pack manifest paths.
func ProjectManifestPaths(root string) []string {
	var paths []string

	rootPack := filepath.Join(root, "skillex", Filename)
	if fileExists(rootPack) {
		paths = append(paths, rootPack)
	}

	packsDir := filepath.Join(root, "skillex", "packs")
	entries, err := os.ReadDir(packsDir)
	if err != nil {
		return paths
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(packsDir, entry.Name(), Filename)
		if fileExists(path) {
			paths = append(paths, path)
		}
	}

	return paths
}

// ActivateSkill resolves the scopes created by one skill activation rule.
func ActivateSkill(root string, skill SkillRef) ([]string, error) {
	var scopes []string
	for _, pattern := range skill.ActivateWhen.FilesPresent {
		matches, err := MatchRepoFiles(root, pattern)
		if err != nil {
			return nil, err
		}
		for _, match := range matches {
			scopes = appendUnique(scopes, ScopeForMatch(match, skill.Scope)...)
		}
	}
	return scopes, nil
}

// MatchRepoFiles matches a glob against repository files.
func MatchRepoFiles(root string, pattern string) ([]string, error) {
	normPattern := filepath.ToSlash(pattern)
	g, err := glob.Compile(normPattern, '/')
	if err != nil {
		return nil, err
	}

	var matches []string
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && shouldSkipActivationDir(d.Name()) {
			return filepath.SkipDir
		}
		if d.IsDir() {
			return nil
		}

		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if g.Match(rel) || g.Match(filepath.Base(rel)) {
			matches = append(matches, rel)
		}
		return nil
	})
	return matches, err
}

func shouldSkipActivationDir(name string) bool {
	switch name {
	case ".git", ".skillex", "node_modules":
		return true
	default:
		return false
	}
}

// ScopeForMatch maps a matching file path to the scope requested by a skill.
func ScopeForMatch(match string, scope string) []string {
	switch scope {
	case "repo":
		return []string{"**"}
	case "directory":
		dir := filepath.ToSlash(filepath.Dir(match))
		if dir == "." {
			return []string{"*"}
		}
		return []string{filepath.ToSlash(filepath.Join(dir, "*"))}
	case "", "subtree":
		dir := filepath.ToSlash(filepath.Dir(match))
		if dir == "." {
			return []string{"**"}
		}
		return []string{filepath.ToSlash(filepath.Join(dir, "**"))}
	default:
		return nil
	}
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func appendUnique(slice []string, items ...string) []string {
	seen := map[string]bool{}
	for _, s := range slice {
		seen[s] = true
	}
	for _, item := range items {
		if item == "" || seen[item] {
			continue
		}
		seen[item] = true
		slice = append(slice, item)
	}
	return slice
}
