package tix

import (
	"reflect"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

// The bug this table fixes: four spellings of one project name used to produce
// four projects, each hiding the others' tickets.
func TestVariantSpellingsResolveToOneProject(t *testing.T) {
	setupTestDB(t)
	newProjects(t, "frontend")

	for _, variant := range []string{"frontend", "Frontend", "front-end", "frontend ", "FRONT END"} {
		if _, err := Add("ticket", "body", 2, variant); err != nil {
			t.Fatalf("Add(project=%q): %v", variant, err)
		}
	}

	projects, err := Projects()
	if err != nil {
		t.Fatalf("Projects(): %v", err)
	}
	if len(projects) != 1 {
		t.Fatalf("got %d projects, want 1: %#v", len(projects), projects)
	}
	if projects[0].Project != "frontend" || projects[0].Total != 5 {
		t.Errorf("Projects()[0] = %#v, want frontend with 5 tickets", projects[0])
	}

	// Reads fold the same way, so a variant never shows a partial backlog.
	if got := List("Front-End", false); len(got) != 5 {
		t.Errorf("List(Front-End) returned %d tickets, want 5: %v", len(got), got)
	}
}

// A variant spelling has to display the prefix of the project the ticket
// landed in. Deriving one from the typed name instead produced a prefix that
// pointed at nothing: "acme!" sorts after "acme", so it derived "acm" for a
// ticket that is really ac-1.
func TestPrefixForResolvesVariantSpellings(t *testing.T) {
	setupTestDB(t)
	newProjects(t, "acme")

	want, err := PrefixFor("acme")
	if err != nil {
		t.Fatalf("PrefixFor(acme): %v", err)
	}
	for _, variant := range []string{"acme!", "ACME", "a-c-m-e", " acme "} {
		got, err := PrefixFor(variant)
		if err != nil {
			t.Errorf("PrefixFor(%q): %v", variant, err)
			continue
		}
		if got != want {
			t.Errorf("PrefixFor(%q) = %q, want %q", variant, got, want)
		}
	}
}

func TestUnknownProjectIsRejectedEverywhere(t *testing.T) {
	setupTestDB(t)
	newProjects(t, "acme")

	if _, err := Add("ticket", "body", 2, "acme"); err != nil {
		t.Fatalf("Add: %v", err)
	}

	cases := map[string]error{
		"Add": func() error {
			_, err := Add("ticket", "body", 2, "backend")
			return err
		}(),
		"ListTickets": func() error {
			_, err := ListTickets(ListOptions{Project: "backend"})
			return err
		}(),
		"NextTicket": func() error {
			_, err := NextTicket("backend")
			return err
		}(),
		"ClaimNext": func() error {
			_, err := ClaimNext("backend")
			return err
		}(),
		"Remove": Remove(1, "backend"),
		"Edit": func() error {
			project := "backend"
			_, err := Edit(1, EditOptions{Project: &project})
			return err
		}(),
	}
	for name, err := range cases {
		if err == nil {
			t.Errorf("%s accepted an unknown project, want an error", name)
			continue
		}
		if !strings.Contains(err.Error(), `unknown project "backend"`) {
			t.Errorf("%s error = %q, want it to name the unknown project", name, err)
		}
		if !strings.Contains(err.Error(), "acme") {
			t.Errorf("%s error = %q, want it to list the known projects", name, err)
		}
		if !strings.Contains(err.Error(), "--create") {
			t.Errorf("%s error = %q, want it to say how to create the project", name, err)
		}
	}

	// The ticket must stay where it was, in the project that does exist.
	ticket, err := Show(1)
	if err != nil {
		t.Fatalf("Show(1): %v", err)
	}
	if ticket.Project != "acme" {
		t.Errorf("ticket project = %q, want acme", ticket.Project)
	}
}

func TestUnknownProjectErrorWithNoProjectsYet(t *testing.T) {
	setupTestDB(t)

	_, err := Add("ticket", "body", 2, "acme")
	if err == nil {
		t.Fatal("Add into an empty database succeeded, want an unknown-project error")
	}
	if !strings.Contains(err.Error(), "no projects exist yet") {
		t.Errorf("error = %q, want it to say no projects exist yet", err)
	}
}

// A database that fragmented before this table has both spellings as rows.
// Neither can be folded away silently, so an inexact name has to be refused —
// guessing would list or close the wrong project's tickets.
func TestAlreadyFragmentedProjectsRequireAnExactName(t *testing.T) {
	setupTestDB(t)

	for _, name := range []string{"frontend", "front-end"} {
		if _, err := db.Exec(`INSERT INTO projects (name) VALUES (?)`, name); err != nil {
			t.Fatalf("insert %q: %v", name, err)
		}
	}

	for _, exact := range []string{"frontend", "front-end"} {
		if got, err := ResolveProject(exact); err != nil || got != exact {
			t.Errorf("ResolveProject(%q) = (%q, %v), want it resolved exactly", exact, got, err)
		}
	}

	_, err := ResolveProject("Frontend")
	if err == nil {
		t.Fatal("ResolveProject(Frontend) succeeded, want an ambiguity error")
	}
	if !strings.Contains(err.Error(), "ambiguous") ||
		!strings.Contains(err.Error(), "front-end") ||
		!strings.Contains(err.Error(), "frontend") {
		t.Errorf("error = %q, want it to name both candidates as ambiguous", err)
	}

	// --create must not answer the ambiguity by adding a third spelling.
	if _, _, err := EnsureProject("Frontend"); err == nil {
		t.Error("EnsureProject(Frontend) succeeded, want the ambiguity error")
	}
	if _, err := CreateProject(Project{Name: "Frontend"}); err == nil {
		t.Error("CreateProject(Frontend) succeeded, want the ambiguity error")
	}
	projects, err := ListProjects()
	if err != nil {
		t.Fatalf("ListProjects(): %v", err)
	}
	if len(projects) != 2 {
		t.Errorf("got %d projects, want the original 2: %#v", len(projects), projects)
	}
}

func TestEnsureProjectCreatesOnceAndReuses(t *testing.T) {
	setupTestDB(t)

	name, created, err := EnsureProject("frontend")
	if err != nil {
		t.Fatalf("EnsureProject(frontend): %v", err)
	}
	if name != "frontend" || !created {
		t.Fatalf("EnsureProject(frontend) = (%q, %v), want (frontend, true)", name, created)
	}

	// A variant of a project that already exists reuses it rather than
	// creating the second spelling.
	name, created, err = EnsureProject("Front-End")
	if err != nil {
		t.Fatalf("EnsureProject(Front-End): %v", err)
	}
	if name != "frontend" || created {
		t.Fatalf("EnsureProject(Front-End) = (%q, %v), want (frontend, false)", name, created)
	}

	projects, err := ListProjects()
	if err != nil {
		t.Fatalf("ListProjects(): %v", err)
	}
	if len(projects) != 1 {
		t.Errorf("got %d projects, want 1: %#v", len(projects), projects)
	}
}

func TestCreateProjectRejectsDuplicatesAndEmptyNames(t *testing.T) {
	setupTestDB(t)
	newProjects(t, "frontend")

	if _, err := CreateProject(Project{Name: "frontend"}); err == nil {
		t.Error("CreateProject(frontend) succeeded twice, want an error")
	}
	_, err := CreateProject(Project{Name: "Front-End"})
	if err == nil {
		t.Fatal("CreateProject(Front-End) succeeded, want it refused as a variant")
	}
	if !strings.Contains(err.Error(), `already exists as "frontend"`) {
		t.Errorf("error = %q, want it to name the existing project", err)
	}

	for _, bad := range []string{"", "   ", "!!!"} {
		if _, err := CreateProject(Project{Name: bad}); err == nil {
			t.Errorf("CreateProject(%q) succeeded, want an error", bad)
		}
	}
}

func TestCreateProjectStoresKeyAndRepoPath(t *testing.T) {
	setupTestDB(t)

	project, err := CreateProject(Project{Name: "frontend", Key: "fe", RepoPath: "/src/frontend"})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if !reflect.DeepEqual(project, Project{Name: "frontend", Key: "fe", RepoPath: "/src/frontend"}) {
		t.Fatalf("CreateProject = %#v", project)
	}

	// A pinned key is the ticket ID prefix, so ids render with it right away.
	if got, err := PrefixFor("frontend"); err != nil || got != "fe" {
		t.Fatalf("PrefixFor(frontend) = (%q, %v), want (fe, nil)", got, err)
	}

	// A second project cannot take a key another one pinned.
	if _, err := CreateProject(Project{Name: "features", Key: "fe"}); err == nil {
		t.Error("CreateProject reused a pinned key, want an error")
	}
	if _, err := CreateProject(Project{Name: "backend", Key: "b e"}); err == nil {
		t.Error("CreateProject accepted a non-alphanumeric key, want an error")
	}
}

func TestSetRepoPathRecordsWhereCodeLives(t *testing.T) {
	setupTestDB(t)
	newProjects(t, "frontend")

	if _, err := SetRepoPath("Front-End", "/src/frontend"); err != nil {
		t.Fatalf("SetRepoPath: %v", err)
	}
	projects, err := ListProjects()
	if err != nil {
		t.Fatalf("ListProjects(): %v", err)
	}
	if len(projects) != 1 || projects[0].RepoPath != "/src/frontend" {
		t.Fatalf("ListProjects() = %#v, want frontend at /src/frontend", projects)
	}

	if _, err := SetRepoPath("nope", "/src/nope"); err == nil {
		t.Error("SetRepoPath on an unknown project succeeded, want an error")
	}
}

func TestProjectsIncludesProjectWithoutTickets(t *testing.T) {
	setupTestDB(t)
	newProjects(t, "empty")

	projects, err := Projects()
	if err != nil {
		t.Fatalf("Projects(): %v", err)
	}
	want := []ProjectSummary{{Project: "empty", Key: "em", StatusCounts: map[string]int{}}}
	if !reflect.DeepEqual(projects, want) {
		t.Fatalf("Projects() = %#v, want %#v", projects, want)
	}
}

// A database written before the projects table keeps working: its project names
// are backfilled as rows, so the tickets stay reachable and no name that was
// already in use is suddenly unknown.
func TestMigrationBackfillsProjectsAndPinnedPrefixes(t *testing.T) {
	setupTestDB(t)

	if _, err := db.Exec(`DROP TABLE projects`); err != nil {
		t.Fatalf("drop projects: %v", err)
	}
	if _, err := db.Exec(`CREATE TABLE project_prefixes (project TEXT PRIMARY KEY, prefix TEXT NOT NULL UNIQUE)`); err != nil {
		t.Fatalf("recreate project_prefixes: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO tickets (title, body, priority, order_rank, project) VALUES ('legacy', '', 2, 1, 'acme')`,
	); err != nil {
		t.Fatalf("insert legacy ticket: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO project_prefixes (project, prefix) VALUES ('acme', 'acm'), ('unused', 'un')`); err != nil {
		t.Fatalf("insert legacy prefixes: %v", err)
	}
	if _, err := db.Exec(`PRAGMA user_version = 2`); err != nil {
		t.Fatalf("reset user_version: %v", err)
	}

	if err := migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	projects, err := ListProjects()
	if err != nil {
		t.Fatalf("ListProjects(): %v", err)
	}
	want := []Project{{Name: "acme", Key: "acm"}, {Name: "unused", Key: "un"}}
	if !reflect.DeepEqual(projects, want) {
		t.Fatalf("ListProjects() = %#v, want %#v", projects, want)
	}
	if got := List("acme", false); len(got) != 1 || !strings.HasPrefix(got[0], "acm-") {
		t.Errorf("List(acme) = %v, want one ticket prefixed acm-", got)
	}
}
