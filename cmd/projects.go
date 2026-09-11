/*
Copyright © 2026 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/waygatetech/tix/internal/tix"
)

var projectsJSONOutput bool
var projectKey string
var projectRepoPath string

// projectsCmd represents the projects command
var projectsCmd = &cobra.Command{
	Use:   "projects",
	Short: "List all projects",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		projects, err := tix.Projects()
		if err != nil {
			return fmt.Errorf("query failed: %w", err)
		}
		if projectsJSONOutput {
			return printJSON(projects, compactOutput, fieldsOutput)
		}
		for _, project := range projects {
			fmt.Printf("%s (%s) %s total=%d\n",
				project.Project, project.Key,
				statusCounts(project.StatusCounts), project.Total)
		}
		return nil
	},
}

// projectsAddCmd is the explicit way to create a project; `tix add --create`
// covers the case where the first ticket and the project arrive together.
var projectsAddCmd = &cobra.Command{
	Use:   "add NAME",
	Short: "Create a project",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		project, err := tix.CreateProject(tix.Project{
			Name:     args[0],
			Key:      projectKey,
			RepoPath: projectRepoPath,
		})
		if err != nil {
			return err
		}
		prefix, err := tix.PrefixFor(project.Name)
		if err != nil {
			return err
		}
		fmt.Printf("created project %s (%s)\n", project.Name, prefix)
		return nil
	},
}

func init() {
	projectsCmd.Flags().BoolVar(&projectsJSONOutput, "json", false, "output projects as JSON")
	projectsCmd.Flags().BoolVar(&compactOutput, "compact", false, "minify JSON output")
	projectsCmd.Flags().StringVar(&fieldsOutput, "fields", "", "comma-separated JSON fields to include")
	projectsAddCmd.Flags().StringVar(&projectKey, "key", "", "pin the ticket ID prefix instead of deriving it")
	projectsAddCmd.Flags().StringVar(&projectRepoPath, "repo-path", "", "path to the project's repository")
	projectsCmd.AddCommand(projectsAddCmd)
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
