package cmd

import (
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
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
// bound to localhost and read-only — it reads the same file the CLI writes, so
// there is nothing here to authenticate and nothing to lose.
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
