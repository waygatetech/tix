package cmd

import (
	"os"

	"github.com/spf13/cobra"
	"github.com/waygatetech/tix/internal/tix"
	_ "modernc.org/sqlite"
)

var rootCmd = &cobra.Command{
	Use:          "tix",
	Short:        "A barebones ticket tracker",
	SilenceUsage: true,
}

// Execute opens the database and runs the command named on the command line.
// The version comes from main so release builds can stamp it via ldflags.
func Execute(version string) {
	rootCmd.Version = version
	tix.InitDB()
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}
