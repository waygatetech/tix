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

func InitDB() {
	path := dbPath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		fmt.Fprintln(os.Stderr, "mkdir failed:", err)
		os.Exit(1)
	}
	var err error
	db, err = sql.Open("sqlite", path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "db open failed:", err)
		os.Exit(1)
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
