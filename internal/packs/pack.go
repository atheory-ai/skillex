package packs

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

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
