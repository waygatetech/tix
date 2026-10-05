package tix

import (
	"context"
	"errors"
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

func TestPlanLeases(t *testing.T) {
	setupTestDB(t)
	t.Setenv("HOME", t.TempDir())
	repos := map[string]string{"frontend": t.TempDir(), "backend": t.TempDir()}
	keys := map[string]string{"frontend": "fe", "backend": "be"}
	for name, repo := range repos {
		if _, err := CreateProject(Project{Name: name, Key: keys[name], RepoPath: repo}); err != nil {
			t.Fatalf("CreateProject: %v", err)
		}
		if err := os.MkdirAll(filepath.Join(repo, "plans"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	add := func(project, front string) int {
		id64, err := Add("lease me", "", 1, project, nil)
		if err != nil {
			t.Fatalf("Add: %v", err)
		}
		id := int(id64)
		ref := FormatID(keys[project], id)
		plan := "---\nticket: " + ref + "\n" + front + "---\n"
		if err := os.WriteFile(filepath.Join(repos[project], "plans", ref+".md"), []byte(plan), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`UPDATE tickets SET status = 'in_progress' WHERE id = ?`, id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	holder := add("frontend", "concepts: [auth]\n")
	rival := add("frontend", "new_concepts: [auth, billing]\n")
	other := add("backend", "concepts: [auth]\n")

	// Steps run in order; setup is SQL run against the holder first.
	steps := []struct {
		name    string
		setup   string
		id      int
		wantErr string
	}{
		{"holder acquires", "", holder, ""},
		{"holder replans its own lease", "", holder, ""},
		{"overlap is refused", "", rival, FormatID("fe", holder)},
		{"other project is independent", "", other, ""},
		{"reopening releases", `UPDATE tickets SET status = 'open' WHERE id = ?`, rival, ""},
		{"rival now holds it", `UPDATE tickets SET status = 'in_progress' WHERE id = ?`, holder, FormatID("fe", rival)},
		{"done releases", `UPDATE tickets SET status = 'done' WHERE id = ?`, rival, ""},
	}
	for _, step := range steps {
		t.Run(step.name, func(t *testing.T) {
			if step.setup != "" {
				if _, err := db.Exec(step.setup, holder); err != nil {
					t.Fatal(err)
				}
			}
			_, err := IndexPlan(context.Background(), step.id, "")
			if step.wantErr == "" && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if step.wantErr != "" && (err == nil || !strings.Contains(err.Error(), step.wantErr) || !strings.Contains(err.Error(), `"frontend"`)) {
				t.Fatalf("error = %v, want naming %s in project frontend", err, step.wantErr)
			}
		})
	}

	leases, err := Leases("frontend")
	if err != nil {
		t.Fatalf("Leases: %v", err)
	}
	want := []Lease{{"frontend", "auth", rival}, {"frontend", "billing", rival}}
	if !reflect.DeepEqual(leases, want) {
		t.Fatalf("Leases() = %v, want %v", leases, want)
	}
	// A reopened ticket keeps its cached plan; claim-next must pass it over
	// while another ticket leases its concepts, then claim it once released.
	if _, err := db.Exec(`UPDATE tickets SET status = 'open' WHERE id = ?`, holder); err != nil {
		t.Fatal(err)
	}
	plain64, err := Add("no plan", "", 2, "frontend", nil)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	for _, claim := range []struct {
		name  string
		setup string // SQL run against the rival first
		want  int
	}{
		{"skips overlapping plan", "", int(plain64)},
		{"claims once released", `UPDATE tickets SET status = 'done' WHERE id = ?`, holder},
	} {
		t.Run(claim.name, func(t *testing.T) {
			if claim.setup != "" {
				if _, err := db.Exec(claim.setup, rival); err != nil {
					t.Fatal(err)
				}
			}
			got, err := ClaimNext("frontend")
			if err != nil || got.ID != claim.want {
				t.Fatalf("ClaimNext() = %d, %v; want %d", got.ID, err, claim.want)
			}
		})
	}
	if _, err := db.Exec(`UPDATE tickets SET status = 'in_progress' WHERE id = ?`, rival); err != nil {
		t.Fatal(err)
	}

	stale, err := StaleTickets(StaleOptions{Project: "frontend"})
	if err != nil {
		t.Fatalf("StaleTickets: %v", err)
	}
	for _, ticket := range stale {
		if ticket.ID == rival && !reflect.DeepEqual(ticket.Leases, []string{"auth", "billing"}) {
			t.Fatalf("stale rival Leases = %v", ticket.Leases)
		}
	}
}

func TestPlanConceptRegistry(t *testing.T) {
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
	planPath := filepath.Join(repo, "plan.md")
	index := func(front string) error {
		if err := os.WriteFile(planPath, []byte("---\nticket: "+ref+"\n"+front+"---\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		_, err := IndexPlan(context.Background(), id, planPath)
		return err
	}

	if err := index("concepts: [anything]\n"); err != nil {
		t.Fatalf("without a registry, IndexPlan() = %v, want skipped validation", err)
	}
	if _, err := Concepts("frontend"); !errors.Is(err, ErrNoRegistry) {
		t.Fatalf("Concepts() without registry = %v, want ErrNoRegistry", err)
	}

	registry := "auth:\n  owner: team-a\n  module: internal/auth\nbilling:\n"
	if err := os.WriteFile(filepath.Join(repo, conceptsFile), []byte(registry), 0o644); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name    string
		front   string
		wantErr string
	}{
		{"registered", "concepts: [auth, billing]\n", ""},
		{"unregistered", "concepts: [auth, authn]\n", "concepts not in concepts.yaml: authn"},
		{"declared new", "concepts: [auth]\nnew_concepts: [authn]\n", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := index(tc.front)
			if tc.wantErr == "" && err != nil {
				t.Fatalf("IndexPlan(): %v", err)
			}
			if tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)) {
				t.Fatalf("IndexPlan() error = %v, want containing %q", err, tc.wantErr)
			}
		})
	}

	if _, err := db.Exec(`UPDATE tickets SET status = 'in_progress' WHERE id = ?`, id); err != nil {
		t.Fatal(err)
	}
	got, err := Concepts("frontend")
	if err != nil {
		t.Fatalf("Concepts: %v", err)
	}
	want := []Concept{
		{Name: "auth", Owner: "team-a", Module: "internal/auth", LeasedBy: []int{id}},
		{Name: "billing"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Concepts() = %+v, want %+v", got, want)
	}
}

func TestInitRegistry(t *testing.T) {
	dir := t.TempDir()
	path, err := InitRegistry(dir)
	if err != nil {
		t.Fatalf("InitRegistry: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if concepts, err := parseConcepts(data); err != nil || len(concepts) != 0 {
		t.Fatalf("parseConcepts(template) = %v, %v; want empty", concepts, err)
	}
	if _, err := InitRegistry(dir); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("second InitRegistry() = %v, want already exists", err)
	}
}

func TestListByConcept(t *testing.T) {
	setupTestDB(t)
	if _, err := CreateProject(Project{Name: "acme"}); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	add := func(plan string) int {
		id, err := Add("concept ticket", "", 1, "acme", nil)
		if err != nil {
			t.Fatalf("Add: %v", err)
		}
		if plan != "" {
			if _, err := db.Exec(`UPDATE tickets SET plan = ? WHERE id = ?`, plan, id); err != nil {
				t.Fatal(err)
			}
		}
		return int(id)
	}
	auth := add(`{"concepts":["auth.session"],"new_concepts":["auth.token"]}`)
	billing := add(`{"concepts":["billing"]}`)
	add("")

	cases := []struct {
		name     string
		concepts []string
		want     map[int][]string
	}{
		{"concepts", []string{"auth.session"}, map[int][]string{auth: {"auth.session", "auth.token"}}},
		{"new_concepts", []string{"auth.token"}, map[int][]string{auth: {"auth.session", "auth.token"}}},
		{"any of several", []string{"billing", "auth.session"}, map[int][]string{auth: {"auth.session", "auth.token"}, billing: {"billing"}}},
		{"unknown", []string{"nope"}, map[int][]string{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tickets, err := ListTickets(ListOptions{Project: "acme", Concepts: tc.concepts})
			if err != nil {
				t.Fatalf("ListTickets: %v", err)
			}
			got := map[int][]string{}
			for _, ticket := range tickets {
				got[ticket.ID] = ticket.Concepts
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}
