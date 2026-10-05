package cmd

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/waygatetech/tix/internal/tix"
)

var showAll bool
var jsonOutput bool
var compactOutput bool
var statusFilter string
var priorityFilter string
var limitFilter int
var parentFilter string
var conceptFilter []string

var listCmd = &cobra.Command{
	Use:               "list",
	Short:             "List tickets",
	PersistentPreRunE: initDB,
	RunE: func(cmd *cobra.Command, args []string) error {
		options, err := listOptions()
		if err != nil {
			return err
		}
		if jsonOutput {
			tickets, err := tix.ListTickets(options)
			if err != nil {
				return fmt.Errorf("query failed: %w", err)
			}
			return printJSON(tickets, compactOutput, fieldsOutput)
		}

		tickets, err := tix.ListTickets(options)
		if err != nil {
			return fmt.Errorf("query failed: %w", err)
		}
		return printTickets(tickets)
	},
}

// printTickets writes the one-line form every ticket listing shares.
func printTickets(tickets []tix.Ticket) error {
	prefixes, err := tix.Prefixes()
	if err != nil {
		return err
	}
	for _, ticket := range tickets {
		marker := ""
		if ticket.Status == "done" {
			marker = "x "
		}
		concepts := ""
		if len(ticket.Concepts) > 0 {
			concepts = " {" + strings.Join(ticket.Concepts, ", ") + "}"
		}
		fmt.Printf("%s%s [P%d] %s%s\n",
			marker, tix.FormatID(prefixes[ticket.Project], ticket.ID), ticket.Priority, ticket.Title, concepts)
	}
	return nil
}

func init() {
	listCmd.Flags().StringVarP(&project, "project", "p", "", "project name (default: the project registered for this directory)")
	listCmd.Flags().BoolVarP(&showAll, "all", "a", false, "include tickets in all statuses")
	listCmd.Flags().StringVar(&statusFilter, "status", "", "filter by status")
	listCmd.Flags().StringVar(&priorityFilter, "priority", "", "filter by priority")
	listCmd.Flags().IntVar(&limitFilter, "limit", 0, "limit number of tickets")
	listCmd.Flags().StringVar(&parentFilter, "parent", "", "only tickets grouped under this parent ticket (epic)")
	listCmd.Flags().StringArrayVar(&conceptFilter, "concept", nil, "only tickets whose plan declares this concept (repeatable)")
	listCmd.Flags().BoolVar(&jsonOutput, "json", false, "output tickets as JSON")
	listCmd.Flags().BoolVar(&compactOutput, "compact", false, "minify JSON output")
	listCmd.Flags().StringVar(&fieldsOutput, "fields", "", "comma-separated JSON fields to include")
	rootCmd.AddCommand(listCmd)
}

func listOptions() (tix.ListOptions, error) {
	scope, err := projectScope()
	if err != nil {
		return tix.ListOptions{}, err
	}
	options := tix.ListOptions{
		Project:  scope,
		ShowAll:  showAll,
		Status:   statusFilter,
		Limit:    limitFilter,
		Concepts: conceptFilter,
	}
	if priorityFilter != "" {
		priority, err := strconv.Atoi(priorityFilter)
		if err != nil {
			return tix.ListOptions{}, fmt.Errorf("invalid priority: %s", priorityFilter)
		}
		options.Priority = &priority
	}
	if options.Limit < 0 {
		return tix.ListOptions{}, fmt.Errorf("limit must be >= 0")
	}
	if parentFilter != "" {
		parentID, err := tix.ResolveRef(parentFilter)
		if err != nil {
			return tix.ListOptions{}, err
		}
		options.ParentID = &parentID
	}
	return options, nil
}
