package tix

import (
	"reflect"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func setupTestDB(t *testing.T) {
	t.Helper()

	if db != nil {
		_ = db.Close()
	}

	t.Setenv("TIX_DB", t.TempDir()+"/tix.db")
	InitDB()

	t.Cleanup(func() {
		if db != nil {
			_ = db.Close()
			db = nil
		}
	})
}

func TestDBPathUsesTIXDBOverride(t *testing.T) {
	t.Setenv("TIX_DB", "/tmp/custom-tix.db")

	if got := dbPath(); got != "/tmp/custom-tix.db" {
		t.Fatalf("dbPath() = %q, want %q", got, "/tmp/custom-tix.db")
	}
}

func TestAddListNextAndProjectIsolation(t *testing.T) {
	setupTestDB(t)

	firstID, err := Add("fix login bug", "users cannot log in", 2, "acme")
	if err != nil {
		t.Fatalf("Add first ticket: %v", err)
	}
	secondID, err := Add("write docs", "README", 0, "acme")
	if err != nil {
		t.Fatalf("Add second ticket: %v", err)
	}
	otherID, err := Add("refactor parser", "split tokens", 2, "zen")
	if err != nil {
		t.Fatalf("Add other project ticket: %v", err)
	}

	if firstID != 1 || secondID != 2 || otherID != 3 {
		t.Fatalf("unexpected IDs: got %d, %d, %d", firstID, secondID, otherID)
	}

	got := List("acme", false)
	want := []string{
		"ac-2 [P0] write docs",
		"ac-1 [P2] fix login bug",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("List(acme, false) = %#v, want %#v", got, want)
	}

	id, title, body, priority, err := Next("acme")
	if err != nil {
		t.Fatalf("Next(acme): %v", err)
	}
	if id != 2 || title != "write docs" || body != "README" || priority != 0 {
		t.Fatalf("Next(acme) = (%d, %q, %q, %d), want (2, %q, %q, 0)", id, title, body, priority, "write docs", "README")
	}

	got = List("zen", false)
	want = []string{"ze-3 [P2] refactor parser"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("List(zen, false) = %#v, want %#v", got, want)
	}
}

func TestDoneFiltersOpenListAndNext(t *testing.T) {
	setupTestDB(t)

	firstID, err := Add("first", "body one", 1, "acme")
	if err != nil {
		t.Fatalf("Add first ticket: %v", err)
	}
	secondID, err := Add("second", "body two", 2, "acme")
	if err != nil {
		t.Fatalf("Add second ticket: %v", err)
	}

	if err := Done(int(firstID)); err != nil {
		t.Fatalf("Done(%d): %v", firstID, err)
	}

	got := List("acme", false)
	want := []string{"ac-2 [P2] second"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("List(acme, false) after Done = %#v, want %#v", got, want)
	}

	got = List("acme", true)
	want = []string{"x ac-1 [P1] first", "ac-2 [P2] second"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("List(acme, true) after Done = %#v, want %#v", got, want)
	}

	id, title, _, _, err := Next("acme")
	if err != nil {
		t.Fatalf("Next(acme): %v", err)
	}
	if id != int(secondID) || title != "second" {
		t.Fatalf("Next(acme) = (%d, %q, ...), want (%d, %q, ...)", id, title, secondID, "second")
	}

	if err := Done(int(secondID)); err != nil {
		t.Fatalf("Done(%d): %v", secondID, err)
	}
	_, _, _, _, err = Next("acme")
	if err == nil || err.Error() != "no open tickets" {
		t.Fatalf("Next(acme) after all done error = %v, want no open tickets", err)
	}
}

func TestListTicketsFilters(t *testing.T) {
	setupTestDB(t)

	firstID, err := Add("first", "body", 1, "acme")
	if err != nil {
		t.Fatalf("Add first ticket: %v", err)
	}
	if _, err := Add("second", "body", 2, "acme"); err != nil {
		t.Fatalf("Add second ticket: %v", err)
	}
	if _, err := Add("third", "body", 1, "acme"); err != nil {
		t.Fatalf("Add third ticket: %v", err)
	}
	if _, err := Add("other project", "body", 1, "zen"); err != nil {
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
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ListTickets filtered = %#v, want %#v", got, want)
	}
}

func TestShowReturnsTicketByID(t *testing.T) {
	setupTestDB(t)

	id, err := Add("show me", "full details", 1, "acme")
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
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Show(%d) = %#v, want %#v", id, got, want)
	}

	if _, err := Show(99); err == nil || !strings.Contains(err.Error(), "no such ticket: 99") {
		t.Fatalf("Show missing ticket error = %v, want no such ticket: 99", err)
	}
}

func TestClaimNextMarksTicketInProgress(t *testing.T) {
	setupTestDB(t)

	if _, err := Add("first", "body", 2, "acme"); err != nil {
		t.Fatalf("Add first ticket: %v", err)
	}
	if _, err := Add("second", "body", 0, "acme"); err != nil {
		t.Fatalf("Add second ticket: %v", err)
	}

	ticket, err := ClaimNext("acme")
	if err != nil {
		t.Fatalf("ClaimNext(acme): %v", err)
	}
	if ticket.ID != 2 || ticket.Status != "in_progress" {
		t.Fatalf("ClaimNext(acme) = %#v, want ticket 2 in_progress", ticket)
	}

	_, _, _, _, err = Next("acme")
	if err != nil {
		t.Fatalf("Next(acme) after claim should find remaining open ticket: %v", err)
	}

	claimed, err := Show(2)
	if err != nil {
		t.Fatalf("Show(2): %v", err)
	}
	if claimed.Status != "in_progress" {
		t.Fatalf("claimed ticket status = %q, want in_progress", claimed.Status)
	}
}

func TestBumpChangesPriorityAndOrdering(t *testing.T) {
	setupTestDB(t)

	firstID, err := Add("first", "body", 3, "acme")
	if err != nil {
		t.Fatalf("Add first ticket: %v", err)
	}
	if _, err := Add("second", "body", 1, "acme"); err != nil {
		t.Fatalf("Add second ticket: %v", err)
	}

	id, priority, err := Bump(int(firstID), 0)
	if err != nil {
		t.Fatalf("Bump(%d, 0): %v", firstID, err)
	}
	if id != int(firstID) || priority != 0 {
		t.Fatalf("Bump returned (%d, %d), want (%d, 0)", id, priority, firstID)
	}

	got := List("acme", false)
	want := []string{"ac-1 [P0] first", "ac-2 [P1] second"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("List(acme, false) after Bump = %#v, want %#v", got, want)
	}
}

func TestProjectsListsStatusCounts(t *testing.T) {
	setupTestDB(t)

	firstID, err := Add("first acme", "body", 2, "acme")
	if err != nil {
		t.Fatalf("Add acme ticket: %v", err)
	}
	if _, err := Add("zen", "body", 2, "zen"); err != nil {
		t.Fatalf("Add zen ticket: %v", err)
	}
	if _, err := Add("second acme", "body", 2, "acme"); err != nil {
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
			StatusCounts: map[string]int{
				"done": 1,
				"open": 1,
			},
			Total: 2,
		},
		{
			Project: "zen",
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

	acmeID, err := Add("shared", "acme body", 2, "acme")
	if err != nil {
		t.Fatalf("Add acme ticket: %v", err)
	}
	if _, err := Add("shared", "zen body", 2, "zen"); err != nil {
		t.Fatalf("Add zen ticket: %v", err)
	}

	if err := Remove(int(acmeID), "zen"); err == nil || !strings.Contains(err.Error(), "no such ticket") {
		t.Fatalf("Remove(%d, zen) error = %v, want no such ticket", acmeID, err)
	}
	if err := Remove(int(acmeID), "acme"); err != nil {
		t.Fatalf("Remove(%d, acme): %v", acmeID, err)
	}

	if got := List("acme", true); len(got) != 0 {
		t.Fatalf("List(acme, true) after Remove = %#v, want empty", got)
	}

	got := List("zen", true)
	want := []string{"ze-2 [P2] shared"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("List(zen, true) after Remove = %#v, want %#v", got, want)
	}
}

func TestMissingTicketErrors(t *testing.T) {
	setupTestDB(t)

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
