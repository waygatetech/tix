package mcp

import (
	"context"
	"strings"
	"testing"

	"github.com/waygatetech/tix/internal/tix"
	_ "modernc.org/sqlite"
)

func setupTestDB(t *testing.T) {
	t.Helper()

	t.Setenv("TIX_DB", t.TempDir()+"/tix.db")
	if err := tix.InitDB(); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	t.Cleanup(tix.CloseDB)
}

func ptr(i int) *int { return &i }

func TestAddBatchCreatesEveryTicket(t *testing.T) {
	setupTestDB(t)

	_, _, err := addBatchTool(context.Background(), nil, addBatchArgs{
		Project: "Brainstorm-Output",
		Create:  true,
		Tickets: []batchTicket{
			{Title: "first", Body: "one"},
			{Title: "second", Body: "two", Priority: ptr(0)},
			{Title: "third", Body: "three", Priority: ptr(9)},
		},
	})
	if err != nil {
		t.Fatalf("addBatchTool: %v", err)
	}

	_, out, err := addBatchTool(context.Background(), nil, addBatchArgs{
		Project: "brainstormoutput",
		Tickets: []batchTicket{{Title: "fourth", Body: "four"}},
	})
	if err != nil {
		t.Fatalf("addBatchTool into the existing project: %v", err)
	}
	// The second call spells the project differently, so the reply naming the
	// resolved name is what tells the caller the tickets share one project.
	if out.Project != "Brainstorm-Output" {
		t.Fatalf("project = %q, want %q", out.Project, "Brainstorm-Output")
	}

	tickets, err := tix.ListTickets(tix.ListOptions{Project: "Brainstorm-Output"})
	if err != nil {
		t.Fatalf("ListTickets: %v", err)
	}
	if len(tickets) != 4 {
		t.Fatalf("got %d tickets, want 4", len(tickets))
	}

	byTitle := map[string]tix.Ticket{}
	for _, ticket := range tickets {
		byTitle[ticket.Title] = ticket
	}
	for _, want := range []struct {
		title    string
		body     string
		priority int
	}{
		{"first", "one", 2},
		{"second", "two", 0},
		{"third", "three", 2}, // out of range, clamped to the default
		{"fourth", "four", 2},
	} {
		got, ok := byTitle[want.title]
		if !ok {
			t.Fatalf("ticket %q was not created", want.title)
		}
		if got.Body != want.body || got.Priority != want.priority {
			t.Errorf("ticket %q = body %q priority %d, want body %q priority %d",
				want.title, got.Body, got.Priority, want.body, want.priority)
		}
	}
}

func TestAddBatchRejectsBadBatchWholesale(t *testing.T) {
	setupTestDB(t)

	for _, tc := range []struct {
		name string
		args addBatchArgs
		want string
	}{
		{
			name: "no tickets",
			args: addBatchArgs{Project: "batch", Create: true},
			want: "tickets is required",
		},
		{
			name: "no project",
			args: addBatchArgs{Tickets: []batchTicket{{Title: "t", Body: "b"}}},
			want: "project is required",
		},
		{
			name: "unknown project",
			args: addBatchArgs{Project: "nope", Tickets: []batchTicket{{Title: "t", Body: "b"}}},
			want: "nope",
		},
		{
			name: "missing title",
			args: addBatchArgs{Project: "batch", Create: true, Tickets: []batchTicket{
				{Title: "ok", Body: "b"},
				{Body: "b"},
			}},
			want: "tickets[1]: title is required",
		},
		{
			name: "missing body",
			args: addBatchArgs{Project: "batch", Create: true, Tickets: []batchTicket{
				{Title: "ok", Body: "b"},
				{Title: "no body"},
			}},
			want: "tickets[1]: body is required",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := addBatchTool(context.Background(), nil, tc.args)
			if err == nil {
				t.Fatalf("addBatchTool succeeded, want error containing %q", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %q, want it to contain %q", err, tc.want)
			}
		})
	}

	// A rejected batch writes nothing at all, not even the valid tickets that
	// preceded the bad one, and never creates the project on the way.
	projects, err := tix.Projects()
	if err != nil {
		t.Fatalf("Projects: %v", err)
	}
	if len(projects) != 0 {
		t.Fatalf("got %d projects, want none created by rejected batches", len(projects))
	}
}
