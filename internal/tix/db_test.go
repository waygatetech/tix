package tix

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"

	_ "modernc.org/sqlite"
)

// writeRepoConfig gives dir a .tix/config.json holding body, which is how a
// repo says its tickets do not belong in the global database.
func writeRepoConfig(t *testing.T, dir, body string) string {
	t.Helper()

	tixDir := filepath.Join(dir, tixDirName)
	if err := os.MkdirAll(tixDir, 0755); err != nil {
		t.Fatalf("mkdir %s: %v", tixDir, err)
	}
	path := filepath.Join(tixDir, configFileName)
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

// noRepoDB keeps a test that resolves the database from leaking its answer
// into the next one, since the resolved path is package state.
func noRepoDB(t *testing.T) {
	t.Helper()

	t.Setenv("TIX_DB", "")
	repoDB = ""
	t.Cleanup(func() { repoDB = "" })
}

// A repo names its database in .tix/config.json, relative to the config that
// names it, so the whole opt-in is one checked-in file.
func TestDiscoverRepoDBReadsTheRepoConfig(t *testing.T) {
	noRepoDB(t)

	repo := t.TempDir()
	writeRepoConfig(t, repo, `{"db": "tix.db"}`)

	got, err := discoverRepoDB(repo)
	if err != nil {
		t.Fatalf("discoverRepoDB: %v", err)
	}
	want := filepath.Join(repo, tixDirName, dbFileName)
	if got != want {
		t.Errorf("discoverRepoDB() = %q, want %q", got, want)
	}
}

// Commands run from wherever the work is, so the config has to be found from a
// subdirectory the same way git finds its own.
func TestDiscoverRepoDBFindsTheConfigFromASubdirectory(t *testing.T) {
	noRepoDB(t)

	repo := t.TempDir()
	writeRepoConfig(t, repo, `{"db": "../backlog.db"}`)
	nested := filepath.Join(repo, "internal", "tix")
	if err := os.MkdirAll(nested, 0755); err != nil {
		t.Fatalf("mkdir %s: %v", nested, err)
	}

	got, err := discoverRepoDB(nested)
	if err != nil {
		t.Fatalf("discoverRepoDB: %v", err)
	}
	want := filepath.Join(repo, "backlog.db")
	if got != want {
		t.Errorf("discoverRepoDB() = %q, want %q", got, want)
	}
}

// A checkout inside another repo gets its own backlog rather than the outer
// repo's, which is what makes the nearest config the one that counts.
func TestDiscoverRepoDBPrefersTheNearestConfig(t *testing.T) {
	noRepoDB(t)

	outer := t.TempDir()
	writeRepoConfig(t, outer, `{"db": "outer.db"}`)
	inner := filepath.Join(outer, "vendor", "inner")
	if err := os.MkdirAll(inner, 0755); err != nil {
		t.Fatalf("mkdir %s: %v", inner, err)
	}
	writeRepoConfig(t, inner, `{"db": "inner.db"}`)

	got, err := discoverRepoDB(inner)
	if err != nil {
		t.Fatalf("discoverRepoDB: %v", err)
	}
	want := filepath.Join(inner, tixDirName, "inner.db")
	if got != want {
		t.Errorf("discoverRepoDB() = %q, want %q", got, want)
	}
}

// An absolute path is taken as written, so a repo can point at a database that
// lives outside the checkout.
func TestDiscoverRepoDBAcceptsAnAbsolutePath(t *testing.T) {
	noRepoDB(t)

	repo := t.TempDir()
	elsewhere := filepath.Join(t.TempDir(), "shared.db")
	writeRepoConfig(t, repo, fmt.Sprintf(`{"db": %q}`, elsewhere))

	got, err := discoverRepoDB(repo)
	if err != nil {
		t.Fatalf("discoverRepoDB: %v", err)
	}
	if got != elsewhere {
		t.Errorf("discoverRepoDB() = %q, want %q", got, elsewhere)
	}
}

// Repos that already keep their database at .tix/tix.db by exporting TIX_DB
// get the same result with nothing exported and no config to write.
func TestDiscoverRepoDBFindsADatabaseWithoutAConfig(t *testing.T) {
	noRepoDB(t)

	repo := t.TempDir()
	local := filepath.Join(repo, tixDirName, dbFileName)
	if err := os.MkdirAll(filepath.Dir(local), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(local, nil, 0644); err != nil {
		t.Fatalf("write %s: %v", local, err)
	}

	got, err := discoverRepoDB(filepath.Join(repo))
	if err != nil {
		t.Fatalf("discoverRepoDB: %v", err)
	}
	if got != local {
		t.Errorf("discoverRepoDB() = %q, want %q", got, local)
	}
}

// A config naming a database this tix cannot open — the shape a hosted backend
// would arrive in — has to fail rather than quietly file tickets into the
// global backlog.
func TestDiscoverRepoDBRejectsAConfigThatNamesNoDatabase(t *testing.T) {
	noRepoDB(t)

	repo := t.TempDir()
	writeRepoConfig(t, repo, `{"remote": "https://tix.example.com/acme"}`)

	if _, err := discoverRepoDB(repo); err == nil {
		t.Fatal("discoverRepoDB() = nil error, want one for a config with no db")
	} else if !strings.Contains(err.Error(), `sets no "db"`) {
		t.Errorf("discoverRepoDB() error = %q, want it to name the missing db", err)
	}
}

func TestDiscoverRepoDBRejectsAMalformedConfig(t *testing.T) {
	noRepoDB(t)

	repo := t.TempDir()
	writeRepoConfig(t, repo, `{"db": `)

	if _, err := discoverRepoDB(repo); err == nil {
		t.Fatal("discoverRepoDB() = nil error, want one for unparseable JSON")
	}
}

// The solo case: a directory that opts into nothing keeps the global backlog,
// so nothing about ~/.tix/tix.db changes for anyone who never writes a config.
func TestUseRepoDBLeavesTheGlobalDatabaseAlone(t *testing.T) {
	noRepoDB(t)

	if err := UseRepoDB(t.TempDir()); err != nil {
		t.Fatalf("UseRepoDB: %v", err)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home directory: %v", err)
	}
	want := filepath.Join(home, tixDirName, dbFileName)
	if got := dbPath(); got != want {
		t.Errorf("dbPath() = %q, want %q", got, want)
	}
}

// TIX_DB is the caller speaking for this one command, so it outranks a config
// checked in by someone else.
func TestUseRepoDBDefersToTIXDB(t *testing.T) {
	noRepoDB(t)

	repo := t.TempDir()
	writeRepoConfig(t, repo, `{"db": "tix.db"}`)
	t.Setenv("TIX_DB", "/tmp/custom-tix.db")

	if err := UseRepoDB(repo); err != nil {
		t.Fatalf("UseRepoDB: %v", err)
	}
	if got := dbPath(); got != "/tmp/custom-tix.db" {
		t.Errorf("dbPath() = %q, want the TIX_DB path", got)
	}
}

// An unusable database path is an error for the caller to report, not a reason
// to exit the process: commands that never open the database still have to run.
func TestInitDBReportsAnUnusableDatabasePath(t *testing.T) {
	setupTestDB(t)

	blocker := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(blocker, nil, 0644); err != nil {
		t.Fatalf("write blocker: %v", err)
	}
	t.Setenv("TIX_DB", filepath.Join(blocker, "tix.db"))

	if err := InitDB(); err == nil {
		t.Fatal("InitDB succeeded with a file where the database directory belongs")
	}
}

func TestInitDBAppliesConcurrencyPragmas(t *testing.T) {
	setupTestDB(t)

	var mode string
	if err := db.QueryRow(`PRAGMA journal_mode`).Scan(&mode); err != nil {
		t.Fatalf("read journal_mode: %v", err)
	}
	if !strings.EqualFold(mode, "wal") {
		t.Errorf("journal_mode = %q, want wal", mode)
	}

	var timeout int
	if err := db.QueryRow(`PRAGMA busy_timeout`).Scan(&timeout); err != nil {
		t.Fatalf("read busy_timeout: %v", err)
	}
	if timeout != 5000 {
		t.Errorf("busy_timeout = %d, want 5000", timeout)
	}
}

func TestInitDBRecordsSchemaVersion(t *testing.T) {
	setupTestDB(t)

	version, err := schemaVersion()
	if err != nil {
		t.Fatalf("schemaVersion: %v", err)
	}
	if version != len(migrations) {
		t.Errorf("user_version = %d, want %d", version, len(migrations))
	}
}

// claim-next and next are the queries that run on every agent turn, so they
// have to reach their row through the index rather than scanning the shared
// database. A plan naming the index proves both the seek and the ordering: the
// index carries priority and order_rank, so SQLite reads the rows already
// sorted and never falls back to a temporary b-tree.
func TestClaimNextQueryUsesTheCoveringIndex(t *testing.T) {
	setupTestDB(t)

	rows, err := db.Query(
		`EXPLAIN QUERY PLAN
		 SELECT id FROM tickets
		 WHERE status = 'open' AND project = ?
		 ORDER BY priority ASC, order_rank ASC LIMIT 1`,
		"acme",
	)
	if err != nil {
		t.Fatalf("explain: %v", err)
	}
	defer rows.Close()

	var plan []string
	for rows.Next() {
		var id, parent, notUsed int
		var detail string
		if err := rows.Scan(&id, &parent, &notUsed, &detail); err != nil {
			t.Fatalf("scan plan row: %v", err)
		}
		plan = append(plan, detail)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read plan: %v", err)
	}

	joined := strings.Join(plan, "; ")
	if !strings.Contains(joined, "tickets_project_status_order") {
		t.Errorf("query plan does not use the index: %s", joined)
	}
	if strings.Contains(joined, "TEMP B-TREE") {
		t.Errorf("query plan sorts instead of reading the index in order: %s", joined)
	}
}

// A database created before migrations existed reports version 0 but already
// has the tickets table and its rows. Migrating it must bring the version
// forward without disturbing what is there.
func TestMigrateUpgradesPreMigrationDatabase(t *testing.T) {
	seedLegacyDB(t, 1, `
		INSERT INTO tickets (title, project, created_at)
			VALUES ('existing ticket', 'acme', '2026-01-01 00:00:00');
		PRAGMA user_version = 0;`)

	if err := migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	version, err := schemaVersion()
	if err != nil {
		t.Fatalf("schemaVersion: %v", err)
	}
	if version != len(migrations) {
		t.Errorf("user_version = %d, want %d", version, len(migrations))
	}
	if lines := listLines(t, "acme", false); len(lines) != 1 {
		t.Errorf("got %d tickets after migrating, want 1: %v", len(lines), lines)
	}

	// The backfill reads created_at, not the clock, so a ticket that has sat
	// untouched for a month is stale the moment the column exists rather than
	// looking like it was worked on during the migration.
	id, err := ResolveRef("1")
	if err != nil {
		t.Fatalf("ResolveRef(1) after migrating: %v", err)
	}
	ticket, err := Show(id)
	if err != nil {
		t.Fatalf("Show(%s) after migrating: %v", id, err)
	}
	if ticket.UpdatedAt.IsZero() || !ticket.UpdatedAt.Equal(ticket.CreatedAt) {
		t.Errorf("backfilled updated_at = %v, want created_at %v", ticket.UpdatedAt, ticket.CreatedAt)
	}
}

// seedLegacyDB builds a database at schema version n, runs seed against it,
// and opens it with InitDB, which applies every later migration.
func seedLegacyDB(t *testing.T, n int, seed string) {
	t.Helper()
	t.Setenv("TIX_JEV_MODE", "off")
	if db != nil {
		_ = db.Close()
	}
	path := filepath.Join(t.TempDir(), "tix.db")
	t.Setenv("TIX_DB", path)
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	for i, m := range migrations[:n] {
		if _, err := raw.Exec(m); err != nil {
			t.Fatalf("migration %d: %v", i+1, err)
		}
	}
	if _, err := raw.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, n)); err != nil {
		t.Fatalf("set user_version: %v", err)
	}
	if _, err := raw.Exec(seed); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := raw.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if err := InitDB(); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	t.Cleanup(func() {
		if db != nil {
			_ = db.Close()
			db = nil
		}
	})
}

// Running an older binary against a newer database must fail loudly rather
// than issue queries against columns it does not know about.
func TestMigrateRejectsNewerSchema(t *testing.T) {
	setupTestDB(t)

	future := len(migrations) + 1
	if _, err := db.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, future)); err != nil {
		t.Fatalf("set user_version: %v", err)
	}

	err := migrate()
	if err == nil {
		t.Fatal("migrate() = nil, want an error for a newer schema")
	}
	if !strings.Contains(err.Error(), "newer than this tix understands") {
		t.Errorf("migrate() error = %q, want it to mention a newer schema", err)
	}
}

// A failing migration must leave the version untouched, so the next run
// retries it instead of skipping past a schema change that never landed.
func TestApplyMigrationLeavesVersionUnchangedOnFailure(t *testing.T) {
	setupTestDB(t)

	original := migrations
	t.Cleanup(func() { migrations = original })
	migrations = append(append([]string{}, original...), `THIS IS NOT SQL`)

	if _, err := db.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, len(original))); err != nil {
		t.Fatalf("set user_version: %v", err)
	}

	if err := migrate(); err == nil {
		t.Fatal("migrate() = nil, want an error for a broken migration")
	}

	version, err := schemaVersion()
	if err != nil {
		t.Fatalf("schemaVersion: %v", err)
	}
	if version != len(original) {
		t.Errorf("user_version = %d after a failed migration, want %d", version, len(original))
	}
}

// A second process holding the write lock must not stop this one from opening
// the database and reading, which is what running CREATE TABLE on every
// invocation used to cause.
func TestInitDBReadsWhileAnotherConnectionWrites(t *testing.T) {
	setupTestDB(t)
	newProjects(t, "acme")

	if _, err := Add("existing ticket", "", 2, "acme", nil); err != nil {
		t.Fatalf("Add: %v", err)
	}
	path := dbPath()
	_ = db.Close()
	db = nil

	writer, err := sql.Open("sqlite", dsn(path))
	if err != nil {
		t.Fatalf("open writer: %v", err)
	}
	defer writer.Close()

	tx, err := writer.Begin()
	if err != nil {
		t.Fatalf("begin writer tx: %v", err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE tickets SET title = 'held' WHERE id = 1`); err != nil {
		t.Fatalf("writer update: %v", err)
	}

	if err := InitDB(); err != nil {
		t.Fatalf("InitDB: %v", err)
	}

	lines := listLines(t, "acme", false)
	if len(lines) != 1 {
		t.Fatalf("got %d tickets while a writer held the lock, want 1: %v", len(lines), lines)
	}
}

func TestClaimNextConcurrentlyClaimsEachTicketOnce(t *testing.T) {
	setupTestDB(t)
	newProjects(t, "acme")

	const total = 8
	for i := 0; i < total; i++ {
		if _, err := Add("ticket", "", 2, "acme", nil); err != nil {
			t.Fatalf("Add: %v", err)
		}
	}

	var (
		mu     sync.Mutex
		seen   = map[string]bool{}
		errs   []error
		wg     sync.WaitGroup
		start  = make(chan struct{})
		claims = total
	)
	for i := 0; i < claims; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			ticket, err := ClaimNext("acme")
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs = append(errs, err)
				return
			}
			if seen[ticket.ID] {
				errs = append(errs, sql.ErrNoRows)
				t.Errorf("ticket %s claimed twice", ticket.ID)
				return
			}
			seen[ticket.ID] = true
		}()
	}
	close(start)
	wg.Wait()

	for _, err := range errs {
		t.Errorf("concurrent ClaimNext: %v", err)
	}
	if len(seen) != total {
		t.Errorf("claimed %d distinct tickets, want %d", len(seen), total)
	}
}

var uuidV4 = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

// uuidMigration rewrites every reference to a ticket in one transaction, so a
// database seeded at the current schema must come out with each link pointing
// at the same ticket it did before, just by UUID.
func TestUUIDMigrationRewritesTicketReferences(t *testing.T) {
	// Seeded at the last integer-id schema: the epic was closed by its only
	// child, blocked depends on child, and ticket 4 was removed but left a
	// claim event behind.
	const (
		epic    int64 = 1
		child   int64 = 2
		blocked int64 = 3
	)
	seedLegacyDB(t, len(migrations)-1, `
		INSERT INTO projects (name) VALUES ('acme');
		INSERT INTO tickets (id, title, status, project, parent_id) VALUES
			(1, 'epic', 'done', 'acme', NULL),
			(2, 'child', 'done', 'acme', 1),
			(3, 'blocked', 'open', 'acme', NULL);
		INSERT INTO ticket_dependencies (ticket_id, depends_on_id) VALUES (3, 2);
		INSERT INTO ticket_events (ticket_id, event, data) VALUES
			(4, 'claim', '{"status":"in_progress"}'),
			(2, 'done', NULL),
			(1, 'done', '{"closed_by":2}');
		INSERT INTO jev_evaluations (ticket_id, policy_decision, latency_ms, mode, status)
			VALUES (3, 'allow', 0, 'off', 'ok');`)

	ids := map[int64]string{}
	rows, err := db.Query(`SELECT id, legacy_id FROM tickets`)
	if err != nil {
		t.Fatalf("select tickets: %v", err)
	}
	for rows.Next() {
		var id string
		var legacy int64
		if err := rows.Scan(&id, &legacy); err != nil {
			t.Fatalf("scan ticket: %v", err)
		}
		if !uuidV4.MatchString(id) {
			t.Errorf("ticket %d id = %q, want a UUIDv4", legacy, id)
		}
		ids[legacy] = id
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read tickets: %v", err)
	}
	for _, legacy := range []int64{epic, child, blocked} {
		if ids[legacy] == "" {
			t.Fatalf("no ticket with legacy_id %d; got %v", legacy, ids)
		}
	}
	if len(ids) != 3 {
		t.Errorf("got %d tickets, want 3: %v", len(ids), ids)
	}

	tests := []struct {
		name  string
		query string
		args  []any
		want  string
	}{
		{"parent", `SELECT parent_id FROM tickets WHERE id = ?`, []any{ids[child]}, ids[epic]},
		{"dependency", `SELECT depends_on_id FROM ticket_dependencies WHERE ticket_id = ?`, []any{ids[blocked]}, ids[child]},
		{"jev", `SELECT ticket_id FROM jev_evaluations`, nil, ids[blocked]},
		{"closed_by", `SELECT json_extract(data, '$.closed_by') FROM ticket_events WHERE ticket_id = ? AND event = 'done'`, []any{ids[epic]}, ids[child]},
		{"child events", `SELECT ticket_id FROM ticket_events WHERE event = 'done' AND data IS NULL`, nil, ids[child]},
		{"tickets sequence", `SELECT COUNT(*) FROM sqlite_sequence WHERE name = 'tickets'`, nil, "0"},
		{"untyped ticket ids", `SELECT COUNT(*) FROM ticket_events WHERE typeof(ticket_id) != 'text'`, nil, "0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got string
			if err := db.QueryRow(tt.query, tt.args...).Scan(&got); err != nil {
				t.Fatalf("query: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}

	// A removed ticket's history keeps one UUID, so it still groups as one ticket.
	var orphan string
	var orphanCount int
	if err := db.QueryRow(`SELECT ticket_id, COUNT(*) FROM ticket_events
		WHERE ticket_id NOT IN (SELECT id FROM tickets) GROUP BY ticket_id`).Scan(&orphan, &orphanCount); err != nil {
		t.Fatalf("removed ticket events: %v", err)
	}
	if !uuidV4.MatchString(orphan) || orphanCount == 0 {
		t.Errorf("removed ticket events = %q x%d, want a UUIDv4 with events", orphan, orphanCount)
	}

	if _, err := db.Exec(`UPDATE ticket_events SET event = 'x'`); err == nil || !strings.Contains(err.Error(), "append-only") {
		t.Errorf("UPDATE ticket_events error = %v, want append-only", err)
	}
}

// Integer ids stop meaning creation order once they are random UUIDs, so
// dependencies list by created_at and use id only to break ties.
func TestDependenciesOrderByCreatedAt(t *testing.T) {
	setupTestDB(t)
	newProjects(t, "acme")

	first, err := Add("first", "", 2, "acme", nil)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	second, err := Add("second", "", 2, "acme", nil)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if _, err := db.Exec(`UPDATE tickets SET created_at = '2020-01-02 00:00:00' WHERE id = ?`, first); err != nil {
		t.Fatalf("backdate: %v", err)
	}
	if _, err := db.Exec(`UPDATE tickets SET created_at = '2020-01-01 00:00:00' WHERE id = ?`, second); err != nil {
		t.Fatalf("backdate: %v", err)
	}
	blocked, err := Add("blocked", "", 2, "acme", nil, first, second)
	if err != nil {
		t.Fatalf("Add blocked: %v", err)
	}

	deps, _, err := dependenciesFor(blocked)
	if err != nil {
		t.Fatalf("dependenciesFor: %v", err)
	}
	if len(deps) != 2 || deps[0] != second || deps[1] != first {
		t.Errorf("dependencies = %v, want [%s %s] (created_at order)", deps, second, first)
	}
}
