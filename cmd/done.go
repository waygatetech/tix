package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/waygatetech/tix/internal/tix"
)

var doneCmd = &cobra.Command{
	Use:               "done",
	Short:             "Mark a ticket as done",
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
		if err := tix.Done(id); err != nil {
			return fmt.Errorf("done failed: %w", err)
		}
		prefix, err := tix.PrefixFor(ticket.Project)
		if err != nil {
			return err
		}
		fmt.Printf("%s done\n", tix.FormatID(prefix, id))
		return nil
	},
}

func init() {
	rootCmd.AddCommand(doneCmd)
}
