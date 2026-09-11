// Package mcp exposes tix's ticket operations as MCP tools over stdio.
package mcp

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/waygatetech/tix/internal/tix"
)

// NewServer builds an MCP server exposing tix's authoring operations as tools.
//
// The set is deliberately narrow: writing tickets down, reading them back, and
// correcting them. Claiming and closing tickets belong to whoever does the
// work, so tix_claim_next, tix_done, and tix_rm stay on the CLI rather than
// letting a client here close tickets it never worked on.
func NewServer() *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "tix", Version: "0.1.0"}, nil)

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

type ticketOut struct {
	ID       int    `json:"id"`
	Title    string `json:"title"`
	Body     string `json:"body,omitempty"`
	Status   string `json:"status"`
	Priority int    `json:"priority"`
	Project  string `json:"project"`
}

func toTicketOut(t tix.Ticket) ticketOut {
	return ticketOut{
		ID:       t.ID,
		Title:    t.Title,
		Body:     t.Body,
		Status:   t.Status,
		Priority: t.Priority,
		Project:  t.Project,
	}
}

type addArgs struct {
	Title    string `json:"title" jsonschema:"the ticket title"`
	Body     string `json:"body" jsonschema:"the ticket body/description"`
	Priority *int   `json:"priority,omitempty" jsonschema:"priority 0-5, lower is more urgent (default 2)"`
	Project  string `json:"project" jsonschema:"the project the ticket belongs to; must be an existing project unless create is true"`
	Create   bool   `json:"create,omitempty" jsonschema:"create the project if it does not exist yet"`
}

type addOut struct {
	ID       int64  `json:"id"`
	Title    string `json:"title"`
	Priority int    `json:"priority"`
	Project  string `json:"project"`
}

func addTool(_ context.Context, _ *mcp.CallToolRequest, args addArgs) (*mcp.CallToolResult, addOut, error) {
	if args.Title == "" {
		return nil, addOut{}, fmt.Errorf("title is required")
	}
	if args.Body == "" {
		return nil, addOut{}, fmt.Errorf("body is required")
	}
	if args.Project == "" {
		return nil, addOut{}, fmt.Errorf("project is required")
	}
	priority := normalizePriority(args.Priority)
	project, err := resolveProject(args.Project, args.Create)
	if err != nil {
		return nil, addOut{}, err
	}
	id, err := tix.Add(args.Title, args.Body, priority, project)
	if err != nil {
		return nil, addOut{}, fmt.Errorf("add failed: %w", err)
	}
	return nil, addOut{ID: id, Title: args.Title, Priority: priority, Project: project}, nil
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

type addBatchOut struct {
	Tickets []addOut `json:"tickets"`
	Project string   `json:"project"`
	Created int      `json:"created"`
}

func addBatchTool(_ context.Context, _ *mcp.CallToolRequest, args addBatchArgs) (*mcp.CallToolResult, addBatchOut, error) {
	if len(args.Tickets) == 0 {
		return nil, addBatchOut{}, fmt.Errorf("tickets is required and must not be empty")
	}
	if args.Project == "" {
		return nil, addBatchOut{}, fmt.Errorf("project is required")
	}
	// Validate the whole batch before writing any of it: a batch that is going
	// to be rejected should be rejected whole, not half-inserted.
	for i, ticket := range args.Tickets {
		if ticket.Title == "" {
			return nil, addBatchOut{}, fmt.Errorf("tickets[%d]: title is required", i)
		}
		if ticket.Body == "" {
			return nil, addBatchOut{}, fmt.Errorf("tickets[%d]: body is required", i)
		}
	}
	project, err := resolveProject(args.Project, args.Create)
	if err != nil {
		return nil, addBatchOut{}, err
	}

	out := addBatchOut{Project: project, Tickets: make([]addOut, 0, len(args.Tickets))}
	for i, ticket := range args.Tickets {
		priority := normalizePriority(ticket.Priority)
		id, err := tix.Add(ticket.Title, ticket.Body, priority, project)
		if err != nil {
			// Report what did land so the caller can retry the rest instead of
			// re-sending the whole batch and duplicating tickets.
			return nil, out, fmt.Errorf("add failed for tickets[%d] (%d of %d already created): %w", i, len(out.Tickets), len(args.Tickets), err)
		}
		out.Tickets = append(out.Tickets, addOut{ID: id, Title: ticket.Title, Priority: priority, Project: project})
	}
	out.Created = len(out.Tickets)
	return nil, out, nil
}

type listArgs struct {
	Project  string `json:"project" jsonschema:"the project to list tickets for"`
	All      bool   `json:"all,omitempty" jsonschema:"include tickets in all statuses, not just open ones"`
	Status   string `json:"status,omitempty" jsonschema:"filter by exact status (e.g. open, in_progress, done)"`
	Priority *int   `json:"priority,omitempty" jsonschema:"filter by exact priority"`
	Limit    int    `json:"limit,omitempty" jsonschema:"limit the number of tickets returned (0 = no limit)"`
}

type listOut struct {
	Tickets []ticketOut `json:"tickets"`
}

func listTool(_ context.Context, _ *mcp.CallToolRequest, args listArgs) (*mcp.CallToolResult, listOut, error) {
	if args.Project == "" {
		return nil, listOut{}, fmt.Errorf("project is required")
	}
	if args.Limit < 0 {
		return nil, listOut{}, fmt.Errorf("limit must be >= 0")
	}
	tickets, err := tix.ListTickets(tix.ListOptions{
		Project:  args.Project,
		ShowAll:  args.All,
		Status:   args.Status,
		Priority: args.Priority,
		Limit:    args.Limit,
	})
	if err != nil {
		return nil, listOut{}, fmt.Errorf("list failed: %w", err)
	}
	out := make([]ticketOut, len(tickets))
	for i, t := range tickets {
		out[i] = toTicketOut(t)
	}
	return nil, listOut{Tickets: out}, nil
}

type showArgs struct {
	ID int `json:"id" jsonschema:"the ticket id"`
}

func showTool(_ context.Context, _ *mcp.CallToolRequest, args showArgs) (*mcp.CallToolResult, ticketOut, error) {
	ticket, err := tix.Show(args.ID)
	if err != nil {
		return nil, ticketOut{}, err
	}
	return nil, toTicketOut(ticket), nil
}

type editArgs struct {
	ID       int     `json:"id" jsonschema:"the ticket id"`
	Title    *string `json:"title,omitempty" jsonschema:"new ticket title"`
	Body     *string `json:"body,omitempty" jsonschema:"new ticket body"`
	Status   *string `json:"status,omitempty" jsonschema:"new ticket status (e.g. open, in_progress, done)"`
	Priority *int    `json:"priority,omitempty" jsonschema:"new ticket priority"`
	Project  *string `json:"project,omitempty" jsonschema:"new project name"`
}

func editTool(_ context.Context, _ *mcp.CallToolRequest, args editArgs) (*mcp.CallToolResult, ticketOut, error) {
	ticket, err := tix.Edit(args.ID, tix.EditOptions{
		Title:    args.Title,
		Body:     args.Body,
		Status:   args.Status,
		Priority: args.Priority,
		Project:  args.Project,
	})
	if err != nil {
		return nil, ticketOut{}, fmt.Errorf("edit failed: %w", err)
	}
	return nil, toTicketOut(ticket), nil
}

type projectsOut struct {
	Projects []tix.ProjectSummary `json:"projects"`
}

func projectsTool(_ context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, projectsOut, error) {
	projects, err := tix.Projects()
	if err != nil {
		return nil, projectsOut{}, fmt.Errorf("projects failed: %w", err)
	}
	return nil, projectsOut{Projects: projects}, nil
}
