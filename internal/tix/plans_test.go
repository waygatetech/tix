package tix

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestIndexPlan(t *testing.T) {
	setupTestDB(t)
	t.Setenv("HOME", t.TempDir())
	repo := t.TempDir()
	if _, err := CreateProject(Project{Name: "frontend", Key: "fe", RepoPath: repo}); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	id64, err := Add("plan me", "", 1, "frontend", nil)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	id := int(id64)
	ref := FormatID("fe", id)
	if err := os.MkdirAll(filepath.Join(repo, "plans"), 0o755); err != nil {
		t.Fatal(err)
	}
	defaultPath := filepath.Join(repo, "plans", ref+".md")

	tests := []struct {
		name    string
		front   string
		wantErr string
	}{
		{"no frontmatter", "", "no frontmatter"},
		{"unclosed", "---\nticket: " + ref + "\n", "not closed"},
		{"unknown field", "---\nticket: " + ref + "\nconcept: [x]\n---\n", "concept"},
		{"wrong ticket", "---\nticket: fe-99999\n---\n", "plan is for ticket"},
		{"bare ticket", "---\nticket: \"" + strings.TrimPrefix(ref, "fe-") + "\"\n---\n", "plan is for ticket"},
		{"valid", "---\nticket: " + ref + "\nconcepts: [auth]\npaths: [cmd/*.go]\ndecisions:\n  - q: which?\n    options: [a, b]\n    recommend: a\n    why: simpler\n---\nbody\n", ""},
		{"reindex replaces", "---\nticket: " + ref + "\nconcepts: [billing]\n---\n", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.WriteFile(defaultPath, []byte(tc.front+"body\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			_, err := IndexPlan(context.Background(), id, "")
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("IndexPlan() error = %v, want containing %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("IndexPlan(): %v", err)
			}
		})
	}

	ticket, err := Show(id)
	if err != nil {
		t.Fatalf("Show: %v", err)
	}
	if ticket.Plan == nil || ticket.Plan.Path != defaultPath || !reflect.DeepEqual(ticket.Plan.Concepts, []string{"billing"}) || ticket.Plan.Decisions != nil {
		t.Fatalf("Show().Plan = %#v, want reindexed plan at %s", ticket.Plan, defaultPath)
	}
}

func TestPlanHookReview(t *testing.T) {
	setupTestDB(t)
	t.Setenv("HOME", t.TempDir())
	repo := t.TempDir()
	if _, err := CreateProject(Project{Name: "frontend", Key: "fe", RepoPath: repo}); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	id64, err := Add("plan me", "", 1, "frontend", nil)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	id := int(id64)
	for _, dir := range []string{"plans", ".tix"} {
		if err := os.MkdirAll(filepath.Join(repo, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	front := "---\nticket: " + FormatID("fe", id) + "\n---\n"
	if err := os.WriteFile(filepath.Join(repo, "plans", FormatID("fe", id)+".md"), []byte(front), 0o644); err != nil {
		t.Fatal(err)
	}
	hook := filepath.Join(repo, ".tix", "plan-hook")

	// Steps run in order: each builds on the review state the last one left.
	steps := []struct {
		name      string
		output    string // hook stdout; "" means run tix approve instead
		wantErr   string
		wantTier  int
		wantState string
	}{
		{"tier 1 awaits review", `{"tier":1,"reason":"decisions"}`, "", 1, ReviewAwaiting},
		{"approve", "", "", 1, ReviewApproved},
		{"same tier keeps approval", `{"tier":1}`, "", 1, ReviewApproved},
		{"rising tier resets approval", `{"tier":2}`, "", 2, ReviewAwaiting},
		{"malformed hook output", `nope`, "parsing plan hook", 2, ReviewAwaiting},
		{"out of range tier", `{"tier":3}`, "tier from 0 to 2", 2, ReviewAwaiting},
		{"tier 0 proceeds", `{"tier":0}`, "", 0, ""},
		{"approve with nothing awaiting", "", "no plan awaiting review", 0, ""},
	}
	for _, step := range steps {
		t.Run(step.name, func(t *testing.T) {
			if step.output == "" {
				_, err = Approve(id)
			} else {
				script := "#!/bin/sh\ncat >/dev/null\necho '" + step.output + "'\n"
				if err := os.WriteFile(hook, []byte(script), 0o755); err != nil {
					t.Fatal(err)
				}
				_, err = IndexPlan(context.Background(), id, "")
			}
			if step.wantErr == "" && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if step.wantErr != "" && (err == nil || !strings.Contains(err.Error(), step.wantErr)) {
				t.Fatalf("error = %v, want containing %q", err, step.wantErr)
			}
			ticket, err := Show(id)
			if err != nil {
				t.Fatalf("Show: %v", err)
			}
			if got := ticket.Plan.Review; got == nil || got.Tier != step.wantTier || got.State != step.wantState {
				t.Fatalf("Review = %#v, want tier %d state %q", got, step.wantTier, step.wantState)
			}
		})
	}
}
