package cmd

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/waygatetech/tix/internal/tix"
)

// The dashboard is only useful if it is there when you think of it, so it can
// be installed as a login-time agent instead of a command you remember to run.
// The agent is deliberately the least privileged thing that works: it runs as
// you, on an unprivileged port, bound to localhost. Reaching it by name costs
// one line in /etc/hosts, which is the only step that needs root and the one
// step tix will not take on your behalf.
const (
	// webServiceLabel is the launchd label, and so also the plist's name.
	webServiceLabel = "tech.waygate.tix.web"
	// webServiceHost is the name the hosts entry points at loopback.
	webServiceHost = "tix.local"
)

var webServicePort int

// webServiceHostsLine is the line /etc/hosts needs for webServiceHost to
// resolve. macOS answers .local over mDNS, but consults /etc/hosts first, so a
// plain loopback entry is enough and does not require advertising anything.
func webServiceHostsLine() string {
	return "127.0.0.1\t" + webServiceHost
}

// webServiceSudoLine is the command a person runs to add that entry. tix prints
// it rather than running it: editing /etc/hosts is a root-owned, machine-wide
// change, and it should be something you typed.
func webServiceSudoLine() string {
	return fmt.Sprintf("echo '%s' | sudo tee -a /etc/hosts", webServiceHostsLine())
}

// requireDarwin reports why the service commands do nothing elsewhere. launchd
// is macOS's, and a systemd or Windows equivalent would be a different plist, a
// different loader, and a different answer for .local — so tix declines rather
// than pretending.
func requireDarwin() error {
	if runtime.GOOS != "darwin" {
		return fmt.Errorf("tix web install is macOS-only (launchd); on %s, run `tix web` under whatever supervisor you already use", runtime.GOOS)
	}
	return nil
}

func webServicePlistPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "LaunchAgents", webServiceLabel+".plist"), nil
}

func webServiceLogPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".tix", "web.log"), nil
}

// webServiceTarget names the agent to launchctl. gui/<uid> is the per-user
// domain, which is what keeps this a login agent rather than a system daemon.
func webServiceTarget() string {
	return fmt.Sprintf("gui/%d/%s", os.Getuid(), webServiceLabel)
}

// webServicePlist renders the launchd job. It passes --global because a login
// agent has no meaningful working directory: the same reason `tix mcp` refuses
// to pick a database off the filesystem, applied to a process that starts
// before you have opened a terminal at all.
func webServicePlist(execPath string, port int, logPath string) (string, error) {
	var body bytes.Buffer
	body.WriteString(xml.Header)
	body.WriteString("<!DOCTYPE plist PUBLIC \"-//Apple//DTD PLIST 1.0//EN\" \"http://www.apple.com/DTDs/PropertyList-1.0.dtd\">\n")
	body.WriteString("<plist version=\"1.0\">\n<dict>\n")

	write := func(key, value string) error {
		var escaped bytes.Buffer
		if err := xml.EscapeText(&escaped, []byte(value)); err != nil {
			return err
		}
		fmt.Fprintf(&body, "\t<key>%s</key>\n\t<string>%s</string>\n", key, escaped.String())
		return nil
	}

	if err := write("Label", webServiceLabel); err != nil {
		return "", err
	}

	body.WriteString("\t<key>ProgramArguments</key>\n\t<array>\n")
	for _, arg := range []string{execPath, "web", "--global", "--port", strconv.Itoa(port)} {
		var escaped bytes.Buffer
		if err := xml.EscapeText(&escaped, []byte(arg)); err != nil {
			return "", err
		}
		fmt.Fprintf(&body, "\t\t<string>%s</string>\n", escaped.String())
	}
	body.WriteString("\t</array>\n")

	// RunAtLoad starts it at login; KeepAlive restarts it if it dies, which is
	// the whole difference between this and remembering to run `tix web`.
	body.WriteString("\t<key>RunAtLoad</key>\n\t<true/>\n")
	body.WriteString("\t<key>KeepAlive</key>\n\t<true/>\n")

	if err := write("StandardOutPath", logPath); err != nil {
		return "", err
	}
	if err := write("StandardErrorPath", logPath); err != nil {
		return "", err
	}

	body.WriteString("</dict>\n</plist>\n")
	return body.String(), nil
}

// hostsEntryPresent reports whether the given hosts-file text already points
// host at loopback. It reads the file the way the resolver does — comments
// stripped, first field the address, the rest names — so a commented-out line
// does not count as configured.
func hostsEntryPresent(contents, host string) bool {
	for _, line := range strings.Split(contents, "\n") {
		if hash := strings.IndexByte(line, '#'); hash >= 0 {
			line = line[:hash]
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		if fields[0] != "127.0.0.1" && fields[0] != "::1" {
			continue
		}
		for _, name := range fields[1:] {
			if strings.EqualFold(name, host) {
				return true
			}
		}
	}
	return false
}

func hostsConfigured() bool {
	contents, err := os.ReadFile("/etc/hosts")
	if err != nil {
		return false
	}
	return hostsEntryPresent(string(contents), webServiceHost)
}

// webServiceListening reports whether something answers on the port. It is a
// dial rather than a request because all status needs to know is that the
// agent got far enough to bind.
// webServiceWaitListening gives launchd a moment to get the process up before
// install passes judgment on it.
func webServiceWaitListening(port int, within time.Duration) bool {
	deadline := time.Now().Add(within)
	for {
		if webServiceListening(port) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func webServiceListening(port int) bool {
	conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), 500*time.Millisecond)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// webServiceExec resolves the binary the agent should run. launchd needs a real
// path that survives a shell it will never start, and symlinks are resolved so
// that replacing a version-manager shim later does not silently repoint the
// running service.
func webServiceExec() (string, error) {
	path, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	return filepath.Abs(path)
}

// launchctlRun runs a launchctl subcommand, returning its combined output so
// callers can decide whether a failure is worth reporting. bootout on a job
// that was never loaded fails, and that failure is not interesting.
func launchctlRun(args ...string) (string, error) {
	out, err := exec.Command("launchctl", args...).CombinedOutput()
	return string(out), err
}

var webInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "Install the dashboard as a launchd agent so it is always running",
	Long: `Install the dashboard as a launchd agent so it is always running.

The agent runs as you, on localhost, and starts again at every login. It serves
the global database (or TIX_DB from its own environment), never a repo-local
one, because a login agent has no meaningful working directory.

Reaching it by name needs one line in /etc/hosts. That is the only root-owned
step, and tix prints the command rather than running it.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireDarwin(); err != nil {
			return err
		}

		execPath, err := webServiceExec()
		if err != nil {
			return fmt.Errorf("find the tix binary: %w", err)
		}
		plistPath, err := webServicePlistPath()
		if err != nil {
			return err
		}
		logPath, err := webServiceLogPath()
		if err != nil {
			return err
		}

		if err := os.MkdirAll(filepath.Dir(plistPath), 0o755); err != nil {
			return fmt.Errorf("create %s: %w", filepath.Dir(plistPath), err)
		}
		if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
			return fmt.Errorf("create %s: %w", filepath.Dir(logPath), err)
		}

		plist, err := webServicePlist(execPath, webServicePort, logPath)
		if err != nil {
			return err
		}
		if err := os.WriteFile(plistPath, []byte(plist), 0o644); err != nil {
			return fmt.Errorf("write %s: %w", plistPath, err)
		}

		// Replacing a loaded job means unloading it first; the unload fails
		// when nothing was loaded, which is the ordinary first install.
		launchctlRun("bootout", webServiceTarget())
		if out, err := launchctlRun("bootstrap", fmt.Sprintf("gui/%d", os.Getuid()), plistPath); err != nil {
			return fmt.Errorf("launchctl bootstrap: %w: %s", err, strings.TrimSpace(out))
		}
		// bootstrap registers the job but does not reliably run it in the same
		// breath, so install would otherwise mean "running after your next
		// login" while reporting success now.
		if out, err := launchctlRun("kickstart", "-k", webServiceTarget()); err != nil {
			return fmt.Errorf("launchctl kickstart: %w: %s", err, strings.TrimSpace(out))
		}

		stdout := cmd.OutOrStdout()
		fmt.Fprintf(stdout, "installed %s\n", plistPath)
		fmt.Fprintf(stdout, "serving %s on port %d\n", tix.DBPath(), webServicePort)
		fmt.Fprintf(stdout, "logs at %s\n", logPath)

		// Say whether it is actually up. A port already taken by something else
		// is the likeliest way this fails, and the log is where that shows.
		if !webServiceWaitListening(webServicePort, 3*time.Second) {
			fmt.Fprintf(stdout, "\nWarning: nothing is listening on port %d yet. Check %s, or pick another port with --port.\n", webServicePort, logPath)
		}

		if hostsConfigured() {
			fmt.Fprintf(stdout, "\nopen http://%s:%d\n", webServiceHost, webServicePort)
			return nil
		}
		fmt.Fprintf(stdout, "\nOne step left — point %s at loopback:\n\n    %s\n\nThen open http://%s:%d\n",
			webServiceHost, webServiceSudoLine(), webServiceHost, webServicePort)
		return nil
	},
}

var webUninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Stop the dashboard agent and remove its launchd job",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireDarwin(); err != nil {
			return err
		}

		plistPath, err := webServicePlistPath()
		if err != nil {
			return err
		}

		launchctlRun("bootout", webServiceTarget())
		if err := os.Remove(plistPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove %s: %w", plistPath, err)
		}

		stdout := cmd.OutOrStdout()
		fmt.Fprintf(stdout, "removed %s\n", plistPath)
		// The hosts entry is machine-wide and tix never wrote it, so it is not
		// tix's to delete — but leaving it unmentioned would strand a name that
		// now resolves to nothing.
		if hostsConfigured() {
			fmt.Fprintf(stdout, "\n/etc/hosts still points %s at loopback; remove that line by hand if you want it gone.\n", webServiceHost)
		}
		return nil
	},
}

var webStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Report whether the dashboard agent is installed, running, and reachable",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireDarwin(); err != nil {
			return err
		}

		plistPath, err := webServicePlistPath()
		if err != nil {
			return err
		}
		stdout := cmd.OutOrStdout()

		installed := true
		if _, err := os.Stat(plistPath); err != nil {
			installed = false
		}
		fmt.Fprintf(stdout, "agent:    %s\n", yesNo(installed, plistPath, "not installed (run `tix web install`)"))

		_, loadErr := launchctlRun("print", webServiceTarget())
		fmt.Fprintf(stdout, "loaded:   %s\n", yesNo(loadErr == nil, "yes", "no"))

		port := webServicePort
		if installed {
			if found, err := webServicePlistPort(plistPath); err == nil {
				port = found
			}
		}
		fmt.Fprintf(stdout, "port:     %d %s\n", port, yesNo(webServiceListening(port), "(listening)", "(nothing listening)"))
		fmt.Fprintf(stdout, "hostname: %s\n", yesNo(hostsConfigured(), webServiceHost+" resolves to loopback", webServiceHost+" is not in /etc/hosts: "+webServiceSudoLine()))
		fmt.Fprintf(stdout, "database: %s\n", tix.DBPath())

		if installed && loadErr == nil && hostsConfigured() {
			fmt.Fprintf(stdout, "\nhttp://%s:%d\n", webServiceHost, port)
		}
		return nil
	},
}

func yesNo(ok bool, yes, no string) string {
	if ok {
		return yes
	}
	return no
}

// webServicePlistPort recovers the port from an installed plist, so status
// reports the port the agent is actually serving rather than today's default.
func webServicePlistPort(plistPath string) (int, error) {
	contents, err := os.ReadFile(plistPath)
	if err != nil {
		return 0, err
	}

	// The plist is read as a token stream rather than unmarshalled: a plist
	// dict is a flat alternating list of keys and values, which no Go struct
	// describes usefully, and the only thing wanted here is the argument
	// vector's strings in order.
	decoder := xml.NewDecoder(bytes.NewReader(contents))
	var values []string
	for {
		token, err := decoder.Token()
		if err != nil {
			break
		}
		start, ok := token.(xml.StartElement)
		if !ok || start.Name.Local != "string" {
			continue
		}
		var value string
		if err := decoder.DecodeElement(&value, &start); err != nil {
			continue
		}
		values = append(values, value)
	}

	for i, value := range values {
		if value == "--port" && i+1 < len(values) {
			return strconv.Atoi(values[i+1])
		}
	}
	return 0, fmt.Errorf("no --port in %s", plistPath)
}

func init() {
	webInstallCmd.Flags().IntVar(&webServicePort, "port", webDefaultPort, "port the agent serves on")
	webStatusCmd.Flags().IntVar(&webServicePort, "port", webDefaultPort, "port to check when no agent is installed")
	webCmd.AddCommand(webInstallCmd, webUninstallCmd, webStatusCmd)
}
