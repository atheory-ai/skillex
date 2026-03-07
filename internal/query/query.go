package query

import (
	"strings"

	"github.com/gobwas/glob"
	"github.com/ladyhunterbear/skillex/internal/registry"
)

// Format controls what query results include.
type Format string

const (
	FormatContent = Format("content")
	FormatSummary = Format("summary")
)

// Result is a query result entry.
type Result struct {
	Path           string   `json:"path"`
	PackageName    string   `json:"package,omitempty"`
	PackageVersion string   `json:"version,omitempty"`
	Visibility     string   `json:"visibility"`
	SourceType     string   `json:"source_type"`
	Topics         []string `json:"topics,omitempty"`
	Tags           []string `json:"tags,omitempty"`
	Scopes         []string `json:"scopes,omitempty"`
	Content        string   `json:"content,omitempty"`
}

// Params holds query parameters.
type Params struct {
	// Path is a file path or glob pattern.
	Path string
	// Topics filters skills by topic (intersection).
	Topics []string
	// Tags filters skills by tag (intersection).
	Tags []string
	// Package filters skills by package name.
	Package string
	// Format controls output detail.
	Format Format
}

// Engine executes structured skill queries against the registry.
type Engine struct {
	reg *registry.Registry
}

// New creates a new query Engine.
func New(reg *registry.Registry) *Engine {
	return &Engine{reg: reg}
}

// Execute runs a query with the given parameters and returns matching results.
func (e *Engine) Execute(p Params) ([]Result, error) {
	// Use the registry's compound query
	skills, err := e.reg.Query(p.Path, p.Package, p.Topics, p.Tags)
	if err != nil {
		return nil, err
	}

	// If path is provided, post-filter by scope glob matching
	if p.Path != "" {
		skills = filterByPath(skills, p.Path)
	}

	results := make([]Result, 0, len(skills))
	for _, s := range skills {
		r := Result{
			Path:           s.Path,
			PackageName:    s.PackageName,
			PackageVersion: s.PackageVersion,
			Visibility:     s.Visibility,
			SourceType:     s.SourceType,
			Topics:         s.Topics,
			Tags:           s.Tags,
			Scopes:         s.Scopes,
		}
		if p.Format == FormatContent || p.Format == "" {
			r.Content = s.Content
		}
		results = append(results, r)
	}

	return results, nil
}

// ContentString concatenates skill contents for piping into agent context.
func ContentString(results []Result) string {
	var parts []string
	for _, r := range results {
		if r.Content != "" {
			parts = append(parts, r.Content)
		}
	}
	return strings.Join(parts, "\n\n---\n\n")
}

// filterByPath filters skills whose scopes match the given path glob.
func filterByPath(skills []registry.Skill, path string) []registry.Skill {
	var filtered []registry.Skill
	for _, s := range skills {
		if scopesMatchPath(s.Scopes, path) {
			filtered = append(filtered, s)
		}
	}
	return filtered
}

// scopesMatchPath returns true if any of the skill's scopes match the given path.
func scopesMatchPath(scopes []string, path string) bool {
	normalPath := strings.ReplaceAll(path, "\\", "/")
	for _, scope := range scopes {
		normalScope := strings.ReplaceAll(scope, "\\", "/")
		if normalScope == "**" {
			return true
		}
		g, err := glob.Compile(normalScope, '/')
		if err != nil {
			// Fallback: prefix match
			trimmed := strings.TrimSuffix(normalScope, "/**")
			if strings.HasPrefix(normalPath, trimmed) {
				return true
			}
			continue
		}
		if g.Match(normalPath) {
			return true
		}
	}
	return false
}
