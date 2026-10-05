package tix

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestIndexPlan(t *testing.T) {
	setupTestDB(t)
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
			_, err := IndexPlan(id, "")
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
