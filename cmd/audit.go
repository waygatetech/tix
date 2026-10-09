package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/waygatetech/tix/internal/tix"
)

var auditDays int

var auditCmd = &cobra.Command{
	Use:   "audit",
	Short: "Record reverts of done tickets' commits",
	Long: `Scan the last --days of git history in each project's repo_path (or only
--project's) for "This reverts commit <sha>" and record a reverted event on
the ticket whose done event recorded that commit. A done recorded before the
commit existed is matched to the first commit on its branch made on top of
the recorded HEAD.

Reverts already recorded are skipped, so this is safe to run on a schedule.
Each newly recorded revert is printed.`,
	PersistentPreRunE: initDB,
	RunE: func(cmd *cobra.Command, args []string) error {
		reverts, err := tix.Audit(cmd.Context(), project, auditDays)
		if err != nil {
			return err
		}
		if jsonOutput {
			return printJSON(reverts, compactOutput, fieldsOutput)
		}
		prefixes, err := tix.Prefixes()
		if err != nil {
			return err
		}
		for _, r := range reverts {
			fmt.Printf("%s reverted: %s by %s\n", tix.FormatID(prefixes[r.Project], r.TicketID), shortSHA(r.SHA), shortSHA(r.RevertSHA))
		}
		return nil
	},
}

func shortSHA(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}

func init() {
	auditCmd.Flags().StringVarP(&project, "project", "p", "", "only audit this project (default: every project with a repo_path)")
	auditCmd.Flags().IntVar(&auditDays, "days", tix.DefaultAuditDays, "how many days of history to scan")
	auditCmd.Flags().BoolVar(&jsonOutput, "json", false, "output recorded reverts as JSON")
	auditCmd.Flags().BoolVar(&compactOutput, "compact", false, "minify JSON output")
	auditCmd.Flags().StringVar(&fieldsOutput, "fields", "", "comma-separated JSON fields to include")
	rootCmd.AddCommand(auditCmd)
}
