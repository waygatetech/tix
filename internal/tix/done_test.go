package tix

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestRunDoneRecordsAttempts(t *testing.T) {
	tests := []struct {
		name       string
		hook       string // script body; "" means no hook
		wantStatus string
		wantData   string // last done_attempt's data
	}{
		{"no hook passes", "", "done", `{"result":"pass"}`},
		{"passing hook keeps JSON output", `echo '{"findings":2}'`, "done", `{"result":"pass","output":{"findings":2}}`},
		{"failing hook names its stage", `echo '{"stage":"test","findings":1}'; exit 1`, "in_progress", `{"result":"fail","stage":"test","output":{"stage":"test","findings":1}}`},
		{"plain output falls back to hook name", `echo 'vet: bad'; exit 2`, "in_progress", `{"result":"fail","stage":"done-hook","output":"vet: bad\n"}`},
		{"hook runs in the repo with the ticket ID", `[ -d .tix ] && [ "$1" = "$TICKET" ]`, "done", `{"result":"pass"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setupTestDB(t)
			t.Setenv("HOME", t.TempDir())
			repo := t.TempDir()
			if _, err := CreateProject(Project{Name: "acme", Key: "ac", RepoPath: repo}); err != nil {
				t.Fatalf("CreateProject: %v", err)
			}
			id64, err := Add("ship it", "", 1, "acme", nil)
			if err != nil {
				t.Fatalf("Add: %v", err)
			}
			id := int(id64)
			if _, err := ClaimNext("acme"); err != nil {
				t.Fatalf("ClaimNext: %v", err)
			}
			t.Setenv("TICKET", strconv.Itoa(id))
			if tt.hook != "" {
				if err := os.MkdirAll(filepath.Join(repo, ".tix"), 0o755); err != nil {
					t.Fatal(err)
				}
				script := "#!/bin/sh\n" + tt.hook + "\n"
				if err := os.WriteFile(filepath.Join(repo, ".tix", "done-hook"), []byte(script), 0o755); err != nil {
					t.Fatal(err)
				}
			}

			err = RunDone(context.Background(), id)
			if (err != nil) != (tt.wantStatus != "done") {
				t.Fatalf("RunDone err = %v, want error only when the ticket stays open", err)
			}
			ticket, err := Show(id)
			if err != nil {
				t.Fatalf("Show: %v", err)
			}
			if ticket.Status != tt.wantStatus {
				t.Fatalf("status = %q, want %q", ticket.Status, tt.wantStatus)
			}
			events, err := Events(id)
			if err != nil {
				t.Fatalf("Events: %v", err)
			}
			var got string
			for _, e := range events {
				if e.Event == "done_attempt" {
					got = string(e.Data)
				}
			}
			if !jsonEqual(t, got, tt.wantData) {
				t.Fatalf("done_attempt data = %s, want %s", got, tt.wantData)
			}
		})
	}
}

func TestHookOutputKeepsTruncatedJSONAsString(t *testing.T) {
	cut := `{"findings":` + strings.Repeat("1", doneOutputCap)
	if got := hookOutput([]byte(cut[:doneOutputCap])); got != cut[:doneOutputCap] {
		t.Fatalf("hookOutput kept %T, want the string", got)
	}
	if got := hookOutput([]byte("  \n")); got != nil {
		t.Fatalf("hookOutput(blank) = %v, want nil", got)
	}
}

func jsonEqual(t *testing.T, a, b string) bool {
	t.Helper()
	var x, y any
	if err := json.Unmarshal([]byte(a), &x); err != nil {
		t.Fatalf("parsing %q: %v", a, err)
	}
	if err := json.Unmarshal([]byte(b), &y); err != nil {
		t.Fatalf("parsing %q: %v", b, err)
	}
	xb, _ := json.Marshal(x)
	yb, _ := json.Marshal(y)
	return string(xb) == string(yb)
}
