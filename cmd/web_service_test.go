package cmd

import (
	"encoding/xml"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The plist is only ever read by launchd, so the test that matters is that it
// is well-formed XML carrying the exact argument vector the agent needs.
func TestWebServicePlistIsWellFormedAndPassesTheRightArguments(t *testing.T) {
	plist, err := webServicePlist("/usr/local/bin/tix", 7433, "/Users/x/.tix/web.log")
	if err != nil {
		t.Fatalf("webServicePlist: %v", err)
	}

	decoder := xml.NewDecoder(strings.NewReader(plist))
	decoder.Strict = true
	for {
		if _, err := decoder.Token(); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			t.Fatalf("plist is not well-formed XML: %v\n%s", err, plist)
		}
	}

	for _, want := range []string{
		"<string>" + webServiceLabel + "</string>",
		"<string>/usr/local/bin/tix</string>",
		"<string>web</string>",
		"<string>--global</string>", // a login agent must never pick up a repo database
		"<string>--port</string>",
		"<string>7433</string>",
		"<key>RunAtLoad</key>",
		"<key>KeepAlive</key>",
		"/Users/x/.tix/web.log",
	} {
		if !strings.Contains(plist, want) {
			t.Fatalf("plist is missing %q:\n%s", want, plist)
		}
	}
}

// A path with an ampersand or a quote in it would produce a plist launchd
// silently refuses to load, so escaping is not cosmetic.
func TestWebServicePlistEscapesPaths(t *testing.T) {
	plist, err := webServicePlist(`/Users/a&b/<tix>`, 80, "/tmp/log")
	if err != nil {
		t.Fatalf("webServicePlist: %v", err)
	}
	if strings.Contains(plist, "/Users/a&b/<tix>") {
		t.Fatalf("plist did not escape the executable path:\n%s", plist)
	}
	if !strings.Contains(plist, "/Users/a&amp;b/&lt;tix&gt;") {
		t.Fatalf("plist escaping is wrong:\n%s", plist)
	}
}

func TestWebServicePlistPortRoundTrips(t *testing.T) {
	plist, err := webServicePlist("/usr/local/bin/tix", 9111, "/tmp/log")
	if err != nil {
		t.Fatalf("webServicePlist: %v", err)
	}
	path := filepath.Join(t.TempDir(), "agent.plist")
	if err := os.WriteFile(path, []byte(plist), 0o644); err != nil {
		t.Fatalf("write plist: %v", err)
	}

	port, err := webServicePlistPort(path)
	if err != nil {
		t.Fatalf("webServicePlistPort: %v", err)
	}
	if port != 9111 {
		t.Fatalf("port = %d, want 9111", port)
	}
}

func TestWebServicePlistPortReportsAPlistWithoutOne(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent.plist")
	if err := os.WriteFile(path, []byte("<plist><dict></dict></plist>"), 0o644); err != nil {
		t.Fatalf("write plist: %v", err)
	}
	if _, err := webServicePlistPort(path); err == nil {
		t.Fatal("webServicePlistPort accepted a plist with no --port")
	}
}

// status and install both branch on this, and a wrong answer either nags about
// a step already done or silently skips the one step that needs root.
func TestHostsEntryPresent(t *testing.T) {
	for _, tc := range []struct {
		name     string
		contents string
		want     bool
	}{
		{"missing", "127.0.0.1\tlocalhost\n255.255.255.255\tbroadcasthost\n", false},
		{"present", "127.0.0.1\tlocalhost\n127.0.0.1\ttix.local\n", true},
		{"shares a line with localhost", "127.0.0.1 localhost tix.local\n", true},
		{"ipv6 loopback", "::1 tix.local\n", true},
		{"commented out", "# 127.0.0.1 tix.local\n", false},
		{"trailing comment does not hide it", "127.0.0.1 tix.local # the dashboard\n", true},
		{"pointed somewhere else", "10.0.0.5 tix.local\n", false},
		{"case insensitive", "127.0.0.1 TIX.LOCAL\n", true},
		{"a longer name that merely contains it", "127.0.0.1 nottix.local\n", false},
		{"empty file", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := hostsEntryPresent(tc.contents, webServiceHost); got != tc.want {
				t.Fatalf("hostsEntryPresent(%q) = %v, want %v", tc.contents, got, tc.want)
			}
		})
	}
}

// The sudo line is copy-pasted by a person, so a malformed one wastes a
// root-privileged command.
func TestWebServiceSudoLineAddsAnEntryThatCounts(t *testing.T) {
	if !hostsEntryPresent(webServiceHostsLine(), webServiceHost) {
		t.Fatalf("the line install tells people to add is not one hostsEntryPresent accepts: %q", webServiceHostsLine())
	}
	if !strings.Contains(webServiceSudoLine(), "/etc/hosts") {
		t.Fatalf("sudo line does not mention /etc/hosts: %q", webServiceSudoLine())
	}
}

func TestWebServiceTargetIsAPerUserDomain(t *testing.T) {
	target := webServiceTarget()
	if !strings.HasPrefix(target, "gui/") {
		t.Fatalf("target = %q, want a gui/<uid> domain so the agent is not a system daemon", target)
	}
	if !strings.HasSuffix(target, "/"+webServiceLabel) {
		t.Fatalf("target = %q, want it to end in the label", target)
	}
}
