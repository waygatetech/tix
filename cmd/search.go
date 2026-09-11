package cmd

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/waygatetech/tix/internal/tix"
)

var searchCmd = &cobra.Command{
	Use:   "search [term]",
	Short: "Search tickets by title and body",
	Long: "Search a project's tickets for a term appearing in the title or body.\n\n" +
		"Matching ignores case and is a plain substring match, not a pattern: % and _\n" +
		"are searched for literally. Every status is searched, including done, since a\n" +
		"ticket that already shipped is as much a duplicate as an open one; narrow with\n" +
		"--status when only part of the backlog is interesting.",
	PersistentPreRunE: initDB,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Multiple words are joined rather than rejected, so an unquoted
		// `tix search search timeout` searches for the phrase a caller meant
		// instead of erroring on the second word.
		term := strings.TrimSpace(strings.Join(args, " "))
		if term == "" {
			return errors.New("search term is required")
		}

		options, err := listOptions()
		if err != nil {
			return err
		}
		options.Query = term
		options.ShowAll = true

		tickets, err := tix.ListTickets(options)
		if err != nil {
			return fmt.Errorf("query failed: %w", err)
		}
		if jsonOutput {
			return printJSON(tickets, compactOutput, fieldsOutput)
		}
		return printTickets(tickets)
	},
}

func init() {
	searchCmd.Flags().StringVarP(&project, "project", "p", "", "project name (default: the project registered for this directory)")
	searchCmd.Flags().StringVar(&statusFilter, "status", "", "filter by status")
	searchCmd.Flags().StringVar(&priorityFilter, "priority", "", "filter by priority")
	searchCmd.Flags().IntVar(&limitFilter, "limit", 0, "limit number of tickets")
	searchCmd.Flags().BoolVar(&jsonOutput, "json", false, "output tickets as JSON")
	searchCmd.Flags().BoolVar(&compactOutput, "compact", false, "minify JSON output")
	searchCmd.Flags().StringVar(&fieldsOutput, "fields", "", "comma-separated JSON fields to include")
	rootCmd.AddCommand(searchCmd)
}
