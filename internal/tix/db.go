package tix

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

var db *sql.DB

const (
	// tixDirName holds whatever a repo says about its own tickets: a config
	// file naming the database, or the database itself.
	tixDirName = ".tix"
	// configFileName is the per-repo config, checked in alongside the code.
	configFileName = "config.json"
	// dbFileName is the database's name in both ~/.tix and a repo's .tix.
	dbFileName = "tix.db"
)

// repoDB is the database the working directory's repo opted into, empty when
// it did not. UseRepoDB sets it; everything else keeps the global database.
var repoDB string

// dbPath answers which database commands read and write. TIX_DB is the most
// explicit thing a caller can say, so it outranks a config file the caller may
// not have written; a repo that opted into its own database comes next; and
// everything else — the solo case — shares one backlog in ~/.tix.
func dbPath() string {
	if path := os.Getenv("TIX_DB"); path != "" {
		return path
	}
	if repoDB != "" {
		return repoDB
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, tixDirName, dbFileName)
}

// repoConfig is the shape of .tix/config.json: what a repo says about where
// its tickets live. Only a local database path is understood today, so a
// future hosted backend adds a key here rather than changing how any caller
// asks for the database.
type repoConfig struct {
	DB string `json:"db"`
}

// UseRepoDB points the database at the one dir or one of its parents opts
// into, and leaves the global database in place when nothing does. Solo work
// keeps every project in ~/.tix/tix.db; a repo that wants its tickets to
// itself checks in .tix/config.json, or simply keeps its database at
// .tix/tix.db.
func UseRepoDB(dir string) error {
	// dbPath prefers the environment regardless, so an explicit TIX_DB makes
	// the walk pointless rather than merely overridden.
	if os.Getenv("TIX_DB") != "" {
		return nil
	}
	path, err := discoverRepoDB(dir)
	if err != nil {
		return err
	}
	repoDB = path
	return nil
}

// discoverRepoDB walks dir and its parents for the nearest repo-local
// database, and reports "" when no directory on the way to the root opts into
// one. The nearest wins, so a checkout sitting inside another repo gets its
// own backlog instead of the outer repo's.
func discoverRepoDB(dir string) (string, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	for {
		configPath := filepath.Join(dir, tixDirName, configFileName)
		data, err := os.ReadFile(configPath)
		switch {
		case err == nil:
			return configuredDB(configPath, data)
		case !errors.Is(err, fs.ErrNotExist):
			return "", fmt.Errorf("read %s: %w", configPath, err)
		}

		// A repo with no config still opts in by keeping its database where
		// the docs put it, which is what `TIX_DB=.tix/tix.db tix` had every
		// such repo doing by hand.
		local := filepath.Join(dir, tixDirName, dbFileName)
		if info, err := os.Stat(local); err == nil && !info.IsDir() {
			return local, nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", nil
		}
		dir = parent
	}
}

// configuredDB reads the database out of a .tix/config.json. A relative path
// is relative to the file naming it, so `"db": "tix.db"` means the database
// beside the config.
//
// A config that names no database is an error rather than a quiet fall back to
// the global one: that is what an older tix sees when it meets a config for a
// backend it does not understand, and writing tickets into the wrong backlog
// is worse than refusing.
func configuredDB(configPath string, data []byte) (string, error) {
	var cfg repoConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return "", fmt.Errorf("parse %s: %w", configPath, err)
	}
	if strings.TrimSpace(cfg.DB) == "" {
		return "", fmt.Errorf(
			`%s sets no "db"; this tix understands only a local database path`,
			configPath,
		)
	}
	if filepath.IsAbs(cfg.DB) {
		return cfg.DB, nil
	}
	return filepath.Join(filepath.Dir(configPath), cfg.DB), nil
}

// DBPath is the database file commands read and write, so a caller can tell a
// reader which backlog they are looking at.
func DBPath() string {
	return dbPath()
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
	// updated_at is what makes a stuck ticket findable: an agent that crashes
	// mid-ticket leaves it in_progress forever, and without a timestamp nothing
	// can tell that apart from work started a minute ago. SQLite rejects a
	// non-constant column default, so the column arrives without one and every
	// write sets it explicitly.
	//
	// Existing rows are backfilled from created_at rather than from now, so a
	// ticket claimed last week reads as stale immediately instead of looking
	// fresh because the migration touched it.
	`ALTER TABLE tickets ADD COLUMN updated_at DATETIME;
	UPDATE tickets SET created_at = CURRENT_TIMESTAMP WHERE created_at IS NULL;
	UPDATE tickets SET updated_at = created_at WHERE updated_at IS NULL;`,
	// Every ticket lookup starts by narrowing to one project, and next and
	// claim-next add status = 'open' before ordering by priority and
	// order_rank. Leading with those two equality columns lets SQLite seek
	// straight to the matching rows and read them already sorted, so the hot
	// path stops scanning the whole table as the shared database accumulates
	// history from every repo.
	`CREATE INDEX IF NOT EXISTS tickets_project_status_order
		ON tickets(project, status, priority, order_rank);`,
	// parent_id groups tickets under an epic: a ticket whose parent_id points
	// at another ticket is one of that ticket's children. NULL means no
	// parent, which is why the column carries no NOT NULL/default the way
	// project and status do.
	`ALTER TABLE tickets ADD COLUMN parent_id INTEGER REFERENCES tickets(id);
	CREATE INDEX IF NOT EXISTS tickets_parent_id ON tickets(parent_id);`,
	// ticket_dependencies records prerequisites between tickets in the same
	// project: ticket_id is blocked until depends_on_id is done.
	`CREATE TABLE IF NOT EXISTS ticket_dependencies (
		ticket_id INTEGER NOT NULL REFERENCES tickets(id) ON DELETE CASCADE,
		depends_on_id INTEGER NOT NULL REFERENCES tickets(id) ON DELETE CASCADE,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		PRIMARY KEY (ticket_id, depends_on_id)
	);
	CREATE INDEX IF NOT EXISTS ticket_dependencies_depends_on
		ON ticket_dependencies(depends_on_id, ticket_id);`,
	// Jev evaluations are deliberately append-only telemetry. Keeping them
	// separate from tickets means semantic observations do not become workflow
	// state, and a failed evaluation remains distinguishable from a 0.0 answer.
	`CREATE TABLE IF NOT EXISTS jev_evaluations (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		ticket_id INTEGER NOT NULL,
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		model TEXT NOT NULL DEFAULT '',
		product_decision_required REAL,
		requirements_ambiguous REAL,
		scope_risk REAL,
		policy_decision TEXT NOT NULL,
		latency_ms INTEGER NOT NULL,
		input_tokens INTEGER NOT NULL DEFAULT 0,
		output_tokens INTEGER NOT NULL DEFAULT 0,
		mode TEXT NOT NULL,
		http_status INTEGER NOT NULL DEFAULT 0,
		status TEXT NOT NULL,
		error TEXT NOT NULL DEFAULT ''
	);
	CREATE INDEX IF NOT EXISTS jev_evaluations_ticket_id
		ON jev_evaluations(ticket_id, created_at);`,
	// plan caches a repo plan's parsed frontmatter as JSON, so plan-driven
	// features can query concepts and paths without reading the repo. The
	// plan file itself stays canonical; NULL means none has been indexed.
	`ALTER TABLE tickets ADD COLUMN plan TEXT;`,
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
