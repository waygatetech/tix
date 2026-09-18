package tix

import "sort"

// One database holds every project, so a single view can show the whole backlog
// at once — the thing a tracker that lives inside one repository structurally
// cannot do. Dashboard assembles that view: every project, including one with no
// tickets, each ticket carrying the prefixed reference the CLI prints, and
// rollups so a reader sees the shape of all their work before scrolling.

// statusOrder is the order statuses are presented in, which is the order work
// moves through them rather than alphabetical.
var statusOrder = []string{"open", "in_progress", "done"}

// DashboardTicket is a ticket plus the reference the CLI would print for it, so
// an ID copied off the page can be pasted straight into a command.
type DashboardTicket struct {
	Ticket
	Ref string `json:"ref"`
}

// StatusCount is a presentable tally for one ticket status.
type StatusCount struct {
	Status string `json:"status"`
	Count  int    `json:"count"`
}

// DashboardProject is one project's tickets with the counts that summarize them.
type DashboardProject struct {
	Project      string            `json:"project"`
	Key          string            `json:"key"`
	RepoPath     string            `json:"repo_path,omitempty"`
	StatusCounts map[string]int    `json:"status_counts"`
	Total        int               `json:"total"`
	Tickets      []DashboardTicket `json:"tickets"`
}

// Dashboard is every project's backlog in one value.
type Dashboard struct {
	Projects     []DashboardProject `json:"projects"`
	StatusCounts map[string]int     `json:"status_counts"`
	Total        int                `json:"total"`
	// Statuses and Priorities are the values actually present, so a filter can
	// offer exactly the choices that would match something.
	Statuses   []string `json:"statuses"`
	Priorities []int    `json:"priorities"`
}

// StatusTallies returns every status present across the dashboard in display order.
func (d Dashboard) StatusTallies() []StatusCount {
	return statusTallies(d.StatusCounts)
}

// Done reports how many tickets are finished, which is the numerator of the
// progress a reader is looking for.
func (p DashboardProject) Done() int {
	return p.StatusCounts["done"]
}

// Open reports how many tickets are neither started nor finished.
func (p DashboardProject) Open() int {
	return p.StatusCounts["open"]
}

// InProgress reports how many tickets are claimed.
func (p DashboardProject) InProgress() int {
	return p.StatusCounts["in_progress"]
}

// StatusTallies returns every status present in the project in display order.
func (p DashboardProject) StatusTallies() []StatusCount {
	return statusTallies(p.StatusCounts)
}

// Percent is how much of the project is done, rounded down, and 0 for a project
// with no tickets rather than a division by zero.
func (p DashboardProject) Percent() int {
	if p.Total == 0 {
		return 0
	}
	return p.Done() * 100 / p.Total
}

// BuildDashboard reads every project and ticket and groups them. Projects come
// from the table rather than from the tickets, so a project created ahead of its
// first ticket still shows up — an empty repo is information too.
func BuildDashboard() (Dashboard, error) {
	projects, err := ListProjects()
	if err != nil {
		return Dashboard{}, err
	}
	prefixes, err := Prefixes()
	if err != nil {
		return Dashboard{}, err
	}
	tickets, err := AllTickets()
	if err != nil {
		return Dashboard{}, err
	}

	board := Dashboard{
		Projects:     make([]DashboardProject, 0, len(projects)),
		StatusCounts: map[string]int{},
	}
	indexes := map[string]int{}
	for _, project := range projects {
		indexes[project.Name] = len(board.Projects)
		board.Projects = append(board.Projects, DashboardProject{
			Project:      project.Name,
			Key:          prefixes[project.Name],
			RepoPath:     project.RepoPath,
			StatusCounts: map[string]int{},
		})
	}

	statuses := map[string]bool{}
	priorities := map[int]bool{}
	for _, ticket := range tickets {
		index, ok := indexes[ticket.Project]
		if !ok {
			// A ticket whose project predates the projects table has nowhere
			// else to go, and dropping it would understate the backlog.
			index = len(board.Projects)
			indexes[ticket.Project] = index
			board.Projects = append(board.Projects, DashboardProject{
				Project:      ticket.Project,
				Key:          prefixes[ticket.Project],
				StatusCounts: map[string]int{},
			})
		}

		project := &board.Projects[index]
		project.Tickets = append(project.Tickets, DashboardTicket{
			Ticket: ticket,
			Ref:    FormatID(prefixes[ticket.Project], ticket.ID),
		})
		project.StatusCounts[ticket.Status]++
		project.Total++

		board.StatusCounts[ticket.Status]++
		board.Total++
		statuses[ticket.Status] = true
		priorities[ticket.Priority] = true
	}

	// Finished tickets sink to the bottom of their project. They are the part of
	// the list a reader scans last, and keeping them out of the middle means
	// showing them never pushes live work off the screen.
	for i := range board.Projects {
		tickets := board.Projects[i].Tickets
		sort.SliceStable(tickets, func(a, b int) bool {
			return tickets[a].Status != "done" && tickets[b].Status == "done"
		})
	}

	board.Statuses = sortStatuses(statuses)
	board.Priorities = sortedKeys(priorities)
	return board, nil
}

// sortStatuses orders the statuses present by where they sit in the workflow,
// with anything unrecognized after them in alphabetical order so a status added
// outside this package is still listed.
func sortStatuses(present map[string]bool) []string {
	statuses := make([]string, 0, len(present))
	for _, status := range statusOrder {
		if present[status] {
			statuses = append(statuses, status)
			delete(present, status)
		}
	}
	rest := make([]string, 0, len(present))
	for status := range present {
		rest = append(rest, status)
	}
	sort.Strings(rest)
	return append(statuses, rest...)
}

func statusTallies(counts map[string]int) []StatusCount {
	present := map[string]bool{}
	for status, count := range counts {
		if count > 0 {
			present[status] = true
		}
	}

	statuses := sortStatuses(present)
	tallies := make([]StatusCount, 0, len(statuses))
	for _, status := range statuses {
		tallies = append(tallies, StatusCount{Status: status, Count: counts[status]})
	}
	return tallies
}

func sortedKeys(present map[int]bool) []int {
	keys := make([]int, 0, len(present))
	for key := range present {
		keys = append(keys, key)
	}
	sort.Ints(keys)
	return keys
}
