package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

type bestPracticesGuide struct {
	Use      string   `json:"use"`
	Loop     []string `json:"loop"`
	Commands []string `json:"commands"`
}

var guide = bestPracticesGuide{
	Use: "TIX_DB=.tix/tix.db tix",
	Loop: []string{
		"claim-next --project NAME --json --compact --fields id,title,body",
		"implement and verify",
		"done ID",
	},
	Commands: []string{
		"list --project NAME --status open --limit N --json --compact --fields id,title,priority,status",
		"show ID --json --compact",
		"add --project NAME --body-file PATH --priority N TITLE",
		"projects --json --compact  # project names; --project must be one of them",
		"projects add NAME  # or add --create, for a project that does not exist yet",
	},
}

var bestPracticesCmd = &cobra.Command{
	Use:     "best-practices",
	Aliases: []string{"agent-guide"},
	Short:   "Show terse agent workflow guidance",
	RunE: func(cmd *cobra.Command, args []string) error {
		if jsonOutput {
			return printJSON(guide, compactOutput, fieldsOutput)
		}

		fmt.Println("Use: " + guide.Use)
		fmt.Println("Loop: " + guide.Loop[0] + " -> " + guide.Loop[1] + " -> " + guide.Loop[2])
		for _, command := range guide.Commands {
			fmt.Println(command)
		}
		return nil
	},
}

func init() {
	bestPracticesCmd.Flags().BoolVar(&jsonOutput, "json", false, "output guide as JSON")
	bestPracticesCmd.Flags().BoolVar(&compactOutput, "compact", false, "minify JSON output")
	bestPracticesCmd.Flags().StringVar(&fieldsOutput, "fields", "", "comma-separated JSON fields to include")
	rootCmd.AddCommand(bestPracticesCmd)
}
