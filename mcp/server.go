// Package mcp exposes tix's ticket operations as MCP tools over stdio.
package mcp

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/waygatetech/tix/internal/tix"
)

// NewServer builds an MCP server exposing tix's authoring operations as tools.
//
// The set is deliberately narrow: writing tickets down, reading them back, and
// correcting them. Claiming and closing tickets belong to whoever does the
// work, so tix_claim_next, tix_done, and tix_rm stay on the CLI rather than
// letting a client here close tickets it never worked on.
//
// The version is the binary's own, threaded from main rather than written here,
// so what a client reads in serverInfo is the build it is talking to instead of
// a literal that stops matching the release tags the first time one is cut.
func NewServer(version string) *mcp.Server {
	server := mcp.NewServer(
		&mcp.Implementation{Name: "tix", Version: version},
		&mcp.ServerOptions{Instructions: instructions()},
	)

	mcp.AddTool(server, &mcp.Tool{
		Name: "tix_add",
		Description: "Create a new ticket in a project. The project must already exist; " +
			"call tix_projects to see the names, or pass create=true to make a new one.",
	}, addTool)

	mcp.AddTool(server, &mcp.Tool{
		Name: "tix_add_batch",
		Description: "Create several tickets in one project in a single call. Use this " +
			"instead of repeated tix_add whenever a session produces more than one " +
			"ticket. The project must already exist; call tix_projects to see the " +
			"names, or pass create=true to make a new one.",
	}, addBatchTool)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "tix_list",
		Description: "List tickets in a project, optionally filtered by status/priority.",
	}, listTool)

	mcp.AddTool(server, &mcp.Tool{
		Name: "tix_search",
		Description: "Search a project's tickets for a term in the title or body. " +
			"Use this before filing a ticket to check whether one already covers " +
			"the same work.",
	}, searchTool)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "tix_show",
		Description: "Show a single ticket by id.",
	}, showTool)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "tix_edit",
		Description: "Edit fields of an existing ticket. Only fields provided are changed.",
	}, editTool)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "tix_projects",
		Description: "List all projects with ticket counts per status.",
	}, projectsTool)

	return server
}

// projectRule is the half of the instructions that does not depend on which
// projects exist: a client that picks a name off the list still has to spell it
// the way the list does, and has one explicit way to add a name to it.
const projectRule = "Pass one of those names exactly as written. A name that matches none of " +
	"them is rejected rather than filed somewhere new, so pick from the list " +
	"instead of inventing a project; call tix_projects if the list looks stale. " +
	"Work that genuinely belongs to a project nobody has filed yet goes in with " +
	"create=true on tix_add or tix_add_batch, which registers the name before " +
	"filing the ticket."

// instructions tells the client which projects it may file against, because
// nothing else here does: a client that cannot see the list guesses a name, and
// a guessed name is how one backlog becomes four. The list is read once at
// startup, so tix_projects stays the way to see projects created since.
//
// A database that cannot be read is not worth refusing to start over — the
// tools still work, so the instructions fall back to naming the tool that
// answers the same question live.
func instructions() string {
	projects, err := tix.ListProjects()
	if err != nil || len(projects) == 0 {
		return "tix keeps tickets in project-scoped backlogs, and every tool that " +
			"takes a project needs the name of one that exists. Call tix_projects " +
			"for the list. " + projectRule
	}

	var b strings.Builder
	b.WriteString("tix keeps tickets in project-scoped backlogs. These projects exist:\n\n")
	for _, project := range projects {
		fmt.Fprintf(&b, "- %s\n", project.Name)
	}
	b.WriteString("\n")
	b.WriteString(projectRule)
	return b.String()
}

// Every tool replies with one text block and nothing else.
//
// The SDK derives an output schema from a typed result and then sends that
// result twice — once as structured content, once as its JSON text — so six
// typed tools cost 2,097 measured bytes of schema in every tools/list plus a
// doubled payload on every call. Nothing here consumes the structured half: a
// client reads the reply, and the ids it needs to pass back are in the text.
// Handlers therefore return an `any` result, which is how the SDK is told to
// generate no output schema, and build the text themselves.
//
// The text mirrors the CLI's formatting, which is measurably shorter than the
// JSON it replaced. The one deliberate difference is that ids print bare
// rather than prefixed: the tools take an integer id, so `78` is what a client
// can hand back to tix_show, and `ti-78` is not.
func textResult(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}
}

// ticketLine is the one-line form shared by list and the add replies.
func ticketLine(id int, priority int, title string) string {
	return fmt.Sprintf("%d [P%d] %s", id, priority, title)
}

// ticketDetail is the full form: the line, the status and project that a bare
// id does not reveal, when it was last written, and the body.
//
// Only the last write is named, and only here. It is what says whether an
// in_progress ticket is being worked on or was abandoned by an agent that
// died, which a status alone cannot; created_at answers nothing a caller acts
// on, so it stays out of a reply that is paid for by the token.
func ticketDetail(t tix.Ticket) string {
	detail := fmt.Sprintf("%s (%s, %s, updated %s)",
		ticketLine(t.ID, t.Priority, t.Title), t.Status, t.Project,
		t.UpdatedAt.UTC().Format(time.RFC3339))
	if t.Body != "" {
		detail += "\n" + t.Body
	}
	return detail
}

type addArgs struct {
	Title    string `json:"title" jsonschema:"the ticket title"`
	Body     string `json:"body" jsonschema:"the ticket body/description"`
	Priority *int   `json:"priority,omitempty" jsonschema:"priority 0-5, lower is more urgent (default 2)"`
	Project  string `json:"project" jsonschema:"the project the ticket belongs to; must be an existing project unless create is true"`
	Create   bool   `json:"create,omitempty" jsonschema:"create the project if it does not exist yet"`
}

func addTool(_ context.Context, _ *mcp.CallToolRequest, args addArgs) (*mcp.CallToolResult, any, error) {
	if args.Title == "" {
		return nil, nil, fmt.Errorf("title is required")
	}
	if args.Body == "" {
		return nil, nil, fmt.Errorf("body is required")
	}
	if args.Project == "" {
		return nil, nil, fmt.Errorf("project is required")
	}
	priority := normalizePriority(args.Priority)
	project, err := resolveProject(args.Project, args.Create)
	if err != nil {
		return nil, nil, err
	}
	id, err := tix.Add(args.Title, args.Body, priority, project)
	if err != nil {
		return nil, nil, fmt.Errorf("add failed: %w", err)
	}
	// The project is named even though the caller passed one: it was resolved,
	// so this is what tells a caller who guessed at the spelling where the
	// ticket actually landed.
	return textResult(fmt.Sprintf("%s (%s)", ticketLine(int(id), priority, args.Title), project)), nil, nil
}

// normalizePriority defaults a missing priority and clamps an out-of-range one
// rather than rejecting it, so one odd number in a batch does not cost the
// caller a round trip.
func normalizePriority(priority *int) int {
	if priority == nil || *priority < 0 || *priority > 5 {
		return 2
	}
	return *priority
}

// resolveProject resolves here rather than leaning on tix.Add so the reply
// names the project the tickets actually landed in, letting a caller that
// guessed at the spelling learn the real one.
func resolveProject(name string, create bool) (string, error) {
	if create {
		project, _, err := tix.EnsureProject(name)
		return project, err
	}
	return tix.ResolveProject(name)
}

type batchTicket struct {
	Title    string `json:"title" jsonschema:"the ticket title"`
	Body     string `json:"body" jsonschema:"the ticket body/description"`
	Priority *int   `json:"priority,omitempty" jsonschema:"priority 0-5, lower is more urgent (default 2)"`
}

type addBatchArgs struct {
	Tickets []batchTicket `json:"tickets" jsonschema:"the tickets to create, in order"`
	Project string        `json:"project" jsonschema:"the project every ticket in the batch belongs to; must be an existing project unless create is true"`
	Create  bool          `json:"create,omitempty" jsonschema:"create the project if it does not exist yet"`
}

func addBatchTool(_ context.Context, _ *mcp.CallToolRequest, args addBatchArgs) (*mcp.CallToolResult, any, error) {
	if len(args.Tickets) == 0 {
		return nil, nil, fmt.Errorf("tickets is required and must not be empty")
	}
	if args.Project == "" {
		return nil, nil, fmt.Errorf("project is required")
	}
	// Validate the whole batch before writing any of it: a batch that is going
	// to be rejected should be rejected whole, not half-inserted.
	for i, ticket := range args.Tickets {
		if ticket.Title == "" {
			return nil, nil, fmt.Errorf("tickets[%d]: title is required", i)
		}
		if ticket.Body == "" {
			return nil, nil, fmt.Errorf("tickets[%d]: body is required", i)
		}
	}
	project, err := resolveProject(args.Project, args.Create)
	if err != nil {
		return nil, nil, err
	}

	lines := make([]string, 0, len(args.Tickets))
	for i, ticket := range args.Tickets {
		priority := normalizePriority(ticket.Priority)
		id, err := tix.Add(ticket.Title, ticket.Body, priority, project)
		if err != nil {
			// Name what did land so the caller can retry the rest instead of
			// re-sending the whole batch and duplicating tickets. The ids go in
			// the error because an error reply carries no payload of its own.
			return nil, nil, fmt.Errorf("add failed for tickets[%d] (%d of %d already created: %s): %w",
				i, len(lines), len(args.Tickets), strings.Join(lines, "; "), err)
		}
		lines = append(lines, ticketLine(int(id), priority, ticket.Title))
	}
	return textResult(fmt.Sprintf("created %d in %s\n%s", len(lines), project, strings.Join(lines, "\n"))), nil, nil
}

type listArgs struct {
	Project  string `json:"project" jsonschema:"the project to list tickets for"`
	All      bool   `json:"all,omitempty" jsonschema:"include tickets in all statuses, not just open ones"`
	Status   string `json:"status,omitempty" jsonschema:"filter by exact status (e.g. open, in_progress, done)"`
	Priority *int   `json:"priority,omitempty" jsonschema:"filter by exact priority"`
	Limit    int    `json:"limit,omitempty" jsonschema:"limit the number of tickets returned (0 = no limit)"`
}

func listTool(_ context.Context, _ *mcp.CallToolRequest, args listArgs) (*mcp.CallToolResult, any, error) {
	if args.Project == "" {
		return nil, nil, fmt.Errorf("project is required")
	}
	if args.Limit < 0 {
		return nil, nil, fmt.Errorf("limit must be >= 0")
	}
	tickets, err := tix.ListTickets(tix.ListOptions{
		Project:  args.Project,
		ShowAll:  args.All,
		Status:   args.Status,
		Priority: args.Priority,
		Limit:    args.Limit,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("list failed: %w", err)
	}
	// An empty list says so rather than returning a blank reply, which reads as
	// a failed call.
	if len(tickets) == 0 {
		return textResult("no tickets"), nil, nil
	}
	return textResult(ticketLines(tickets)), nil, nil
}

// ticketLines renders a set of tickets one per line, the way the CLI does.
func ticketLines(tickets []tix.Ticket) string {
	lines := make([]string, len(tickets))
	for i, t := range tickets {
		// Done tickets are marked the way the CLI marks them, since a list that
		// includes them otherwise looks like a list of open work.
		marker := ""
		if t.Status == "done" {
			marker = "x "
		}
		lines[i] = marker + ticketLine(t.ID, t.Priority, t.Title)
	}
	return strings.Join(lines, "\n")
}

type searchArgs struct {
	Project  string `json:"project" jsonschema:"the project to search tickets in"`
	Query    string `json:"query" jsonschema:"the term to look for in ticket titles and bodies; matching ignores case and is a plain substring match"`
	Status   string `json:"status,omitempty" jsonschema:"limit the search to one status (e.g. open, in_progress, done); by default every status is searched"`
	Priority *int   `json:"priority,omitempty" jsonschema:"filter by exact priority"`
	Limit    int    `json:"limit,omitempty" jsonschema:"limit the number of tickets returned (0 = no limit)"`
}

func searchTool(_ context.Context, _ *mcp.CallToolRequest, args searchArgs) (*mcp.CallToolResult, any, error) {
	if args.Project == "" {
		return nil, nil, fmt.Errorf("project is required")
	}
	if strings.TrimSpace(args.Query) == "" {
		return nil, nil, fmt.Errorf("query is required")
	}
	if args.Limit < 0 {
		return nil, nil, fmt.Errorf("limit must be >= 0")
	}
	tickets, err := tix.ListTickets(tix.ListOptions{
		Project: args.Project,
		// Done tickets are searched too: work that already shipped makes a new
		// ticket just as duplicate as an open one does, and checking for
		// duplicates is what this tool is for.
		ShowAll:  true,
		Status:   args.Status,
		Query:    args.Query,
		Priority: args.Priority,
		Limit:    args.Limit,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("search failed: %w", err)
	}
	// The term is echoed because an empty result is the answer a caller acts on,
	// and it should be obvious which spelling produced it.
	if len(tickets) == 0 {
		return textResult(fmt.Sprintf("no tickets matching %q", args.Query)), nil, nil
	}
	return textResult(ticketLines(tickets)), nil, nil
}

type showArgs struct {
	ID int `json:"id" jsonschema:"the ticket id"`
}

func showTool(_ context.Context, _ *mcp.CallToolRequest, args showArgs) (*mcp.CallToolResult, any, error) {
	ticket, err := tix.Show(args.ID)
	if err != nil {
		return nil, nil, err
	}
	return textResult(ticketDetail(ticket)), nil, nil
}

type editArgs struct {
	ID       int     `json:"id" jsonschema:"the ticket id"`
	Title    *string `json:"title,omitempty" jsonschema:"new ticket title"`
	Body     *string `json:"body,omitempty" jsonschema:"new ticket body"`
	Status   *string `json:"status,omitempty" jsonschema:"new ticket status (e.g. open, in_progress, done)"`
	Priority *int    `json:"priority,omitempty" jsonschema:"new ticket priority"`
	Project  *string `json:"project,omitempty" jsonschema:"new project name"`
}

func editTool(_ context.Context, _ *mcp.CallToolRequest, args editArgs) (*mcp.CallToolResult, any, error) {
	ticket, err := tix.Edit(args.ID, tix.EditOptions{
		Title:    args.Title,
		Body:     args.Body,
		Status:   args.Status,
		Priority: args.Priority,
		Project:  args.Project,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("edit failed: %w", err)
	}
	// The full detail, not just the fields that changed: an edit is where a
	// caller most wants to see what the ticket now says.
	return textResult(ticketDetail(ticket)), nil, nil
}

func projectsTool(_ context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
	projects, err := tix.Projects()
	if err != nil {
		return nil, nil, fmt.Errorf("projects failed: %w", err)
	}
	if len(projects) == 0 {
		return textResult("no projects"), nil, nil
	}
	lines := make([]string, len(projects))
	for i, project := range projects {
		// The CLI names each project's id prefix here; this reply leaves it out
		// because these tools take an integer id, so the prefix is nothing the
		// caller could pass back.
		lines[i] = fmt.Sprintf("%s %s total=%d",
			project.Project, tix.FormatStatusCounts(project.StatusCounts), project.Total)
	}
	return textResult(strings.Join(lines, "\n")), nil, nil
}
