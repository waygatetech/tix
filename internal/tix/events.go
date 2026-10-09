package tix

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Event is one lifecycle transition from a ticket's append-only history.
type Event struct {
	Event string          `json:"event"`
	At    time.Time       `json:"at"`
	Data  json.RawMessage `json:"data,omitempty"`
}

// recordEvent appends a lifecycle event. Callers pass their transaction so the
// event commits or rolls back with the transition it describes.
func recordEvent(exec sqlExecer, id int, event string, data any) error {
	var encoded any
	if data != nil {
		b, err := json.Marshal(data)
		if err != nil {
			return fmt.Errorf("encoding %s event: %w", event, err)
		}
		encoded = string(b)
	}
	if _, err := exec.Exec(`INSERT INTO ticket_events (ticket_id, event, data) VALUES (?, ?, ?)`, id, event, encoded); err != nil {
		return fmt.Errorf("recording %s event: %w", event, err)
	}
	return nil
}

// Note appends a free-form handoff note to a ticket's history. It changes
// nothing else: a note records context, not a transition.
func Note(id int, body string) error {
	if strings.TrimSpace(body) == "" {
		return errors.New("a note body is required")
	}
	if _, err := Show(id); err != nil {
		return err
	}
	return recordEvent(db, id, "note", map[string]string{"body": body})
}

// NoteEntry is one handoff note, decoded from its event.
type NoteEntry struct {
	At   time.Time `json:"at"`
	Body string    `json:"body"`
}

// Notes returns a ticket's notes, oldest first.
func Notes(id int) ([]NoteEntry, error) {
	events, err := Events(id)
	if err != nil {
		return nil, err
	}
	notes := []NoteEntry{}
	for _, e := range events {
		if e.Event != "note" {
			continue
		}
		var data struct {
			Body string `json:"body"`
		}
		if err := json.Unmarshal(e.Data, &data); err != nil {
			return nil, fmt.Errorf("decoding note: %w", err)
		}
		notes = append(notes, NoteEntry{At: e.At, Body: data.Body})
	}
	return notes, nil
}

// Events returns a ticket's history, oldest first.
func Events(id int) ([]Event, error) {
	rows, err := db.Query(`SELECT event, at, data FROM ticket_events WHERE ticket_id = ? ORDER BY id`, id)
	if err != nil {
		return nil, fmt.Errorf("loading events: %w", err)
	}
	defer rows.Close()
	events := []Event{}
	for rows.Next() {
		var e Event
		var data sql.NullString
		if err := rows.Scan(&e.Event, &e.At, &data); err != nil {
			return nil, fmt.Errorf("scanning event: %w", err)
		}
		if data.Valid {
			e.Data = json.RawMessage(data.String)
		}
		events = append(events, e)
	}
	return events, rows.Err()
}
