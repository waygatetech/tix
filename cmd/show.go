/*
Copyright © 2026 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"fmt"
	"strconv"

	"github.com/brianseitel/tix/internal/tix"
	"github.com/spf13/cobra"
)

// showCmd represents the show command
var showCmd = &cobra.Command{
	Use:   "show [id]",
	Short: "Show a ticket",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id, err := strconv.Atoi(args[0])
		if err != nil {
			return fmt.Errorf("invalid id: %s", args[0])
		}

		ticket, err := tix.Show(id)
		if err != nil {
			return err
		}
		if jsonOutput {
			return printJSON(ticket, compactOutput, fieldsOutput)
		}

		fmt.Printf("#%d [P%d] %s (%s, %s)\n", ticket.ID, ticket.Priority, ticket.Title, ticket.Status, ticket.Project)
		if ticket.Body != "" {
			fmt.Println(ticket.Body)
		}
		return nil
	},
}

func init() {
	showCmd.Flags().BoolVar(&jsonOutput, "json", false, "output ticket as JSON")
	showCmd.Flags().BoolVar(&compactOutput, "compact", false, "minify JSON output")
	showCmd.Flags().StringVar(&fieldsOutput, "fields", "", "comma-separated JSON fields to include")
	rootCmd.AddCommand(showCmd)
}
