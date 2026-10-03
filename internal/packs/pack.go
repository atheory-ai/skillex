package packs

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/atheory-ai/skillex/internal/packregistry"
	"github.com/gobwas/glob"
	"gopkg.in/yaml.v3"
)

const Filename = "pack.yaml"

// Manifest describes a Skillex pack.
type Manifest struct {
	Name        string                 `yaml:"name"`
	Version     string                 `yaml:"version"`
	Description string                 `yaml:"description"`
	Source      string                 `yaml:"source"`
	Detectors   Detectors              `yaml:"detectors"`
	Skills      []SkillRef             `yaml:"skills"`
	MCPServers  []MCPServerRef         `yaml:"mcp-servers"`
	Registry    map[string]interface{} `yaml:"registry"`
}

// Detectors maps friendly detector names to match rules.
type Detectors map[string]DetectorDef

// DetectorDef describes how to detect a project fact.
type DetectorDef struct {
	Matches []DetectorMatch `yaml:"matches"`
}

// DetectorMatch describes one way a detector can match.
type DetectorMatch struct {
	File       *FileCondition       `yaml:"file"`
	Dependency *DependencyCondition `yaml:"dependency"`
}

// FileCondition matches a repository file.
type FileCondition struct {
	Path string `yaml:"path"`
}

// SkillRef describes one skill file and its activation rules.
type SkillRef struct {
	File         string       `yaml:"file"`
	ActivateWhen ActivateWhen `yaml:"activate-when"`
	Scope        string       `yaml:"scope"`
	Files        []string     `yaml:"files"`
}

// MCPServerRef is a non-executable, non-secret suggestion shipped by a pack.
// A pack can express relevance, but trusted local or enterprise configuration
// remains the only authority that can configure a transport or credentials.
type MCPServerRef struct {
	Ref          string                `yaml:"ref"`
	Version      string                `yaml:"version"`
	Relationship string                `yaml:"relationship"`
	ActivateWhen ActivateWhen          `yaml:"activate-when"`
	Scope        string                `yaml:"scope"`
	Files        []string              `yaml:"files"`
	Capabilities MCPServerCapabilities `yaml:"capabilities"`
}

// MCPServerCapabilities narrows a server suggestion to named capabilities.
type MCPServerCapabilities struct {
	Prefer []string `yaml:"prefer"`
}

// ActivateWhen contains refresh-time activation conditions.
type ActivateWhen struct {
	All                []ActivateWhen        `yaml:"all"`
	FilesPresent       []string              `yaml:"files-present"`
	FilesMatching      []string              `yaml:"files-matching"`
	DependencyDeclared []DependencyCondition `yaml:"dependency-declared"`
	Detector           string                `yaml:"detector"`
}

// DependencyCondition matches a declared dependency fact.
type DependencyCondition struct {
	Source  string `yaml:"source"`
	Name    string `yaml:"name"`
	Version string `yaml:"version"`
}

// ActivationContext provides non-file facts used during activation.
type ActivationContext struct {
	Dependency     DependencyFact
	BoundaryRel    string
	DetectorKnown  map[string]bool
	DetectorActive map[string]bool
}

// DependencyFact identifies a dependency available in the current boundary.
type DependencyFact struct {
	Source  string
	Name    string
	Version string
}

// Pack is a parsed manifest with its filesystem location.
type Pack struct {
	Path          string
	Dir           string
	Manifest      Manifest
	VerifiedFiles map[string][]byte
}

// ActivatedSkill is a manifest skill whose activation rules matched the repo.
type ActivatedSkill struct {
	Pack   *Pack
	Skill  SkillRef
	Scopes []string
}

// ActivatedMCPServer is a contextual server suggestion whose activation rules matched.
type ActivatedMCPServer struct {
	Pack   *Pack
	Server MCPServerRef
	Scopes []string
}

// DetectorRegistration records where a detector definition came from.
type DetectorRegistration struct {
	Name   string
	Def    DetectorDef
	Source string
}

// DetectorRegistry holds detector definitions for one refresh run.
type DetectorRegistry struct {
	defs map[string]DetectorRegistration
}

// Load reads and validates a pack manifest.
func Load(path string) (*Pack, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	path = filepath.Clean(absolute)
	var data []byte
	var verifiedFiles map[string][]byte
	if strings.Contains(filepath.ToSlash(path), "/.skillex/packs/") {
		lock, err := packregistry.ValidateDirectory(filepath.Dir(path))
		if err != nil {
			return nil, fmt.Errorf("validating installed pack: %w", err)
		}
		verifiedFiles = lock.VerifiedFiles
		data = verifiedFiles[Filename]
	} else {
		data, err = os.ReadFile(path)
	}
	if err != nil {
		return nil, err
	}

	var manifest Manifest
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&manifest); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}

	pack := &Pack{
		Path:          path,
		Dir:           filepath.Dir(path),
		Manifest:      manifest,
		VerifiedFiles: verifiedFiles,
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
	if len(p.Manifest.Skills) == 0 && len(p.Manifest.MCPServers) == 0 {
		errs = append(errs, "skills or mcp-servers must contain at least one entry")
	}

	for name, detector := range p.Manifest.Detectors {
		prefix := fmt.Sprintf("detectors[%s]", name)
		if strings.TrimSpace(name) == "" {
			errs = append(errs, "detector name is required")
		}
		if len(detector.Matches) == 0 {
			errs = append(errs, prefix+".matches must contain at least one entry")
		}
		for i, match := range detector.Matches {
			matchPrefix := fmt.Sprintf("%s.matches[%d]", prefix, i)
			if match.File == nil && match.Dependency == nil {
				errs = append(errs, matchPrefix+" must contain file or dependency")
			}
			if match.File != nil && strings.TrimSpace(match.File.Path) == "" {
				errs = append(errs, matchPrefix+".file.path is required")
			}
			if match.Dependency != nil &&
				strings.TrimSpace(match.Dependency.Source) == "" &&
				strings.TrimSpace(match.Dependency.Name) == "" &&
				strings.TrimSpace(match.Dependency.Version) == "" {
				errs = append(errs, matchPrefix+".dependency must contain source, name, or version")
			}
		}
	}

	for i, skill := range p.Manifest.Skills {
		prefix := fmt.Sprintf("skills[%d]", i)
		if strings.TrimSpace(skill.File) == "" {
			errs = append(errs, prefix+".file is required")
		} else if !isSafeRelativePath(skill.File) {
			errs = append(errs, prefix+".file must be a relative path inside the pack")
		} else if p.VerifiedFiles != nil {
			if _, ok := p.VerifiedFiles[skill.File]; !ok {
				errs = append(errs, fmt.Sprintf("%s.file %q not in verified archive", prefix, skill.File))
			}
		} else if _, err := os.Stat(filepath.Join(p.Dir, skill.File)); err != nil {
			errs = append(errs, fmt.Sprintf("%s.file %q not found", prefix, skill.File))
		}

		if err := validateActivation(skill.ActivateWhen, prefix+".activate-when", 0); err != nil {
			errs = append(errs, err.Error())
		}

		switch skill.Scope {
		case "", "subtree", "repo", "directory", "matching-files", "nearest-ancestor", "boundary":
		default:
			errs = append(errs, prefix+".scope must be one of: repo, subtree, directory, matching-files, nearest-ancestor, boundary")
		}
	}

	for i, server := range p.Manifest.MCPServers {
		prefix := fmt.Sprintf("mcp-servers[%d]", i)
		if strings.TrimSpace(server.Ref) == "" {
			errs = append(errs, prefix+".ref is required")
		}
		if strings.TrimSpace(server.Version) == "" || strings.ContainsAny(server.Version, "*<>=^~ ") {
			errs = append(errs, prefix+".version must be an exact version")
		}
		if server.Relationship != "suggested" {
			errs = append(errs, prefix+".relationship must be suggested")
		}
		if err := validateActivation(server.ActivateWhen, prefix+".activate-when", 0); err != nil {
			errs = append(errs, err.Error())
		}
		if err := validateScope(server.Scope, prefix); err != nil {
			errs = append(errs, err.Error())
		}
		for j, name := range server.Capabilities.Prefer {
			if strings.TrimSpace(name) == "" {
				errs = append(errs, fmt.Sprintf("%s.capabilities.prefer[%d] is required", prefix, j))
			}
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("invalid pack %s: %s", p.Path, strings.Join(errs, "; "))
	}
	return nil
}

// validateActivation rejects ambiguous composition while retaining legacy OR rules.
func validateActivation(when ActivateWhen, prefix string, depth int) error {
	if depth > 32 {
		return fmt.Errorf("%s exceeds maximum activation nesting depth of 32", prefix)
	}
	leaf := len(when.FilesPresent) > 0 || len(when.FilesMatching) > 0 ||
		len(when.DependencyDeclared) > 0 || strings.TrimSpace(when.Detector) != ""
	if when.All != nil {
		if len(when.All) == 0 || leaf {
			return fmt.Errorf("%s.all must contain at least one condition and cannot be combined with leaf fields", prefix)
		}
		for i, child := range when.All {
			if err := validateActivation(child, fmt.Sprintf("%s.all[%d]", prefix, i), depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	if !leaf {
		return fmt.Errorf("%s must contain files-present, files-matching, dependency-declared, detector, or all", prefix)
	}
	return nil
}

func validateScope(scope, prefix string) error {
	switch scope {
	case "", "subtree", "repo", "directory", "matching-files", "nearest-ancestor", "boundary":
		return nil
	default:
		return fmt.Errorf("%s.scope must be one of: repo, subtree, directory, matching-files, nearest-ancestor, boundary", prefix)
	}
}

// BuiltInDetectors returns Skillex's intentionally small baseline detector set.
func BuiltInDetectors() Detectors {
	return Detectors{
		"docker": {
			Matches: []DetectorMatch{
				{File: &FileCondition{Path: "Dockerfile"}},
				{File: &FileCondition{Path: "Dockerfile.*"}},
			},
		},
		"go": {
			Matches: []DetectorMatch{
				{File: &FileCondition{Path: "go.mod"}},
			},
		},
		"javascript": {
			Matches: []DetectorMatch{
				{File: &FileCondition{Path: "package.json"}},
			},
		},
		"typescript": {
			Matches: []DetectorMatch{
				{File: &FileCondition{Path: "tsconfig.json"}},
			},
		},
	}
}

// NewDetectorRegistry creates a registry seeded with built-in detectors.
func NewDetectorRegistry() (*DetectorRegistry, error) {
	registry := &DetectorRegistry{defs: map[string]DetectorRegistration{}}
	if err := registry.RegisterAll(BuiltInDetectors(), "builtin", true); err != nil {
		return nil, err
	}
	return registry, nil
}

// RegisterAll adds detector definitions to the registry.
func (r *DetectorRegistry) RegisterAll(detectors Detectors, source string, builtin bool) error {
	for name, def := range detectors {
		if err := r.Register(name, def, source, builtin); err != nil {
			return err
		}
	}
	return nil
}

// Register adds one detector definition with conflict checks.
func (r *DetectorRegistry) Register(name string, def DetectorDef, source string, builtin bool) error {
	if r.defs == nil {
		r.defs = map[string]DetectorRegistration{}
	}
	existing, ok := r.defs[name]
	if !ok {
		r.defs[name] = DetectorRegistration{Name: name, Def: def, Source: source}
		return nil
	}
	if reflect.DeepEqual(existing.Def, def) {
		return nil
	}
	if existing.Source == "builtin" || builtin {
		return fmt.Errorf("detector %q from %s conflicts with built-in detector", name, source)
	}
	return fmt.Errorf("detector %q from %s conflicts with detector from %s", name, source, existing.Source)
}

// Known reports whether a detector name is registered.
func (r *DetectorRegistry) Known(name string) bool {
	if r == nil {
		return false
	}
	_, ok := r.defs[name]
	return ok
}

// Evaluate returns detector active states for this activation context.
func (r *DetectorRegistry) Evaluate(root string, ctx ActivationContext) (map[string]bool, []error) {
	active := map[string]bool{}
	var errs []error
	if r == nil {
		return active, nil
	}
	for name, registration := range r.defs {
		ok, err := DetectorMatches(root, registration.Def, ctx)
		if err != nil {
			errs = append(errs, fmt.Errorf("detector %s: %w", name, err))
			continue
		}
		if ok {
			active[name] = true
		}
	}
	return active, errs
}

// KnownMap returns a lookup of registered detector names.
func (r *DetectorRegistry) KnownMap() map[string]bool {
	known := map[string]bool{}
	if r == nil {
		return known
	}
	for name := range r.defs {
		known[name] = true
	}
	return known
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
	var projectPacks []*Pack

	for _, manifestPath := range ProjectManifestPaths(root) {
		pack, err := Load(manifestPath)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		projectPacks = append(projectPacks, pack)
	}

	registry, err := NewDetectorRegistry()
	if err != nil {
		errs = append(errs, err)
		return activated, errs
	}
	for _, pack := range projectPacks {
		if err := registry.RegisterAll(pack.Manifest.Detectors, pack.Manifest.Name, false); err != nil {
			errs = append(errs, err)
			continue
		}
	}

	active, detectorErrs := registry.Evaluate(root, ActivationContext{})
	errs = append(errs, detectorErrs...)
	ctx := ActivationContext{
		DetectorKnown:  registry.KnownMap(),
		DetectorActive: active,
	}

	for _, pack := range projectPacks {
		for _, skill := range pack.Manifest.Skills {
			scopes, err := ActivateSkillWithContext(root, skill, ctx)
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

// ActivateProjectMCPServers discovers contextually relevant MCP suggestions in
// project-local packs. It evaluates metadata only and never configures or starts
// a downstream server.
func ActivateProjectMCPServers(root string) ([]ActivatedMCPServer, []error) {
	var activated []ActivatedMCPServer
	var errs []error
	var projectPacks []*Pack

	for _, manifestPath := range ProjectManifestPaths(root) {
		pack, err := Load(manifestPath)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		projectPacks = append(projectPacks, pack)
	}

	registry, err := NewDetectorRegistry()
	if err != nil {
		return activated, append(errs, err)
	}
	for _, pack := range projectPacks {
		if err := registry.RegisterAll(pack.Manifest.Detectors, pack.Manifest.Name, false); err != nil {
			errs = append(errs, err)
		}
	}
	active, detectorErrs := registry.Evaluate(root, ActivationContext{})
	errs = append(errs, detectorErrs...)
	ctx := ActivationContext{DetectorKnown: registry.KnownMap(), DetectorActive: active}

	for _, pack := range projectPacks {
		for _, server := range pack.Manifest.MCPServers {
			scopes, err := ActivateMCPServerWithContext(root, server, ctx)
			if err != nil {
				errs = append(errs, fmt.Errorf("activating pack %s MCP server %s: %w", pack.Manifest.Name, server.Ref, err))
				continue
			}
			if len(scopes) > 0 {
				activated = append(activated, ActivatedMCPServer{Pack: pack, Server: server, Scopes: scopes})
			}
		}
	}
	return activated, errs
}

// ContextForPack evaluates built-in and pack-defined detectors for a dependency context.
func ContextForPack(root string, pack *Pack, ctx ActivationContext) (ActivationContext, []error) {
	registry, err := NewDetectorRegistry()
	if err != nil {
		return ctx, []error{err}
	}
	if pack != nil {
		if err := registry.RegisterAll(pack.Manifest.Detectors, pack.Manifest.Name, false); err != nil {
			return ctx, []error{err}
		}
	}
	active, errs := registry.Evaluate(root, ctx)
	ctx.DetectorKnown = registry.KnownMap()
	ctx.DetectorActive = active
	return ctx, errs
}

// ProjectManifestPaths returns supported project-local pack manifest paths.
func ProjectManifestPaths(root string) []string {
	var paths []string

	rootPack := filepath.Join(root, "skillex", Filename)
	if fileExists(rootPack) {
		paths = append(paths, rootPack)
	}

	for _, packsDir := range []string{filepath.Join(root, "skillex", "packs"), filepath.Join(root, ".skillex", "packs")} {
		entries, err := os.ReadDir(packsDir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
				continue
			}
			path := filepath.Join(packsDir, entry.Name(), Filename)
			if fileExists(path) {
				paths = append(paths, path)
			}
		}
	}

	return paths
}

// ActivateSkill resolves the scopes created by one skill activation rule.
func ActivateSkill(root string, skill SkillRef) ([]string, error) {
	return ActivateSkillWithContext(root, skill, ActivationContext{})
}

// ActivateSkillWithContext resolves scopes using file and dependency activation facts.
func ActivateSkillWithContext(root string, skill SkillRef, ctx ActivationContext) ([]string, error) {
	return activateWithContext(root, skill.ActivateWhen, skill.Scope, skill.Files, ctx)
}

// ActivateMCPServerWithContext resolves contextual scopes for a pack suggestion.
func ActivateMCPServerWithContext(root string, server MCPServerRef, ctx ActivationContext) ([]string, error) {
	return activateWithContext(root, server.ActivateWhen, server.Scope, server.Files, ctx)
}

func activateWithContext(root string, when ActivateWhen, scope string, files []string, ctx ActivationContext) ([]string, error) {
	if err := validateActivation(when, "activate-when", 0); err != nil {
		return nil, err
	}
	matched, matches, err := evaluateActivation(root, when, ctx)
	if err != nil {
		return nil, err
	}
	if !matched {
		return nil, nil
	}
	if scope == "matching-files" && len(files) > 0 {
		matches = nil
		for _, pattern := range files {
			fileMatches, err := MatchRepoFiles(root, pattern)
			if err != nil {
				return nil, err
			}
			matches = appendUnique(matches, fileMatches...)
		}
	}
	if scope == "boundary" {
		return ScopeForContext(ctx, scope), nil
	}
	if len(matches) == 0 {
		return ScopeForContext(ctx, scope), nil
	}
	var scopes []string
	for _, match := range matches {
		scopes = appendUnique(scopes, ScopeForMatch(match, scope)...)
	}
	return scopes, nil
}

// evaluateActivation keeps file evidence for scope mapping after all gates match.
func evaluateActivation(root string, when ActivateWhen, ctx ActivationContext) (bool, []string, error) {
	if when.All != nil {
		matched := true
		var files []string
		for _, child := range when.All {
			ok, matches, err := evaluateActivation(root, child, ctx)
			if err != nil {
				return false, nil, err
			}
			matched = matched && ok
			files = appendUnique(files, matches...)
		}
		return matched, files, nil
	}
	files, err := activationMatches(root, when)
	if err != nil {
		return false, nil, err
	}
	detector, err := DetectorActive(ctx, when.Detector)
	if err != nil {
		return false, nil, err
	}
	return len(files) > 0 || detector || DependencyMatches(ctx.Dependency, when.DependencyDeclared), files, nil
}

// DetectorActive reports whether the named detector matched in the context.
func DetectorActive(ctx ActivationContext, name string) (bool, error) {
	if strings.TrimSpace(name) == "" {
		return false, nil
	}
	if ctx.DetectorKnown == nil || !ctx.DetectorKnown[name] {
		return false, fmt.Errorf("unknown detector %q", name)
	}
	return ctx.DetectorActive[name], nil
}

// DependencyMatches reports whether a dependency fact satisfies any condition.
func DependencyMatches(dep DependencyFact, conditions []DependencyCondition) bool {
	if len(conditions) == 0 {
		return false
	}
	for _, condition := range conditions {
		if condition.Source != "" && condition.Source != dep.Source {
			continue
		}
		if condition.Name != "" && condition.Name != dep.Name {
			continue
		}
		if condition.Version != "" && condition.Version != dep.Version {
			continue
		}
		return true
	}
	return false
}

// ActivationMatches returns repo files that satisfy a skill's file activation rules.
func ActivationMatches(root string, skill SkillRef) ([]string, error) {
	return activationMatches(root, skill.ActivateWhen)
}

func activationMatches(root string, when ActivateWhen) ([]string, error) {
	var matches []string
	for _, pattern := range append(when.FilesPresent, when.FilesMatching...) {
		fileMatches, err := MatchRepoFiles(root, pattern)
		if err != nil {
			return nil, err
		}
		matches = appendUnique(matches, fileMatches...)
	}
	return matches, nil
}

// DetectorMatches reports whether any detector match rule matches the context.
func DetectorMatches(root string, detector DetectorDef, ctx ActivationContext) (bool, error) {
	for _, match := range detector.Matches {
		if match.File != nil {
			matches, err := MatchRepoFiles(root, match.File.Path)
			if err != nil {
				return false, err
			}
			if len(matches) > 0 {
				return true, nil
			}
		}
		if match.Dependency != nil && DependencyMatches(ctx.Dependency, []DependencyCondition{*match.Dependency}) {
			return true, nil
		}
	}
	return false, nil
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
	case "boundary":
		return nil
	case "directory":
		dir := filepath.ToSlash(filepath.Dir(match))
		if dir == "." {
			return []string{"*"}
		}
		return []string{filepath.ToSlash(filepath.Join(dir, "*"))}
	case "matching-files":
		return []string{filepath.ToSlash(match)}
	case "nearest-ancestor":
		dir := filepath.ToSlash(filepath.Dir(match))
		if dir == "." {
			return []string{"**"}
		}
		return []string{filepath.ToSlash(filepath.Join(dir, "**"))}
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

// ScopeForContext maps non-file activation context to a scope.
func ScopeForContext(ctx ActivationContext, scope string) []string {
	switch scope {
	case "repo":
		return []string{"**"}
	case "boundary", "", "subtree", "nearest-ancestor":
		if ctx.BoundaryRel == "" || ctx.BoundaryRel == "." {
			return []string{"**"}
		}
		return []string{filepath.ToSlash(filepath.Join(ctx.BoundaryRel, "**"))}
	case "directory":
		if ctx.BoundaryRel == "" || ctx.BoundaryRel == "." {
			return []string{"*"}
		}
		return []string{filepath.ToSlash(filepath.Join(ctx.BoundaryRel, "*"))}
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
