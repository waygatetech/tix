package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/waygatetech/tix/internal/tix"
)

var claimNextVerbose bool

var claimNextCmd = &cobra.Command{
	Use:               "claim-next",
	Short:             "Claim the next open ticket",
	PersistentPreRunE: initDB,
	RunE: func(cmd *cobra.Command, args []string) error {
		project, err := projectScope()
		if err != nil {
			return err
		}
		ticket, evaluation, err := tix.ClaimNextEvaluated(cmd.Context(), project)
		if err != nil {
			return err
		}
		if claimNextVerbose && evaluation != nil {
			fmt.Fprintln(os.Stderr, tix.FormatJevEvaluation(*evaluation))
		} else if evaluation != nil && evaluation.Mode == tix.JevEnforce && evaluation.Err != nil {
			fmt.Fprintf(os.Stderr, "warning: Jev evaluation failed; claiming ticket (fail open): %v\n", evaluation.Err)
		}
		if jsonOutput {
			return printJSON(ticket, compactOutput, fieldsOutput)
		}

		prefix, err := tix.PrefixFor(ticket.Project)
		if err != nil {
			return err
		}
		fmt.Printf("%s [P%d] %s\n", tix.FormatID(prefix, ticket.ID), ticket.Priority, ticket.Title)
		if ticket.Body != "" {
			fmt.Println(ticket.Body)
		}
		if ticket.Status == "blocked" && evaluation != nil {
			fmt.Println("Human input required; ticket left blocked.")
		}
		return nil
	},
}

func init() {
	claimNextCmd.Flags().StringVarP(&project, "project", "p", "", "project name (default: the project registered for this directory)")
	claimNextCmd.Flags().BoolVar(&jsonOutput, "json", false, "output ticket as JSON")
	claimNextCmd.Flags().BoolVar(&compactOutput, "compact", false, "minify JSON output")
	claimNextCmd.Flags().StringVar(&fieldsOutput, "fields", "", "comma-separated JSON fields to include")
	claimNextCmd.Flags().BoolVarP(&claimNextVerbose, "verbose", "v", false, "show Jev evaluation details")
	rootCmd.AddCommand(claimNextCmd)
}
