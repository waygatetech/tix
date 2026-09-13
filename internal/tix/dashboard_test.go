package tix

import (
	"reflect"
	"testing"

	_ "modernc.org/sqlite"
)

func TestBuildDashboardGroupsEveryProject(t *testing.T) {
	setupTestDB(t)
	newProjects(t, "acme", "zen")

	if _, err := Add("fix login", "cannot log in", 0, "acme", nil); err != nil {
		t.Fatalf("Add: %v", err)
	}
	shipped, err := Add("ship docs", "", 2, "acme", nil)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if _, err := Add("tune cache", "", 1, "zen", nil); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := Done(int(shipped)); err != nil {
		t.Fatalf("Done: %v", err)
	}

	board, err := BuildDashboard()
	if err != nil {
		t.Fatalf("BuildDashboard: %v", err)
	}

	if board.Total != 3 {
		t.Fatalf("Total = %d, want 3", board.Total)
	}
	want := map[string]int{"open": 2, "done": 1}
	if !reflect.DeepEqual(board.StatusCounts, want) {
		t.Fatalf("StatusCounts = %v, want %v", board.StatusCounts, want)
	}
	if got := []string{board.Projects[0].Project, board.Projects[1].Project}; !reflect.DeepEqual(got, []string{"acme", "zen"}) {
		t.Fatalf("projects = %v, want [acme zen]", got)
	}

	acme := board.Projects[0]
	if acme.Total != 2 || acme.Open() != 1 || acme.Done() != 1 {
		t.Fatalf("acme counts = total %d open %d done %d, want 2/1/1", acme.Total, acme.Open(), acme.Done())
	}
	if acme.Percent() != 50 {
		t.Fatalf("acme Percent() = %d, want 50", acme.Percent())
	}
	if len(acme.Tickets) != 2 {
		t.Fatalf("acme tickets = %d, want 2", len(acme.Tickets))
	}
}

// A ticket's reference is what a reader copies off the page into a command, so
// it has to be the prefixed form the CLI prints rather than the raw row ID.
func TestBuildDashboardUsesPrefixedRefs(t *testing.T) {
	setupTestDB(t)
	newProjects(t, "acme")

	id, err := Add("fix login", "", 2, "acme", nil)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := SetPrefix("acme", "ac"); err != nil {
		t.Fatalf("SetPrefix: %v", err)
	}

	board, err := BuildDashboard()
	if err != nil {
		t.Fatalf("BuildDashboard: %v", err)
	}

	want := FormatID("ac", int(id))
	if got := board.Projects[0].Tickets[0].Ref; got != want {
		t.Fatalf("Ref = %q, want %q", got, want)
	}
	if board.Projects[0].Key != "ac" {
		t.Fatalf("Key = %q, want %q", board.Projects[0].Key, "ac")
	}
}

// A project created ahead of its first ticket is still part of the cross-repo
// picture: "nothing filed here yet" is information.
func TestBuildDashboardKeepsEmptyProjects(t *testing.T) {
	setupTestDB(t)
	newProjects(t, "acme", "empty")

	if _, err := Add("fix login", "", 2, "acme", nil); err != nil {
		t.Fatalf("Add: %v", err)
	}

	board, err := BuildDashboard()
	if err != nil {
		t.Fatalf("BuildDashboard: %v", err)
	}

	if len(board.Projects) != 2 {
		t.Fatalf("projects = %d, want 2", len(board.Projects))
	}
	blank := board.Projects[1]
	if blank.Project != "empty" || blank.Total != 0 || len(blank.Tickets) != 0 {
		t.Fatalf("empty project = %+v, want a zero-ticket entry", blank)
	}
	if blank.Percent() != 0 {
		t.Fatalf("Percent() = %d, want 0 for an empty project", blank.Percent())
	}
}

// Done tickets sink to the bottom of their project so showing them never pushes
// live work down the page.
func TestBuildDashboardSinksDoneTickets(t *testing.T) {
	setupTestDB(t)
	newProjects(t, "acme")

	first, err := Add("finish first", "", 0, "acme", nil)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if _, err := Add("still open", "", 1, "acme", nil); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if _, err := Add("also open", "", 2, "acme", nil); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := Done(int(first)); err != nil {
		t.Fatalf("Done: %v", err)
	}

	board, err := BuildDashboard()
	if err != nil {
		t.Fatalf("BuildDashboard: %v", err)
	}

	var titles []string
	for _, ticket := range board.Projects[0].Tickets {
		titles = append(titles, ticket.Title)
	}
	if !reflect.DeepEqual(titles, []string{"still open", "also open", "finish first"}) {
		t.Fatalf("ticket order = %v, want open tickets in priority order then the done one", titles)
	}
}

func TestBuildDashboardListsPresentStatusesAndPriorities(t *testing.T) {
	setupTestDB(t)
	newProjects(t, "acme")

	if _, err := Add("claim me", "", 1, "acme", nil); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if _, err := Add("leave me", "", 3, "acme", nil); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if _, err := ClaimNext("acme"); err != nil {
		t.Fatalf("ClaimNext: %v", err)
	}

	board, err := BuildDashboard()
	if err != nil {
		t.Fatalf("BuildDashboard: %v", err)
	}

	// Statuses come back in workflow order, not alphabetical, and only the ones
	// actually present are offered as filters.
	if !reflect.DeepEqual(board.Statuses, []string{"open", "in_progress"}) {
		t.Fatalf("Statuses = %v, want [open in_progress]", board.Statuses)
	}
	if !reflect.DeepEqual(board.Priorities, []int{1, 3}) {
		t.Fatalf("Priorities = %v, want [1 3]", board.Priorities)
	}
}

func TestBuildDashboardOnEmptyDatabase(t *testing.T) {
	setupTestDB(t)

	board, err := BuildDashboard()
	if err != nil {
		t.Fatalf("BuildDashboard: %v", err)
	}

	if board.Total != 0 || len(board.Projects) != 0 {
		t.Fatalf("board = %+v, want no projects and no tickets", board)
	}
	if len(board.Statuses) != 0 || len(board.Priorities) != 0 {
		t.Fatalf("filters = %v / %v, want both empty", board.Statuses, board.Priorities)
	}
}

func TestSortStatusesPutsUnknownStatusesLast(t *testing.T) {
	got := sortStatuses(map[string]bool{"done": true, "blocked": true, "open": true, "archived": true})
	if !reflect.DeepEqual(got, []string{"open", "done", "archived", "blocked"}) {
		t.Fatalf("sortStatuses = %v, want [open done archived blocked]", got)
	}
}
