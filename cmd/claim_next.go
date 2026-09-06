/*
Copyright © 2026 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"fmt"

	"github.com/brianseitel/tix/internal/tix"
	"github.com/spf13/cobra"
)

// claimNextCmd represents the claim-next command
var claimNextCmd = &cobra.Command{
	Use:   "claim-next",
	Short: "Claim the next open ticket",
	RunE: func(cmd *cobra.Command, args []string) error {
		ticket, err := tix.ClaimNext(project)
		if err != nil {
			return err
		}
		if jsonOutput {
			return printJSON(ticket, compactOutput, fieldsOutput)
		}

		fmt.Printf("#%d [P%d] %s\n", ticket.ID, ticket.Priority, ticket.Title)
		if ticket.Body != "" {
			fmt.Println(ticket.Body)
		}
		return nil
	},
}

func init() {
	claimNextCmd.Flags().StringVarP(&project, "project", "p", "", "project name")
	_ = claimNextCmd.MarkFlagRequired("project")
	claimNextCmd.Flags().BoolVar(&jsonOutput, "json", false, "output ticket as JSON")
	claimNextCmd.Flags().BoolVar(&compactOutput, "compact", false, "minify JSON output")
	claimNextCmd.Flags().StringVar(&fieldsOutput, "fields", "", "comma-separated JSON fields to include")
	rootCmd.AddCommand(claimNextCmd)
}
