package cmd

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/waygatetech/tix/internal/tix"
)

var noteBodyFile string

var noteCmd = &cobra.Command{
	Use:   "note [id] [text]",
	Short: "Append a handoff note to a ticket",
	Long: `Append a timestamped note to a ticket's history.

Notes are append-only and change nothing else about the ticket. Leave one when
stopping work without tix done: what was done, what was decided and by whom,
what's next, and which files were touched. tix show prints them.`,
	Args:              cobra.RangeArgs(1, 2),
	PersistentPreRunE: initDB,
	RunE: func(cmd *cobra.Command, args []string) error {
		var body string
		if len(args) == 2 {
			body = args[1]
		}
		if body != "" && noteBodyFile != "" {
			return errors.New("use either note text or --body-file, not both")
		}
		if noteBodyFile != "" {
			data, err := os.ReadFile(noteBodyFile)
			if err != nil {
				return fmt.Errorf("reading note body file: %w", err)
			}
			body = string(data)
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
		if err := tix.Note(id, body); err != nil {
			return err
		}
		fmt.Printf("noted %s\n", args[0])
		return nil
	},
}

func init() {
	noteCmd.Flags().StringVarP(&project, "project", "p", "", "project the ticket must belong to (errors on mismatch)")
	noteCmd.Flags().StringVar(&noteBodyFile, "body-file", "", "read the note from file")
	rootCmd.AddCommand(noteCmd)
}
