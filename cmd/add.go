package cmd

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/waygatetech/tix/internal/tix"
)

var project string
var priority int
var body string
var bodyFile string
var createProject bool

var addCmd = &cobra.Command{
	Use:               "add",
	Short:             "Add a ticket to the list",
	PersistentPreRunE: initDB,
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return errors.New("ticket title is required")
		}

		if body != "" && bodyFile != "" {
			return errors.New("use either --body or --body-file, not both")
		}
		if bodyFile != "" {
			data, err := os.ReadFile(bodyFile)
			if err != nil {
				return fmt.Errorf("read body file failed: %w", err)
			}
			body = string(data)
		}
		if body == "" {
			return errors.New("ticket body is required")
		}

		if priority < 0 || priority > 5 {
			// default to low priority
			priority = 2
		}

		title := args[0]
		for _, a := range args[1:] {
			title += " " + a
		}

		project, err := projectScope()
		if err != nil {
			return err
		}

		if createProject {
			resolved, created, err := tix.EnsureProject(project)
			if err != nil {
				return err
			}
			if created {
				fmt.Printf("created project %s\n", resolved)
			}
			project = resolved
		}

		id, err := tix.Add(title, body, priority, project)
		if err != nil {
			return fmt.Errorf("add failed: %w", err)
		}
		prefix, err := tix.PrefixFor(project)
		if err != nil {
			return err
		}
		fmt.Printf("%s [P%d] %s\n", tix.FormatID(prefix, int(id)), priority, title)
		return nil
	},
}

func init() {
	addCmd.Flags().StringVar(&project, "project", "", "project name (default: the project registered for this directory)")
	addCmd.Flags().BoolVar(&createProject, "create", false, "create the project if it does not exist yet")
	addCmd.Flags().StringVarP(&body, "body", "b", "", "ticket body")
	addCmd.Flags().StringVar(&bodyFile, "body-file", "", "read ticket body from file")
	addCmd.Flags().IntVarP(&priority, "priority", "p", 2, "ticket priority")
	rootCmd.AddCommand(addCmd)
}
