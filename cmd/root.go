package cmd

import (
	"errors"
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
//
// The working directory picks which database that is: a repo that opted into
// its own backlog gets it without anyone exporting TIX_DB, and everything else
// shares the global one.
func initDB(cmd *cobra.Command, args []string) error {
	// A working directory tix cannot name is no reason to fail a command the
	// global database can serve.
	if dir, err := os.Getwd(); err == nil {
		if err := tix.UseRepoDB(dir); err != nil {
			return err
		}
	}
	return tix.InitDB()
}

// initDBWithoutRepoLookup opens the database without letting the working
// directory choose it, for callers whose working directory says nothing about
// the tickets they mean.
func initDBWithoutRepoLookup(cmd *cobra.Command, args []string) error {
	return tix.InitDB()
}

// Execute runs the command named on the command line. The version comes from
// main so release builds can stamp it via ldflags.
func Execute(version string) {
	rootCmd.Version = version
	err := rootCmd.Execute()
	var exit exitError
	if errors.As(err, &exit) {
		os.Exit(exit.code)
	}
	if err != nil {
		os.Exit(1)
	}
}

// exitAwaitingReview tells an agent to stop for a human rather than treat the
// command as failed.
const exitAwaitingReview = 3

// exitError is an error that exits with its own code instead of 1.
type exitError struct {
	code int
	err  error
}

func (e exitError) Error() string { return e.err.Error() }
func (e exitError) Unwrap() error { return e.err }
