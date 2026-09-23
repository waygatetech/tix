package tix

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func setupTestDB(t *testing.T) {
	t.Helper()
	t.Setenv("TIX_JEV_MODE", "off")

	if db != nil {
		_ = db.Close()
	}

	t.Setenv("TIX_DB", t.TempDir()+"/tix.db")
	if err := InitDB(); err != nil {
		t.Fatalf("InitDB: %v", err)
	}

	t.Cleanup(func() {
		if db != nil {
			_ = db.Close()
			db = nil
		}
	})
}

// newProjects creates the projects a test's tickets belong to, which every
// ticket now needs: an unknown --project is an error, not a new project.
func newProjects(t *testing.T, names ...string) {
	t.Helper()

	for _, name := range names {
		if _, err := CreateProject(Project{Name: name}); err != nil {
			t.Fatalf("CreateProject(%q): %v", name, err)
		}
	}
}

// listLines renders a project's tickets the way the list command does, so tests
// that care about ordering and prefixes can assert on one line per ticket.
func listLines(t *testing.T, project string, showAll bool) []string {
	t.Helper()

	tickets, err := ListTickets(ListOptions{Project: project, ShowAll: showAll})
	if err != nil {
		t.Fatalf("ListTickets(%q, showAll=%v): %v", project, showAll, err)
	}
	prefixes, err := Prefixes()
	if err != nil {
		t.Fatalf("Prefixes(): %v", err)
	}

	var lines []string
	for _, ticket := range tickets {
		marker := ""
		if ticket.Status == "done" {
			marker = "x "
		}
		lines = append(lines, fmt.Sprintf("%s%s [P%d] %s",
			marker, FormatID(prefixes[ticket.Project], ticket.ID), ticket.Priority, ticket.Title))
	}
	return lines
}

// stripTimestamps checks that a read populated the timestamps and then clears
// them, so a test can compare the rest against a literal it can actually
// predict.
func stripTimestamps(t *testing.T, tickets []Ticket) []Ticket {
	t.Helper()

	stripped := make([]Ticket, len(tickets))
	for i, ticket := range tickets {
		if ticket.CreatedAt.IsZero() || ticket.UpdatedAt.IsZero() {
			t.Errorf("ticket %d timestamps = (%v, %v), want both set", ticket.ID, ticket.CreatedAt, ticket.UpdatedAt)
		}
		ticket.CreatedAt = time.Time{}
		ticket.UpdatedAt = time.Time{}
		ticket.Order = 0
		stripped[i] = ticket
	}
	return stripped
}

// backdate moves a ticket's last write into the past, which is the only way to
// test staleness without a test that sleeps for a day.
func backdate(t *testing.T, id int, age time.Duration) {
	t.Helper()

	when := time.Now().UTC().Add(-age).Format(sqlTimeLayout)
	if _, err := db.Exec(`UPDATE tickets SET updated_at = ? WHERE id = ?`, when, id); err != nil {
		t.Fatalf("backdate ticket %d: %v", id, err)
	}
}

func TestDBPathUsesTIXDBOverride(t *testing.T) {
	t.Setenv("TIX_DB", "/tmp/custom-tix.db")

	if got := dbPath(); got != "/tmp/custom-tix.db" {
		t.Fatalf("dbPath() = %q, want %q", got, "/tmp/custom-tix.db")
	}
}

func TestAddListNextAndProjectIsolation(t *testing.T) {
	setupTestDB(t)
	newProjects(t, "acme", "zen")

	firstID, err := Add("fix login bug", "users cannot log in", 2, "acme", nil)
	if err != nil {
		t.Fatalf("Add first ticket: %v", err)
	}
	secondID, err := Add("write docs", "README", 0, "acme", nil)
	if err != nil {
		t.Fatalf("Add second ticket: %v", err)
	}
	otherID, err := Add("refactor parser", "split tokens", 2, "zen", nil)
	if err != nil {
		t.Fatalf("Add other project ticket: %v", err)
	}

	if firstID != 1 || secondID != 2 || otherID != 3 {
		t.Fatalf("unexpected IDs: got %d, %d, %d", firstID, secondID, otherID)
	}

	got := listLines(t, "acme", false)
	want := []string{
		"ac-2 [P0] write docs",
		"ac-1 [P2] fix login bug",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("listLines(acme, false) = %#v, want %#v", got, want)
	}

	next, err := NextTicket("acme")
	if err != nil {
		t.Fatalf("NextTicket(acme): %v", err)
	}
	if next.ID != 2 || next.Title != "write docs" || next.Body != "README" || next.Priority != 0 {
		t.Fatalf("NextTicket(acme) = (%d, %q, %q, %d), want (2, %q, %q, 0)",
			next.ID, next.Title, next.Body, next.Priority, "write docs", "README")
	}

	got = listLines(t, "zen", false)
	want = []string{"ze-3 [P2] refactor parser"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("listLines(zen, false) = %#v, want %#v", got, want)
	}
}

func TestDoneFiltersOpenListAndNext(t *testing.T) {
	setupTestDB(t)
	newProjects(t, "acme")

	firstID, err := Add("first", "body one", 1, "acme", nil)
	if err != nil {
		t.Fatalf("Add first ticket: %v", err)
	}
	secondID, err := Add("second", "body two", 2, "acme", nil)
	if err != nil {
		t.Fatalf("Add second ticket: %v", err)
	}

	if err := Done(int(firstID)); err != nil {
		t.Fatalf("Done(%d): %v", firstID, err)
	}

	got := listLines(t, "acme", false)
	want := []string{"ac-2 [P2] second"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("listLines(acme, false) after Done = %#v, want %#v", got, want)
	}

	got = listLines(t, "acme", true)
	want = []string{"x ac-1 [P1] first", "ac-2 [P2] second"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("listLines(acme, true) after Done = %#v, want %#v", got, want)
	}

	next, err := NextTicket("acme")
	if err != nil {
		t.Fatalf("NextTicket(acme): %v", err)
	}
	if next.ID != int(secondID) || next.Title != "second" {
		t.Fatalf("NextTicket(acme) = (%d, %q, ...), want (%d, %q, ...)", next.ID, next.Title, secondID, "second")
	}

	if err := Done(int(secondID)); err != nil {
		t.Fatalf("Done(%d): %v", secondID, err)
	}
	_, err = NextTicket("acme")
	if err == nil || err.Error() != "no open tickets" {
		t.Fatalf("NextTicket(acme) after all done error = %v, want no open tickets", err)
	}
}

func TestClaimNextDescendsIntoEpicAndDoneClosesIt(t *testing.T) {
	setupTestDB(t)
	newProjects(t, "acme")

	add := func(title string, priority int, parent *int) int {
		t.Helper()
		id, err := Add(title, "", priority, "acme", parent)
		if err != nil {
			t.Fatalf("Add(%q): %v", title, err)
		}
		return int(id)
	}
	epic := add("epic", 1, nil)
	loose := add("loose", 2, nil)
	child1 := add("child one", 3, &epic)
	child2 := add("child two", 3, &epic)

	// The epic outranks the loose ticket, so its lower-priority children do too.
	for _, want := range []int{child1, child2, loose} {
		got, err := ClaimNext("acme")
		if err != nil {
			t.Fatalf("ClaimNext: %v", err)
		}
		if got.ID != want {
			t.Fatalf("ClaimNext = %d (%s), want %d", got.ID, got.Title, want)
		}
	}
	if _, err := ClaimNext("acme"); err == nil || err.Error() != "no open tickets" {
		t.Fatalf("ClaimNext with only the epic left = %v, want no open tickets", err)
	}

	if err := Done(child1); err != nil {
		t.Fatalf("Done(child1): %v", err)
	}
	if ticket, _ := Show(epic); ticket.Status != "open" {
		t.Fatalf("epic status after one child done = %q, want open", ticket.Status)
	}
	if err := Done(child2); err != nil {
		t.Fatalf("Done(child2): %v", err)
	}
	if ticket, _ := Show(epic); ticket.Status != "done" {
		t.Fatalf("epic status after all children done = %q, want done", ticket.Status)
	}
}

func TestListTicketsFilters(t *testing.T) {
	setupTestDB(t)
	newProjects(t, "acme", "zen")

	firstID, err := Add("first", "body", 1, "acme", nil)
	if err != nil {
		t.Fatalf("Add first ticket: %v", err)
	}
	if _, err := Add("second", "body", 2, "acme", nil); err != nil {
		t.Fatalf("Add second ticket: %v", err)
	}
	if _, err := Add("third", "body", 1, "acme", nil); err != nil {
		t.Fatalf("Add third ticket: %v", err)
	}
	if _, err := Add("other project", "body", 1, "zen", nil); err != nil {
		t.Fatalf("Add other project ticket: %v", err)
	}
	if err := Done(int(firstID)); err != nil {
		t.Fatalf("Done(%d): %v", firstID, err)
	}

	priority := 1
	got, err := ListTickets(ListOptions{
		Project:  "acme",
		Status:   "open",
		Priority: &priority,
		Limit:    1,
	})
	if err != nil {
		t.Fatalf("ListTickets filtered: %v", err)
	}
	want := []Ticket{
		{
			ID:       3,
			Title:    "third",
			Body:     "body",
			Status:   "open",
			Priority: 1,
			Project:  "acme",
		},
	}
	if got := stripTimestamps(t, got); !reflect.DeepEqual(got, want) {
		t.Fatalf("ListTickets filtered = %#v, want %#v", got, want)
	}
}

// TestListTicketsQueryMatchesTitleAndBody covers the search filter: what it
// looks in, what it ignores, and that it composes with the other filters rather
// than replacing them.
func TestListTicketsQueryMatchesTitleAndBody(t *testing.T) {
	setupTestDB(t)
	newProjects(t, "acme", "zen")

	for _, ticket := range []struct {
		title, body string
		project     string
	}{
		{"fix search timeout", "the query never returns", "acme"},
		{"add tests", "cover the SEARCH path", "acme"},
		{"unrelated", "nothing to see", "acme"},
		{"search elsewhere", "another backlog", "zen"},
	} {
		if _, err := Add(ticket.title, ticket.body, 2, ticket.project, nil); err != nil {
			t.Fatalf("Add(%q): %v", ticket.title, err)
		}
	}

	for _, tc := range []struct {
		name    string
		options ListOptions
		want    []string
	}{
		{
			name:    "matches title and body, ignoring case",
			options: ListOptions{Project: "acme", Query: "search"},
			want:    []string{"fix search timeout", "add tests"},
		},
		{
			name:    "stays inside the project",
			options: ListOptions{Project: "zen", Query: "search"},
			want:    []string{"search elsewhere"},
		},
		{
			name:    "composes with the other filters",
			options: ListOptions{Project: "acme", Query: "search", Limit: 1},
			want:    []string{"fix search timeout"},
		},
		{
			name:    "an empty query filters nothing",
			options: ListOptions{Project: "acme", Query: "  "},
			want:    []string{"fix search timeout", "add tests", "unrelated"},
		},
		{
			// A wildcard is a character to look for, not a pattern: a query of
			// "%" that matched everything would be worse than useless for the
			// duplicate check this exists for.
			name:    "wildcards are matched literally",
			options: ListOptions{Project: "acme", Query: "%"},
			want:    nil,
		},
		{
			name:    "no match returns nothing",
			options: ListOptions{Project: "acme", Query: "deploy"},
			want:    nil,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tickets, err := ListTickets(tc.options)
			if err != nil {
				t.Fatalf("ListTickets: %v", err)
			}
			var got []string
			for _, ticket := range tickets {
				got = append(got, ticket.Title)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("titles = %#v, want %#v", got, tc.want)
			}
		})
	}
}

// TestListTicketsQuerySeesDoneTickets keeps the status filter in charge of which
// statuses a search covers: a caller asking for every status gets the done
// ticket too, which is the one that says the work already happened.
func TestListTicketsQuerySeesDoneTickets(t *testing.T) {
	setupTestDB(t)
	newProjects(t, "acme")

	id, err := Add("fix search timeout", "body", 2, "acme", nil)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := Done(int(id)); err != nil {
		t.Fatalf("Done(%d): %v", id, err)
	}

	open, err := ListTickets(ListOptions{Project: "acme", Query: "search"})
	if err != nil {
		t.Fatalf("ListTickets: %v", err)
	}
	if len(open) != 0 {
		t.Errorf("default search = %#v, want the done ticket hidden", open)
	}

	all, err := ListTickets(ListOptions{Project: "acme", Query: "search", ShowAll: true})
	if err != nil {
		t.Fatalf("ListTickets showing all: %v", err)
	}
	if len(all) != 1 || all[0].ID != int(id) {
		t.Errorf("search over all statuses = %#v, want ticket %d", all, id)
	}
}

func TestShowReturnsTicketByID(t *testing.T) {
	setupTestDB(t)
	newProjects(t, "acme")

	id, err := Add("show me", "full details", 1, "acme", nil)
	if err != nil {
		t.Fatalf("Add ticket: %v", err)
	}

	got, err := Show(int(id))
	if err != nil {
		t.Fatalf("Show(%d): %v", id, err)
	}
	want := Ticket{
		ID:       int(id),
		Title:    "show me",
		Body:     "full details",
		Status:   "open",
		Priority: 1,
		Project:  "acme",
	}
	if got := stripTimestamps(t, []Ticket{got})[0]; !reflect.DeepEqual(got, want) {
		t.Fatalf("Show(%d) = %#v, want %#v", id, got, want)
	}

	if _, err := Show(99); err == nil || !strings.Contains(err.Error(), "no such ticket: 99") {
		t.Fatalf("Show missing ticket error = %v, want no such ticket: 99", err)
	}
	if _, err := Show(99); err == nil || !strings.Contains(err.Error(), dbPath()) {
		t.Fatalf("Show missing ticket error = %v, want it to name %s", err, dbPath())
	}
}

// The point of updated_at is that a write moves it: a ticket whose last write
// is old is one nothing has touched, which is what staleness means.
func TestWritesMoveUpdatedAt(t *testing.T) {
	for _, tc := range []struct {
		name  string
		write func(id int) error
	}{
		{"Done", func(id int) error { return Done(id) }},
		{"Bump", func(id int) error { _, _, err := Bump(id, 0); return err }},
		{"Edit", func(id int) error {
			title := "retitled"
			_, err := Edit(id, EditOptions{Title: &title})
			return err
		}},
		{"ClaimNext", func(int) error { _, err := ClaimNext("acme"); return err }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// A database per case, so ClaimNext cannot claim a ticket an
			// earlier case left lying around.
			setupTestDB(t)
			newProjects(t, "acme")

			id, err := Add(tc.name, "body", 2, "acme", nil)
			if err != nil {
				t.Fatalf("Add: %v", err)
			}
			backdate(t, int(id), 48*time.Hour)

			before, err := Show(int(id))
			if err != nil {
				t.Fatalf("Show before write: %v", err)
			}
			if err := tc.write(int(id)); err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}

			after, err := Show(int(id))
			if err != nil {
				t.Fatalf("Show after write: %v", err)
			}
			if !after.UpdatedAt.After(before.UpdatedAt) {
				t.Errorf("updated_at = %v after %s, want later than %v", after.UpdatedAt, tc.name, before.UpdatedAt)
			}
			// created_at is the one timestamp a write must not touch, or every
			// ticket looks as though it were filed the moment it was last edited.
			if !after.CreatedAt.Equal(before.CreatedAt) {
				t.Errorf("created_at = %v after %s, want %v", after.CreatedAt, tc.name, before.CreatedAt)
			}
		})
	}
}

func TestEditStoresStructuredBody(t *testing.T) {
	setupTestDB(t)
	newProjects(t, "acme")

	id, err := Add("avatar dropdown", "old body", 2, "acme", nil)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	body := "## Problem\n\n" +
		"The avatar dropdown menu is implemented differently on admin dashboard pages and attendee-facing pages.\n\n" +
		"## Scope\n\n" +
		"Consolidate the avatar dropdown into one shared component used by `templates/layout.html` and `templates/admin/layout.html`.\n"
	ticket, err := Edit(int(id), EditOptions{Body: &body})
	if err != nil {
		t.Fatalf("Edit body: %v", err)
	}
	if ticket.Body != body {
		t.Fatalf("body = %q, want %q", ticket.Body, body)
	}

	shown, err := Show(int(id))
	if err != nil {
		t.Fatalf("Show: %v", err)
	}
	if shown.Body != body {
		t.Fatalf("stored body = %q, want %q", shown.Body, body)
	}
}

// An edit that sets no fields is rejected, and must not count as a write: a
// failed call should not make an abandoned ticket look freshly worked on.
func TestEmptyEditLeavesUpdatedAtAlone(t *testing.T) {
	setupTestDB(t)
	newProjects(t, "acme")

	id, err := Add("untouched", "body", 2, "acme", nil)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	backdate(t, int(id), 48*time.Hour)
	before, err := Show(int(id))
	if err != nil {
		t.Fatalf("Show: %v", err)
	}

	if _, err := Edit(int(id), EditOptions{}); err == nil {
		t.Fatal("Edit with no fields = nil, want an error")
	}

	after, err := Show(int(id))
	if err != nil {
		t.Fatalf("Show: %v", err)
	}
	if !after.UpdatedAt.Equal(before.UpdatedAt) {
		t.Errorf("updated_at = %v after a rejected edit, want %v", after.UpdatedAt, before.UpdatedAt)
	}
}

func TestStaleTicketsFindsAbandonedClaims(t *testing.T) {
	setupTestDB(t)
	newProjects(t, "acme", "zen")

	for _, title := range []string{"abandoned", "very abandoned", "just claimed", "still open"} {
		if _, err := Add(title, "body", 2, "acme", nil); err != nil {
			t.Fatalf("Add(%q): %v", title, err)
		}
	}
	if _, err := Add("other backlog", "body", 2, "zen", nil); err != nil {
		t.Fatalf("Add(zen): %v", err)
	}

	inProgress := "in_progress"
	for id, age := range map[int]time.Duration{
		1: 30 * time.Hour,     // claimed and abandoned
		2: 7 * 24 * time.Hour, // abandoned longer ago
		3: 10 * time.Minute,   // claimed, still being worked on
	} {
		if _, err := Edit(id, EditOptions{Status: &inProgress}); err != nil {
			t.Fatalf("Edit(%d): %v", id, err)
		}
		backdate(t, id, age)
	}
	// An old ticket nobody claimed is a backlog, not a stuck ticket.
	backdate(t, 4, 30*24*time.Hour)
	if _, err := Edit(5, EditOptions{Status: &inProgress}); err != nil {
		t.Fatalf("Edit(5): %v", err)
	}
	backdate(t, 5, 30*time.Hour)

	stale, err := StaleTickets(StaleOptions{Project: "acme", Older: StaleAge})
	if err != nil {
		t.Fatalf("StaleTickets: %v", err)
	}
	// Oldest first, so the ticket most likely abandoned is read first.
	if got := ticketIDs(stale); !reflect.DeepEqual(got, []int{2, 1}) {
		t.Fatalf("StaleTickets(acme) = %v, want [2 1]", got)
	}

	// The threshold is a knob: narrow it and the ticket claimed minutes ago
	// shows up too. Zero asks for no threshold at all, which is the same set
	// here rather than the default quietly coming back.
	for _, older := range []time.Duration{time.Minute, 0} {
		stale, err = StaleTickets(StaleOptions{Project: "acme", Older: older})
		if err != nil {
			t.Fatalf("StaleTickets(older=%v): %v", older, err)
		}
		if got := ticketIDs(stale); !reflect.DeepEqual(got, []int{2, 1, 3}) {
			t.Fatalf("StaleTickets(acme, %v) = %v, want [2 1 3]", older, got)
		}
	}

	// So is the status: an untouched open ticket is findable on request.
	stale, err = StaleTickets(StaleOptions{Project: "acme", Status: "open", Older: StaleAge})
	if err != nil {
		t.Fatalf("StaleTickets with a status: %v", err)
	}
	if got := ticketIDs(stale); !reflect.DeepEqual(got, []int{4}) {
		t.Fatalf("StaleTickets(acme, open) = %v, want [4]", got)
	}

	stale, err = StaleTickets(StaleOptions{Project: "acme", Older: StaleAge, Limit: 1})
	if err != nil {
		t.Fatalf("StaleTickets with a limit: %v", err)
	}
	if got := ticketIDs(stale); !reflect.DeepEqual(got, []int{2}) {
		t.Fatalf("StaleTickets(acme, limit 1) = %v, want [2]", got)
	}

	if _, err := StaleTickets(StaleOptions{Project: "acme", Limit: -1}); err == nil {
		t.Error("StaleTickets with a negative limit = nil, want an error")
	}
	if _, err := StaleTickets(StaleOptions{Project: "nope"}); err == nil {
		t.Error("StaleTickets on an unknown project = nil, want an error")
	}
}

func ticketIDs(tickets []Ticket) []int {
	ids := make([]int, len(tickets))
	for i, ticket := range tickets {
		ids[i] = ticket.ID
	}
	return ids
}

func TestFormatAge(t *testing.T) {
	for _, tc := range []struct {
		age  time.Duration
		want string
	}{
		{-time.Hour, "<1m"},
		{30 * time.Second, "<1m"},
		{90 * time.Second, "1m"},
		{59 * time.Minute, "59m"},
		{time.Hour + 30*time.Minute, "1h30m"},
		{25 * time.Hour, "1d1h"},
		{7 * 24 * time.Hour, "7d0h"},
	} {
		if got := FormatAge(tc.age); got != tc.want {
			t.Errorf("FormatAge(%v) = %q, want %q", tc.age, got, tc.want)
		}
	}
}

func TestClaimNextMarksTicketInProgress(t *testing.T) {
	setupTestDB(t)
	newProjects(t, "acme")

	if _, err := Add("first", "body", 2, "acme", nil); err != nil {
		t.Fatalf("Add first ticket: %v", err)
	}
	if _, err := Add("second", "body", 0, "acme", nil); err != nil {
		t.Fatalf("Add second ticket: %v", err)
	}

	ticket, err := ClaimNext("acme")
	if err != nil {
		t.Fatalf("ClaimNext(acme): %v", err)
	}
	if ticket.ID != 2 || ticket.Status != "in_progress" {
		t.Fatalf("ClaimNext(acme) = %#v, want ticket 2 in_progress", ticket)
	}

	if _, err := NextTicket("acme"); err != nil {
		t.Fatalf("NextTicket(acme) after claim should find remaining open ticket: %v", err)
	}

	claimed, err := Show(2)
	if err != nil {
		t.Fatalf("Show(2): %v", err)
	}
	if claimed.Status != "in_progress" {
		t.Fatalf("claimed ticket status = %q, want in_progress", claimed.Status)
	}
}

func TestDependenciesBlockQueueAndUnblockWhenDone(t *testing.T) {
	setupTestDB(t)
	newProjects(t, "acme")

	aID, err := Add("A", "body", 2, "acme", nil)
	if err != nil {
		t.Fatalf("Add(A): %v", err)
	}
	a := int(aID)
	bID, err := Add("B", "body", 0, "acme", nil, a)
	if err != nil {
		t.Fatalf("Add(B): %v", err)
	}
	cID, err := Add("C", "body", 1, "acme", nil, a)
	if err != nil {
		t.Fatalf("Add(C): %v", err)
	}
	c := int(cID)
	dID, err := Add("D", "body", 0, "acme", nil, c)
	if err != nil {
		t.Fatalf("Add(D): %v", err)
	}

	for _, id := range []int{int(bID), int(cID), int(dID)} {
		ticket, err := Show(id)
		if err != nil {
			t.Fatalf("Show(%d): %v", id, err)
		}
		if ticket.Status != "blocked" {
			t.Fatalf("ticket %d status = %q, want blocked", id, ticket.Status)
		}
		if len(ticket.BlockedByIDs) != 1 {
			t.Fatalf("ticket %d blocked_by = %v, want one blocker", id, ticket.BlockedByIDs)
		}
	}

	next, err := NextTicket("acme")
	if err != nil {
		t.Fatalf("NextTicket(acme): %v", err)
	}
	if next.ID != a {
		t.Fatalf("NextTicket(acme) = %d, want A (%d)", next.ID, a)
	}

	if err := Done(a); err != nil {
		t.Fatalf("Done(A): %v", err)
	}

	b, err := Show(int(bID))
	if err != nil {
		t.Fatalf("Show(B): %v", err)
	}
	if b.Status != "open" || len(b.BlockedByIDs) != 0 {
		t.Fatalf("B after Done(A) = status %q blocked_by %v, want open and unblocked", b.Status, b.BlockedByIDs)
	}
	cTicket, err := Show(c)
	if err != nil {
		t.Fatalf("Show(C): %v", err)
	}
	if cTicket.Status != "open" || len(cTicket.BlockedByIDs) != 0 {
		t.Fatalf("C after Done(A) = status %q blocked_by %v, want open and unblocked", cTicket.Status, cTicket.BlockedByIDs)
	}
	d, err := Show(int(dID))
	if err != nil {
		t.Fatalf("Show(D): %v", err)
	}
	if d.Status != "blocked" || !reflect.DeepEqual(d.BlockedByIDs, []int{c}) {
		t.Fatalf("D after Done(A) = status %q blocked_by %v, want blocked by C", d.Status, d.BlockedByIDs)
	}
}

func TestOpenTicketWithUnfinishedDependencyIsSkipped(t *testing.T) {
	setupTestDB(t)
	newProjects(t, "acme")

	blockerID, err := Add("blocker", "body", 2, "acme", nil)
	if err != nil {
		t.Fatalf("Add(blocker): %v", err)
	}
	blocker := int(blockerID)
	if _, err := Add("blocked", "body", 0, "acme", nil, blocker); err != nil {
		t.Fatalf("Add(blocked): %v", err)
	}
	open := "open"
	if _, err := Edit(2, EditOptions{Status: &open}); err != nil {
		t.Fatalf("Edit(blocked open): %v", err)
	}

	next, err := ClaimNext("acme")
	if err != nil {
		t.Fatalf("ClaimNext(acme): %v", err)
	}
	if next.ID != blocker {
		t.Fatalf("ClaimNext(acme) = %d, want blocker %d", next.ID, blocker)
	}
}

func TestEditSetsClearsAndRejectsDependencyCycles(t *testing.T) {
	setupTestDB(t)
	newProjects(t, "acme", "zen")

	firstID, err := Add("first", "body", 2, "acme", nil)
	if err != nil {
		t.Fatalf("Add(first): %v", err)
	}
	secondID, err := Add("second", "body", 2, "acme", nil)
	if err != nil {
		t.Fatalf("Add(second): %v", err)
	}
	otherID, err := Add("other", "body", 2, "zen", nil)
	if err != nil {
		t.Fatalf("Add(other): %v", err)
	}

	first := int(firstID)
	deps := []int{first}
	second, err := Edit(int(secondID), EditOptions{DependsOnIDs: &deps})
	if err != nil {
		t.Fatalf("Edit(set dependencies): %v", err)
	}
	if second.Status != "blocked" || !reflect.DeepEqual(second.DependsOnIDs, []int{first}) {
		t.Fatalf("second after dependency edit = %#v, want blocked depending on first", second)
	}

	if _, err := Edit(first, EditOptions{DependsOnIDs: &[]int{int(secondID)}}); err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("Edit(cyclic dependency) error = %v, want cycle", err)
	}
	if _, err := Edit(int(secondID), EditOptions{DependsOnIDs: &[]int{int(otherID)}}); err == nil || !strings.Contains(err.Error(), "belongs to project") {
		t.Fatalf("Edit(cross-project dependency) error = %v, want project mismatch", err)
	}

	second, err = Edit(int(secondID), EditOptions{ClearDependencies: true})
	if err != nil {
		t.Fatalf("Edit(clear dependencies): %v", err)
	}
	if second.Status != "open" || len(second.DependsOnIDs) != 0 || len(second.BlockedByIDs) != 0 {
		t.Fatalf("second after clear = %#v, want open with no dependencies", second)
	}
}

func TestBumpChangesPriorityAndOrdering(t *testing.T) {
	setupTestDB(t)
	newProjects(t, "acme")

	firstID, err := Add("first", "body", 3, "acme", nil)
	if err != nil {
		t.Fatalf("Add first ticket: %v", err)
	}
	if _, err := Add("second", "body", 1, "acme", nil); err != nil {
		t.Fatalf("Add second ticket: %v", err)
	}

	id, priority, err := Bump(int(firstID), 0)
	if err != nil {
		t.Fatalf("Bump(%d, 0): %v", firstID, err)
	}
	if id != int(firstID) || priority != 0 {
		t.Fatalf("Bump returned (%d, %d), want (%d, 0)", id, priority, firstID)
	}

	got := listLines(t, "acme", false)
	want := []string{"ac-1 [P0] first", "ac-2 [P1] second"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("listLines(acme, false) after Bump = %#v, want %#v", got, want)
	}
}

func TestReorderChangesSequenceWithinPriority(t *testing.T) {
	setupTestDB(t)
	newProjects(t, "acme")

	firstID, err := Add("first", "body", 1, "acme", nil)
	if err != nil {
		t.Fatalf("Add first ticket: %v", err)
	}
	secondID, err := Add("second", "body", 1, "acme", nil)
	if err != nil {
		t.Fatalf("Add second ticket: %v", err)
	}
	thirdID, err := Add("third", "body", 1, "acme", nil)
	if err != nil {
		t.Fatalf("Add third ticket: %v", err)
	}

	if err := Reorder([]int{int(thirdID), int(firstID), int(secondID)}); err != nil {
		t.Fatalf("Reorder: %v", err)
	}

	got := listLines(t, "acme", false)
	want := []string{"ac-3 [P1] third", "ac-1 [P1] first", "ac-2 [P1] second"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("listLines(acme, false) after Reorder = %#v, want %#v", got, want)
	}
	ticket, err := Show(int(thirdID))
	if err != nil {
		t.Fatalf("Show reordered ticket: %v", err)
	}
	if ticket.Order == 0 {
		t.Fatalf("Order = 0 after Reorder, want internal order populated")
	}

	if err := Reorder([]int{99999}); err == nil {
		t.Fatal("Reorder with unknown id: want error, got nil")
	}
}

func TestReorderRejectsDuplicatesWithoutChangingSequence(t *testing.T) {
	setupTestDB(t)
	newProjects(t, "acme")

	firstID, err := Add("first", "body", 1, "acme", nil)
	if err != nil {
		t.Fatalf("Add first ticket: %v", err)
	}
	secondID, err := Add("second", "body", 1, "acme", nil)
	if err != nil {
		t.Fatalf("Add second ticket: %v", err)
	}

	err = Reorder([]int{int(secondID), int(firstID), int(firstID)})
	if err == nil || !strings.Contains(err.Error(), "duplicate ticket") {
		t.Fatalf("Reorder duplicate error = %v, want duplicate ticket", err)
	}

	got := listLines(t, "acme", false)
	want := []string{"ac-1 [P1] first", "ac-2 [P1] second"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("listLines(acme, false) after duplicate Reorder = %#v, want %#v", got, want)
	}
}

func TestReorderRejectsMissingTicketWithoutChangingSequence(t *testing.T) {
	setupTestDB(t)
	newProjects(t, "acme")

	firstID, err := Add("first", "body", 1, "acme", nil)
	if err != nil {
		t.Fatalf("Add first ticket: %v", err)
	}
	secondID, err := Add("second", "body", 1, "acme", nil)
	if err != nil {
		t.Fatalf("Add second ticket: %v", err)
	}

	err = Reorder([]int{int(secondID), 99999, int(firstID)})
	if err == nil || !strings.Contains(err.Error(), "no such ticket: 99999") {
		t.Fatalf("Reorder missing error = %v, want no such ticket", err)
	}

	got := listLines(t, "acme", false)
	want := []string{"ac-1 [P1] first", "ac-2 [P1] second"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("listLines(acme, false) after missing Reorder = %#v, want %#v", got, want)
	}
}

func TestProjectsListsStatusCounts(t *testing.T) {
	setupTestDB(t)
	newProjects(t, "acme", "zen")

	firstID, err := Add("first acme", "body", 2, "acme", nil)
	if err != nil {
		t.Fatalf("Add acme ticket: %v", err)
	}
	if _, err := Add("zen", "body", 2, "zen", nil); err != nil {
		t.Fatalf("Add zen ticket: %v", err)
	}
	if _, err := Add("second acme", "body", 2, "acme", nil); err != nil {
		t.Fatalf("Add second acme ticket: %v", err)
	}
	if err := Done(int(firstID)); err != nil {
		t.Fatalf("Done(%d): %v", firstID, err)
	}

	got, err := Projects()
	if err != nil {
		t.Fatalf("Projects(): %v", err)
	}
	want := []ProjectSummary{
		{
			Project: "acme",
			Key:     "ac",
			StatusCounts: map[string]int{
				"done": 1,
				"open": 1,
			},
			Total: 2,
		},
		{
			Project: "zen",
			Key:     "ze",
			StatusCounts: map[string]int{
				"open": 1,
			},
			Total: 1,
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Projects() = %#v, want %#v", got, want)
	}
}

func TestRemoveDeletesOnlyWithinProject(t *testing.T) {
	setupTestDB(t)
	newProjects(t, "acme", "zen")

	acmeID, err := Add("shared", "acme body", 2, "acme", nil)
	if err != nil {
		t.Fatalf("Add acme ticket: %v", err)
	}
	if _, err := Add("shared", "zen body", 2, "zen", nil); err != nil {
		t.Fatalf("Add zen ticket: %v", err)
	}

	if err := Remove(int(acmeID), "zen"); err == nil || !strings.Contains(err.Error(), "no such ticket") {
		t.Fatalf("Remove(%d, zen) error = %v, want no such ticket", acmeID, err)
	}
	if err := Remove(int(acmeID), "acme"); err != nil {
		t.Fatalf("Remove(%d, acme): %v", acmeID, err)
	}

	if got := listLines(t, "acme", true); len(got) != 0 {
		t.Fatalf("listLines(acme, true) after Remove = %#v, want empty", got)
	}

	got := listLines(t, "zen", true)
	want := []string{"ze-2 [P2] shared"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("listLines(zen, true) after Remove = %#v, want %#v", got, want)
	}
}

func TestAddWithParentGroupsUnderEpic(t *testing.T) {
	setupTestDB(t)
	newProjects(t, "acme")

	epicID, err := Add("epic", "body", 2, "acme", nil)
	if err != nil {
		t.Fatalf("Add(epic): %v", err)
	}
	epic := int(epicID)
	childID, err := Add("child", "body", 2, "acme", &epic)
	if err != nil {
		t.Fatalf("Add(child): %v", err)
	}

	child, err := Show(int(childID))
	if err != nil {
		t.Fatalf("Show(child): %v", err)
	}
	if child.ParentID == nil || *child.ParentID != epic {
		t.Fatalf("child.ParentID = %v, want %d", child.ParentID, epic)
	}

	children, err := ListTickets(ListOptions{Project: "acme", ShowAll: true, ParentID: &epic})
	if err != nil {
		t.Fatalf("ListTickets(ParentID): %v", err)
	}
	if len(children) != 1 || children[0].ID != int(childID) {
		t.Fatalf("ListTickets(ParentID) = %#v, want just the child", children)
	}
}

func TestAddRejectsAParentFromAnotherProject(t *testing.T) {
	setupTestDB(t)
	newProjects(t, "acme", "zen")

	epicID, err := Add("epic", "body", 2, "zen", nil)
	if err != nil {
		t.Fatalf("Add(epic): %v", err)
	}
	epic := int(epicID)
	if _, err := Add("child", "body", 2, "acme", &epic); err == nil || !strings.Contains(err.Error(), "belongs to project") {
		t.Fatalf("Add(child) error = %v, want a cross-project parent error", err)
	}
}

func TestEditSetsAndClearsParent(t *testing.T) {
	setupTestDB(t)
	newProjects(t, "acme")

	epicID, err := Add("epic", "body", 2, "acme", nil)
	if err != nil {
		t.Fatalf("Add(epic): %v", err)
	}
	childID, err := Add("child", "body", 2, "acme", nil)
	if err != nil {
		t.Fatalf("Add(child): %v", err)
	}

	epic := int(epicID)
	ticket, err := Edit(int(childID), EditOptions{ParentID: &epic})
	if err != nil {
		t.Fatalf("Edit(set parent): %v", err)
	}
	if ticket.ParentID == nil || *ticket.ParentID != epic {
		t.Fatalf("ticket.ParentID = %v, want %d", ticket.ParentID, epic)
	}

	ticket, err = Edit(int(childID), EditOptions{ClearParent: true})
	if err != nil {
		t.Fatalf("Edit(clear parent): %v", err)
	}
	if ticket.ParentID != nil {
		t.Fatalf("ticket.ParentID = %v, want nil after clearing", ticket.ParentID)
	}
}

func TestEditRejectsSelfAndCyclicParents(t *testing.T) {
	setupTestDB(t)
	newProjects(t, "acme")

	firstID, err := Add("first", "body", 2, "acme", nil)
	if err != nil {
		t.Fatalf("Add(first): %v", err)
	}
	first := int(firstID)
	if _, err := Edit(first, EditOptions{ParentID: &first}); err == nil || !strings.Contains(err.Error(), "own parent") {
		t.Fatalf("Edit(self parent) error = %v, want a self-parent error", err)
	}

	secondID, err := Add("second", "body", 2, "acme", &first)
	if err != nil {
		t.Fatalf("Add(second, parent=first): %v", err)
	}
	second := int(secondID)

	if _, err := Edit(first, EditOptions{ParentID: &second}); err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("Edit(first, parent=second) error = %v, want a cycle error", err)
	}
}

func TestMissingTicketErrors(t *testing.T) {
	setupTestDB(t)
	newProjects(t, "acme")

	if _, _, err := Bump(99, 1); err == nil || !strings.Contains(err.Error(), "no such ticket: 99") {
		t.Fatalf("Bump missing ticket error = %v, want no such ticket: 99", err)
	}
	if err := Done(99); err == nil || !strings.Contains(err.Error(), "no such ticket: 99") {
		t.Fatalf("Done missing ticket error = %v, want no such ticket: 99", err)
	}
	if err := Remove(99, "acme"); err == nil || !strings.Contains(err.Error(), "no such ticket: 99") {
		t.Fatalf("Remove missing ticket error = %v, want no such ticket: 99", err)
	}
}
