/*
Copyright © 2026 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"fmt"

	"github.com/brianseitel/tix/internal/tix"
	"github.com/spf13/cobra"
)

var showAll bool

// listCmd represents the list command
var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List all tickets",
	Run: func(cmd *cobra.Command, args []string) {
		tickets := tix.List(project, showAll)
		for _, ticket := range tickets {
			fmt.Println(ticket)
		}
	},
}

func init() {
	listCmd.Flags().StringVarP(&project, "project", "p", "", "project name")
	_ = listCmd.MarkFlagRequired("project")
	listCmd.Flags().BoolVarP(&showAll, "all", "a", false, "include done tickets")
	rootCmd.AddCommand(listCmd)

	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// listCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// listCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
}
