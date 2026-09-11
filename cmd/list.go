/*
Copyright © 2026 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"fmt"
	"strconv"

	"github.com/spf13/cobra"
	"github.com/waygatetech/tix/internal/tix"
)

var showAll bool
var jsonOutput bool
var compactOutput bool
var statusFilter string
var priorityFilter string
var limitFilter int

// listCmd represents the list command
var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List tickets",
	RunE: func(cmd *cobra.Command, args []string) error {
		options, err := listOptions()
		if err != nil {
			return err
		}
		if jsonOutput {
			tickets, err := tix.ListTickets(options)
			if err != nil {
				return fmt.Errorf("query failed: %w", err)
			}
			return printJSON(tickets, compactOutput, fieldsOutput)
		}

		tickets, err := tix.ListTickets(options)
		if err != nil {
			return fmt.Errorf("query failed: %w", err)
		}
		prefixes, err := tix.Prefixes()
		if err != nil {
			return err
		}
		for _, ticket := range tickets {
			marker := ""
			if ticket.Status == "done" {
				marker = "x "
			}
			fmt.Printf("%s%s [P%d] %s\n",
				marker, tix.FormatID(prefixes[ticket.Project], ticket.ID), ticket.Priority, ticket.Title)
		}
		return nil
	},
}

func init() {
	listCmd.Flags().StringVarP(&project, "project", "p", "", "project name")
	_ = listCmd.MarkFlagRequired("project")
	listCmd.Flags().BoolVarP(&showAll, "all", "a", false, "include tickets in all statuses")
	listCmd.Flags().StringVar(&statusFilter, "status", "", "filter by status")
	listCmd.Flags().StringVar(&priorityFilter, "priority", "", "filter by priority")
	listCmd.Flags().IntVar(&limitFilter, "limit", 0, "limit number of tickets")
	listCmd.Flags().BoolVar(&jsonOutput, "json", false, "output tickets as JSON")
	listCmd.Flags().BoolVar(&compactOutput, "compact", false, "minify JSON output")
	listCmd.Flags().StringVar(&fieldsOutput, "fields", "", "comma-separated JSON fields to include")
	rootCmd.AddCommand(listCmd)

	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// listCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// listCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
}

func listOptions() (tix.ListOptions, error) {
	options := tix.ListOptions{
		Project: project,
		ShowAll: showAll,
		Status:  statusFilter,
		Limit:   limitFilter,
	}
	if priorityFilter != "" {
		priority, err := strconv.Atoi(priorityFilter)
		if err != nil {
			return tix.ListOptions{}, fmt.Errorf("invalid priority: %s", priorityFilter)
		}
		options.Priority = &priority
	}
	if options.Limit < 0 {
		return tix.ListOptions{}, fmt.Errorf("limit must be >= 0")
	}
	return options, nil
}
