/*
Copyright © 2026 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"fmt"
	"strconv"

	"github.com/brianseitel/tix/internal/tix"
	"github.com/spf13/cobra"
)

// rmCmd represents the rm command
var rmCmd = &cobra.Command{
	Use:   "rm",
	Short: "Remove a ticket from the list",
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) == 0 {
			fmt.Println("error: ticket id is required")
			fmt.Println("usage: tix rm [id] --project NAME")
			return
		}
		id, err := strconv.Atoi(args[0])
		if err != nil {
			fmt.Println("error: invalid ticket id:", args[0])
			return
		}
		err = tix.Remove(id, project)
		if err != nil {
			fmt.Println("error removing ticket:", err)
			return
		}
		fmt.Printf("#%d removed\n", id)
	},
}

func init() {
	rmCmd.Flags().StringVarP(&project, "project", "p", "", "project name")
	_ = rmCmd.MarkFlagRequired("project")
	rootCmd.AddCommand(rmCmd)

	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// rmCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// rmCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
}
