package tix

import (
	"database/sql"
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
