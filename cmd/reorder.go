package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/waygatetech/tix/internal/tix"
)

var reorderCmd = &cobra.Command{
	Use:               "reorder [id...]",
	Short:             "Arrange tickets in a custom sequence",
	PersistentPreRunE: initDB,
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return fmt.Errorf("usage: tix reorder [id...]")
		}

		ids := make([]int, len(args))
		labels := make([]string, len(args))
		for i, arg := range args {
			id, err := tix.ResolveRef(arg)
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
			prefix, err := tix.PrefixFor(ticket.Project)
			if err != nil {
				return err
			}
			ids[i] = id
			labels[i] = tix.FormatID(prefix, id)
		}

		if err := tix.Reorder(ids); err != nil {
			return fmt.Errorf("reorder failed: %w", err)
		}
		fmt.Printf("reordered: %s\n", strings.Join(labels, " -> "))
		return nil
	},
}

func init() {
	reorderCmd.Flags().StringVarP(&project, "project", "p", "", "project every ticket must belong to (errors on mismatch)")
	rootCmd.AddCommand(reorderCmd)
}
