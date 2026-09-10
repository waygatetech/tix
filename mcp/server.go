// Package mcp exposes tix's ticket operations as MCP tools over stdio.
package mcp

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/waygatetech/tix/internal/tix"
)

// NewServer builds an MCP server with one tool per tix ticket operation.
func NewServer() *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "tix", Version: "0.1.0"}, nil)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "tix_add",
		Description: "Create a new ticket in a project.",
	}, addTool)

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
		Name:        "tix_done",
		Description: "Mark a ticket as done.",
	}, doneTool)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "tix_bump",
		Description: "Change a ticket's priority.",
	}, bumpTool)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "tix_rm",
		Description: "Delete a ticket from a project.",
	}, rmTool)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "tix_next",
		Description: "Show the next open ticket to work on in a project, ordered by priority.",
	}, nextTool)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "tix_claim_next",
		Description: "Atomically claim the next open ticket in a project, marking it in_progress.",
	}, claimNextTool)

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
	Project  string `json:"project" jsonschema:"the project the ticket belongs to"`
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
	priority := 2
	if args.Priority != nil {
		priority = *args.Priority
	}
	if priority < 0 || priority > 5 {
		priority = 2
	}
	id, err := tix.Add(args.Title, args.Body, priority, args.Project)
	if err != nil {
		return nil, addOut{}, fmt.Errorf("add failed: %w", err)
	}
	return nil, addOut{ID: id, Title: args.Title, Priority: priority, Project: args.Project}, nil
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

type doneArgs struct {
	ID int `json:"id" jsonschema:"the ticket id"`
}

type doneOut struct {
	ID     int    `json:"id"`
	Status string `json:"status"`
}

func doneTool(_ context.Context, _ *mcp.CallToolRequest, args doneArgs) (*mcp.CallToolResult, doneOut, error) {
	if err := tix.Done(args.ID); err != nil {
		return nil, doneOut{}, fmt.Errorf("done failed: %w", err)
	}
	return nil, doneOut{ID: args.ID, Status: "done"}, nil
}

type bumpArgs struct {
	ID       int `json:"id" jsonschema:"the ticket id"`
	Priority int `json:"priority" jsonschema:"the new priority"`
}

type bumpOut struct {
	ID       int `json:"id"`
	Priority int `json:"priority"`
}

func bumpTool(_ context.Context, _ *mcp.CallToolRequest, args bumpArgs) (*mcp.CallToolResult, bumpOut, error) {
	id, priority, err := tix.Bump(args.ID, args.Priority)
	if err != nil {
		return nil, bumpOut{}, fmt.Errorf("bump failed: %w", err)
	}
	return nil, bumpOut{ID: id, Priority: priority}, nil
}

type rmArgs struct {
	ID      int    `json:"id" jsonschema:"the ticket id"`
	Project string `json:"project" jsonschema:"the project the ticket belongs to"`
}

type rmOut struct {
	ID      int  `json:"id"`
	Removed bool `json:"removed"`
}

func rmTool(_ context.Context, _ *mcp.CallToolRequest, args rmArgs) (*mcp.CallToolResult, rmOut, error) {
	if args.Project == "" {
		return nil, rmOut{}, fmt.Errorf("project is required")
	}
	if err := tix.Remove(args.ID, args.Project); err != nil {
		return nil, rmOut{}, fmt.Errorf("remove failed: %w", err)
	}
	return nil, rmOut{ID: args.ID, Removed: true}, nil
}

type nextArgs struct {
	Project string `json:"project" jsonschema:"the project to find the next ticket in"`
}

func nextTool(_ context.Context, _ *mcp.CallToolRequest, args nextArgs) (*mcp.CallToolResult, ticketOut, error) {
	if args.Project == "" {
		return nil, ticketOut{}, fmt.Errorf("project is required")
	}
	ticket, err := tix.NextTicket(args.Project)
	if err != nil {
		return nil, ticketOut{}, err
	}
	return nil, toTicketOut(ticket), nil
}

type claimNextArgs struct {
	Project string `json:"project" jsonschema:"the project to claim the next ticket from"`
}

func claimNextTool(_ context.Context, _ *mcp.CallToolRequest, args claimNextArgs) (*mcp.CallToolResult, ticketOut, error) {
	if args.Project == "" {
		return nil, ticketOut{}, fmt.Errorf("project is required")
	}
	ticket, err := tix.ClaimNext(args.Project)
	if err != nil {
		return nil, ticketOut{}, err
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
