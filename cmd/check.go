package cmd

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/waygatetech/tix/internal/tix"
)

var checkBase string

var checkCmd = &cobra.Command{
	Use:   "check [id]",
	Short: "Verify a ticket's diff stays within its plan",
	Long: `Compare the files changed in the project's repo_path since the merge base
with --base (committed, uncommitted, and untracked) to the paths globs of the
ticket's indexed plan. "**" matches any number of directories.

Exempt from paths: the plan file itself, concepts.yaml, and the globs in
.tix/check-exempt (one per line; go.mod and go.sum when the file is absent).
concepts.yaml may only add concepts the plan declares in new_concepts.

Exits non-zero listing each violation. Meant to run from the done hook.`,
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
		result, err := tix.Check(cmd.Context(), id, checkBase)
		if err != nil {
			return err
		}
		if jsonOutput {
			if err := printJSON(result, compactOutput, fieldsOutput); err != nil {
				return err
			}
		} else {
			for _, file := range result.OutOfScope {
				fmt.Printf("out of scope: %s\n", file)
			}
			for _, concept := range result.UndeclaredConcepts {
				fmt.Printf("undeclared concept: %s\n", concept)
			}
		}
		if !result.OK() {
			return errors.New("diff does not conform to the plan")
		}
		return nil
	},
}

func init() {
	checkCmd.Flags().StringVar(&checkBase, "base", "main", "branch whose merge base the diff starts from")
	checkCmd.Flags().StringVarP(&project, "project", "p", "", "project the ticket must belong to (errors on mismatch)")
	checkCmd.Flags().BoolVar(&jsonOutput, "json", false, "output violations as JSON")
	checkCmd.Flags().BoolVar(&compactOutput, "compact", false, "minify JSON output")
	checkCmd.Flags().StringVar(&fieldsOutput, "fields", "", "comma-separated JSON fields to include")
	rootCmd.AddCommand(checkCmd)
}
