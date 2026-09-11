package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/waygatetech/tix/internal/tix"
)

func setupWebDB(t *testing.T) {
	t.Helper()

	tix.CloseDB()
	t.Setenv("TIX_DB", t.TempDir()+"/tix.db")
	if err := tix.InitDB(); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	t.Cleanup(tix.CloseDB)
}

func get(t *testing.T, handler http.HandlerFunc, target string) *httptest.ResponseRecorder {
	t.Helper()

	recorder := httptest.NewRecorder()
	handler(recorder, httptest.NewRequest(http.MethodGet, target, nil))
	return recorder
}

// The page is one big template, so the test that matters most is that it renders
// real data at all: a bad field name or function only fails at request time.
func TestWebHandlerRendersEveryProject(t *testing.T) {
	setupWebDB(t)
	if _, err := tix.CreateProject(tix.Project{Name: "acme", Key: "ac", RepoPath: "/src/acme"}); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if _, err := tix.CreateProject(tix.Project{Name: "zen"}); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if _, err := tix.Add("fix login", "users cannot log in", 0, "acme"); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if _, err := tix.Add("tune cache", "", 2, "zen"); err != nil {
		t.Fatalf("Add: %v", err)
	}

	recorder := get(t, webHandler, "/")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if got := recorder.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/html") {
		t.Fatalf("Content-Type = %q, want text/html", got)
	}

	body := recorder.Body.String()
	for _, want := range []string{
		"fix login",           // a ticket title
		"users cannot log in", // its body, available to expand and to search
		`data-project="acme"`, // the project filter hooks
		`data-project="zen"`,  // including the project with no repo path
		"ac-1",                // the prefixed ref, not the bare row id
		"/src/acme",           // where the project's code lives
		`data-status="open"`,  // the status filter hooks
		`data-priority="0"`,   // the priority filter hooks
		"/api/dashboard",      // the JSON view the page polls
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("page is missing %q", want)
		}
	}
}

func TestWebHandlerEmptyDatabaseExplainsWhatToDo(t *testing.T) {
	setupWebDB(t)

	recorder := get(t, webHandler, "/")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}

	body := recorder.Body.String()
	if !strings.Contains(body, "No projects yet") {
		t.Fatalf("empty page does not say the backlog is empty:\n%s", body)
	}
	if !strings.Contains(body, "tix add") {
		t.Fatalf("empty page does not show how to add a ticket")
	}
}

// An unknown path must not render the dashboard, or every typo would look like a
// working page.
func TestWebHandlerUnknownPathIs404(t *testing.T) {
	setupWebDB(t)

	recorder := get(t, webHandler, "/nope")
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", recorder.Code)
	}
}

func TestWebAPIHandlerServesTheSameView(t *testing.T) {
	setupWebDB(t)
	if _, err := tix.CreateProject(tix.Project{Name: "acme", Key: "ac"}); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if _, err := tix.Add("fix login", "users cannot log in", 0, "acme"); err != nil {
		t.Fatalf("Add: %v", err)
	}

	recorder := get(t, webAPIHandler, "/api/dashboard")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if got := recorder.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}

	var board tix.Dashboard
	if err := json.Unmarshal(recorder.Body.Bytes(), &board); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if board.Total != 1 || len(board.Projects) != 1 {
		t.Fatalf("board = %+v, want one project with one ticket", board)
	}
	if board.Projects[0].Tickets[0].Ref != "ac-1" {
		t.Fatalf("Ref = %q, want ac-1", board.Projects[0].Tickets[0].Ref)
	}
}

// The page polls this endpoint and reloads when the bytes change, so a ticket
// added behind its back has to change them.
func TestWebAPIHandlerOutputChangesWithTheBacklog(t *testing.T) {
	setupWebDB(t)
	if _, err := tix.CreateProject(tix.Project{Name: "acme"}); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	before := get(t, webAPIHandler, "/api/dashboard").Body.String()
	if _, err := tix.Add("fix login", "", 2, "acme"); err != nil {
		t.Fatalf("Add: %v", err)
	}
	after := get(t, webAPIHandler, "/api/dashboard").Body.String()

	if before == after {
		t.Fatal("adding a ticket did not change the JSON view")
	}
}

func TestWebLabelAndStatusClass(t *testing.T) {
	if got := webLabel("in_progress"); got != "in progress" {
		t.Fatalf("webLabel(in_progress) = %q, want %q", got, "in progress")
	}
	if got := webStatusClass("done"); got != "done" {
		t.Fatalf("webStatusClass(done) = %q, want done", got)
	}
	if got := webStatusClass("blocked"); got != "other" {
		t.Fatalf("webStatusClass(blocked) = %q, want other", got)
	}
}
