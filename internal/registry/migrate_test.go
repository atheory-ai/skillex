package registry

import (
	"database/sql"
	"path/filepath"
	"sync"
	"testing"

	_ "modernc.org/sqlite"
)

// openRawDB opens a SQLite database without running any schema or migrations.
func openRawDB(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec("PRAGMA busy_timeout = 5000"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("PRAGMA journal_mode = WAL"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// schemaV1 is the original skill_scopes table without path_prefix/pattern_type.
const schemaV1 = `
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
	skill_id INTEGER NOT NULL REFERENCES skills(id) ON DELETE CASCADE,
	topic    TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS skill_tags (
	skill_id INTEGER NOT NULL REFERENCES skills(id) ON DELETE CASCADE,
	tag      TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS skill_scopes (
	skill_id INTEGER NOT NULL REFERENCES skills(id) ON DELETE CASCADE,
	scope    TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS skill_tests (
	id           INTEGER PRIMARY KEY AUTOINCREMENT,
	skill_id     INTEGER NOT NULL REFERENCES skills(id) ON DELETE CASCADE,
	name         TEXT NOT NULL,
	prompt       TEXT NOT NULL,
	extra_skills TEXT,
	criteria     TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_skill_topics ON skill_topics(topic);
CREATE INDEX IF NOT EXISTS idx_skill_tags ON skill_tags(tag);
CREATE INDEX IF NOT EXISTS idx_skill_scopes ON skill_scopes(scope);
CREATE INDEX IF NOT EXISTS idx_skill_tests ON skill_tests(skill_id);
`

func TestMigrate_FreshDB_SetsVersion(t *testing.T) {
	dir := t.TempDir()
	reg, err := Open(filepath.Join(dir, "fresh.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	reg.Close()

	db := openRawDB(t, filepath.Join(dir, "fresh.db"))
	var ver int
	db.QueryRow("PRAGMA user_version").Scan(&ver)
	if ver != currentSchemaVersion {
		t.Errorf("user_version = %d, want %d", ver, currentSchemaVersion)
	}
}

func TestMigrate_FreshDB_HasNewColumns(t *testing.T) {
	dir := t.TempDir()
	reg, err := Open(filepath.Join(dir, "fresh.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	reg.Close()

	db := openRawDB(t, filepath.Join(dir, "fresh.db"))
	cols, err := columnNames(db, "skill_scopes")
	if err != nil {
		t.Fatal(err)
	}
	if !cols["path_prefix"] {
		t.Error("missing path_prefix column on fresh DB")
	}
	if !cols["pattern_type"] {
		t.Error("missing pattern_type column on fresh DB")
	}
}

func TestMigrate_OldSchema_AddsColumns(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "old.db")

	// Create a v1 database manually (no path_prefix/pattern_type).
	rawDB := openRawDB(t, dbPath)
	if _, err := rawDB.Exec(schemaV1); err != nil {
		t.Fatalf("creating v1 schema: %v", err)
	}
	// Verify the old columns are absent.
	cols, _ := columnNames(rawDB, "skill_scopes")
	if cols["path_prefix"] || cols["pattern_type"] {
		t.Fatal("test setup error: v1 schema should not have new columns")
	}
	rawDB.Close()

	// Open via registry.Open() — should migrate.
	reg, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open (migration): %v", err)
	}
	reg.Close()

	// Verify migration ran.
	db := openRawDB(t, dbPath)
	var ver int
	db.QueryRow("PRAGMA user_version").Scan(&ver)
	if ver != currentSchemaVersion {
		t.Errorf("user_version = %d, want %d", ver, currentSchemaVersion)
	}
	cols, err = columnNames(db, "skill_scopes")
	if err != nil {
		t.Fatal(err)
	}
	if !cols["path_prefix"] {
		t.Error("path_prefix column missing after migration")
	}
	if !cols["pattern_type"] {
		t.Error("pattern_type column missing after migration")
	}
}

func TestMigrate_Idempotent(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "idempotent.db")

	// Open twice — should not error on second call.
	for i := 0; i < 2; i++ {
		reg, err := Open(dbPath)
		if err != nil {
			t.Fatalf("Open #%d: %v", i+1, err)
		}
		reg.Close()
	}

	db := openRawDB(t, dbPath)
	var ver int
	db.QueryRow("PRAGMA user_version").Scan(&ver)
	if ver != currentSchemaVersion {
		t.Errorf("user_version = %d after double open, want %d", ver, currentSchemaVersion)
	}
}

func TestMigrate_OldSchema_ExistingRowsDefaultToGlob(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "existing.db")

	// Create v1 DB and insert a skill with a scope.
	rawDB := openRawDB(t, dbPath)
	if _, err := rawDB.Exec(schemaV1); err != nil {
		t.Fatal(err)
	}
	rawDB.Exec(`INSERT INTO skills (path, content, visibility, source_type, indexed_at)
	             VALUES ('skills/repo.md','content','public','local','2024-01-01T00:00:00Z')`)
	rawDB.Exec(`INSERT INTO skill_scopes (skill_id, scope) VALUES (1, 'packages/**')`)
	rawDB.Close()

	// Migrate.
	reg, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	reg.Close()

	// Existing row should have default values.
	db := openRawDB(t, dbPath)
	var pt, pp string
	db.QueryRow(`SELECT pattern_type, path_prefix FROM skill_scopes WHERE scope = 'packages/**'`).Scan(&pt, &pp)
	if pt != "glob" {
		t.Errorf("pattern_type = %q, want %q (existing rows default to glob for safety)", pt, "glob")
	}
	if pp != "" {
		t.Errorf("path_prefix = %q, want %q (existing rows default to empty)", pp, "")
	}
}

func TestMigrate_Concurrent(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "concurrent.db")

	// Create a v1 database first.
	rawDB := openRawDB(t, dbPath)
	if _, err := rawDB.Exec(schemaV1); err != nil {
		t.Fatal(err)
	}
	rawDB.Close()

	const goroutines = 4
	errs := make(chan error, goroutines)
	var wg sync.WaitGroup

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			reg, err := Open(dbPath)
			if err != nil {
				errs <- err
				return
			}
			reg.Close()
		}()
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		if err != nil {
			t.Errorf("concurrent Open error: %v", err)
		}
	}

	// Final version should be correct.
	db := openRawDB(t, dbPath)
	var ver int
	db.QueryRow("PRAGMA user_version").Scan(&ver)
	if ver != currentSchemaVersion {
		t.Errorf("user_version = %d after concurrent open, want %d", ver, currentSchemaVersion)
	}
}

// TestMigrate_PartialMigration_IsRecoverable simulates a crash between the two
// ALTER TABLE statements (path_prefix added, pattern_type not yet added).
// The migration must be able to complete on the next Open() call, which is the
// observable consequence of the BEGIN IMMEDIATE / ROLLBACK guarantee: any
// partial state must either be fully rolled back or left in a state that the
// idempotent migration can safely finish.
func TestMigrate_PartialMigration_IsRecoverable(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "partial.db")

	// Create a v1 DB and manually add only path_prefix (simulating a crash
	// after the first ALTER TABLE committed but before the second).
	rawDB := openRawDB(t, dbPath)
	if _, err := rawDB.Exec(schemaV1); err != nil {
		t.Fatal(err)
	}
	if _, err := rawDB.Exec(`ALTER TABLE skill_scopes ADD COLUMN path_prefix TEXT NOT NULL DEFAULT ''`); err != nil {
		t.Fatal(err)
	}
	// user_version remains 0 — migration never finished.
	rawDB.Close()

	// Open should detect the partial state and complete the migration.
	reg, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open after partial migration: %v", err)
	}
	reg.Close()

	db := openRawDB(t, dbPath)
	var ver int
	db.QueryRow("PRAGMA user_version").Scan(&ver)
	if ver != currentSchemaVersion {
		t.Errorf("user_version = %d, want %d", ver, currentSchemaVersion)
	}
	cols, err := columnNames(db, "skill_scopes")
	if err != nil {
		t.Fatal(err)
	}
	if !cols["pattern_type"] {
		t.Error("pattern_type missing after recovery from partial migration")
	}
}
