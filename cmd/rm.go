/*
Copyright © 2026 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/waygatetech/tix/internal/tix"
)

// rmCmd represents the rm command
var rmCmd = &cobra.Command{
	Use:   "rm",
	Short: "Remove a ticket from the list",
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return fmt.Errorf("ticket id is required")
		}
		id, err := tix.ResolveRef(args[0])
		if err != nil {
			return err
		}
		if err := tix.Remove(id, project); err != nil {
			return fmt.Errorf("error removing ticket: %w", err)
		}
		prefix, err := tix.PrefixFor(project)
		if err != nil {
			return err
		}
		fmt.Printf("%s removed\n", tix.FormatID(prefix, id))
		return nil
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
