package query

import (
	"sort"
	"strings"

	"github.com/atheory-ai/skillex/internal/registry"
	"github.com/gobwas/glob"
)

// Format controls what query results include.
type Format string

const (
	FormatContent = Format("content")
	FormatSummary = Format("summary")
)

// ResponseType distinguishes query response kinds.
type ResponseType string

const (
	// ResponseTypeResults is returned when filters matched one or more skills.
	ResponseTypeResults ResponseType = "results"
	// ResponseTypeVocabulary is returned when no filters were provided.
	// It contains scoped metadata to help callers construct a real query.
	ResponseTypeVocabulary ResponseType = "vocabulary"
	// ResponseTypeNoMatch is returned when filters were provided but matched nothing.
	// It contains the echoed query and scoped vocabulary as a hint.
	ResponseTypeNoMatch ResponseType = "no_match"
)

// Response is the unified return type for all query executions.
type Response struct {
	Type       ResponseType `json:"type"`
	Results    []Result     `json:"results,omitempty"`
	Vocabulary *Vocabulary  `json:"vocabulary,omitempty"`
	Query      *QueryEcho   `json:"query,omitempty"`
}

// QueryEcho captures the filters that were searched, included in no_match responses.
type QueryEcho struct {
	Path    string   `json:"path,omitempty"`
	Topics  []string `json:"topics,omitempty"`
	Tags    []string `json:"tags,omitempty"`
	Package string   `json:"package,omitempty"`
}

// Vocabulary describes the skill dimensions available in the registry.
// All counts are scoped to the same visibility/scope rules as a real query.
type Vocabulary struct {
	Topics      []TopicEntry   `json:"topics,omitempty"`
	Tags        []TagEntry     `json:"tags,omitempty"`
	Packages    []PackageEntry `json:"packages,omitempty"`
	TotalSkills int            `json:"total_skills"`
}

// TopicEntry is a topic name with the count of skills that carry it.
type TopicEntry struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// TagEntry is a tag name with the count of skills that carry it.
type TagEntry struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// PackageEntry is a package with version and skill count.
type PackageEntry struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
	Count   int    `json:"count"`
}

// Result is a single skill query result.
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
	// Path is a file path or glob pattern to scope the query.
	Path string
	// Topics filters skills by topic (intersection — all topics must match).
	Topics []string
	// Tags filters skills by tag (intersection — all tags must match).
	Tags []string
	// Package filters skills by package name.
	Package string
	// Format controls output detail for result responses.
	Format Format
}

// hasFilters reports whether any filter dimension is set.
func (p Params) hasFilters() bool {
	return p.Path != "" || len(p.Topics) > 0 || len(p.Tags) > 0 || p.Package != ""
}

// Engine executes structured skill queries against the registry.
type Engine struct {
	reg *registry.Registry
}

// New creates a new query Engine.
func New(reg *registry.Registry) *Engine {
	return &Engine{reg: reg}
}

// Execute runs a query and returns a typed Response.
//
// Behaviour:
//   - No filters → ResponseTypeVocabulary: scoped metadata to guide filter selection.
//   - Filters set, results found → ResponseTypeResults: matching skills.
//   - Filters set, no results → ResponseTypeNoMatch: echoed query + scoped vocabulary hint.
//
// No code path returns all skill content as a fallback.
func (e *Engine) Execute(p Params) (*Response, error) {
	if !p.hasFilters() {
		return e.vocabularyResponse()
	}

	// SQL-level filters: topics, tags, package. Path is always post-filtered in process.
	hasSQLFilters := len(p.Topics) > 0 || len(p.Tags) > 0 || p.Package != ""

	var (
		skills []registry.Skill
		err    error
	)
	if hasSQLFilters {
		skills, err = e.reg.Query(p.Path, p.Package, p.Topics, p.Tags)
	} else {
		// Path is the only filter — load all skills then apply in-process scope matching
		// below. SQLite does not evaluate the stored glob patterns against a given path,
		// so there is no SQL-level shortcut: the full scan is O(n) by design.
		skills, err = e.reg.AllSkills()
	}
	if err != nil {
		return nil, err
	}

	if p.Path != "" {
		skills = filterByPath(skills, p.Path)
	}

	if len(skills) == 0 {
		return e.noMatchResponse(p)
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

	return &Response{
		Type:    ResponseTypeResults,
		Results: results,
	}, nil
}

// vocabularyResponse builds a vocabulary response from all registry skills.
func (e *Engine) vocabularyResponse() (*Response, error) {
	skills, err := e.reg.AllSkills()
	if err != nil {
		return nil, err
	}
	return &Response{
		Type:       ResponseTypeVocabulary,
		Vocabulary: buildVocabulary(skills),
	}, nil
}

// noMatchResponse builds a no-match response with a scoped vocabulary hint.
//
// Scoping: when a --path filter was provided, the vocabulary is built from skills
// that ARE reachable from that path (their scopes match it). This gives the agent
// topics/tags that are actually relevant to their working context rather than the
// entire global registry.
//
// When no path context is available (e.g. topic-only or package-only queries),
// the vocabulary falls back to the full registry — the global set is the only
// reasonable hint when there is no scope to narrow against.
func (e *Engine) noMatchResponse(p Params) (*Response, error) {
	all, err := e.reg.AllSkills()
	if err != nil {
		return nil, err
	}

	// Scope the vocabulary to skills reachable from the given path, if any.
	vocab := all
	if p.Path != "" {
		if scoped := filterByPath(all, p.Path); len(scoped) > 0 {
			vocab = scoped
		}
		// If scoped is empty (no skills at that path at all), fall back to the
		// full registry so the agent still has useful hints.
	}

	return &Response{
		Type: ResponseTypeNoMatch,
		Query: &QueryEcho{
			Path:    p.Path,
			Topics:  p.Topics,
			Tags:    p.Tags,
			Package: p.Package,
		},
		Vocabulary: buildVocabulary(vocab),
	}, nil
}

// buildVocabulary aggregates topic, tag, and package counts from a skill slice.
// Results are sorted alphabetically within each dimension.
func buildVocabulary(skills []registry.Skill) *Vocabulary {
	topicCounts := map[string]int{}
	tagCounts := map[string]int{}
	pkgEntries := map[string]*PackageEntry{}

	for _, s := range skills {
		for _, t := range s.Topics {
			topicCounts[t]++
		}
		for _, t := range s.Tags {
			tagCounts[t]++
		}
		if s.PackageName != "" {
			if e, ok := pkgEntries[s.PackageName]; ok {
				e.Count++
			} else {
				pkgEntries[s.PackageName] = &PackageEntry{
					Name:    s.PackageName,
					Version: s.PackageVersion,
					Count:   1,
				}
			}
		}
	}

	topics := make([]TopicEntry, 0, len(topicCounts))
	for name, count := range topicCounts {
		topics = append(topics, TopicEntry{Name: name, Count: count})
	}
	sort.Slice(topics, func(i, j int) bool { return topics[i].Name < topics[j].Name })

	tags := make([]TagEntry, 0, len(tagCounts))
	for name, count := range tagCounts {
		tags = append(tags, TagEntry{Name: name, Count: count})
	}
	sort.Slice(tags, func(i, j int) bool { return tags[i].Name < tags[j].Name })

	pkgs := make([]PackageEntry, 0, len(pkgEntries))
	for _, e := range pkgEntries {
		pkgs = append(pkgs, *e)
	}
	sort.Slice(pkgs, func(i, j int) bool { return pkgs[i].Name < pkgs[j].Name })

	return &Vocabulary{
		Topics:      topics,
		Tags:        tags,
		Packages:    pkgs,
		TotalSkills: len(skills),
	}
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
