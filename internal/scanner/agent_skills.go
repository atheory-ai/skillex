package scanner

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// projectAgentSkillParents are the project directories that agent tools use
// for skills. Each one contains a skills/ directory of <name>/SKILL.md trees.
// Home directories such as ~/.cursor/skills are not scanned.
var projectAgentSkillParents = []string{
	".agents",
	".claude",
	".codex",
	".cursor",
	".gemini",
	".github",
}

// scanProjectAgentSkills indexes SKILL.md files that Cursor, Claude Code,
// VS Code / Copilot, and other agent tools write inside the repository.
// A later refresh scans these directories again, so skills added or removed
// since the last index are picked up without a config change.
func (s *Scanner) scanProjectAgentSkills() ([]SkillFile, []error) {
	skills, _, errs := s.discoverAgentSkills()
	return skills, errs
}

// DiscoverAgentSkills returns project SKILL.md files and the skill roots that
// contain them. Roots are the directories a watcher should register.
func DiscoverAgentSkills(root string) ([]SkillFile, []string, []error) {
	s := &Scanner{root: root}
	return s.discoverAgentSkills()
}

// ReadAgentSkill reads one SKILL.md and its sibling SKILL.test.md.
func ReadAgentSkill(root, absPath string) ([]SkillFile, error) {
	s := &Scanner{root: root}
	var skills []SkillFile
	var errs []error
	s.indexAgentSkill(absPath, map[string]struct{}{}, &skills, &errs)
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return skills, nil
}

func (s *Scanner) discoverAgentSkills() ([]SkillFile, []string, []error) {
	var skills []SkillFile
	var errs []error
	roots := map[string]struct{}{}
	s.walkAgentSkillDir(s.root, map[string]struct{}{}, map[string]struct{}{}, &skills, &errs, roots)
	list := make([]string, 0, len(roots))
	for root := range roots {
		list = append(list, root)
	}
	sort.Strings(list)
	return skills, list, errs
}

func (s *Scanner) walkAgentSkillDir(dir string, seenDirs, seenFiles map[string]struct{}, skills *[]SkillFile, errs *[]error, roots map[string]struct{}) {
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		*errs = append(*errs, fmt.Errorf("agent skills: %s: %w", dir, err))
		return
	}
	if _, seen := seenDirs[resolved]; seen {
		return
	}
	escapes, err := pathEscapesRoot(s.root, resolved)
	if err != nil {
		*errs = append(*errs, fmt.Errorf("agent skills: %s: %w", dir, err))
		return
	}
	if escapes {
		return
	}
	seenDirs[resolved] = struct{}{}
	if roots != nil && isAgentSkillRoot(s.root, dir) {
		roots[dir] = struct{}{}
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		*errs = append(*errs, fmt.Errorf("agent skills: %s: %w", dir, err))
		return
	}
	for _, entry := range entries {
		if skipAgentWalkDir(entry.Name()) {
			continue
		}
		entryPath := filepath.Join(dir, entry.Name())
		if entry.Type()&os.ModeSymlink != 0 {
			target, statErr := os.Stat(entryPath)
			if statErr != nil {
				if entry.Name() == "SKILL.md" {
					*errs = append(*errs, fmt.Errorf("agent skill %s: %w", entryPath, statErr))
				}
				continue
			}
			if target.IsDir() {
				s.walkAgentSkillDir(entryPath, seenDirs, seenFiles, skills, errs, roots)
				continue
			}
		} else if entry.IsDir() {
			s.walkAgentSkillDir(entryPath, seenDirs, seenFiles, skills, errs, roots)
			continue
		}
		if entry.Name() == "SKILL.md" {
			s.indexAgentSkill(entryPath, seenFiles, skills, errs)
		}
	}
}

func (s *Scanner) indexAgentSkill(skillPath string, seenFiles map[string]struct{}, skills *[]SkillFile, errs *[]error) {
	resolved, err := filepath.EvalSymlinks(skillPath)
	if err != nil {
		*errs = append(*errs, fmt.Errorf("agent skill %s: %w", skillPath, err))
		return
	}
	if _, seen := seenFiles[resolved]; seen {
		return
	}
	escapes, err := pathEscapesRoot(s.root, resolved)
	if err != nil {
		*errs = append(*errs, fmt.Errorf("agent skill %s: %w", skillPath, err))
		return
	}
	rel, relErr := filepath.Rel(s.root, skillPath)
	if relErr != nil {
		*errs = append(*errs, fmt.Errorf("agent skill %s: %w", skillPath, relErr))
		return
	}
	rel = filepath.ToSlash(rel)
	if _, ok := agentSkillAnchor(rel); !ok {
		return
	}
	if escapes {
		*errs = append(*errs, fmt.Errorf("agent skill %s: path escapes the repository", rel))
		return
	}
	seenFiles[resolved] = struct{}{}

	files, err := s.readSkillFile(skillPath, rel, "", "", "repo", "agent", "", "")
	if err != nil {
		*errs = append(*errs, fmt.Errorf("agent skill %s: %w", rel, err))
		return
	}
	for i := range files {
		if files[i].IsTest {
			continue
		}
		if strings.TrimSpace(files[i].Frontmatter.Name) == "" {
			files[i].Frontmatter.Name = agentSkillDirectoryName(rel)
		}
		files[i].ExplicitScopes = agentSkillScopes(rel, files[i].Frontmatter.PathGlobs())
	}
	*skills = append(*skills, files...)

	testRel := strings.TrimSuffix(rel, ".md") + ".test.md"
	testAbs := filepath.Join(s.root, filepath.FromSlash(testRel))
	testResolved, err := filepath.EvalSymlinks(testAbs)
	if err != nil {
		if os.IsNotExist(err) {
			return
		}
		*errs = append(*errs, fmt.Errorf("agent test %s: %w", testRel, err))
		return
	}
	testEscapes, err := pathEscapesRoot(s.root, testResolved)
	if err != nil {
		*errs = append(*errs, fmt.Errorf("agent test %s: %w", testRel, err))
		return
	}
	if testEscapes {
		*errs = append(*errs, fmt.Errorf("agent test %s: path escapes the repository", testRel))
		return
	}
	testFiles, err := s.readSkillFile(testAbs, testRel, "", "", "repo", "agent", "", "")
	if err != nil {
		*errs = append(*errs, fmt.Errorf("agent test %s: %w", testRel, err))
		return
	}
	*skills = append(*skills, testFiles...)
}

func skipAgentWalkDir(name string) bool {
	switch name {
	case ".git", ".skillex", "node_modules":
		return true
	default:
		return false
	}
}

func isAgentSkillRoot(repoRoot, dir string) bool {
	rel, err := filepath.Rel(repoRoot, dir)
	if err != nil {
		return false
	}
	rel = filepath.ToSlash(rel)
	return path.Base(rel) == "skills" && isAgentSkillParent(path.Base(path.Dir(rel)))
}

func isAgentSkillParent(name string) bool {
	for _, parent := range projectAgentSkillParents {
		if name == parent {
			return true
		}
	}
	return false
}

// agentSkillAnchor reports the project directory that owns a skill file.
// ".cursor/skills/land-it/SKILL.md" is repo-wide. A nested
// "apps/web/.cursor/skills/deploy/SKILL.md" is owned by apps/web.
func agentSkillAnchor(rel string) (string, bool) {
	parts := strings.Split(filepath.ToSlash(rel), "/")
	for i := 0; i+1 < len(parts); i++ {
		if parts[i+1] == "skills" && isAgentSkillParent(parts[i]) {
			if i == 0 {
				return "", true
			}
			return strings.Join(parts[:i], "/"), true
		}
	}
	return "", false
}

func agentSkillDirectoryName(rel string) string {
	base := path.Base(path.Dir(filepath.ToSlash(rel)))
	if base == "skills" || base == "." {
		return ""
	}
	return base
}

// agentSkillScopes maps a discovered skill onto skillex scope globs.
// Repo-root skills apply everywhere. Nested skill directories apply inside
// the package that contains them. An explicit paths or globs list narrows
// that further, and paths wins when both are set.
func agentSkillScopes(rel string, globs []string) []string {
	anchor, _ := agentSkillAnchor(rel)
	if len(globs) == 0 {
		if anchor == "" {
			return []string{"**"}
		}
		return []string{anchor + "/**"}
	}
	return qualifyAgentGlobs(anchor, globs)
}

func qualifyAgentGlobs(anchor string, globs []string) []string {
	var out []string
	seen := map[string]bool{}
	add := func(glob string) {
		for _, expanded := range expandEmptyDoubleStar(glob) {
			if expanded == "" || seen[expanded] {
				continue
			}
			seen[expanded] = true
			out = append(out, expanded)
		}
	}
	for _, glob := range globs {
		glob = filepath.ToSlash(strings.TrimSpace(glob))
		if glob == "" {
			continue
		}
		if anchor == "" {
			add(glob)
			continue
		}
		switch {
		case glob == "**" || glob == "**/**":
			add(anchor + "/**")
		case strings.HasPrefix(glob, "**/"):
			add(anchor + "/" + glob)
		case glob == anchor || strings.HasPrefix(glob, anchor+"/"):
			add(glob)
		}
	}
	return out
}

// expandEmptyDoubleStar compensates for the registry matcher, where "**" does
// not match an empty directory segment. "**/*.tsx" misses page.tsx, and
// "apps/web/**/*.tsx" misses apps/web/page.tsx, unless the zero-segment form
// is stored beside the original pattern.
func expandEmptyDoubleStar(glob string) []string {
	out := []string{glob}
	current := glob
	for strings.Contains(current, "/**/") || strings.HasPrefix(current, "**/") {
		next := current
		if strings.HasPrefix(next, "**/") {
			next = strings.TrimPrefix(next, "**/")
		} else {
			next = strings.Replace(next, "/**/", "/", 1)
		}
		if next == "" || next == current {
			break
		}
		out = append(out, next)
		current = next
	}
	return out
}

func pathEscapesRoot(root, target string) (bool, error) {
	rootResolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		return false, err
	}
	targetResolved, err := filepath.EvalSymlinks(target)
	if err != nil {
		return false, err
	}
	rel, err := filepath.Rel(rootResolved, targetResolved)
	if err != nil {
		return true, nil
	}
	return rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)), nil
}
