/*
Copyright © 2026 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"fmt"
	"strconv"

	"github.com/spf13/cobra"
	"github.com/waygatetech/tix/internal/tix"
)

// bumpCmd represents the bump command
var bumpCmd = &cobra.Command{
	Use:   "bump",
	Short: "Change a ticket's priority",
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) != 2 {
			return fmt.Errorf("usage: tix bump [id] [priority]")
		}

		id, err := tix.ResolveRef(args[0])
		if err != nil {
			return err
		}
		priority, err := strconv.Atoi(args[1])
		if err != nil {
			return fmt.Errorf("usage: tix bump [id] [priority]")
		}

		ticket, err := tix.Show(id)
		if err != nil {
			return err
		}
		id, priority, err = tix.Bump(id, priority)
		if err != nil {
			return fmt.Errorf("bump failed: %w", err)
		}
		prefix, err := tix.PrefixFor(ticket.Project)
		if err != nil {
			return err
		}
		fmt.Printf("%s -> P%d\n", tix.FormatID(prefix, id), priority)
		return nil
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
