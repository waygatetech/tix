/*
Copyright © 2026 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"fmt"

	"github.com/brianseitel/tix/internal/tix"
	"github.com/spf13/cobra"
)

// nextCmd represents the next command
var nextCmd = &cobra.Command{
	Use:   "next",
	Short: "Show the next ticket",
	Run: func(cmd *cobra.Command, args []string) {
		id, title, body, priority, err := tix.Next(project)
		if err != nil {
			fmt.Println("no open tickets")
			return
		}
		fmt.Printf("#%d [P%d] %s\n", id, priority, title)
		if body != "" {
			fmt.Println(body)
		}
	},
}

func init() {
	nextCmd.Flags().StringVarP(&project, "project", "p", "", "project name")
	rootCmd.AddCommand(nextCmd)

	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// nextCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// nextCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
}
