package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/waygatetech/tix/internal/tix"
)

var claimNextCmd = &cobra.Command{
	Use:   "claim-next",
	Short: "Claim the next open ticket",
	RunE: func(cmd *cobra.Command, args []string) error {
		project, err := projectScope()
		if err != nil {
			return err
		}
		ticket, err := tix.ClaimNext(project)
		if err != nil {
			return err
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
		return nil
	},
}

func init() {
	claimNextCmd.Flags().StringVarP(&project, "project", "p", "", "project name (default: the project registered for this directory)")
	claimNextCmd.Flags().BoolVar(&jsonOutput, "json", false, "output ticket as JSON")
	claimNextCmd.Flags().BoolVar(&compactOutput, "compact", false, "minify JSON output")
	claimNextCmd.Flags().StringVar(&fieldsOutput, "fields", "", "comma-separated JSON fields to include")
	rootCmd.AddCommand(claimNextCmd)
}
