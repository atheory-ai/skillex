package query

import (
	"encoding/base64"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/atheory-ai/skillex/internal/registry"
	"github.com/gobwas/glob"
)

// Format controls what query results include.
type Format string

const (
	// FormatDefault means "not explicitly specified" — the engine auto-selects:
	// summary when --search is used (discovery mode), content otherwise.
	FormatDefault = Format("")
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
	Type          ResponseType `json:"type"`
	Results       []Result     `json:"results,omitempty"`
	Vocabulary    *Vocabulary  `json:"vocabulary,omitempty"`
	Query         *Echo        `json:"query,omitempty"`
	MatchCount    int          `json:"match_count,omitempty"`
	ReturnedCount int          `json:"returned_count,omitempty"`
	TooBroad      bool         `json:"too_broad,omitempty"`
	NextCursor    string       `json:"next_cursor,omitempty"`
	NarrowWith    *NarrowWith  `json:"narrow_with,omitempty"`
}

// Echo captures the filters that were searched, included in no_match responses.
type Echo struct {
	Path    string   `json:"path,omitempty"`
	Topics  []string `json:"topics,omitempty"`
	Tags    []string `json:"tags,omitempty"`
	Package string   `json:"package,omitempty"`
	Search  string   `json:"search,omitempty"`
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
	Ref            string    `json:"ref"`
	Path           string    `json:"path"`
	Name           string    `json:"name,omitempty"`
	Description    string    `json:"description,omitempty"`
	PackageName    string    `json:"package,omitempty"`
	PackageVersion string    `json:"version,omitempty"`
	Visibility     string    `json:"visibility"`
	SourceType     string    `json:"source_type"`
	Topics         []string  `json:"topics,omitempty"`
	Tags           []string  `json:"tags,omitempty"`
	Scopes         []string  `json:"scopes,omitempty"`
	ContentBytes   int       `json:"content_bytes"`
	Score          float64   `json:"score,omitempty"`
	Sections       []Section `json:"sections,omitempty"`
	MatchedIn      []string  `json:"matched_in,omitempty"`
	Excerpt        string    `json:"excerpt,omitempty"`
	Content        string    `json:"content,omitempty"`
}

// Section is a compact, stable address for a Markdown section.
type Section struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Level int    `json:"level"`
}

// Facet gives an agent a candidate-scoped way to narrow a broad query.
type Facet struct {
	Value string `json:"value"`
	Count int    `json:"count"`
}

// NarrowWith contains only values represented by the current candidate set.
type NarrowWith struct {
	Topics   []Facet `json:"topics,omitempty"`
	Tags     []Facet `json:"tags,omitempty"`
	Packages []Facet `json:"packages,omitempty"`
	Paths    []Facet `json:"paths,omitempty"`
	Advice   string  `json:"advice,omitempty"`
}

// ReadResponse is the bounded second stage of progressive skill retrieval.
type ReadResponse struct {
	Ref          string   `json:"ref"`
	Path         string   `json:"path"`
	Section      *Section `json:"section,omitempty"`
	Content      string   `json:"content"`
	ContentBytes int      `json:"content_bytes"`
	Truncated    bool     `json:"truncated,omitempty"`
	NextAction   string   `json:"next_action,omitempty"`
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
	// Search performs keyword search across skill name and description.
	// Whitespace/comma-separated tokens are each matched independently (OR).
	Search string
	// Format controls output detail for result responses.
	// FormatDefault always selects summary. Content is available only through an
	// explicit read operation; this keeps discovery bounded and useful.
	Format Format
	// Limit bounds discovery results. Zero selects the safe default.
	Limit int
	// Cursor resumes a deterministic discovery result page.
	Cursor string
}

// hasFilters reports whether any filter dimension is set.
func (p Params) hasFilters() bool {
	return p.Path != "" || len(p.Topics) > 0 || len(p.Tags) > 0 || p.Package != "" || p.Search != ""
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

	hasClassicFilters := len(p.Topics) > 0 || len(p.Tags) > 0 || p.Package != ""

	var (
		skills []registry.Skill
		err    error
	)

	if hasClassicFilters {
		skills, err = e.reg.Query(p.Path, p.Package, p.Topics, p.Tags)
		if err != nil {
			return nil, err
		}
		if p.Path != "" {
			skills = filterByPath(skills, p.Path)
		}
	}

	if p.Search != "" {
		searchSkills, err := e.reg.QueryBySearch(p.Search)
		if err != nil {
			return nil, err
		}
		if hasClassicFilters {
			// Intersect search results with the already-filtered classic set.
			// ATH-172: a search that matches nothing in an otherwise valid set
			// must return no_match, not all the classic results.
			classicIDs := make(map[int64]bool, len(skills))
			for _, s := range skills {
				classicIDs[s.ID] = true
			}
			var intersected []registry.Skill
			for _, s := range searchSkills {
				if classicIDs[s.ID] {
					intersected = append(intersected, s)
				}
			}
			skills = intersected
		} else {
			// Search is the only SQL filter; path is applied post-search if set.
			skills = searchSkills
			if p.Path != "" {
				skills = filterByPath(skills, p.Path)
			}
		}
	} else if !hasClassicFilters {
		// Path is the only filter.
		skills, err = e.reg.QueryByPath(p.Path)
		if err != nil {
			return nil, err
		}
	}

	if len(skills) == 0 {
		return e.noMatchResponse(p)
	}

	// Queries are discovery-only. Content is available exclusively through Read,
	// which requires an explicit skill ref and enforces a byte budget.
	effectiveFormat := FormatSummary

	// Pagination is only meaningful with a stable order.
	if p.Search == "" {
		sort.Slice(skills, func(i, j int) bool { return skills[i].Path < skills[j].Path })
	}
	matchCount := len(skills)
	start := decodeCursor(p.Cursor)
	if start > matchCount {
		start = matchCount
	}
	limit := p.Limit
	if limit <= 0 {
		limit = 8
	}
	if limit > 20 {
		limit = 20
	}
	end := start + limit
	if end > matchCount {
		end = matchCount
	}

	results := make([]Result, 0, end-start)
	for _, s := range skills[start:end] {
		r := Result{
			Ref:            skillRef(s.Path),
			Path:           s.Path,
			Name:           compactText(s.Name, 200),
			Description:    compactText(s.Description, 600),
			PackageName:    s.PackageName,
			PackageVersion: s.PackageVersion,
			Visibility:     s.Visibility,
			SourceType:     s.SourceType,
			Topics:         s.Topics,
			Tags:           s.Tags,
			Scopes:         s.Scopes,
			ContentBytes:   len([]byte(s.Content)),
			Score:          s.Score,
			Sections:       Sections(s.Content),
		}
		if p.Search != "" {
			r.MatchedIn, r.Excerpt = matchDetails(s, p.Search)
		}
		if effectiveFormat == FormatContent {
			r.Content = s.Content
		}
		results = append(results, r)
	}

	resp := &Response{Type: ResponseTypeResults, Results: results, MatchCount: matchCount, ReturnedCount: len(results)}
	if end < matchCount {
		resp.NextCursor = encodeCursor(end)
	}
	resp.TooBroad = matchCount > limit
	if resp.TooBroad {
		resp.NarrowWith = buildNarrowWith(skills)
	}
	return resp, nil
}

func matchDetails(s registry.Skill, search string) ([]string, string) {
	tokens := strings.FieldsFunc(strings.ToLower(search), func(r rune) bool { return r == ' ' || r == '\t' || r == '\n' || r == ',' })
	contains := func(value string) bool {
		value = strings.ToLower(value)
		for _, t := range tokens {
			if strings.Contains(value, t) {
				return true
			}
		}
		return false
	}
	var fields []string
	if contains(s.Name) {
		fields = append(fields, "name")
	}
	if contains(s.Description) {
		fields = append(fields, "description")
	}
	if contains(headings(s.Content)) {
		fields = append(fields, "heading")
	}
	if contains(s.Content) {
		fields = append(fields, "body")
	}
	for _, t := range tokens {
		idx := strings.Index(strings.ToLower(s.Content), t)
		if idx >= 0 {
			start := idx - 120
			if start < 0 {
				start = 0
			}
			end := idx + 220
			if end > len(s.Content) {
				end = len(s.Content)
			}
			excerpt := strings.TrimSpace(s.Content[start:end])
			if start > 0 {
				excerpt = "…" + excerpt
			}
			if end < len(s.Content) {
				excerpt += "…"
			}
			return fields, excerpt
		}
	}
	return fields, ""
}

func headings(content string) string {
	var b strings.Builder
	for _, s := range Sections(content) {
		b.WriteString(s.Title)
		b.WriteByte('\n')
	}
	return b.String()
}

// compactText puts a fixed upper bound on discovery metadata. This keeps a
// malformed or unusually verbose frontmatter field from defeating discovery
// response budgeting.
func compactText(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	end := maxLen
	for end > 0 && (s[end]&0xc0) == 0x80 {
		end--
	}
	return s[:end] + "…"
}

func skillRef(path string) string {
	return "skill:" + base64.RawURLEncoding.EncodeToString([]byte(path))
}

// PathFromRef resolves a stable skill reference without exposing database ids.
func PathFromRef(ref string) (string, bool) {
	if !strings.HasPrefix(ref, "skill:") {
		return "", false
	}
	b, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(ref, "skill:"))
	return string(b), err == nil && len(b) > 0
}

// Read returns one selected skill, optionally one selected section. Content is
// never returned above maxBytes; callers can select a section to narrow it.
func (e *Engine) Read(ref, sectionID string, maxBytes int) (*ReadResponse, error) {
	path, ok := PathFromRef(ref)
	if !ok {
		return nil, fmt.Errorf("invalid skill ref")
	}
	s, err := e.reg.GetSkillByPath(path)
	if err != nil {
		return nil, err
	}
	if s == nil {
		return nil, fmt.Errorf("skill not found")
	}
	content := s.Content
	var selected *Section
	if sectionID != "" {
		section, found := SelectSection(content, sectionID)
		if !found {
			return nil, fmt.Errorf("section %q not found", sectionID)
		}
		content = section
		for _, h := range Sections(s.Content) {
			if h.ID == sectionID {
				hCopy := h
				selected = &hCopy
				break
			}
		}
	}
	if maxBytes <= 0 {
		maxBytes = 24 * 1024
	}
	if maxBytes > 64*1024 {
		maxBytes = 64 * 1024
	}
	fullBytes := len([]byte(content))
	resp := &ReadResponse{Ref: ref, Path: s.Path, Section: selected, ContentBytes: fullBytes}
	if fullBytes > maxBytes {
		// UTF-8-safe enough for Markdown display: backing up to a rune boundary.
		cut := maxBytes
		for cut > 0 && (content[cut]&0xc0) == 0x80 {
			cut--
		}
		resp.Content = content[:cut]
		resp.Truncated = true
		resp.NextAction = "Select a narrower section or request another bounded read."
		return resp, nil
	}
	resp.Content = content
	return resp, nil
}

func encodeCursor(offset int) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.Itoa(offset)))
}
func decodeCursor(cursor string) int {
	b, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return 0
	}
	n, err := strconv.Atoi(string(b))
	if err != nil || n < 0 {
		return 0
	}
	return n
}

func buildNarrowWith(skills []registry.Skill) *NarrowWith {
	counts := func(values func(registry.Skill) []string) []Facet {
		m := map[string]int{}
		for _, s := range skills {
			for _, v := range values(s) {
				if v != "" {
					m[v]++
				}
			}
		}
		out := make([]Facet, 0, len(m))
		for v, c := range m {
			out = append(out, Facet{Value: v, Count: c})
		}
		sort.Slice(out, func(i, j int) bool {
			if out[i].Count == out[j].Count {
				return out[i].Value < out[j].Value
			}
			return out[i].Count > out[j].Count
		})
		if len(out) > 8 {
			out = out[:8]
		}
		return out
	}
	return &NarrowWith{
		Topics: counts(func(s registry.Skill) []string { return s.Topics }), Tags: counts(func(s registry.Skill) []string { return s.Tags }),
		Packages: counts(func(s registry.Skill) []string { return []string{s.PackageName} }), Paths: counts(func(s registry.Skill) []string { return s.Scopes }),
		Advice: "This query is broad. Narrow it with a suggested path, topic, tag, package, or more specific search terms before reading content.",
	}
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
	var vocab []registry.Skill
	if p.Path != "" {
		scoped, err := e.reg.QueryByPath(p.Path)
		if err != nil {
			return nil, err
		}
		if len(scoped) > 0 {
			vocab = scoped
		}
	}
	if vocab == nil {
		all, err := e.reg.AllSkills()
		if err != nil {
			return nil, err
		}
		vocab = all
	}

	return &Response{
		Type: ResponseTypeNoMatch,
		Query: &Echo{
			Path:    p.Path,
			Topics:  p.Topics,
			Tags:    p.Tags,
			Package: p.Package,
			Search:  p.Search,
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
