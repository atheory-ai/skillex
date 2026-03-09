package registry

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS skills (
	id            INTEGER PRIMARY KEY AUTOINCREMENT,
	path          TEXT NOT NULL UNIQUE,
	content       TEXT NOT NULL,
	package_name  TEXT,
	package_ver   TEXT,
	visibility    TEXT NOT NULL,
	source_type   TEXT NOT NULL,
	indexed_at    TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS skill_topics (
	skill_id      INTEGER NOT NULL REFERENCES skills(id) ON DELETE CASCADE,
	topic         TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS skill_tags (
	skill_id      INTEGER NOT NULL REFERENCES skills(id) ON DELETE CASCADE,
	tag           TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS skill_scopes (
	skill_id      INTEGER NOT NULL REFERENCES skills(id) ON DELETE CASCADE,
	scope         TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS skill_tests (
	id            INTEGER PRIMARY KEY AUTOINCREMENT,
	skill_id      INTEGER NOT NULL REFERENCES skills(id) ON DELETE CASCADE,
	name          TEXT NOT NULL,
	prompt        TEXT NOT NULL,
	extra_skills  TEXT,
	criteria      TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_skill_topics ON skill_topics(topic);
CREATE INDEX IF NOT EXISTS idx_skill_tags ON skill_tags(tag);
CREATE INDEX IF NOT EXISTS idx_skill_scopes ON skill_scopes(scope);
CREATE INDEX IF NOT EXISTS idx_skill_tests ON skill_tests(skill_id);
`

// Registry wraps a SQLite database for skill storage and retrieval.
type Registry struct {
	db   *sql.DB
	path string
}

// Skill is a registry entry.
type Skill struct {
	ID             int64
	Path           string
	Content        string
	PackageName    string
	PackageVersion string
	Visibility     string
	SourceType     string
	Topics         []string
	Tags           []string
	Scopes         []string
}

// TestScenario holds a parsed test validation scenario.
type TestScenario struct {
	ID          int64
	SkillID     int64
	Name        string
	Prompt      string
	ExtraSkills []string
	Criteria    []string
}

// Open opens or creates the registry database at the given path.
func Open(path string) (*Registry, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("creating registry directory: %w", err)
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("opening registry: %w", err)
	}

	db.SetMaxOpenConns(1) // SQLite is single-writer

	// Wait up to 5s for concurrent writers instead of failing immediately.
	if _, err := db.Exec("PRAGMA busy_timeout = 5000"); err != nil {
		return nil, err
	}
	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		return nil, err
	}
	if _, err := db.Exec("PRAGMA journal_mode = WAL"); err != nil {
		return nil, err
	}

	if _, err := db.Exec(schema); err != nil {
		return nil, fmt.Errorf("creating schema: %w", err)
	}

	return &Registry{db: db, path: path}, nil
}

// Close closes the database.
func (r *Registry) Close() error {
	return r.db.Close()
}

// Path returns the database file path.
func (r *Registry) Path() string {
	return r.path
}

// Clear removes all data from the registry (for a full rebuild).
func (r *Registry) Clear() error {
	_, err := r.db.Exec(`
		DELETE FROM skill_tests;
		DELETE FROM skill_scopes;
		DELETE FROM skill_tags;
		DELETE FROM skill_topics;
		DELETE FROM skills;
	`)
	return err
}

// InsertSkill inserts a skill into the registry.
func (r *Registry) InsertSkill(s Skill) (int64, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := r.db.Exec(
		`INSERT INTO skills (path, content, package_name, package_ver, visibility, source_type, indexed_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(path) DO UPDATE SET
			content=excluded.content,
			package_name=excluded.package_name,
			package_ver=excluded.package_ver,
			visibility=excluded.visibility,
			source_type=excluded.source_type,
			indexed_at=excluded.indexed_at`,
		s.Path, s.Content, nullStr(s.PackageName), nullStr(s.PackageVersion),
		s.Visibility, s.SourceType, now,
	)
	if err != nil {
		return 0, fmt.Errorf("inserting skill %s: %w", s.Path, err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		// ON CONFLICT path: get the existing ID
		id, err = r.getSkillIDByPath(s.Path)
		if err != nil {
			return 0, err
		}
	}

	// Delete and re-insert topics, tags, scopes
	if _, err := r.db.Exec(`DELETE FROM skill_topics WHERE skill_id = ?`, id); err != nil {
		return 0, err
	}
	if _, err := r.db.Exec(`DELETE FROM skill_tags WHERE skill_id = ?`, id); err != nil {
		return 0, err
	}
	if _, err := r.db.Exec(`DELETE FROM skill_scopes WHERE skill_id = ?`, id); err != nil {
		return 0, err
	}

	for _, topic := range s.Topics {
		if _, err := r.db.Exec(`INSERT INTO skill_topics (skill_id, topic) VALUES (?, ?)`, id, topic); err != nil {
			return 0, err
		}
	}
	for _, tag := range s.Tags {
		if _, err := r.db.Exec(`INSERT INTO skill_tags (skill_id, tag) VALUES (?, ?)`, id, tag); err != nil {
			return 0, err
		}
	}
	for _, scope := range s.Scopes {
		if _, err := r.db.Exec(`INSERT INTO skill_scopes (skill_id, scope) VALUES (?, ?)`, id, scope); err != nil {
			return 0, err
		}
	}

	return id, nil
}

// InsertTestScenario inserts a test scenario linked to a skill.
func (r *Registry) InsertTestScenario(t TestScenario) error {
	_, err := r.db.Exec(
		`INSERT INTO skill_tests (skill_id, name, prompt, extra_skills, criteria) VALUES (?, ?, ?, ?, ?)`,
		t.SkillID, t.Name, t.Prompt,
		nullStr(strings.Join(t.ExtraSkills, ",")),
		strings.Join(t.Criteria, "\n"),
	)
	return err
}

// getSkillIDByPath retrieves a skill ID by path.
func (r *Registry) getSkillIDByPath(path string) (int64, error) {
	var id int64
	err := r.db.QueryRow(`SELECT id FROM skills WHERE path = ?`, path).Scan(&id)
	return id, err
}

// GetSkillByPath retrieves a skill by its path.
func (r *Registry) GetSkillByPath(path string) (*Skill, error) {
	s := &Skill{}
	err := r.db.QueryRow(
		`SELECT id, path, content, COALESCE(package_name,''), COALESCE(package_ver,''), visibility, source_type FROM skills WHERE path = ?`,
		path,
	).Scan(&s.ID, &s.Path, &s.Content, &s.PackageName, &s.PackageVersion, &s.Visibility, &s.SourceType)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := r.populateMeta(s); err != nil {
		return nil, err
	}
	return s, nil
}

// QueryByScope returns all skills visible from the given scope globs.
func (r *Registry) QueryByScope(scopes []string) ([]Skill, error) {
	if len(scopes) == 0 {
		return nil, nil
	}

	placeholders := make([]string, len(scopes))
	args := make([]any, len(scopes))
	for i, s := range scopes {
		placeholders[i] = "?"
		args[i] = s
	}

	query := fmt.Sprintf(`
		SELECT DISTINCT s.id, s.path, s.content, COALESCE(s.package_name,''), COALESCE(s.package_ver,''), s.visibility, s.source_type
		FROM skills s
		JOIN skill_scopes ss ON ss.skill_id = s.id
		WHERE ss.scope IN (%s)
	`, strings.Join(placeholders, ","))

	return r.querySkills(query, args...)
}

// QueryByTopic returns skills matching all given topics.
func (r *Registry) QueryByTopic(topics []string) ([]Skill, error) {
	if len(topics) == 0 {
		return r.AllSkills()
	}

	// Skills that have ALL the given topics
	placeholders := make([]string, len(topics))
	args := make([]any, len(topics)+1)
	for i, t := range topics {
		placeholders[i] = "?"
		args[i] = t
	}
	args[len(topics)] = len(topics)

	query := fmt.Sprintf(`
		SELECT s.id, s.path, s.content, COALESCE(s.package_name,''), COALESCE(s.package_ver,''), s.visibility, s.source_type
		FROM skills s
		WHERE s.id IN (
			SELECT skill_id FROM skill_topics WHERE topic IN (%s)
			GROUP BY skill_id HAVING COUNT(DISTINCT topic) = ?
		)
	`, strings.Join(placeholders, ","))

	return r.querySkills(query, args...)
}

// QueryByTags returns skills matching all given tags.
func (r *Registry) QueryByTags(tags []string) ([]Skill, error) {
	if len(tags) == 0 {
		return r.AllSkills()
	}

	placeholders := make([]string, len(tags))
	args := make([]any, len(tags)+1)
	for i, t := range tags {
		placeholders[i] = "?"
		args[i] = t
	}
	args[len(tags)] = len(tags)

	query := fmt.Sprintf(`
		SELECT s.id, s.path, s.content, COALESCE(s.package_name,''), COALESCE(s.package_ver,''), s.visibility, s.source_type
		FROM skills s
		WHERE s.id IN (
			SELECT skill_id FROM skill_tags WHERE tag IN (%s)
			GROUP BY skill_id HAVING COUNT(DISTINCT tag) = ?
		)
	`, strings.Join(placeholders, ","))

	return r.querySkills(query, args...)
}

// QueryByPackage returns all skills from a specific package.
func (r *Registry) QueryByPackage(pkg string) ([]Skill, error) {
	return r.querySkills(
		`SELECT id, path, content, COALESCE(package_name,''), COALESCE(package_ver,''), visibility, source_type
		 FROM skills WHERE package_name = ?`,
		pkg,
	)
}

// Query executes a compound query with optional filters.
// All provided filters are intersected.
func (r *Registry) Query(path, pkg string, topics, tags []string) ([]Skill, error) {
	// Start with all skills then filter down
	type filterSet struct {
		ids map[int64]bool
		set bool
	}

	var sets []filterSet

	// Note: path filtering is NOT done here at the SQL level.
	// Stored scopes are glob patterns (e.g. "**", "packages/*/**") that require
	// in-process glob matching against the given path. That matching is performed
	// by filterByPath in the query engine after this function returns.

	if len(topics) > 0 {
		skills, err := r.QueryByTopic(topics)
		if err != nil {
			return nil, err
		}
		ids := map[int64]bool{}
		for _, s := range skills {
			ids[s.ID] = true
		}
		sets = append(sets, filterSet{ids: ids, set: true})
	}

	if len(tags) > 0 {
		skills, err := r.QueryByTags(tags)
		if err != nil {
			return nil, err
		}
		ids := map[int64]bool{}
		for _, s := range skills {
			ids[s.ID] = true
		}
		sets = append(sets, filterSet{ids: ids, set: true})
	}

	if pkg != "" {
		skills, err := r.QueryByPackage(pkg)
		if err != nil {
			return nil, err
		}
		ids := map[int64]bool{}
		for _, s := range skills {
			ids[s.ID] = true
		}
		sets = append(sets, filterSet{ids: ids, set: true})
	}

	if len(sets) == 0 {
		return r.AllSkills()
	}

	// Intersect all sets
	merged := sets[0].ids
	for _, fs := range sets[1:] {
		for id := range merged {
			if !fs.ids[id] {
				delete(merged, id)
			}
		}
	}

	// Fetch full skill data for the merged IDs
	if len(merged) == 0 {
		return nil, nil
	}

	ids := make([]any, 0, len(merged))
	placeholders := make([]string, 0, len(merged))
	for id := range merged {
		ids = append(ids, id)
		placeholders = append(placeholders, "?")
	}

	query := fmt.Sprintf(
		`SELECT id, path, content, COALESCE(package_name,''), COALESCE(package_ver,''), visibility, source_type
		 FROM skills WHERE id IN (%s)`,
		strings.Join(placeholders, ","),
	)
	return r.querySkills(query, ids...)
}

// AllSkills returns all non-test skills.
func (r *Registry) AllSkills() ([]Skill, error) {
	return r.querySkills(
		`SELECT id, path, content, COALESCE(package_name,''), COALESCE(package_ver,''), visibility, source_type
		 FROM skills ORDER BY path`,
	)
}

// AllTopics returns a sorted list of all unique topics.
func (r *Registry) AllTopics() ([]string, error) {
	rows, err := r.db.Query(`SELECT DISTINCT topic FROM skill_topics ORDER BY topic`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var topics []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		topics = append(topics, t)
	}
	return topics, rows.Err()
}

// AllTags returns a sorted list of all unique tags.
func (r *Registry) AllTags() ([]string, error) {
	rows, err := r.db.Query(`SELECT DISTINCT tag FROM skill_tags ORDER BY tag`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tags []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		tags = append(tags, t)
	}
	return tags, rows.Err()
}

// AllScopes returns all unique scopes.
func (r *Registry) AllScopes() ([]string, error) {
	rows, err := r.db.Query(`SELECT DISTINCT scope FROM skill_scopes ORDER BY scope`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var scopes []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		scopes = append(scopes, s)
	}
	return scopes, rows.Err()
}

// PackageSummary holds per-package skill counts.
type PackageSummary struct {
	Name    string
	Version string
	Public  int
	Private int
}

// AllPackages returns per-package skill counts.
func (r *Registry) AllPackages() ([]PackageSummary, error) {
	rows, err := r.db.Query(`
		SELECT COALESCE(package_name,''), COALESCE(package_ver,''), visibility, COUNT(*)
		FROM skills
		WHERE package_name IS NOT NULL
		GROUP BY package_name, package_ver, visibility
		ORDER BY package_name
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	pkgMap := map[string]*PackageSummary{}
	for rows.Next() {
		var name, ver, vis string
		var count int
		if err := rows.Scan(&name, &ver, &vis, &count); err != nil {
			return nil, err
		}
		key := name + "@" + ver
		if pkgMap[key] == nil {
			pkgMap[key] = &PackageSummary{Name: name, Version: ver}
		}
		switch vis {
		case "public":
			pkgMap[key].Public += count
		case "private":
			pkgMap[key].Private += count
		}
	}

	result := make([]PackageSummary, 0, len(pkgMap))
	for _, v := range pkgMap {
		result = append(result, *v)
	}
	return result, rows.Err()
}

// GetTestScenarios returns all test scenarios for a skill ID.
func (r *Registry) GetTestScenarios(skillID int64) ([]TestScenario, error) {
	rows, err := r.db.Query(
		`SELECT id, skill_id, name, prompt, COALESCE(extra_skills,''), criteria FROM skill_tests WHERE skill_id = ?`,
		skillID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var scenarios []TestScenario
	for rows.Next() {
		var t TestScenario
		var extraStr, criteriaStr string
		if err := rows.Scan(&t.ID, &t.SkillID, &t.Name, &t.Prompt, &extraStr, &criteriaStr); err != nil {
			return nil, err
		}
		if extraStr != "" {
			t.ExtraSkills = strings.Split(extraStr, ",")
		}
		t.Criteria = strings.Split(criteriaStr, "\n")
		scenarios = append(scenarios, t)
	}
	return scenarios, rows.Err()
}

// SkillCount returns the total number of skills in the registry.
func (r *Registry) SkillCount() (int, error) {
	var count int
	err := r.db.QueryRow(`SELECT COUNT(*) FROM skills`).Scan(&count)
	return count, err
}

// querySkills executes a query and populates Skill slices with metadata.
func (r *Registry) querySkills(query string, args ...any) ([]Skill, error) {
	rows, err := r.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var skills []Skill
	for rows.Next() {
		var s Skill
		if err := rows.Scan(&s.ID, &s.Path, &s.Content, &s.PackageName, &s.PackageVersion, &s.Visibility, &s.SourceType); err != nil {
			return nil, err
		}
		skills = append(skills, s)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for i := range skills {
		if err := r.populateMeta(&skills[i]); err != nil {
			return nil, err
		}
	}

	return skills, nil
}

// populateMeta loads topics, tags, and scopes for a skill.
func (r *Registry) populateMeta(s *Skill) error {
	rows, err := r.db.Query(`SELECT topic FROM skill_topics WHERE skill_id = ?`, s.ID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			rows.Close()
			return err
		}
		s.Topics = append(s.Topics, t)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	rows, err = r.db.Query(`SELECT tag FROM skill_tags WHERE skill_id = ?`, s.ID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			rows.Close()
			return err
		}
		s.Tags = append(s.Tags, t)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	rows, err = r.db.Query(`SELECT scope FROM skill_scopes WHERE skill_id = ?`, s.ID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var sc string
		if err := rows.Scan(&sc); err != nil {
			rows.Close()
			return err
		}
		s.Scopes = append(s.Scopes, sc)
	}
	rows.Close()
	return rows.Err()
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}
