package cmd

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"github.com/waygatetech/tix/internal/tix"
)

// formatTimestamp renders a stored time the way the database holds it: UTC, to
// the second. A ticket's timestamps are compared across machines often enough
// that a local-time rendering would be the wrong default.
func formatTimestamp(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}

var showCmd = &cobra.Command{
	Use:               "show [id]",
	Short:             "Show a ticket",
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
		if jsonOutput {
			return printJSON(ticket, compactOutput, fieldsOutput)
		}

		prefix, err := tix.PrefixFor(ticket.Project)
		if err != nil {
			return err
		}
		fmt.Printf("%s [P%d] %s (%s, %s)\n",
			tix.FormatID(prefix, ticket.ID), ticket.Priority, ticket.Title, ticket.Status, ticket.Project)
		// Timestamps print here and nowhere else in the plain output: a listing
		// is read a screen at a time, and this is the view with room for them.
		fmt.Printf("created %s, updated %s (idle %s)\n",
			formatTimestamp(ticket.CreatedAt), formatTimestamp(ticket.UpdatedAt),
			tix.FormatAge(time.Since(ticket.UpdatedAt)))
		if ticket.ParentID != nil {
			parent, err := tix.Show(*ticket.ParentID)
			if err != nil {
				return err
			}
			fmt.Printf("parent: %s %s\n", tix.FormatID(prefix, parent.ID), parent.Title)
		}
		if ticket.Body != "" {
			fmt.Println(ticket.Body)
		}

		children, err := tix.ListTickets(tix.ListOptions{Project: ticket.Project, ShowAll: true, ParentID: &ticket.ID})
		if err != nil {
			return err
		}
		if len(children) > 0 {
			fmt.Println("children:")
			for _, child := range children {
				marker := ""
				if child.Status == "done" {
					marker = "x "
				}
				fmt.Printf("  %s%s [P%d] %s\n", marker, tix.FormatID(prefix, child.ID), child.Priority, child.Title)
			}
		}
		return nil
	},
}

func init() {
	showCmd.Flags().StringVarP(&project, "project", "p", "", "project the ticket must belong to (errors on mismatch)")
	showCmd.Flags().BoolVar(&jsonOutput, "json", false, "output ticket as JSON")
	showCmd.Flags().BoolVar(&compactOutput, "compact", false, "minify JSON output")
	showCmd.Flags().StringVar(&fieldsOutput, "fields", "", "comma-separated JSON fields to include")
	rootCmd.AddCommand(showCmd)
}
