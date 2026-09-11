package cmd

import (
	"fmt"
	"html/template"
	"net/http"
	"sort"

	"github.com/spf13/cobra"
	"github.com/waygatetech/tix/internal/tix"
)

var webPort int

var webCmd = &cobra.Command{
	Use:   "web",
	Short: "Serve a web view of tickets grouped by project",
	RunE: func(cmd *cobra.Command, args []string) error {
		http.HandleFunc("/", webHandler)
		addr := fmt.Sprintf("localhost:%d", webPort)
		fmt.Printf("tix web running at http://%s\n", addr)
		return http.ListenAndServe(addr, nil)
	},
}

func init() {
	webCmd.Flags().IntVar(&webPort, "port", 8080, "port to serve on")
	rootCmd.AddCommand(webCmd)
}

type webProjectGroup struct {
	Project string
	Tickets []tix.Ticket
}

func webHandler(w http.ResponseWriter, r *http.Request) {
	tickets, err := tix.AllTickets()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	byProject := map[string][]tix.Ticket{}
	var projects []string
	for _, ticket := range tickets {
		if _, ok := byProject[ticket.Project]; !ok {
			projects = append(projects, ticket.Project)
		}
		byProject[ticket.Project] = append(byProject[ticket.Project], ticket)
	}
	sort.Strings(projects)

	groups := make([]webProjectGroup, 0, len(projects))
	for _, project := range projects {
		groups = append(groups, webProjectGroup{Project: project, Tickets: byProject[project]})
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := webTemplate.Execute(w, groups); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

var webTemplate = template.Must(template.New("web").Parse(`<!doctype html>
<html>
<head>
<meta charset="utf-8">
<title>tix</title>
<style>
  body { font-family: -apple-system, BlinkMacSystemFont, sans-serif; background: #f5f5f5; color: #1a1a1a; margin: 2rem; }
  h1 { margin-bottom: 1.5rem; }
  .project { background: #fff; border-radius: 8px; box-shadow: 0 1px 3px rgba(0,0,0,0.1); margin-bottom: 1.5rem; overflow: hidden; }
  .project h2 { margin: 0; padding: 0.75rem 1rem; background: #2d2d2d; color: #fff; font-size: 1rem; }
  table { width: 100%; border-collapse: collapse; }
  td, th { text-align: left; padding: 0.5rem 1rem; border-bottom: 1px solid #eee; }
  tr:last-child td { border-bottom: none; }
  .status { display: inline-block; padding: 0.15rem 0.5rem; border-radius: 4px; font-size: 0.8rem; font-weight: 600; }
  .status-open { background: #dbeafe; color: #1e40af; }
  .status-in_progress { background: #fef3c7; color: #92400e; }
  .status-done { background: #dcfce7; color: #166534; }
  .done-title { text-decoration: line-through; color: #888; }
  .priority { font-family: monospace; color: #555; }
  .empty { color: #888; padding: 1rem; }
</style>
</head>
<body>
<h1>tix</h1>
{{if not .}}<p class="empty">No tickets yet.</p>{{end}}
{{range .}}
<div class="project">
  <h2>{{.Project}}</h2>
  <table>
    {{range .Tickets}}
    <tr>
      <td class="priority">#{{.ID}}</td>
      <td class="priority">P{{.Priority}}</td>
      <td><span class="status status-{{.Status}}">{{.Status}}</span></td>
      <td{{if eq .Status "done"}} class="done-title"{{end}}>{{.Title}}</td>
    </tr>
    {{end}}
  </table>
</div>
{{end}}
</body>
</html>
`))
