package mcp

import (
	"context"
	"strings"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
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

func TestInstructionsNameTheProjectsThatExist(t *testing.T) {
	setupTestDB(t)

	// With nothing in the table there is no list to hand over, so the client is
	// pointed at the tool that will have one later.
	empty := instructions()
	if !strings.Contains(empty, "tix_projects") {
		t.Fatalf("instructions with no projects = %q, want them to name tix_projects", empty)
	}

	for _, name := range []string{"frontend", "Brainstorm-Output"} {
		if _, err := tix.CreateProject(tix.Project{Name: name}); err != nil {
			t.Fatalf("CreateProject(%q): %v", name, err)
		}
	}

	got := instructions()
	for _, name := range []string{"frontend", "Brainstorm-Output"} {
		if !strings.Contains(got, name) {
			t.Errorf("instructions = %q, want them to list project %q", got, name)
		}
	}
	// Listing the names is only half of it: without the rule a client still
	// treats a name it invented as good as one off the list.
	if !strings.Contains(got, "create=true") {
		t.Errorf("instructions = %q, want them to name the way to add a project", got)
	}
}

func TestNewServerCarriesInstructions(t *testing.T) {
	setupTestDB(t)

	if _, err := tix.CreateProject(tix.Project{Name: "frontend"}); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	// The instructions only matter if they reach the client, which is what a
	// server built with nil options never did.
	ctx := context.Background()
	serverTransport, clientTransport := mcpsdk.NewInMemoryTransports()
	serverSession, err := NewServer().Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	defer serverSession.Close()

	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "test", Version: "0.0.1"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer clientSession.Close()

	if got := clientSession.InitializeResult().Instructions; !strings.Contains(got, "frontend") {
		t.Fatalf("instructions the client received = %q, want them to list project %q", got, "frontend")
	}
}

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
