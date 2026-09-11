package cmd

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/waygatetech/tix/internal/tix"
)

var (
	editTitle    string
	editBody     string
	editBodyFile string
	editStatus   string
	editPriority int
	editProject  string
)

var editCmd = &cobra.Command{
	Use:   "edit [id]",
	Short: "Edit a ticket's fields",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id, err := tix.ResolveRef(args[0])
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
	editCmd.Flags().BoolVar(&jsonOutput, "json", false, "output ticket as JSON")
	editCmd.Flags().BoolVar(&compactOutput, "compact", false, "minify JSON output")
	editCmd.Flags().StringVar(&fieldsOutput, "fields", "", "comma-separated JSON fields to include")
	rootCmd.AddCommand(editCmd)
}
