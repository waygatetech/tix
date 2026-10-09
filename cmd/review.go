package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/waygatetech/tix/internal/tix"
)

var reviewReason string

var rejectCmd = &cobra.Command{
	Use:   "reject [id]",
	Short: "Send a plan or delivered work back to in_progress",
	Long: `Record a human sending a ticket's plan or delivered work back.

An in_progress or done ticket returns to in_progress and keeps its plan and
leases. The reason is recorded as a reject event.`,
	Args:              cobra.ExactArgs(1),
	PersistentPreRunE: initDB,
	RunE: func(cmd *cobra.Command, args []string) error {
		return reviewOutcome(args[0], "rejected", tix.Reject)
	},
}

var reopenCmd = &cobra.Command{
	Use:   "reopen [id]",
	Short: "Return a done ticket found broken to open",
	Long: `Record a done ticket found broken later.

The ticket returns to open with its cached plan cleared, so it must be planned
again. The reason is recorded as a reopen event.`,
	Args:              cobra.ExactArgs(1),
	PersistentPreRunE: initDB,
	RunE: func(cmd *cobra.Command, args []string) error {
		return reviewOutcome(args[0], "reopened", tix.Reopen)
	},
}

func reviewOutcome(ref, verb string, apply func(int, string) error) error {
	id, err := tix.ResolveRef(ref)
	if err != nil {
		return err
	}
	ticket, err := tix.Show(id)
	if err != nil {
		return err
	}
	if err := requireProjectMatch(ticket, project); err != nil {
		return err
	}
	if err := apply(id, reviewReason); err != nil {
		return err
	}
	fmt.Printf("%s %s\n", verb, ref)
	return nil
}

func init() {
	for _, c := range []*cobra.Command{rejectCmd, reopenCmd} {
		c.Flags().StringVarP(&project, "project", "p", "", "project the ticket must belong to (errors on mismatch)")
		c.Flags().StringVar(&reviewReason, "reason", "", "why the work is sent back (required)")
		_ = c.MarkFlagRequired("reason")
		rootCmd.AddCommand(c)
	}
}
