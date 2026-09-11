package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/waygatetech/tix/internal/tix"
)

var rmCmd = &cobra.Command{
	Use:   "rm",
	Short: "Remove a ticket from the list",
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
		if err := tix.Remove(id, project); err != nil {
			return fmt.Errorf("error removing ticket: %w", err)
		}
		prefix, err := tix.PrefixFor(project)
		if err != nil {
			return err
		}
		fmt.Printf("%s removed\n", tix.FormatID(prefix, id))
		return nil
	},
}

func init() {
	rmCmd.Flags().StringVarP(&project, "project", "p", "", "project name (default: the project registered for this directory)")
	rootCmd.AddCommand(rmCmd)
}
