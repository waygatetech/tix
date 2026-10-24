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
var parentRef string
var dependsOnRefs []string
var addDryRun bool

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

		if addDryRun && createProject {
			return errors.New("--dry-run cannot be combined with --create")
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
			if created && !jsonOutput {
				fmt.Printf("created project %s\n", resolved)
			}
			project = resolved
		}

		var parentID *int
		if parentRef != "" {
			id, err := tix.ResolveRef(parentRef)
			if err != nil {
				return err
			}
			parentID = &id
		}
		dependsOnIDs, err := resolveRefs(dependsOnRefs)
		if err != nil {
			return err
		}

		if addDryRun {
			ticket, err := tix.NewTicket(title, body, priority, project, parentID, dependsOnIDs...)
			if err != nil {
				return fmt.Errorf("add failed: %w", err)
			}
			if jsonOutput {
				return printJSON(ticket, compactOutput, fieldsOutput)
			}
			return nil
		}

		id, err := tix.Add(title, body, priority, project, parentID, dependsOnIDs...)
		if err != nil {
			return fmt.Errorf("add failed: %w", err)
		}
		if jsonOutput {
			ticket, err := tix.Show(int(id))
			if err != nil {
				return err
			}
			return printJSON(ticket, compactOutput, fieldsOutput)
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
	addCmd.Flags().StringVar(&parentRef, "parent", "", "parent ticket to group this one under (epic)")
	addCmd.Flags().StringSliceVar(&dependsOnRefs, "depends-on", nil, "ticket this one depends on; repeat or comma-separate for more")
	addCmd.Flags().BoolVar(&addDryRun, "dry-run", false, "run every check and write nothing; exit non-zero if the add would fail")
	addCmd.Flags().BoolVar(&jsonOutput, "json", false, "output the created ticket as JSON (with --dry-run, the ticket that would be created)")
	addCmd.Flags().BoolVar(&compactOutput, "compact", false, "minify JSON output")
	addCmd.Flags().StringVar(&fieldsOutput, "fields", "", "comma-separated JSON fields to include")
	rootCmd.AddCommand(addCmd)
}
