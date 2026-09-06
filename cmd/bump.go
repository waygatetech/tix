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

// bumpCmd represents the bump command
var bumpCmd = &cobra.Command{
	Use:   "bump",
	Short: "Change a ticket's priority",
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) != 2 {
			fmt.Println("usage: tix bump [id] [priority]")
			return
		}

		id, err1 := strconv.Atoi(args[0])
		priority, err2 := strconv.Atoi(args[1])
		if err1 != nil || err2 != nil {
			fmt.Println("usage: tix bump [id] [priority]")
			return
		}

		id, priority, err := tix.Bump(id, priority)
		if err != nil {
			fmt.Println("bump failed:", err)
			return
		}
		fmt.Printf("#%d -> P%d\n", id, priority)
	},
}

func init() {
	rootCmd.AddCommand(bumpCmd)

	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// bumpCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// bumpCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
}
