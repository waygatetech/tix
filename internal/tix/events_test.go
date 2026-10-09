package tix

import (
	"slices"
	"testing"
)

func TestLifecycleEvents(t *testing.T) {
	setupTestDB(t)
	newProjects(t, "acme")

	epicID, err := Add("epic", "", 1, "acme", nil)
	if err != nil {
		t.Fatalf("Add(epic): %v", err)
	}
	epic := int(epicID)
	childID, err := Add("child", "", 1, "acme", &epic)
	if err != nil {
		t.Fatalf("Add(child): %v", err)
	}
	child := int(childID)

	if _, err := ClaimNext("acme"); err != nil {
		t.Fatalf("ClaimNext: %v", err)
	}
	if err := Done(child); err != nil {
		t.Fatalf("Done(%d): %v", child, err)
	}

	tests := []struct {
		name string
		id   int
		want []string
	}{
		{"child is claimed then done", child, []string{"claim", "done"}},
		{"epic closes with its last child", epic, []string{"done"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			events, err := Events(tt.id)
			if err != nil {
				t.Fatalf("Events(%d): %v", tt.id, err)
			}
			got := []string{}
			for _, e := range events {
				got = append(got, e.Event)
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("events = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEventsAreAppendOnly(t *testing.T) {
	setupTestDB(t)
	newProjects(t, "acme")

	id, err := Add("ticket", "", 1, "acme", nil)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := Done(int(id)); err != nil {
		t.Fatalf("Done: %v", err)
	}

	for _, stmt := range []string{
		`UPDATE ticket_events SET event = 'x'`,
		`DELETE FROM ticket_events`,
	} {
		t.Run(stmt, func(t *testing.T) {
			if _, err := db.Exec(stmt); err == nil {
				t.Errorf("%s succeeded, want append-only error", stmt)
			}
		})
	}

	if err := Remove(int(id), "acme"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	events, err := Events(int(id))
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	if len(events) != 1 {
		t.Errorf("after rm, events = %v, want the done event kept", events)
	}
}

func TestNote(t *testing.T) {
	setupTestDB(t)
	newProjects(t, "acme")

	id, err := Add("ticket", "", 1, "acme", nil)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	tests := []struct {
		name    string
		id      int
		body    string
		wantErr bool
	}{
		{"appends a note", int(id), "handoff: tests pass, docs next", false},
		{"rejects a blank note", int(id), "  \n", true},
		{"rejects a missing ticket", 9999, "lost", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Note(tt.id, tt.body)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Note(%d, %q) error = %v, wantErr %v", tt.id, tt.body, err, tt.wantErr)
			}
		})
	}

	notes, err := Notes(int(id))
	if err != nil {
		t.Fatalf("Notes: %v", err)
	}
	if len(notes) != 1 || notes[0].Body != "handoff: tests pass, docs next" {
		t.Errorf("notes = %+v, want the one appended note", notes)
	}
}
