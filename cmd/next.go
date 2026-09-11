package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/waygatetech/tix/internal/tix"
)

var nextCmd = &cobra.Command{
	Use:               "next",
	Short:             "Show the next ticket",
	PersistentPreRunE: initDB,
	RunE: func(cmd *cobra.Command, args []string) error {
		project, err := projectScope()
		if err != nil {
			return err
		}
		if jsonOutput {
			ticket, err := tix.NextTicket(project)
			if err != nil {
				return err
			}
			return printJSON(ticket, compactOutput, fieldsOutput)
		}

		id, title, body, priority, err := tix.Next(project)
		if err != nil {
			return err
		}
		prefix, err := tix.PrefixFor(project)
		if err != nil {
			return err
		}
		fmt.Printf("%s [P%d] %s\n", tix.FormatID(prefix, id), priority, title)
		if body != "" {
			fmt.Println(body)
		}
		return nil
	},
}

func init() {
	nextCmd.Flags().StringVarP(&project, "project", "p", "", "project name (default: the project registered for this directory)")
	nextCmd.Flags().BoolVar(&jsonOutput, "json", false, "output ticket as JSON")
	nextCmd.Flags().BoolVar(&compactOutput, "compact", false, "minify JSON output")
	nextCmd.Flags().StringVar(&fieldsOutput, "fields", "", "comma-separated JSON fields to include")
	rootCmd.AddCommand(nextCmd)
}
