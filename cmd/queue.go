package cmd

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"github.com/waygatetech/tix/internal/tix"
)

var queueCmd = &cobra.Command{
	Use:   "queue",
	Short: "List tickets waiting on a human review",
	Long: "List unfinished tickets whose plan awaits review, oldest wait first.\n\n" +
		"This is the bottleneck view, across every project unless --project is\n" +
		"given. When it grows, lower max_active (tix projects set-max-active or\n" +
		"TIX_MAX_ACTIVE). tix stats --by week shows its depth over time.",
	PersistentPreRunE: initDB,
	RunE: func(cmd *cobra.Command, args []string) error {
		items, err := tix.ReviewQueue(project)
		if err != nil {
			return err
		}
		if jsonOutput {
			return printJSON(items, compactOutput, fieldsOutput)
		}
		prefixes, err := tix.Prefixes()
		if err != nil {
			return err
		}
		now := time.Now()
		for _, item := range items {
			fmt.Printf("%s [tier %d] %s (%s, waiting %s)\n",
				tix.FormatID(prefixes[item.Project], item.ID), item.Plan.Review.Tier, item.Title,
				item.Project, tix.FormatAge(now.Sub(item.Since)))
			fmt.Printf("  plan: %s\n", item.Plan.Path)
		}
		return nil
	},
}

func init() {
	queueCmd.Flags().StringVarP(&project, "project", "p", "", "only this project (default: every project)")
	queueCmd.Flags().BoolVar(&jsonOutput, "json", false, "output tickets as JSON")
	queueCmd.Flags().BoolVar(&compactOutput, "compact", false, "minify JSON output")
	queueCmd.Flags().StringVar(&fieldsOutput, "fields", "", "comma-separated JSON fields to include")
	rootCmd.AddCommand(queueCmd)
}
