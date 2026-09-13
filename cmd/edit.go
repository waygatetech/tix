package cmd

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/waygatetech/tix/internal/tix"
)

var (
	editTitle       string
	editBody        string
	editBodyFile    string
	editStatus      string
	editPriority    int
	editProject     string
	editParent      string
	editClearParent bool
)

var editCmd = &cobra.Command{
	Use:               "edit [id]",
	Short:             "Edit a ticket's fields",
	Args:              cobra.ExactArgs(1),
	PersistentPreRunE: initDB,
	RunE: func(cmd *cobra.Command, args []string) error {
		id, err := tix.ResolveRef(args[0])
		if err != nil {
			return err
		}

		// Read the ticket first so a move can be reported as a move: edit's
		// --project sets a new project rather than scoping the lookup, so
		// without the old name the confirmation cannot say where the ticket
		// came from.
		before, err := tix.Show(id)
		if err != nil {
			return err
		}

		if editBody != "" && editBodyFile != "" {
			return errors.New("use either --body or --body-file, not both")
		}
		if editBodyFile != "" {
			data, err := os.ReadFile(editBodyFile)
			if err != nil {
				return fmt.Errorf("read body file failed: %w", err)
			}
			editBody = string(data)
		}

		options := tix.EditOptions{}
		if cmd.Flags().Changed("title") {
			options.Title = &editTitle
		}
		if cmd.Flags().Changed("body") || cmd.Flags().Changed("body-file") {
			options.Body = &editBody
		}
		if cmd.Flags().Changed("status") {
			options.Status = &editStatus
		}
		if cmd.Flags().Changed("priority") {
			options.Priority = &editPriority
		}
		if cmd.Flags().Changed("project") {
			options.Project = &editProject
		}
		if cmd.Flags().Changed("parent") && editClearParent {
			return errors.New("use either --parent or --no-parent, not both")
		}
		if cmd.Flags().Changed("parent") {
			parentID, err := tix.ResolveRef(editParent)
			if err != nil {
				return err
			}
			options.ParentID = &parentID
		}
		if editClearParent {
			options.ClearParent = true
		}

		ticket, err := tix.Edit(id, options)
		if err != nil {
			return fmt.Errorf("edit failed: %w", err)
		}

		if jsonOutput {
			return printJSON(ticket, compactOutput, fieldsOutput)
		}

		prefix, err := tix.PrefixFor(ticket.Project)
		if err != nil {
			return err
		}
		fmt.Printf("%s [P%d] %s (%s, %s)\n",
			tix.FormatID(prefix, ticket.ID), ticket.Priority, ticket.Title, ticket.Status, ticket.Project)
		if before.Project != ticket.Project {
			fmt.Printf("moved from %s to %s\n", before.Project, ticket.Project)
		}
		if ticket.Body != "" {
			fmt.Println(ticket.Body)
		}
		return nil
	},
}

func init() {
	editCmd.Flags().StringVar(&editTitle, "title", "", "new ticket title")
	editCmd.Flags().StringVarP(&editBody, "body", "b", "", "new ticket body")
	editCmd.Flags().StringVar(&editBodyFile, "body-file", "", "read new ticket body from file")
	editCmd.Flags().StringVar(&editStatus, "status", "", "new ticket status")
	editCmd.Flags().IntVarP(&editPriority, "priority", "p", 0, "new ticket priority")
	editCmd.Flags().StringVar(&editProject, "project", "", "new project name")
	editCmd.Flags().StringVar(&editParent, "parent", "", "parent ticket to group this one under (epic)")
	editCmd.Flags().BoolVar(&editClearParent, "no-parent", false, "remove this ticket's parent")
	editCmd.Flags().BoolVar(&jsonOutput, "json", false, "output ticket as JSON")
	editCmd.Flags().BoolVar(&compactOutput, "compact", false, "minify JSON output")
	editCmd.Flags().StringVar(&fieldsOutput, "fields", "", "comma-separated JSON fields to include")
	rootCmd.AddCommand(editCmd)
}
