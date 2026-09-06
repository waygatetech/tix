/*
Copyright © 2026 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"fmt"

	"github.com/brianseitel/tix/internal/tix"
	"github.com/spf13/cobra"
)

var project string
var priority int
var body string

// addCmd represents the add command
var addCmd = &cobra.Command{
	Use:   "add",
	Short: "Add a ticket to the list.",
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) == 0 {
			fmt.Println("error: ticket title is required")
			fmt.Println("usage: tix add [title] --project NAME [--body TEXT] [--priority N]")
			return
		}

		if body == "" {
			fmt.Println("error: ticket body is required (usage: tix add [title] --project NAME --body TEXT)")
			return
		}

		if priority < 0 || priority > 5 {
			// default to low priority
			priority = 2
		}

		title := args[0]
		for _, a := range args[1:] {
			title += " " + a
		}

		id, err := tix.Add(title, body, priority, project)
		if err != nil {
			fmt.Println("add failed:", err)
			return
		}
		fmt.Printf("#%d [P%d] %s\n", id, priority, title)
	},
}
