package tix

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAuditRecordsReverts(t *testing.T) {
	tests := []struct {
		name         string
		commitBefore bool // commit the change before tix done, else after
		revert       bool
		wantReverted bool
	}{
		{"commit before done, reverted", true, true, true},
		{"commit after done, reverted", false, true, true},
		{"commit after done, not reverted", false, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setupTestDB(t)
			t.Setenv("HOME", t.TempDir())
			ctx := context.Background()
			repo := t.TempDir()
			run := func(args ...string) string {
				t.Helper()
				out, err := git(ctx, repo, args...)
				if err != nil {
					t.Fatal(err)
				}
				return strings.TrimSpace(out)
			}
			run("init", "-q", "-b", "main")
			run("config", "user.email", "t@example.com")
			run("config", "user.name", "t")
			run("commit", "-q", "--allow-empty", "-m", "base")
			if _, err := CreateProject(Project{Name: "acme", Key: "ac", RepoPath: repo}); err != nil {
				t.Fatalf("CreateProject: %v", err)
			}
			id64, err := Add("ship it", "", 1, "acme", nil)
			if err != nil {
				t.Fatalf("Add: %v", err)
			}
			id := int(id64)

			if err := os.WriteFile(filepath.Join(repo, "f"), []byte("x"), 0o644); err != nil {
				t.Fatal(err)
			}
			commit := func() string {
				run("add", "f")
				run("commit", "-q", "-m", "ticket work")
				return run("rev-parse", "HEAD")
			}
			var sha string
			if tt.commitBefore {
				sha = commit()
			}
			if err := RunDone(ctx, id, ""); err != nil {
				t.Fatalf("RunDone: %v", err)
			}
			if !tt.commitBefore {
				sha = commit()
			}
			if tt.revert {
				run("revert", "--no-edit", sha)
			}

			for pass := 1; pass <= 2; pass++ {
				reverts, err := Audit(ctx, "acme", DefaultAuditDays)
				if err != nil {
					t.Fatalf("Audit pass %d: %v", pass, err)
				}
				want := 0
				if tt.wantReverted && pass == 1 {
					want = 1
				}
				if len(reverts) != want {
					t.Fatalf("Audit pass %d recorded %v, want %d", pass, reverts, want)
				}
				if want == 1 && (reverts[0].TicketID != id || reverts[0].SHA != sha || reverts[0].RevertSHA != run("rev-parse", "HEAD")) {
					t.Fatalf("Audit recorded %+v, want ticket %d commit %s reverted by HEAD", reverts[0], id, sha)
				}
			}

			events, err := Events(id)
			if err != nil {
				t.Fatalf("Events: %v", err)
			}
			var reverted int
			for _, e := range events {
				switch e.Event {
				case "done":
					var c doneCommit
					if err := json.Unmarshal(e.Data, &c); err != nil || c.SHA == "" || c.Branch != "main" || c.Dirty == tt.commitBefore {
						t.Fatalf("done data = %s, want HEAD on main with dirty=%v", e.Data, !tt.commitBefore)
					}
				case "reverted":
					reverted++
				}
			}
			if (reverted == 1) != tt.wantReverted || reverted > 1 {
				t.Fatalf("%d reverted events, want reverted=%v once", reverted, tt.wantReverted)
			}
		})
	}
}

func TestAuditRejectsBadInput(t *testing.T) {
	setupTestDB(t)
	if _, err := Audit(context.Background(), "", 0); err == nil {
		t.Error("Audit with 0 days succeeded, want an error")
	}
	if _, err := Audit(context.Background(), "nope", 14); err == nil {
		t.Error("Audit on an unknown project succeeded, want an error")
	}
}
