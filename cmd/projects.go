package cmd

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/waygatetech/tix/internal/tix"
)

var projectsJSONOutput bool
var projectKey string
var projectRepoPath string

var projectsCmd = &cobra.Command{
	Use:               "projects",
	Short:             "List all projects",
	Args:              cobra.NoArgs,
	PersistentPreRunE: initDB,
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
				tix.FormatStatusCounts(project.StatusCounts), project.Total)
		}
		return nil
	},
}

// projectsAddCmd is the explicit way to create a project; `tix add --create`
// covers the case where the first ticket and the project arrive together.
var projectsAddCmd = &cobra.Command{
	Use:               "add NAME",
	Short:             "Create a project",
	Args:              cobra.ExactArgs(1),
	PersistentPreRunE: initDB,
	RunE: func(cmd *cobra.Command, args []string) error {
		repoPath, err := absRepoPath(projectRepoPath)
		if err != nil {
			return err
		}
		project, err := tix.CreateProject(tix.Project{
			Name:     args[0],
			Key:      projectKey,
			RepoPath: repoPath,
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

// projectsSetRepoPathCmd points an existing project at its checkout, which is
// what lets commands run there without naming it. Projects created before the
// repo path existed need this; new ones can pass --repo-path instead.
var projectsSetRepoPathCmd = &cobra.Command{
	Use:   "set-repo-path NAME [PATH]",
	Short: "Record where a project's code lives",
	Long: `Commands that take --project infer the project from the working
directory when it sits inside a registered repository.

PATH defaults to the current directory.`,
	Args:              cobra.RangeArgs(1, 2),
	PersistentPreRunE: initDB,
	RunE: func(cmd *cobra.Command, args []string) error {
		path := "."
		if len(args) == 2 {
			path = args[1]
		}
		repoPath, err := absRepoPath(path)
		if err != nil {
			return err
		}
		resolved, err := tix.SetRepoPath(args[0], repoPath)
		if err != nil {
			return err
		}
		fmt.Printf("%s %s\n", resolved, repoPath)
		return nil
	},
}

// projectsSetMaxActiveCmd caps a project's in-progress work at what its human
// can review; claim-next refuses to claim past it.
var projectsSetMaxActiveCmd = &cobra.Command{
	Use:   "set-max-active NAME N",
	Short: "Cap a project's in-progress tickets (0 removes the cap)",
	Long: `claim-next exits 4 instead of claiming once NAME has N tickets in
progress. TIX_MAX_ACTIVE caps every project together (default 3, 0 disables).
Set N from tix stats review throughput, not by feel.`,
	Args:              cobra.ExactArgs(2),
	PersistentPreRunE: initDB,
	RunE: func(cmd *cobra.Command, args []string) error {
		n, err := strconv.Atoi(args[1])
		if err != nil {
			return fmt.Errorf("parsing N: %w", err)
		}
		resolved, err := tix.SetMaxActive(args[0], n)
		if err != nil {
			return err
		}
		fmt.Printf("%s max active %d\n", resolved, n)
		return nil
	},
}

// absRepoPath stores repo paths absolute: a relative one means whatever
// directory it was typed in, which is exactly the thing the stored path is
// supposed to answer.
func absRepoPath(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", nil
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve repo path failed: %w", err)
	}
	return abs, nil
}

func init() {
	projectsCmd.Flags().BoolVar(&projectsJSONOutput, "json", false, "output projects as JSON")
	projectsCmd.Flags().BoolVar(&compactOutput, "compact", false, "minify JSON output")
	projectsCmd.Flags().StringVar(&fieldsOutput, "fields", "", "comma-separated JSON fields to include")
	projectsAddCmd.Flags().StringVar(&projectKey, "key", "", "pin the ticket ID prefix instead of deriving it")
	projectsAddCmd.Flags().StringVar(&projectRepoPath, "repo-path", "", "path to the project's repository")
	projectsCmd.AddCommand(projectsAddCmd)
	projectsCmd.AddCommand(projectsSetRepoPathCmd)
	projectsCmd.AddCommand(projectsSetMaxActiveCmd)
	rootCmd.AddCommand(projectsCmd)
}
