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

// resultText pulls the reply's single text block out, failing if the reply is
// shaped any other way: one text block and nothing else is the contract these
// tools now keep.
func resultText(t *testing.T, res *mcpsdk.CallToolResult) string {
	t.Helper()

	if res == nil {
		t.Fatal("result is nil, want one text block")
	}
	if len(res.Content) != 1 {
		t.Fatalf("result has %d content blocks, want 1", len(res.Content))
	}
	if res.StructuredContent != nil {
		t.Fatalf("result carries structured content %v, want the text block alone", res.StructuredContent)
	}
	text, ok := res.Content[0].(*mcpsdk.TextContent)
	if !ok {
		t.Fatalf("content block is %T, want *mcp.TextContent", res.Content[0])
	}
	return text.Text
}

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

	res, _, err := addBatchTool(context.Background(), nil, addBatchArgs{
		Project: "brainstormoutput",
		Tickets: []batchTicket{{Title: "fourth", Body: "four"}},
	})
	if err != nil {
		t.Fatalf("addBatchTool into the existing project: %v", err)
	}
	// The second call spells the project differently, so the reply naming the
	// resolved name is what tells the caller the tickets share one project.
	if got := resultText(t, res); !strings.Contains(got, "Brainstorm-Output") {
		t.Fatalf("reply = %q, want it to name the resolved project %q", got, "Brainstorm-Output")
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

// TestNoToolDeclaresAnOutputSchema guards the saving rather than the shape: an
// output schema sits in every tools/list for the whole session, and it only
// appears because a handler returns a typed result, so a typed result slipping
// back in is easy to miss without checking the wire.
func TestNoToolDeclaresAnOutputSchema(t *testing.T) {
	setupTestDB(t)

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

	tools, err := clientSession.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(tools.Tools) == 0 {
		t.Fatal("tools/list returned no tools")
	}
	for _, tool := range tools.Tools {
		if tool.OutputSchema != nil {
			t.Errorf("tool %s declares an output schema, want none", tool.Name)
		}
		// The input schema is the half that earns its keep: without it a client
		// cannot tell what to pass.
		if tool.InputSchema == nil {
			t.Errorf("tool %s declares no input schema", tool.Name)
		}
	}
}

func TestToolsRenderPlainText(t *testing.T) {
	setupTestDB(t)

	ctx := context.Background()
	res, _, err := addTool(ctx, nil, addArgs{
		Title: "fix login", Body: "it 500s", Priority: ptr(1), Project: "acme", Create: true,
	})
	if err != nil {
		t.Fatalf("addTool: %v", err)
	}
	// The id has to be readable back out of the text, because it is the only
	// copy now and the other tools need it as an argument.
	if got, want := resultText(t, res), "1 [P1] fix login (acme)"; got != want {
		t.Errorf("add reply = %q, want %q", got, want)
	}

	res, _, err = addBatchTool(ctx, nil, addBatchArgs{
		Project: "acme",
		Tickets: []batchTicket{{Title: "add tests", Body: "none yet"}},
	})
	if err != nil {
		t.Fatalf("addBatchTool: %v", err)
	}
	if got, want := resultText(t, res), "created 1 in acme\n2 [P2] add tests"; got != want {
		t.Errorf("add_batch reply = %q, want %q", got, want)
	}

	res, _, err = showTool(ctx, nil, showArgs{ID: 1})
	if err != nil {
		t.Fatalf("showTool: %v", err)
	}
	if got, want := resultText(t, res), "1 [P1] fix login (open, acme)\nit 500s"; got != want {
		t.Errorf("show reply = %q, want %q", got, want)
	}

	done := "done"
	if _, _, err := editTool(ctx, nil, editArgs{ID: 2, Status: &done}); err != nil {
		t.Fatalf("editTool: %v", err)
	}
	res, _, err = listTool(ctx, nil, listArgs{Project: "acme", All: true})
	if err != nil {
		t.Fatalf("listTool: %v", err)
	}
	// A closed ticket is marked, so an --all listing does not read as a list of
	// open work.
	if got, want := resultText(t, res), "1 [P1] fix login\nx 2 [P2] add tests"; got != want {
		t.Errorf("list reply = %q, want %q", got, want)
	}

	res, _, err = projectsTool(ctx, nil, struct{}{})
	if err != nil {
		t.Fatalf("projectsTool: %v", err)
	}
	if got, want := resultText(t, res), "acme done=1 open=1 total=2"; got != want {
		t.Errorf("projects reply = %q, want %q", got, want)
	}
}

// TestEmptyRepliesSaySo covers the case where a blank text block would read as
// a call that failed rather than one that found nothing.
func TestEmptyRepliesSaySo(t *testing.T) {
	setupTestDB(t)

	ctx := context.Background()
	res, _, err := projectsTool(ctx, nil, struct{}{})
	if err != nil {
		t.Fatalf("projectsTool: %v", err)
	}
	if got, want := resultText(t, res), "no projects"; got != want {
		t.Errorf("projects reply with no projects = %q, want %q", got, want)
	}

	if _, err := tix.CreateProject(tix.Project{Name: "empty"}); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	res, _, err = listTool(ctx, nil, listArgs{Project: "empty"})
	if err != nil {
		t.Fatalf("listTool: %v", err)
	}
	if got, want := resultText(t, res), "no tickets"; got != want {
		t.Errorf("list reply with no tickets = %q, want %q", got, want)
	}
}
