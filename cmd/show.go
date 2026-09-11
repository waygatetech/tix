package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/waygatetech/tix/internal/tix"
)

var showCmd = &cobra.Command{
	Use:   "show [id]",
	Short: "Show a ticket",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id, err := tix.ResolveRef(args[0])
		if err != nil {
			return err
		}

		ticket, err := tix.Show(id)
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
		fmt.Printf("%s [P%d] %s (%s, %s)\n",
			tix.FormatID(prefix, ticket.ID), ticket.Priority, ticket.Title, ticket.Status, ticket.Project)
		if ticket.Body != "" {
			fmt.Println(ticket.Body)
		}
		return nil
	},
}

func init() {
	showCmd.Flags().BoolVar(&jsonOutput, "json", false, "output ticket as JSON")
	showCmd.Flags().BoolVar(&compactOutput, "compact", false, "minify JSON output")
	showCmd.Flags().StringVar(&fieldsOutput, "fields", "", "comma-separated JSON fields to include")
	rootCmd.AddCommand(showCmd)
}
