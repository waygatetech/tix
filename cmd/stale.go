package cmd

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"github.com/waygatetech/tix/internal/tix"
)

// staleFor and staleStatus are this command's own, not the shared filter
// variables list and search use: defining a flag writes its default into the
// variable at init time, so a default of "in_progress" here would become
// list's default status too.
var (
	staleFor    time.Duration
	staleStatus string
)

var staleCmd = &cobra.Command{
	Use:   "stale",
	Short: "List tickets stuck in a status",
	Long: "List tickets that have sat in one status without a write for longer than --for.\n\n" +
		"This is the recovery path for the agent loop: claim-next marks a ticket\n" +
		"in_progress, and an agent that dies before `done` leaves it there. next skips\n" +
		"it because it is no longer open, so nothing surfaces it again. Tickets are\n" +
		"listed oldest first, with how long each has been idle; `tix edit ID --status\n" +
		"open` puts one back in the queue.",
	PersistentPreRunE: initDB,
	RunE: func(cmd *cobra.Command, args []string) error {
		scope, err := projectScope()
		if err != nil {
			return err
		}
		if staleFor < 0 {
			return fmt.Errorf("--for must be >= 0")
		}
		if limitFilter < 0 {
			return fmt.Errorf("limit must be >= 0")
		}

		tickets, err := tix.StaleTickets(tix.StaleOptions{
			Project: scope,
			Status:  staleStatus,
			Older:   staleFor,
			Limit:   limitFilter,
		})
		if err != nil {
			return err
		}
		if jsonOutput {
			return printJSON(tickets, compactOutput, fieldsOutput)
		}

		prefixes, err := tix.Prefixes()
		if err != nil {
			return err
		}
		now := time.Now()
		for _, ticket := range tickets {
			// The age is what the caller is here for, so it prints alongside the
			// status rather than as a raw timestamp to subtract by hand.
			fmt.Printf("%s [P%d] %s (%s, idle %s)\n",
				tix.FormatID(prefixes[ticket.Project], ticket.ID), ticket.Priority, ticket.Title,
				ticket.Status, tix.FormatAge(now.Sub(ticket.UpdatedAt)))
		}
		return nil
	},
}

func init() {
	staleCmd.Flags().StringVarP(&project, "project", "p", "", "project name (default: the project registered for this directory)")
	staleCmd.Flags().DurationVar(&staleFor, "for", tix.StaleAge, "how long a ticket must have gone without a write")
	staleCmd.Flags().StringVar(&staleStatus, "status", tix.StaleStatus, "status to treat as stuck")
	staleCmd.Flags().IntVar(&limitFilter, "limit", 0, "limit number of tickets")
	staleCmd.Flags().BoolVar(&jsonOutput, "json", false, "output tickets as JSON")
	staleCmd.Flags().BoolVar(&compactOutput, "compact", false, "minify JSON output")
	staleCmd.Flags().StringVar(&fieldsOutput, "fields", "", "comma-separated JSON fields to include")
	rootCmd.AddCommand(staleCmd)
}
