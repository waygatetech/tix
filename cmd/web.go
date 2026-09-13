package cmd

import (
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/waygatetech/tix/internal/tix"
)

var webPort int

// webGlobal makes the server ignore a repo-local database. The installed agent
// always passes it; a person running `tix web` in a repo usually means that
// repo's backlog, so it stays off by default.
var webGlobal bool

// webDefaultPort is the port both the foreground server and the installed agent
// use, so the URL is the same either way. 8080 is too popular to claim for a
// service that is meant to sit there for months.
const webDefaultPort = 7433

// tix web is the one view a per-repo tracker cannot offer: every project's
// backlog on one page, because every project lives in one database. It stays
// bound to localhost — it reads and writes the same file the CLI does, so
// there is nothing here to authenticate beyond that.
var webCmd = &cobra.Command{
	Use:   "web",
	Short: "Serve a cross-project dashboard of every ticket",
	// PreRunE rather than PersistentPreRunE: install, uninstall, and status
	// manage a launchd job and have no reason to open a database.
	PreRunE: func(cmd *cobra.Command, args []string) error {
		if webGlobal {
			return initDBWithoutRepoLookup(cmd, args)
		}
		return initDB(cmd, args)
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		mux := http.NewServeMux()
		mux.HandleFunc("/", webHandler)
		mux.HandleFunc("/api/dashboard", webAPIHandler)
		mux.HandleFunc("POST /api/tickets", webCreateTicketHandler)
		mux.HandleFunc("PATCH /api/tickets/{id}", webEditTicketHandler)

		addr := fmt.Sprintf("localhost:%d", webPort)
		fmt.Fprintf(cmd.OutOrStdout(), "tix web running at http://%s\n", addr)
		return http.ListenAndServe(addr, mux)
	},
}

func init() {
	webCmd.Flags().IntVar(&webPort, "port", webDefaultPort, "port to serve on")
	webCmd.Flags().BoolVar(&webGlobal, "global", false, "serve the global database, ignoring any repo-local one")
	rootCmd.AddCommand(webCmd)
}

//go:embed web.html
var webFS embed.FS

var webTemplate = template.Must(template.New("web.html").Funcs(template.FuncMap{
	"label":       webLabel,
	"statusClass": webStatusClass,
}).ParseFS(webFS, "web.html"))

// webPage is the dashboard plus the page-level context that is not per-project.
type webPage struct {
	tix.Dashboard
	DBPath string
}

// webLabel renders a status for people rather than for SQL.
func webLabel(status string) string {
	return strings.ReplaceAll(status, "_", " ")
}

// webStatusClass maps a status to the class the stylesheet knows, so a status
// this build has never heard of still gets a readable pill instead of an
// unstyled one.
func webStatusClass(status string) string {
	switch status {
	case "open", "in_progress", "done":
		return status
	}
	return "other"
}

func webHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	board, err := tix.BuildDashboard()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := webTemplate.Execute(w, webPage{Dashboard: board, DBPath: tix.DBPath()}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// webAPIHandler serves the same view as JSON. The page polls it to notice that
// a CLI command changed the backlog underneath an open tab, and it is the
// obvious thing to curl if you want the whole picture in a script.
func webAPIHandler(w http.ResponseWriter, r *http.Request) {
	board, err := tix.BuildDashboard()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(board); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// webNewTicketRequest is the body of a create request. Priority and body are
// optional the same way `tix add` treats them.
type webNewTicketRequest struct {
	Title    string `json:"title"`
	Body     string `json:"body"`
	Priority int    `json:"priority"`
	Project  string `json:"project"`
	ParentID *int   `json:"parent_id,omitempty"`
}

// webCreateTicketHandler backs the dashboard's "new ticket" form with the same
// path `tix add` takes, so a ticket made from the browser behaves exactly like
// one made from the CLI — project resolution, parent checks, ordering all
// included.
func webCreateTicketHandler(w http.ResponseWriter, r *http.Request) {
	var req webNewTicketRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Title) == "" {
		http.Error(w, "title is required", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Project) == "" {
		http.Error(w, "project is required", http.StatusBadRequest)
		return
	}

	id, err := tix.Add(req.Title, req.Body, req.Priority, req.Project, req.ParentID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	ticket, err := tix.Show(int(id))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(ticket)
}

// webEditTicketRequest is the body of an edit request. Every field is a
// pointer so an absent field leaves that column untouched, matching
// tix.EditOptions.
type webEditTicketRequest struct {
	Title       *string `json:"title"`
	Body        *string `json:"body"`
	Status      *string `json:"status"`
	Priority    *int    `json:"priority"`
	Project     *string `json:"project"`
	ParentID    *int    `json:"parent_id"`
	ClearParent bool    `json:"clear_parent"`
}

// webEditTicketHandler backs inline edits made from an expanded ticket row.
func webEditTicketHandler(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.Error(w, "invalid ticket id", http.StatusBadRequest)
		return
	}

	var req webEditTicketRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	ticket, err := tix.Edit(id, tix.EditOptions{
		Title:       req.Title,
		Body:        req.Body,
		Status:      req.Status,
		Priority:    req.Priority,
		Project:     req.Project,
		ParentID:    req.ParentID,
		ClearParent: req.ClearParent,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(ticket)
}
