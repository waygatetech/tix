package cmd

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"
	"time"

	"github.com/waygatetech/tix/internal/tix"
)

func TestResolveVersion(t *testing.T) {
	withVersion := func(v string) *debug.BuildInfo {
		return &debug.BuildInfo{Main: debug.Module{Path: modulePath, Version: v}}
	}
	tests := []struct {
		name    string
		stamped string
		info    *debug.BuildInfo
		want    string
	}{
		{"go install build", "dev", withVersion("v1.2.3"), "v1.2.3"},
		{"stamped release wins", "v1.5.0", withVersion("v1.2.3"), "v1.5.0"},
		{"goreleaser stamp gains its v", "1.5.0", nil, "v1.5.0"},
		{"other stamp kept", "1.5.0-rc.1", nil, "1.5.0-rc.1"},
		{"local build", "dev", withVersion("(devel)"), "dev"},
		{"no build info", "dev", nil, "dev"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resolveVersion(tt.stamped, tt.info); got != tt.want {
				t.Errorf("resolveVersion = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNotifyUpdate(t *testing.T) {
	now := time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC)
	fresh := now.Add(-time.Hour)
	stale := now.Add(-25 * time.Hour)
	const notice = "tix v1.3.0 is available (running v1.2.0). Ask the user whether to update; if they agree, run tix update.\n"
	tests := []struct {
		name       string
		noUpdate   bool
		running    string
		state      tix.UpdateState
		wantOut    string
		wantDue    bool
		wantNotify string
	}{
		{"newer release", false, "v1.2.0", tix.UpdateState{CheckedAt: fresh, Latest: "v1.3.0"}, notice, false, "v1.3.0"},
		{"already notified", false, "v1.2.0", tix.UpdateState{CheckedAt: fresh, Latest: "v1.3.0", Notified: "v1.3.0"}, "", false, "v1.3.0"},
		{"current", false, "v1.3.0", tix.UpdateState{CheckedAt: fresh, Latest: "v1.3.0"}, "", false, ""},
		{"disabled", true, "v1.2.0", tix.UpdateState{CheckedAt: stale, Latest: "v1.3.0"}, "", false, ""},
		{"dev build", false, "dev", tix.UpdateState{CheckedAt: stale, Latest: "v1.3.0"}, "", false, ""},
		{"stale check", false, "v1.3.0", tix.UpdateState{CheckedAt: stale, Latest: "v1.3.0"}, "", true, ""},
		{"never checked", false, "v1.3.0", tix.UpdateState{}, "", true, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.noUpdate {
				t.Setenv("TIX_NO_UPDATE", "1")
			} else {
				t.Setenv("TIX_NO_UPDATE", "")
			}
			path := filepath.Join(t.TempDir(), "update.json")
			if err := tix.WriteUpdateState(path, tt.state); err != nil {
				t.Fatal(err)
			}
			var out bytes.Buffer
			due, err := notifyUpdate(&out, path, tt.running, now)
			if err != nil {
				t.Fatalf("notifyUpdate: %v", err)
			}
			if out.String() != tt.wantOut {
				t.Errorf("output = %q, want %q", out.String(), tt.wantOut)
			}
			if due != tt.wantDue {
				t.Errorf("checkDue = %v, want %v", due, tt.wantDue)
			}
			got, err := tix.ReadUpdateState(path)
			if err != nil {
				t.Fatal(err)
			}
			if got.Notified != tt.wantNotify {
				t.Errorf("notified = %q, want %q", got.Notified, tt.wantNotify)
			}
			if tt.wantDue && !got.CheckedAt.Equal(now) {
				t.Errorf("checked_at = %v, want the attempt recorded at %v", got.CheckedAt, now)
			}

			// A second command the same day says nothing and starts no check.
			out.Reset()
			due, err = notifyUpdate(&out, path, tt.running, now)
			if err != nil {
				t.Fatalf("second notifyUpdate: %v", err)
			}
			if out.Len() != 0 || due {
				t.Errorf("second run printed %q, checkDue %v; want silence", out.String(), due)
			}
		})
	}
}

// fakeGo puts a go on PATH that records its arguments, and returns where.
func fakeGo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	argsFile := filepath.Join(dir, "args")
	script := "#!/bin/sh\necho \"$@\" > " + argsFile + "\n"
	if err := os.WriteFile(filepath.Join(dir, "go"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	return argsFile
}

func TestRunUpdate(t *testing.T) {
	cached := tix.UpdateState{CheckedAt: time.Now(), Latest: "v1.3.0"}
	tests := []struct {
		name     string
		running  string
		noGo     bool
		wantErr  string
		wantArgs string
	}{
		{"installs the cached release", "v1.2.0", false, "", "install github.com/waygatetech/tix@v1.3.0\n"},
		{"dev build installs too", "dev", false, "", "install github.com/waygatetech/tix@v1.3.0\n"},
		{"already current", "v1.3.0", false, "already current", ""},
		{"go missing", "v1.2.0", true, "needs the go command on PATH", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "update.json")
			if err := tix.WriteUpdateState(path, cached); err != nil {
				t.Fatal(err)
			}
			argsFile := fakeGo(t)
			if tt.noGo {
				t.Setenv("PATH", t.TempDir())
			}
			var stdout, stderr bytes.Buffer
			err := runUpdate(context.Background(), &stdout, &stderr, path, tt.running)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want one containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("runUpdate: %v", err)
			}
			args, err := os.ReadFile(argsFile)
			if err != nil {
				t.Fatalf("go was not run: %v", err)
			}
			if string(args) != tt.wantArgs {
				t.Errorf("go args = %q, want %q", args, tt.wantArgs)
			}
			if stdout.String() != "installed tix v1.3.0\n" {
				t.Errorf("stdout = %q", stdout.String())
			}
		})
	}
}
