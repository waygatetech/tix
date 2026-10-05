package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
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

// webTestMux mirrors the routes wired up in webCmd.RunE, since the edit
// handler reads its id from a pattern that only a ServeMux populates.
func webTestMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/", webHandler)
	mux.HandleFunc("/api/dashboard", webAPIHandler)
	mux.HandleFunc("POST /api/tickets", webCreateTicketHandler)
	mux.HandleFunc("PATCH /api/tickets/{id}", webEditTicketHandler)
	return mux
}

func do(t *testing.T, mux *http.ServeMux, method, target string, body any) *httptest.ResponseRecorder {
	t.Helper()

	var reader *bytes.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal request body: %v", err)
		}
		reader = bytes.NewReader(encoded)
	} else {
		reader = bytes.NewReader(nil)
	}

	req := httptest.NewRequest(method, target, reader)
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, req)
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
	login, err := tix.Add("fix login", "users cannot log in", 0, "acme", nil)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if _, err := tix.Add("deploy app", "waiting on login", 1, "acme", nil, int(login)); err != nil {
		t.Fatalf("Add: %v", err)
	}
	epicID, err := tix.Add("release epic", "", 2, "acme", nil)
	if err != nil {
		t.Fatalf("Add epic: %v", err)
	}
	epic := int(epicID)
	if _, err := tix.Add("release child", "", 2, "acme", &epic); err != nil {
		t.Fatalf("Add child: %v", err)
	}
	if _, err := tix.Add("tune cache", "", 2, "zen", nil); err != nil {
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
		"fix login",                         // a ticket title
		"users cannot log in",               // its body, available to expand and to search
		"deploy app",                        // a dependent ticket
		"deps 1",                            // visible dependency indicator
		`class="kind-badge"`,                // epics are called out explicitly
		`class="kind-badge child"`,          // child tickets are labeled as well as indented
		`data-parent-id=`,                   // the hierarchy is available to styling and scripts
		`draggable="true"`,                  // tickets can be dragged onto an epic/parent
		"Drag a ticket onto another ticket", // the interaction is discoverable
		"blocked",                           // every present status is shown, not just the core three
		`data-project="acme"`,               // the project filter hooks
		`data-project="zen"`,                // including the project with no repo path
		"ac-1",                              // the prefixed ref, not the bare row id
		"/src/acme",                         // where the project's code lives
		`data-status="open"`,                // the status filter hooks
		`data-priority="0"`,                 // the priority filter hooks
		"/api/dashboard",                    // the JSON view the page polls
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
	if _, err := tix.Add("fix login", "users cannot log in", 0, "acme", nil); err != nil {
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
	if _, err := tix.Add("fix login", "", 2, "acme", nil); err != nil {
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

func TestWebCreateTicketHandlerAddsATicket(t *testing.T) {
	setupWebDB(t)
	if _, err := tix.CreateProject(tix.Project{Name: "acme", Key: "ac"}); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	mux := webTestMux()

	recorder := do(t, mux, http.MethodPost, "/api/tickets", webNewTicketRequest{
		Title:    "fix login",
		Body:     "users cannot log in",
		Priority: 1,
		Project:  "acme",
	})
	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", recorder.Code, recorder.Body.String())
	}

	var ticket tix.Ticket
	if err := json.Unmarshal(recorder.Body.Bytes(), &ticket); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if ticket.Title != "fix login" || ticket.Project != "acme" || ticket.Priority != 1 {
		t.Fatalf("ticket = %+v, want title=fix login project=acme priority=1", ticket)
	}

	tickets, err := tix.ListTickets(tix.ListOptions{Project: "acme", ShowAll: true})
	if err != nil {
		t.Fatalf("ListTickets: %v", err)
	}
	if len(tickets) != 1 {
		t.Fatalf("ListTickets returned %d tickets, want 1", len(tickets))
	}
}

func TestWebCreateTicketHandlerRejectsMissingTitle(t *testing.T) {
	setupWebDB(t)
	if _, err := tix.CreateProject(tix.Project{Name: "acme"}); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	mux := webTestMux()

	recorder := do(t, mux, http.MethodPost, "/api/tickets", webNewTicketRequest{Project: "acme"})
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
}

func TestWebCreateTicketHandlerRejectsUnknownProject(t *testing.T) {
	setupWebDB(t)
	mux := webTestMux()

	recorder := do(t, mux, http.MethodPost, "/api/tickets", webNewTicketRequest{Title: "x", Project: "ghost"})
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", recorder.Code, recorder.Body.String())
	}
}

func TestWebEditTicketHandlerUpdatesFields(t *testing.T) {
	setupWebDB(t)
	if _, err := tix.CreateProject(tix.Project{Name: "acme"}); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	id, err := tix.Add("fix login", "old body", 0, "acme", nil)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	mux := webTestMux()

	newTitle := "fix login flow"
	newBody := "new body"
	newStatus := "in_progress"
	newPriority := 2
	recorder := do(t, mux, http.MethodPatch, fmt.Sprintf("/api/tickets/%d", id), webEditTicketRequest{
		Title:    &newTitle,
		Body:     &newBody,
		Status:   &newStatus,
		Priority: &newPriority,
	})
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}

	ticket, err := tix.Show(int(id))
	if err != nil {
		t.Fatalf("Show: %v", err)
	}
	if ticket.Title != newTitle || ticket.Body != newBody || ticket.Status != newStatus || ticket.Priority != newPriority {
		t.Fatalf("ticket = %+v, want updated fields", ticket)
	}
}

func TestWebEditTicketHandlerMakesTicketAChild(t *testing.T) {
	setupWebDB(t)
	if _, err := tix.CreateProject(tix.Project{Name: "acme"}); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	parentID, _ := tix.Add("release", "", 1, "acme", nil)
	childID, _ := tix.Add("build", "", 2, "acme", nil)
	parent := int(parentID)

	recorder := do(t, webTestMux(), http.MethodPatch, fmt.Sprintf("/api/tickets/%d", childID), webEditTicketRequest{ParentID: &parent})
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	ticket, err := tix.Show(int(childID))
	if err != nil {
		t.Fatalf("Show: %v", err)
	}
	if ticket.ParentID == nil || *ticket.ParentID != parent {
		t.Fatalf("ParentID = %v, want %d", ticket.ParentID, parent)
	}
}

func TestWebEditTicketHandlerRejectsUnknownTicket(t *testing.T) {
	setupWebDB(t)
	mux := webTestMux()

	newTitle := "x"
	recorder := do(t, mux, http.MethodPatch, "/api/tickets/999", webEditTicketRequest{Title: &newTitle})
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", recorder.Code, recorder.Body.String())
	}
}

func TestWebEditTicketHandlerRejectsInvalidID(t *testing.T) {
	setupWebDB(t)
	mux := webTestMux()

	newTitle := "x"
	recorder := do(t, mux, http.MethodPatch, "/api/tickets/nope", webEditTicketRequest{Title: &newTitle})
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
}
