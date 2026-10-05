package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/waygatetech/tix/internal/tix"
)

var planFile string

var planCmd = &cobra.Command{
	Use:   "plan [id]",
	Short: "Index a ticket's repo plan",
	Long: `Parse the YAML frontmatter of a ticket's plan and cache it on the ticket.

The plan defaults to plans/<key>-<id>.md under the project's repo_path. The
file stays canonical; re-running replaces the cached fields.

If the project has a plan hook (.tix/plan-hook in its repo_path, else
~/.tix/plan-hook), it receives the parsed plan as JSON on stdin and prints
{"tier": 0-2, "reason": "..."}. Tier 1+ leaves the plan awaiting review and
exits 3 until "tix approve" records a human ruling. No hook means tier 0.`,
	Args:              cobra.ExactArgs(1),
	PersistentPreRunE: initDB,
	RunE: func(cmd *cobra.Command, args []string) error {
		id, err := tix.ResolveRef(args[0])
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
		plan, err := tix.IndexPlan(cmd.Context(), id, planFile)
		if err != nil {
			return err
		}
		if jsonOutput {
			if err := printJSON(plan, compactOutput, fieldsOutput); err != nil {
				return err
			}
		} else {
			fmt.Printf("indexed %s\n", plan.Path)
		}
		if review := plan.Review; review != nil && review.State == tix.ReviewAwaiting {
			return exitError{code: exitAwaitingReview, err: fmt.Errorf(
				"plan is tier %d and awaits review (%s); run tix approve %s after a human rules",
				review.Tier, review.Reason, args[0])}
		}
		return nil
	},
}

var approveCmd = &cobra.Command{
	Use:               "approve [id]",
	Short:             "Record a human approval of a ticket's plan awaiting review",
	Args:              cobra.ExactArgs(1),
	PersistentPreRunE: initDB,
	RunE: func(cmd *cobra.Command, args []string) error {
		id, err := tix.ResolveRef(args[0])
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
		if _, err := tix.Approve(id); err != nil {
			return err
		}
		fmt.Printf("approved plan for %s\n", args[0])
		return nil
	},
}

func init() {
	planCmd.Flags().StringVar(&planFile, "file", "", "plan file (default plans/<key>-<id>.md in the project's repo_path)")
	planCmd.Flags().StringVarP(&project, "project", "p", "", "project the ticket must belong to (errors on mismatch)")
	planCmd.Flags().BoolVar(&jsonOutput, "json", false, "output the parsed plan as JSON")
	planCmd.Flags().BoolVar(&compactOutput, "compact", false, "minify JSON output")
	planCmd.Flags().StringVar(&fieldsOutput, "fields", "", "comma-separated JSON fields to include")
	rootCmd.AddCommand(planCmd)

	approveCmd.Flags().StringVarP(&project, "project", "p", "", "project the ticket must belong to (errors on mismatch)")
	rootCmd.AddCommand(approveCmd)
}
