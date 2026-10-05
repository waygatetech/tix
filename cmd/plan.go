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
file stays canonical; re-running replaces the cached fields.`,
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
		plan, err := tix.IndexPlan(id, planFile)
		if err != nil {
			return err
		}
		if jsonOutput {
			return printJSON(plan, compactOutput, fieldsOutput)
		}
		fmt.Printf("indexed %s\n", plan.Path)
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
}
