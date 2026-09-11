package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

type bestPracticesGuide struct {
	Use      string   `json:"use"`
	Loop     []string `json:"loop"`
	Commands []string `json:"commands"`
	Output   string   `json:"output"`
}

var guide = bestPracticesGuide{
	Use: "TIX_DB=.tix/tix.db tix",
	Loop: []string{
		"claim-next --project NAME",
		"implement and verify",
		"done ID",
	},
	Commands: []string{
		"list --project NAME --status open --limit N",
		"show ID",
		"stale --project NAME  # tickets left in_progress by an agent that died; --for 2h to widen",
		"search --project NAME TERM  # title and body, every status; check before adding",
		"add --project NAME --body-file PATH --priority N TITLE",
		"projects  # project names; --project must be one of them",
		"projects add NAME  # or add --create, for a project that does not exist yet",
	},
	Output: "plain text is the default and is shorter than JSON for every command; add --json (with --compact/--fields) only when a program parses the result",
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
		fmt.Println("Output: " + guide.Output)
		return nil
	},
}

func init() {
	bestPracticesCmd.Flags().BoolVar(&jsonOutput, "json", false, "output guide as JSON")
	bestPracticesCmd.Flags().BoolVar(&compactOutput, "compact", false, "minify JSON output")
	bestPracticesCmd.Flags().StringVar(&fieldsOutput, "fields", "", "comma-separated JSON fields to include")
	rootCmd.AddCommand(bestPracticesCmd)
}
