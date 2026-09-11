package tix

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
)

var db *sql.DB

func dbPath() string {
	if path := os.Getenv("TIX_DB"); path != "" {
		return path
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".tix", "tix.db")
}

// InitDB opens the database and brings its schema up to date. It reports
// failures instead of exiting so callers that only meant to open the database
// on the way to real work can surface the error themselves.
func InitDB() error {
	path := dbPath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("mkdir failed: %w", err)
	}
	var err error
	db, err = sql.Open("sqlite", dsn(path))
	if err != nil {
		return fmt.Errorf("db open failed: %w", err)
	}
	// Agents in different repos share one database file, so writes are
	// serialized in-process too: SQLite allows a single writer, and one
	// connection keeps this process from contending with itself.
	db.SetMaxOpenConns(1)

	if err := migrate(); err != nil {
		return fmt.Errorf("schema migration failed: %w", err)
	}
	return nil
}

// CloseDB closes the open database, if any, so a caller outside this package
// can hand the next InitDB a clean slate — what a test pointing TIX_DB at a
// fresh temporary file between cases needs.
func CloseDB() {
	if db != nil {
		_ = db.Close()
		db = nil
	}
}

// migrations takes the database from one schema version to the next: entry i
// upgrades user_version from i to i+1, so len(migrations) is the version this
// binary expects. Append only — once an entry has shipped, editing it leaves
// already-migrated databases with a schema nothing will fix.
//
// Databases created before migrations existed report user_version 0 but
// already have the tickets table, so entry 0 is written to be a no-op there.
var migrations = []string{
	`CREATE TABLE IF NOT EXISTS tickets (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		title TEXT NOT NULL,
		body TEXT DEFAULT '',
		status TEXT NOT NULL DEFAULT 'open',
		priority INTEGER NOT NULL DEFAULT 2,
		order_rank INTEGER NOT NULL DEFAULT 0,
		project TEXT NOT NULL DEFAULT '',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);`,
	// Prefixes are derived from project names, so this table holds only the
	// manual overrides. An empty table means every project still gets a
	// prefix, which is why no backfill is needed here.
	`CREATE TABLE IF NOT EXISTS project_prefixes (
		project TEXT PRIMARY KEY,
		prefix TEXT NOT NULL UNIQUE
	);`,
	// Projects become rows so an unknown --project is an error instead of a
	// silent new project. Every name already used by a ticket or a pinned
	// prefix is backfilled, so an existing database keeps working untouched.
	// project_prefixes folds into projects.key, which leaves one place that
	// says what a project is called and what its tickets are prefixed with.
	`CREATE TABLE IF NOT EXISTS projects (
		name TEXT PRIMARY KEY,
		key TEXT NOT NULL DEFAULT '',
		repo_path TEXT NOT NULL DEFAULT '',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	INSERT OR IGNORE INTO projects (name) SELECT DISTINCT project FROM tickets WHERE project != '';
	INSERT OR IGNORE INTO projects (name) SELECT project FROM project_prefixes WHERE project != '';
	UPDATE projects SET key = COALESCE(
		(SELECT prefix FROM project_prefixes WHERE project_prefixes.project = projects.name), '');
	DROP TABLE project_prefixes;
	CREATE UNIQUE INDEX IF NOT EXISTS projects_key_unique ON projects(key) WHERE key != '';`,
}

// migrate applies every migration the database has not seen yet. The version
// check is a read, so an up-to-date database never takes a write lock here:
// running schema statements on every invocation made read-only commands fail
// while another process was writing.
func migrate() error {
	version, err := schemaVersion()
	if err != nil {
		return err
	}
	if version > len(migrations) {
		return fmt.Errorf(
			"database schema version %d is newer than this tix understands (%d); upgrade tix",
			version, len(migrations),
		)
	}
	for i := version; i < len(migrations); i++ {
		if err := applyMigration(i); err != nil {
			return fmt.Errorf("migration %d: %w", i+1, err)
		}
	}
	return nil
}

// applyMigration runs migration i and records the new version atomically, so a
// crash mid-migration leaves the database on the version it started from.
func applyMigration(i int) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(migrations[i]); err != nil {
		return err
	}
	// PRAGMA does not accept bound parameters; i is an int, so formatting it
	// cannot inject anything.
	if _, err := tx.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, i+1)); err != nil {
		return err
	}
	return tx.Commit()
}

func schemaVersion() (int, error) {
	var version int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		return 0, err
	}
	return version, nil
}

// dsn builds the driver connection string. The pragmas are applied to every
// connection the pool opens:
//
//   - journal_mode=WAL lets readers run while a writer holds the file.
//   - busy_timeout makes a contended write wait instead of failing immediately
//     with SQLITE_BUSY.
//   - _txlock=immediate begins transactions as writers. ClaimNext reads a
//     ticket and then updates it; under a deferred transaction that upgrade
//     fails outright with SQLITE_BUSY_SNAPSHOT, which busy_timeout does not
//     retry.
func dsn(path string) string {
	return path + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_txlock=immediate"
}
