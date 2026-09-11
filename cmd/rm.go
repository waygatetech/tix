package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/waygatetech/tix/internal/tix"
)

var rmCmd = &cobra.Command{
	Use:               "rm",
	Short:             "Remove a ticket from the list",
	PersistentPreRunE: initDB,
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return fmt.Errorf("ticket id is required")
		}
		id, err := tix.ResolveRef(args[0])
		if err != nil {
			return err
		}
		project, err := projectScope()
		if err != nil {
			return err
		}
		// Read the ticket before it is gone so the confirmation can name what
		// was removed; Remove still does the project-scoped delete, so a
		// ticket in another project fails there rather than here.
		ticket, err := tix.Show(id)
		if err != nil {
			return err
		}
		if err := tix.Remove(id, project); err != nil {
			return fmt.Errorf("error removing ticket: %w", err)
		}
		prefix, err := tix.PrefixFor(project)
		if err != nil {
			return err
		}
		fmt.Printf("%s removed %s\n", tix.FormatID(prefix, id), ticketContext(ticket))
		return nil
	},
}

func init() {
	rmCmd.Flags().StringVarP(&project, "project", "p", "", "project name (default: the project registered for this directory)")
	rootCmd.AddCommand(rmCmd)
}
