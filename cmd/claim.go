package cmd

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/waygatetech/tix/internal/tix"
)

var claimCmd = &cobra.Command{
	Use:               "claim <id>",
	Short:             "Claim a specific open ticket",
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
		ticket, err = tix.Claim(id)
		if err != nil {
			return capacityExit(err)
		}
		if jsonOutput {
			return printJSON(ticket, compactOutput, fieldsOutput)
		}
		return printClaimed(ticket)
	},
}

// capacityExit lists what is waiting on review and returns the at-capacity
// exit code when err is an *AtCapacityError. Any other error passes through.
func capacityExit(err error) error {
	var full *tix.AtCapacityError
	if !errors.As(err, &full) {
		return err
	}
	fmt.Fprintln(os.Stderr, "Waiting on review:")
	for _, t := range full.Waiting {
		prefix, err := tix.PrefixFor(t.Project)
		if err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "  %s %s\n", tix.FormatID(prefix, t.ID), t.Title)
	}
	return exitError{code: exitAtCapacity, err: err}
}

func printClaimed(ticket tix.Ticket) error {
	prefix, err := tix.PrefixFor(ticket.Project)
	if err != nil {
		return err
	}
	fmt.Printf("%s [P%d] %s\n", tix.FormatID(prefix, ticket.ID), ticket.Priority, ticket.Title)
	if ticket.Body != "" {
		fmt.Println(ticket.Body)
	}
	return nil
}

func init() {
	claimCmd.Flags().StringVarP(&project, "project", "p", "", "project the ticket must belong to (errors on mismatch)")
	claimCmd.Flags().BoolVar(&jsonOutput, "json", false, "output ticket as JSON")
	claimCmd.Flags().BoolVar(&compactOutput, "compact", false, "minify JSON output")
	claimCmd.Flags().StringVar(&fieldsOutput, "fields", "", "comma-separated JSON fields to include")
	rootCmd.AddCommand(claimCmd)
}
