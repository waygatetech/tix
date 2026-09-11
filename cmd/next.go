/*
Copyright © 2026 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/waygatetech/tix/internal/tix"
)

// nextCmd represents the next command
var nextCmd = &cobra.Command{
	Use:   "next",
	Short: "Show the next ticket",
	RunE: func(cmd *cobra.Command, args []string) error {
		if jsonOutput {
			ticket, err := tix.NextTicket(project)
			if err != nil {
				return err
			}
			return printJSON(ticket, compactOutput, fieldsOutput)
		}

		id, title, body, priority, err := tix.Next(project)
		if err != nil {
			return err
		}
		prefix, err := tix.PrefixFor(project)
		if err != nil {
			return err
		}
		fmt.Printf("%s [P%d] %s\n", tix.FormatID(prefix, id), priority, title)
		if body != "" {
			fmt.Println(body)
		}
		return nil
	},
}

func init() {
	nextCmd.Flags().StringVarP(&project, "project", "p", "", "project name")
	nextCmd.Flags().BoolVar(&jsonOutput, "json", false, "output ticket as JSON")
	nextCmd.Flags().BoolVar(&compactOutput, "compact", false, "minify JSON output")
	nextCmd.Flags().StringVar(&fieldsOutput, "fields", "", "comma-separated JSON fields to include")
	rootCmd.AddCommand(nextCmd)

	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// nextCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// nextCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
}
