package tix

import (
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"testing"

	_ "modernc.org/sqlite"
)

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

// A database created before migrations existed reports version 0 but already
// has the tickets table and its rows. Migrating it must bring the version
// forward without disturbing what is there.
func TestMigrateUpgradesPreMigrationDatabase(t *testing.T) {
	setupTestDB(t)

	if _, err := Add("existing ticket", "", 2, "acme"); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if _, err := db.Exec(`PRAGMA user_version = 0`); err != nil {
		t.Fatalf("reset user_version: %v", err)
	}

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
	if lines := List("acme", false); len(lines) != 1 {
		t.Errorf("got %d tickets after migrating, want 1: %v", len(lines), lines)
	}
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

	if _, err := Add("existing ticket", "", 2, "acme"); err != nil {
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

	InitDB()

	lines := List("acme", false)
	if len(lines) != 1 {
		t.Fatalf("got %d tickets while a writer held the lock, want 1: %v", len(lines), lines)
	}
}

func TestClaimNextConcurrentlyClaimsEachTicketOnce(t *testing.T) {
	setupTestDB(t)

	const total = 8
	for i := 0; i < total; i++ {
		if _, err := Add("ticket", "", 2, "acme"); err != nil {
			t.Fatalf("Add: %v", err)
		}
	}

	var (
		mu     sync.Mutex
		seen   = map[int]bool{}
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
				t.Errorf("ticket %d claimed twice", ticket.ID)
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
