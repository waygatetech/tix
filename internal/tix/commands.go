package tix

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

type Ticket struct {
	ID       int    `json:"id"`
	Title    string `json:"title"`
	Body     string `json:"body,omitempty"`
	Status   string `json:"status"`
	Priority int    `json:"priority"`
	Project  string `json:"project"`
	// CreatedAt and UpdatedAt are stored UTC, and UpdatedAt moves on every
	// write, so how long a ticket has sat in its current status is answerable.
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ticketColumns is the select list every ticket read shares, paired with
// scanTicket below: a column added to one and not the other is a scan error at
// runtime, so they are kept next to each other.
const ticketColumns = `id, title, body, status, priority, project, created_at, updated_at`

// scanTicket reads one row of ticketColumns. It takes the Scan method rather
// than a row type so both *sql.Row and *sql.Rows can be handed to it.
func scanTicket(scan func(...any) error) (Ticket, error) {
	var ticket Ticket
	err := scan(&ticket.ID, &ticket.Title, &ticket.Body, &ticket.Status,
		&ticket.Priority, &ticket.Project, &ticket.CreatedAt, &ticket.UpdatedAt)
	return ticket, err
}

type ProjectSummary struct {
	Project string `json:"project"`
	// Key is the prefix the project's ticket IDs display with, derived from the
	// name unless it was pinned.
	Key          string         `json:"key"`
	RepoPath     string         `json:"repo_path,omitempty"`
	StatusCounts map[string]int `json:"status_counts"`
	Total        int            `json:"total"`
}

type ListOptions struct {
	Project string
	ShowAll bool
	Status  string
	// Query keeps only tickets whose title or body contains it, ignoring case.
	// An empty query filters nothing.
	Query    string
	Priority *int
	Limit    int
}

func Add(title, body string, priority int, project string) (int64, error) {
	project, err := ResolveProject(project)
	if err != nil {
		return 0, err
	}

	var maxRank int
	db.QueryRow(`SELECT COALESCE(MAX(order_rank), 0) FROM tickets`).Scan(&maxRank)

	res, err := db.Exec(
		`INSERT INTO tickets (title, body, priority, order_rank, project, updated_at)
			 VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP)`,
		title, body, priority, maxRank+1, project,
	)
	if err != nil {
		fmt.Println("insert failed:", err)
		return 0, err
	}
	id, _ := res.LastInsertId()
	return id, nil
}

func Bump(id, priority int) (int, int, error) {
	res, err := db.Exec(`UPDATE tickets SET priority = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, priority, id)
	if err != nil {
		return 0, 0, errors.New("update failed")
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		fmt.Println("no such ticket:", id)
		return 0, 0, fmt.Errorf("no such ticket: %d", id)
	}
	return id, priority, nil
}

func Done(id int) error {
	res, err := db.Exec(`UPDATE tickets SET status = 'done', updated_at = CURRENT_TIMESTAMP WHERE id = ?`, id)
	if err != nil {
		return errors.New("update failed")
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("no such ticket: %d", id)
	}
	return nil
}

func List(project string, showAll bool) []string {
	tickets, err := ListTickets(ListOptions{Project: project, ShowAll: showAll})
	if err != nil {
		fmt.Println("query failed:", err)
		return []string{}
	}

	prefixes, err := Prefixes()
	if err != nil {
		fmt.Println("prefix lookup failed:", err)
		prefixes = map[string]string{}
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

func ListTickets(options ListOptions) ([]Ticket, error) {
	project, err := ResolveProject(options.Project)
	if err != nil {
		return nil, err
	}

	where := []string{`project = ?`}
	args := []any{project}
	if options.Status != "" {
		where = append(where, `status = ?`)
		args = append(args, options.Status)
	} else if !options.ShowAll {
		where = append(where, `status != 'done'`)
	}
	if options.Priority != nil {
		where = append(where, `priority = ?`)
		args = append(args, *options.Priority)
	}
	if term := strings.TrimSpace(options.Query); term != "" {
		where = append(where, `(title LIKE ? ESCAPE '\' OR body LIKE ? ESCAPE '\')`)
		pattern := likePattern(term)
		args = append(args, pattern, pattern)
	}
	query := `SELECT ` + ticketColumns + ` FROM tickets WHERE ` + strings.Join(where, ` AND `) + ` ORDER BY priority ASC, order_rank ASC`
	if options.Limit > 0 {
		query += ` LIMIT ?`
		args = append(args, options.Limit)
	}

	return queryTickets(query, args...)
}

// queryTickets runs a select over ticketColumns and collects the rows.
func queryTickets(query string, args ...any) ([]Ticket, error) {
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	tickets := []Ticket{}
	for rows.Next() {
		ticket, err := scanTicket(rows.Scan)
		if err != nil {
			return nil, err
		}
		tickets = append(tickets, ticket)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return tickets, nil
}

// likeEscaper neutralizes the wildcards a search term may contain, so looking
// for "100%" or "order_rank" matches those characters instead of matching every
// ticket. The escape character itself has to go first.
var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

// likePattern turns a search term into the substring pattern ListTickets
// compares against, paired with the ESCAPE clause in the query.
func likePattern(term string) string {
	return "%" + likeEscaper.Replace(term) + "%"
}

// StaleStatus is the status tickets get stuck in: an agent claims a ticket,
// crashes, and nothing moves it back. next skips it forever because it is no
// longer open, and nothing else mentions it again.
const StaleStatus = "in_progress"

// StaleAge is how long a ticket sits in that status before it is worth
// surfacing. A day is long enough that work in progress overnight is not
// flagged, and short enough that a crash is noticed the next morning. It is
// the CLI's default rather than this package's, so that asking for no
// threshold at all stays possible.
const StaleAge = 24 * time.Hour

type StaleOptions struct {
	Project string
	// Status is the status to consider stuck, defaulting to StaleStatus. Done
	// tickets never go stale, but open ones can, so the status is a knob.
	Status string
	// Older is how long since the last write a ticket must be. Zero means no
	// age filter, the way a zero Limit means no limit, so a caller that asks
	// for a threshold of nothing gets everything in the status instead of a
	// default it did not ask for.
	Older time.Duration
	Limit int
}

// StaleTickets returns the project's tickets that have sat in one status
// without a write for longer than options.Older, oldest first, so the ticket
// most likely abandoned is the one read first.
func StaleTickets(options StaleOptions) ([]Ticket, error) {
	project, err := ResolveProject(options.Project)
	if err != nil {
		return nil, err
	}
	if options.Limit < 0 {
		return nil, errors.New("limit must be >= 0")
	}
	status := options.Status
	if status == "" {
		status = StaleStatus
	}

	where := `project = ? AND status = ?`
	args := []any{project, status}
	if options.Older > 0 {
		// Timestamps are written by CURRENT_TIMESTAMP, which is UTC, so the
		// cutoff is formatted the same way. Both sides go through datetime() so
		// a value that reached the column in some other shape still compares
		// correctly.
		where += ` AND datetime(updated_at) <= datetime(?)`
		args = append(args, time.Now().UTC().Add(-options.Older).Format(sqlTimeLayout))
	}
	query := `SELECT ` + ticketColumns + ` FROM tickets
			 WHERE ` + where + `
			 ORDER BY datetime(updated_at) ASC, id ASC`
	if options.Limit > 0 {
		query += ` LIMIT ?`
		args = append(args, options.Limit)
	}
	return queryTickets(query, args...)
}

// sqlTimeLayout is how SQLite's CURRENT_TIMESTAMP writes a time.
const sqlTimeLayout = "2006-01-02 15:04:05"

// FormatAge renders a duration the way a stale listing reads it: coarse, two
// units at most, because "3d4h idle" is the whole answer and the minutes are
// noise.
func FormatAge(d time.Duration) string {
	if d < time.Minute {
		return "<1m"
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh%dm", int(d.Hours()), int(d.Minutes())%60)
	}
	days := int(d.Hours()) / 24
	return fmt.Sprintf("%dd%dh", days, int(d.Hours())%24)
}

func Next(project string) (int, string, string, int, error) {
	ticket, err := NextTicket(project)
	if err != nil {
		return 0, "", "", 0, err
	}
	return ticket.ID, ticket.Title, ticket.Body, ticket.Priority, nil
}

func NextTicket(project string) (Ticket, error) {
	project, err := ResolveProject(project)
	if err != nil {
		return Ticket{}, err
	}

	ticket, err := scanTicket(db.QueryRow(
		`SELECT `+ticketColumns+` FROM tickets
			 WHERE status = 'open'
			 AND project = ?
			 ORDER BY priority ASC, order_rank ASC LIMIT 1`,
		project,
	).Scan)
	if err != nil {
		return Ticket{}, errors.New("no open tickets")
	}
	return ticket, nil
}

func ClaimNext(project string) (Ticket, error) {
	project, err := ResolveProject(project)
	if err != nil {
		return Ticket{}, err
	}

	tx, err := db.Begin()
	if err != nil {
		return Ticket{}, err
	}
	defer tx.Rollback()

	var id int
	err = tx.QueryRow(
		`SELECT id FROM tickets
			 WHERE status = 'open'
			 AND project = ?
			 ORDER BY priority ASC, order_rank ASC LIMIT 1`,
		project,
	).Scan(&id)
	if err != nil {
		return Ticket{}, errors.New("no open tickets")
	}

	res, err := tx.Exec(
		`UPDATE tickets SET status = 'in_progress', updated_at = CURRENT_TIMESTAMP
			 WHERE id = ? AND status = 'open'`, id)
	if err != nil {
		return Ticket{}, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return Ticket{}, errors.New("ticket already claimed")
	}

	ticket, err := scanTicket(tx.QueryRow(
		`SELECT `+ticketColumns+` FROM tickets WHERE id = ?`, id).Scan)
	if err != nil {
		return Ticket{}, err
	}

	if err := tx.Commit(); err != nil {
		return Ticket{}, err
	}
	return ticket, nil
}

func Show(id int) (Ticket, error) {
	ticket, err := scanTicket(db.QueryRow(
		`SELECT `+ticketColumns+` FROM tickets WHERE id = ?`, id).Scan)
	if err != nil {
		return Ticket{}, fmt.Errorf("no such ticket: %d", id)
	}
	return ticket, nil
}

func AllTickets() ([]Ticket, error) {
	return queryTickets(`SELECT ` + ticketColumns + ` FROM tickets ORDER BY project ASC, priority ASC, order_rank ASC`)
}

// Projects summarizes every project in the table, including one whose tickets
// are all gone or that was created before its first ticket.
func Projects() ([]ProjectSummary, error) {
	known, err := ListProjects()
	if err != nil {
		return nil, err
	}
	prefixes, err := Prefixes()
	if err != nil {
		return nil, err
	}

	projects := make([]ProjectSummary, 0, len(known))
	indexes := map[string]int{}
	for _, project := range known {
		indexes[project.Name] = len(projects)
		projects = append(projects, ProjectSummary{
			Project:      project.Name,
			Key:          prefixes[project.Name],
			RepoPath:     project.RepoPath,
			StatusCounts: map[string]int{},
		})
	}

	rows, err := db.Query(`SELECT project, status, COUNT(*) FROM tickets GROUP BY project, status ORDER BY project ASC, status ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var project, status string
		var count int
		if err := rows.Scan(&project, &status, &count); err != nil {
			return nil, err
		}
		index, ok := indexes[project]
		if !ok {
			// Tickets whose project predates the table and could not be
			// backfilled (an empty name) still have to be counted somewhere.
			index = len(projects)
			indexes[project] = index
			projects = append(projects, ProjectSummary{
				Project:      project,
				Key:          prefixes[project],
				StatusCounts: map[string]int{},
			})
		}
		projects[index].StatusCounts[status] = count
		projects[index].Total += count
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return projects, nil
}

// FormatStatusCounts renders a summary's per-status tallies as "done=1 open=3".
// The order is sorted rather than map order, so two identical summaries render
// identically instead of looking like a change.
func FormatStatusCounts(counts map[string]int) string {
	statuses := make([]string, 0, len(counts))
	for status := range counts {
		statuses = append(statuses, status)
	}
	sort.Strings(statuses)

	parts := make([]string, 0, len(statuses))
	for _, status := range statuses {
		parts = append(parts, fmt.Sprintf("%s=%d", status, counts[status]))
	}
	return strings.Join(parts, " ")
}

type EditOptions struct {
	Title    *string
	Body     *string
	Status   *string
	Priority *int
	Project  *string
}

func Edit(id int, options EditOptions) (Ticket, error) {
	sets := []string{}
	args := []any{}

	if options.Title != nil {
		sets = append(sets, `title = ?`)
		args = append(args, *options.Title)
	}
	if options.Body != nil {
		sets = append(sets, `body = ?`)
		args = append(args, *options.Body)
	}
	if options.Status != nil {
		sets = append(sets, `status = ?`)
		args = append(args, *options.Status)
	}
	if options.Priority != nil {
		sets = append(sets, `priority = ?`)
		args = append(args, *options.Priority)
	}
	if options.Project != nil {
		// Moving a ticket to a project nobody created would hide it just as
		// thoroughly as adding it there in the first place.
		project, err := ResolveProject(*options.Project)
		if err != nil {
			return Ticket{}, err
		}
		sets = append(sets, `project = ?`)
		args = append(args, project)
	}
	if len(sets) == 0 {
		return Ticket{}, errors.New("no fields to update")
	}
	// Appended after the empty check so an edit that changes nothing does not
	// make the ticket look freshly worked on.
	sets = append(sets, `updated_at = CURRENT_TIMESTAMP`)

	args = append(args, id)
	query := `UPDATE tickets SET ` + strings.Join(sets, `, `) + ` WHERE id = ?`
	res, err := db.Exec(query, args...)
	if err != nil {
		return Ticket{}, errors.New("update failed")
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return Ticket{}, fmt.Errorf("no such ticket: %d", id)
	}

	return Show(id)
}

func Remove(id int, project string) error {
	project, err := ResolveProject(project)
	if err != nil {
		return err
	}
	res, err := db.Exec(`DELETE FROM tickets WHERE id = ? AND project = ?`, id, project)
	if err != nil {
		return errors.New("delete failed")
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("no such ticket: %d", id)
	}
	return nil
}
