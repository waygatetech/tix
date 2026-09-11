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

// initDB opens the database for a command that reads or writes tickets. Only
// those commands attach it as their PersistentPreRunE, so help, version, and
// completion still work when the database cannot be opened.
func initDB(cmd *cobra.Command, args []string) error {
	return tix.InitDB()
}

// Execute runs the command named on the command line. The version comes from
// main so release builds can stamp it via ldflags.
func Execute(version string) {
	rootCmd.Version = version
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}
