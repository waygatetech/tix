/*
Copyright © 2026 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"fmt"
	"sort"
	"strings"

	"github.com/brianseitel/tix/internal/tix"
	"github.com/spf13/cobra"
)

var projectsJSONOutput bool

// projectsCmd represents the projects command
var projectsCmd = &cobra.Command{
	Use:   "projects",
	Short: "List all projects",
	RunE: func(cmd *cobra.Command, args []string) error {
		projects, err := tix.Projects()
		if err != nil {
			return fmt.Errorf("query failed: %w", err)
		}
		if projectsJSONOutput {
			return printJSON(projects, compactOutput, fieldsOutput)
		}
		for _, project := range projects {
			fmt.Printf("%s %s total=%d\n", project.Project, statusCounts(project.StatusCounts), project.Total)
		}
		return nil
	},
}

func init() {
	projectsCmd.Flags().BoolVar(&projectsJSONOutput, "json", false, "output projects as JSON")
	projectsCmd.Flags().BoolVar(&compactOutput, "compact", false, "minify JSON output")
	projectsCmd.Flags().StringVar(&fieldsOutput, "fields", "", "comma-separated JSON fields to include")
	rootCmd.AddCommand(projectsCmd)
}

func statusCounts(counts map[string]int) string {
	statuses := make([]string, 0, len(counts))
	for status := range counts {
		statuses = append(statuses, status)
	}
	sort.Strings(statuses)

	parts := make([]string, 0, len(statuses))
	for _, status := range statuses {
		parts = append(parts, fmt.Sprintf("%s=%d", status, counts[status]))
	}
	return strings.Join(parts, " ")
}
