package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/waygatetech/tix/internal/tix"
)

var rulesCmd = &cobra.Command{
	Use:   "rules",
	Short: "Print the ticket template and the rules drafts are checked against",
	RunE: func(cmd *cobra.Command, args []string) error {
		if jsonOutput {
			return printJSON(struct {
				Template string     `json:"template"`
				Rules    []tix.Rule `json:"rules"`
			}{tix.TicketTemplate, tix.Rules()}, compactOutput, "")
		}
		fmt.Println("# Ticket template")
		fmt.Println()
		fmt.Print(tix.TicketTemplate)
		fmt.Println()
		fmt.Println("# Rules")
		fmt.Println()
		for _, rule := range tix.Rules() {
			fmt.Printf("- %s (%s, %s): %s\n", rule.ID, rule.Field, rule.Severity, rule.Description)
		}
		return nil
	},
}

func init() {
	rulesCmd.Flags().BoolVar(&jsonOutput, "json", false, "output {template, rules} as JSON")
	rulesCmd.Flags().BoolVar(&compactOutput, "compact", false, "minify JSON output")
	rootCmd.AddCommand(rulesCmd)
}
