package cmd

import (
	"fmt"
	"sort"

	"github.com/spf13/cobra"
	"github.com/waygatetech/tix/internal/tix"
)

// prefixCmd shows the derived prefixes and pins one when the derived choice is
// not the one a person wants.
var prefixCmd = &cobra.Command{
	Use:   "prefix [project] [prefix]",
	Short: "Show or set the ticket ID prefix for a project",
	Long: `Ticket IDs display as <prefix>-<id>, e.g. fe-47. Prefixes are derived
from project names automatically; set one explicitly to pin it.

With no arguments, lists every project's prefix.`,
	Args:              cobra.MaximumNArgs(2),
	PersistentPreRunE: initDB,
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 2 {
			if err := tix.SetPrefix(args[0], args[1]); err != nil {
				return err
			}
		}

		prefixes, err := tix.Prefixes()
		if err != nil {
			return err
		}
		if len(args) > 0 {
			prefix, err := tix.PrefixFor(args[0])
			if err != nil {
				return err
			}
			fmt.Printf("%s %s\n", args[0], prefix)
			return nil
		}

		projects := make([]string, 0, len(prefixes))
		for project := range prefixes {
			projects = append(projects, project)
		}
		sort.Strings(projects)
		for _, project := range projects {
			fmt.Printf("%s %s\n", project, prefixes[project])
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(prefixCmd)
}
