package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/waygatetech/tix/internal/tix"
)

var doneCmd = &cobra.Command{
	Use:   "done",
	Short: "Mark a ticket as done",
	Long: `Mark a ticket as done.

If the project has a done hook (.tix/done-hook in its repo_path, else
~/.tix/done-hook), it runs first with the ticket ID as its argument: from the
current directory's worktree when that is a linked worktree of the repo, else
the ticket's .worktrees/<ref> worktree, else the repo. A non-zero exit leaves the ticket open. Every attempt is recorded
as a done_attempt event with its stdout, and a JSON "stage" field in that
output names the failing stage.`,
	PersistentPreRunE: initDB,
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) != 1 {
			return fmt.Errorf("usage: tix done [id]")
		}

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
		// A working directory tix cannot name just means no worktree override.
		cwd, _ := os.Getwd()
		if err := tix.RunDone(cmd.Context(), id, cwd); err != nil {
			return fmt.Errorf("done failed: %w", err)
		}
		prefix, err := tix.PrefixFor(ticket.Project)
		if err != nil {
			return err
		}
		fmt.Printf("%s done %s\n", tix.FormatID(prefix, id), ticketContext(ticket))
		return nil
	},
}

func init() {
	doneCmd.Flags().StringVarP(&project, "project", "p", "", "project the ticket must belong to (errors on mismatch)")
	rootCmd.AddCommand(doneCmd)
}
