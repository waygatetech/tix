package cmd

import (
	"fmt"
	"strconv"

	"github.com/spf13/cobra"
	"github.com/waygatetech/tix/internal/tix"
)

var bumpCmd = &cobra.Command{
	Use:               "bump",
	Short:             "Change a ticket's priority",
	PersistentPreRunE: initDB,
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) != 2 {
			return fmt.Errorf("usage: tix bump [id] [priority]")
		}

		id, err := tix.ResolveRef(args[0])
		if err != nil {
			return err
		}
		priority, err := strconv.Atoi(args[1])
		if err != nil {
			return fmt.Errorf("usage: tix bump [id] [priority]")
		}

		ticket, err := tix.Show(id)
		if err != nil {
			return err
		}
		if err := requireProjectMatch(ticket, project); err != nil {
			return err
		}
		id, priority, err = tix.Bump(id, priority)
		if err != nil {
			return fmt.Errorf("bump failed: %w", err)
		}
		prefix, err := tix.PrefixFor(ticket.Project)
		if err != nil {
			return err
		}
		fmt.Printf("%s -> P%d %s\n", tix.FormatID(prefix, id), priority, ticketContext(ticket))
		return nil
	},
}

func init() {
	bumpCmd.Flags().StringVarP(&project, "project", "p", "", "project the ticket must belong to (errors on mismatch)")
	rootCmd.AddCommand(bumpCmd)
}
