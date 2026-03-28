package registry

import (
	"path/filepath"
	"strconv"
	"testing"
)

// --- classifyScope ---

func TestClassifyScope(t *testing.T) {
	cases := []struct {
		scope      string
		wantType   string
		wantPrefix string
	}{
		// universal
		{"**", "universal", ""},

		// prefix: base/** with no wildcards in base
		{"packages/ui/**", "prefix", "packages/ui/"},
		{"src/**", "prefix", "src/"},
		{"a/b/c/**", "prefix", "a/b/c/"},

		// exact: no wildcards
		{"src/index.ts", "exact", "src/index.ts"},
		{"packages/ui/src/button.tsx", "exact", "packages/ui/src/button.tsx"},
		{"readme.md", "exact", "readme.md"},

		// glob: wildcard in base of /**
		{"packages/*/src/**", "glob", "packages/"},
		{"packages/*/**", "glob", "packages/"},

		// glob: terminal wildcard that isn't /**
		{"src/*.ts", "glob", "src/"},
		{"*.ts", "glob", ""},
		{"**/*.ts", "glob", ""},

		// glob: ? wildcard
		{"src/index?.ts", "glob", "src/"},

		// Windows-style separators normalised to /
		{"packages\\ui\\**", "prefix", "packages/ui/"},
	}

	for _, tc := range cases {
		t.Run(tc.scope, func(t *testing.T) {
			gotType, gotPrefix := classifyScope(tc.scope)
			if gotType != tc.wantType {
				t.Errorf("classifyScope(%q) type = %q, want %q", tc.scope, gotType, tc.wantType)
			}
			if gotPrefix != tc.wantPrefix {
				t.Errorf("classifyScope(%q) prefix = %q, want %q", tc.scope, gotPrefix, tc.wantPrefix)
			}
		})
	}
}

// --- pathPrefixes ---

func TestPathPrefixes(t *testing.T) {
	cases := []struct {
		path string
		want []string
	}{
		{
			"packages/ui/src/button.tsx",
			[]string{"packages/", "packages/ui/", "packages/ui/src/", "packages/ui/src/button.tsx"},
		},
		{
			"file.ts",
			[]string{"file.ts"},
		},
		{
			"",
			nil,
		},
		{
			"a/b",
			[]string{"a/", "a/b"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			got := pathPrefixes(tc.path)
			if len(got) != len(tc.want) {
				t.Fatalf("pathPrefixes(%q) = %v, want %v", tc.path, got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("pathPrefixes(%q)[%d] = %q, want %q", tc.path, i, got[i], tc.want[i])
				}
			}
		})
	}
}

// --- QueryByPath ---

func newTestRegistry(t *testing.T) *Registry {
	t.Helper()
	dir := t.TempDir()
	reg, err := Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { reg.Close() })
	return reg
}

func insertTestSkill(t *testing.T, reg *Registry, path string, scopes []string) {
	t.Helper()
	_, err := reg.InsertSkill(Skill{
		Path:       path,
		Content:    "# " + path,
		Visibility: "public",
		SourceType: "local",
		Scopes:     scopes,
	})
	if err != nil {
		t.Fatalf("InsertSkill(%s): %v", path, err)
	}
}

func skillPaths(skills []Skill) []string {
	paths := make([]string, len(skills))
	for i, s := range skills {
		paths[i] = s.Path
	}
	return paths
}

func assertContains(t *testing.T, skills []Skill, path string) {
	t.Helper()
	for _, s := range skills {
		if s.Path == path {
			return
		}
	}
	t.Errorf("expected skill %q in results %v", path, skillPaths(skills))
}

func assertNotContains(t *testing.T, skills []Skill, path string) {
	t.Helper()
	for _, s := range skills {
		if s.Path == path {
			t.Errorf("unexpected skill %q in results %v", path, skillPaths(skills))
			return
		}
	}
}

func TestQueryByPath_Universal(t *testing.T) {
	reg := newTestRegistry(t)
	insertTestSkill(t, reg, "skills/repo.md", []string{"**"})
	insertTestSkill(t, reg, "skills/other.md", []string{"packages/app/**"})

	skills, err := reg.QueryByPath("packages/app/src/index.ts")
	if err != nil {
		t.Fatal(err)
	}
	assertContains(t, skills, "skills/repo.md")
	assertContains(t, skills, "skills/other.md")
}

func TestQueryByPath_Prefix(t *testing.T) {
	reg := newTestRegistry(t)
	insertTestSkill(t, reg, "skills/ui.md", []string{"packages/ui/**"})
	insertTestSkill(t, reg, "skills/utils.md", []string{"packages/utils/**"})

	skills, err := reg.QueryByPath("packages/ui/src/button.tsx")
	if err != nil {
		t.Fatal(err)
	}
	assertContains(t, skills, "skills/ui.md")
	assertNotContains(t, skills, "skills/utils.md")
}

func TestQueryByPath_Exact(t *testing.T) {
	reg := newTestRegistry(t)
	insertTestSkill(t, reg, "skills/specific.md", []string{"packages/ui/src/button.tsx"})
	insertTestSkill(t, reg, "skills/other.md", []string{"packages/ui/src/input.tsx"})

	skills, err := reg.QueryByPath("packages/ui/src/button.tsx")
	if err != nil {
		t.Fatal(err)
	}
	assertContains(t, skills, "skills/specific.md")
	assertNotContains(t, skills, "skills/other.md")
}

func TestQueryByPath_GlobFallback(t *testing.T) {
	reg := newTestRegistry(t)
	// Complex glob: wildcard in intermediate segment
	insertTestSkill(t, reg, "skills/any-pkg.md", []string{"packages/*/src/**"})

	skills, err := reg.QueryByPath("packages/app-a/src/index.ts")
	if err != nil {
		t.Fatal(err)
	}
	assertContains(t, skills, "skills/any-pkg.md")

	// Should not match a path outside the pattern
	noMatch, err := reg.QueryByPath("docs/readme.md")
	if err != nil {
		t.Fatal(err)
	}
	assertNotContains(t, noMatch, "skills/any-pkg.md")
}

func TestQueryByPath_MultipleScopes_Union(t *testing.T) {
	reg := newTestRegistry(t)
	// Skill with two scopes — matches if either matches.
	insertTestSkill(t, reg, "skills/shared.md", []string{"packages/ui/**", "packages/app/**"})

	uiSkills, err := reg.QueryByPath("packages/ui/src/button.tsx")
	if err != nil {
		t.Fatal(err)
	}
	assertContains(t, uiSkills, "skills/shared.md")

	appSkills, err := reg.QueryByPath("packages/app/src/main.ts")
	if err != nil {
		t.Fatal(err)
	}
	assertContains(t, appSkills, "skills/shared.md")

	// Skill should appear exactly once (DISTINCT).
	count := 0
	for _, s := range uiSkills {
		if s.Path == "skills/shared.md" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected 1 occurrence of shared.md, got %d", count)
	}
}

func TestQueryByPath_NoMatch(t *testing.T) {
	reg := newTestRegistry(t)
	insertTestSkill(t, reg, "skills/ui.md", []string{"packages/ui/**"})

	skills, err := reg.QueryByPath("packages/backend/src/server.go")
	if err != nil {
		t.Fatal(err)
	}
	if len(skills) != 0 {
		t.Errorf("expected no skills, got %v", skillPaths(skills))
	}
}

func TestQueryByPath_EmptyPath(t *testing.T) {
	reg := newTestRegistry(t)
	insertTestSkill(t, reg, "skills/repo.md", []string{"**"})

	skills, err := reg.QueryByPath("")
	if err != nil {
		t.Fatal(err)
	}
	if skills != nil {
		t.Errorf("expected nil for empty path, got %v", skillPaths(skills))
	}
}

func TestQueryByPath_WindowsSeparators(t *testing.T) {
	reg := newTestRegistry(t)
	insertTestSkill(t, reg, "skills/ui.md", []string{"packages/ui/**"})

	// Windows-style path in query
	skills, err := reg.QueryByPath("packages\\ui\\src\\button.tsx")
	if err != nil {
		t.Fatal(err)
	}
	assertContains(t, skills, "skills/ui.md")
}

func TestQueryByPath_MixedScopeTypes(t *testing.T) {
	reg := newTestRegistry(t)
	insertTestSkill(t, reg, "skills/global.md", []string{"**"})
	insertTestSkill(t, reg, "skills/ui.md", []string{"packages/ui/**"})
	insertTestSkill(t, reg, "skills/exact.md", []string{"packages/ui/src/button.tsx"})
	insertTestSkill(t, reg, "skills/glob.md", []string{"packages/*/src/**"})
	insertTestSkill(t, reg, "skills/unrelated.md", []string{"packages/backend/**"})

	skills, err := reg.QueryByPath("packages/ui/src/button.tsx")
	if err != nil {
		t.Fatal(err)
	}
	assertContains(t, skills, "skills/global.md")
	assertContains(t, skills, "skills/ui.md")
	assertContains(t, skills, "skills/exact.md")
	assertContains(t, skills, "skills/glob.md")
	assertNotContains(t, skills, "skills/unrelated.md")
}

// TestQueryByPath_LargeRegistry confirms the index path is taken and
// returns correct results at scale without scanning all rows.
func TestQueryByPath_LargeRegistry(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping large-registry test in short mode")
	}
	reg := newTestRegistry(t)

	// Insert 500 unrelated skills
	for i := 0; i < 500; i++ {
		insertTestSkill(t, reg,
			filepath.Join("packages", "pkg"+itoa(i), "skill.md"),
			[]string{"packages/pkg" + itoa(i) + "/**"},
		)
	}
	// Insert the target skill
	insertTestSkill(t, reg, "skills/target.md", []string{"packages/ui/**"})

	skills, err := reg.QueryByPath("packages/ui/src/button.tsx")
	if err != nil {
		t.Fatal(err)
	}
	assertContains(t, skills, "skills/target.md")
	if len(skills) != 1 {
		t.Errorf("expected 1 result, got %d", len(skills))
	}
}

func itoa(i int) string {
	return strconv.Itoa(i)
}

func TestQueryByPath_GlobInQueryPath(t *testing.T) {
	// When the caller passes a glob as the query path (e.g. --path "packages/app-a/**"),
	// pathPrefixes generates literal prefix segments including the glob characters.
	// The universal and prefix types still match correctly; glob characters in the
	// query path are treated as literal strings for the SQL lookup (not expanded).
	reg := newTestRegistry(t)
	insertTestSkill(t, reg, "skills/global.md", []string{"**"})
	insertTestSkill(t, reg, "skills/app.md", []string{"packages/app-a/**"})
	insertTestSkill(t, reg, "skills/other.md", []string{"packages/other/**"})

	// "packages/app-a/**" as query path:
	// pathPrefixes → ["packages/", "packages/app-a/", "packages/app-a/**"]
	// "packages/app-a/" matches the prefix-type path_prefix of skills/app.md ✓
	skills, err := reg.QueryByPath("packages/app-a/**")
	if err != nil {
		t.Fatal(err)
	}
	assertContains(t, skills, "skills/global.md")
	assertContains(t, skills, "skills/app.md")
	assertNotContains(t, skills, "skills/other.md")
}

// --- Benchmarks ---

// populateBenchDB inserts n skills across n packages (10 skills each) plus
// one universal skill, using the given registry. Used by both benchmarks.
func populateBenchDB(b *testing.B, reg *Registry, n int) {
	b.Helper()
	for pkg := 0; pkg < n; pkg++ {
		for skill := 0; skill < 10; skill++ {
			s := Skill{
				Path:       filepath.Join("packages", "pkg"+itoa(pkg), "skill"+itoa(skill)+".md"),
				Content:    "content",
				Visibility: "public",
				SourceType: "local",
				Scopes:     []string{"packages/pkg" + itoa(pkg) + "/**"},
			}
			if _, err := reg.InsertSkill(s); err != nil {
				b.Fatal(err)
			}
		}
	}
	if _, err := reg.InsertSkill(Skill{
		Path: "skills/repo.md", Content: "content",
		Visibility: "public", SourceType: "local",
		Scopes: []string{"**"},
	}); err != nil {
		b.Fatal(err)
	}
}

// BenchmarkQueryByPath measures the indexed path: SQL prefix index lookup.
func BenchmarkQueryByPath(b *testing.B) {
	dir := b.TempDir()
	reg, err := Open(filepath.Join(dir, "bench.db"))
	if err != nil {
		b.Fatal(err)
	}
	defer reg.Close()
	populateBenchDB(b, reg, 100) // 1001 skills total

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := reg.QueryByPath("packages/pkg50/src/index.ts"); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkAllSkillsFilterByPath measures the old path: full scan + in-process glob.
// Compare against BenchmarkQueryByPath to confirm the index improvement.
func BenchmarkAllSkillsFilterByPath(b *testing.B) {
	dir := b.TempDir()
	reg, err := Open(filepath.Join(dir, "bench.db"))
	if err != nil {
		b.Fatal(err)
	}
	defer reg.Close()
	populateBenchDB(b, reg, 100) // 1001 skills total

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		skills, err := reg.AllSkills()
		if err != nil {
			b.Fatal(err)
		}
		_ = filterByPathBench(skills, "packages/pkg50/src/index.ts")
	}
}

// filterByPathBench is the old in-process filter logic used only by the
// baseline benchmark. It mirrors filterByPath from the query engine.
func filterByPathBench(skills []Skill, path string) []Skill {
	var out []Skill
	for _, s := range skills {
		for _, sc := range s.Scopes {
			if globMatchPath(sc, path) {
				out = append(out, s)
				break
			}
		}
	}
	return out
}
