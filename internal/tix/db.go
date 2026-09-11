package tix

import (
	"database/sql"
	"errors"
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

func InitDB() {
	path := dbPath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		fmt.Fprintln(os.Stderr, "mkdir failed:", err)
		os.Exit(1)
	}
	var err error
	db, err = sql.Open("sqlite", dsn(path))
	if err != nil {
		fmt.Fprintln(os.Stderr, "db open failed:", err)
		os.Exit(1)
	}
	// Agents in different repos share one database file, so writes are
	// serialized in-process too: SQLite allows a single writer, and one
	// connection keeps this process from contending with itself.
	db.SetMaxOpenConns(1)

	exists, err := ticketsTableExists()
	if err != nil {
		fmt.Fprintln(os.Stderr, "schema check failed:", err)
		os.Exit(1)
	}
	if exists {
		return
	}
	schema := `
	CREATE TABLE IF NOT EXISTS tickets (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		title TEXT NOT NULL,
		body TEXT DEFAULT '',
		status TEXT NOT NULL DEFAULT 'open',
		priority INTEGER NOT NULL DEFAULT 2,
		order_rank INTEGER NOT NULL DEFAULT 0,
		project TEXT NOT NULL DEFAULT '',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);`
	if _, err := db.Exec(schema); err != nil {
		fmt.Fprintln(os.Stderr, "schema init failed:", err)
		os.Exit(1)
	}
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

// ticketsTableExists reports whether the schema is already in place. Running
// CREATE TABLE on every invocation takes a write lock, which makes read-only
// commands fail while another process is writing.
func ticketsTableExists() (bool, error) {
	var name string
	err := db.QueryRow(
		`SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'tickets'`,
	).Scan(&name)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}
