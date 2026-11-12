package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime/debug"
	"time"

	"github.com/spf13/cobra"
	"github.com/waygatetech/tix/internal/tix"
)

const modulePath = "github.com/waygatetech/tix"

// updateCheckCommand is the hidden command the background check runs as.
const updateCheckCommand = "update-check"

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Install the newest tix release with go install",
	Long: `Install the newest tix release with go install, pinned to the version the
update check found. Run it only after the user agrees to update.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		path, err := tix.UpdateStatePath()
		if err != nil {
			return err
		}
		return runUpdate(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), path, rootCmd.Version)
	},
}

var updateCheckCmd = &cobra.Command{
	Use:    updateCheckCommand,
	Short:  "Record the newest tix release (run in the background by tix itself)",
	Hidden: true,
	Args:   cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		path, err := tix.UpdateStatePath()
		if err != nil {
			return err
		}
		_, err = fetchAndStoreLatest(cmd.Context(), path)
		return err
	},
}

func init() {
	rootCmd.AddCommand(updateCmd)
	rootCmd.AddCommand(updateCheckCmd)
}

// resolveVersion prefers the version GoReleaser stamped, then the module
// version go install records, so `go install ...@vX.Y.Z` builds know what
// they are. GoReleaser's default stamp drops the tag's leading v, so it is put
// back to match the module proxy's versions.
func resolveVersion(stamped string, info *debug.BuildInfo) string {
	if stamped != "dev" {
		if tix.ValidRelease("v" + stamped) {
			return "v" + stamped
		}
		return stamped
	}
	if info == nil {
		return stamped
	}
	if v := info.Main.Version; v != "" && v != "(devel)" {
		return v
	}
	return stamped
}

// updateDisabled reports whether update checks and notices are off.
func updateDisabled(running string) bool {
	return os.Getenv("TIX_NO_UPDATE") != "" || !tix.ValidRelease(running)
}

// notifyUpdate tells the agent once about each newer release the background
// check found, and reports whether a new check is due. It records the check
// attempt before returning true, so a failed check is not retried that day.
func notifyUpdate(w io.Writer, statePath, running string, now time.Time) (checkDue bool, err error) {
	if updateDisabled(running) {
		return false, nil
	}
	state, err := tix.ReadUpdateState(statePath)
	if err != nil {
		return false, err
	}
	changed := false
	if tix.NewerRelease(state.Latest, running) && state.Notified != state.Latest {
		fmt.Fprintf(w, "tix %s is available (running %s). Ask the user whether to update; if they agree, run tix update.\n", state.Latest, running)
		state.Notified = state.Latest
		changed = true
	}
	if now.Sub(state.CheckedAt) >= tix.UpdateCheckInterval {
		state.CheckedAt = now
		checkDue = true
		changed = true
	}
	if changed {
		if err := tix.WriteUpdateState(statePath, state); err != nil {
			return false, err
		}
	}
	return checkDue, nil
}

// startUpdateCheck runs the check as a detached tix process so no command
// waits on the network.
func startUpdateCheck() error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("finding tix executable: %w", err)
	}
	check := exec.Command(exe, updateCheckCommand)
	if err := check.Start(); err != nil {
		return fmt.Errorf("starting update check: %w", err)
	}
	return check.Process.Release()
}

// maybeCheckForUpdate is the notice and background check every command gets.
// Failures here never affect the command, so they are dropped. The MCP server
// is skipped: its clients author tickets and never run tix update. So is shell
// completion, which runs on every tab press.
func maybeCheckForUpdate(args []string, running string) {
	if len(args) > 0 {
		switch args[0] {
		case updateCheckCommand, "mcp", cobra.ShellCompRequestCmd, cobra.ShellCompNoDescRequestCmd:
			return
		}
	}
	path, err := tix.UpdateStatePath()
	if err != nil {
		return
	}
	due, err := notifyUpdate(os.Stderr, path, running, time.Now())
	if err != nil || !due {
		return
	}
	_ = startUpdateCheck()
}

// fetchAndStoreLatest asks the proxy for the newest release and records it.
func fetchAndStoreLatest(ctx context.Context, statePath string) (string, error) {
	latest, err := tix.FetchLatest(ctx, tix.LatestVersionURL)
	if err != nil {
		return "", err
	}
	state, err := tix.ReadUpdateState(statePath)
	if err != nil {
		return "", err
	}
	state.Latest = latest
	if err := tix.WriteUpdateState(statePath, state); err != nil {
		return "", err
	}
	return latest, nil
}

// runUpdate installs the cached latest release, fetching it first when no
// check has found one yet.
func runUpdate(ctx context.Context, stdout, stderr io.Writer, statePath, running string) error {
	state, err := tix.ReadUpdateState(statePath)
	if err != nil {
		return err
	}
	latest := state.Latest
	if latest == "" {
		if latest, err = fetchAndStoreLatest(ctx, statePath); err != nil {
			return err
		}
	}
	if !tix.ValidRelease(latest) {
		return fmt.Errorf("cached latest version %q is not a release version", latest)
	}
	if tix.ValidRelease(running) && !tix.NewerRelease(latest, running) {
		return fmt.Errorf("tix %s is already current (latest is %s)", running, latest)
	}
	goBin, err := exec.LookPath("go")
	if err != nil {
		return fmt.Errorf("tix update needs the go command on PATH: %w", err)
	}
	install := exec.CommandContext(ctx, goBin, "install", modulePath+"@"+latest)
	install.Stdout = stderr
	install.Stderr = stderr
	if err := install.Run(); err != nil {
		return fmt.Errorf("running go install %s@%s: %w", modulePath, latest, err)
	}
	fmt.Fprintf(stdout, "installed tix %s\n", latest)
	return nil
}
