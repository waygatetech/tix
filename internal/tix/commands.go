package tix

import (
	"errors"
	"fmt"
	"strings"
)

type Ticket struct {
	ID       int    `json:"id"`
	Title    string `json:"title"`
	Body     string `json:"body,omitempty"`
	Status   string `json:"status"`
	Priority int    `json:"priority"`
	Project  string `json:"project"`
}

type ProjectSummary struct {
	Project      string         `json:"project"`
	StatusCounts map[string]int `json:"status_counts"`
	Total        int            `json:"total"`
}

type ListOptions struct {
	Project  string
	ShowAll  bool
	Status   string
	Priority *int
	Limit    int
}

func Add(title, body string, priority int, project string) (int64, error) {

	var maxRank int
	db.QueryRow(`SELECT COALESCE(MAX(order_rank), 0) FROM tickets`).Scan(&maxRank)

	res, err := db.Exec(
		`INSERT INTO tickets (title, body, priority, order_rank, project) VALUES (?, ?, ?, ?, ?)`,
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
	res, err := db.Exec(`UPDATE tickets SET priority = ? WHERE id = ?`, priority, id)
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
	res, err := db.Exec(`UPDATE tickets SET status = 'done' WHERE id = ?`, id)
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
	where := []string{`project = ?`}
	args := []any{options.Project}
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
	query := `SELECT id, title, body, status, priority, project FROM tickets WHERE ` + strings.Join(where, ` AND `) + ` ORDER BY priority ASC, order_rank ASC`
	if options.Limit > 0 {
		query += ` LIMIT ?`
		args = append(args, options.Limit)
	}

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	tickets := []Ticket{}
	for rows.Next() {
		var ticket Ticket
		if err := rows.Scan(&ticket.ID, &ticket.Title, &ticket.Body, &ticket.Status, &ticket.Priority, &ticket.Project); err != nil {
			return nil, err
		}
		tickets = append(tickets, ticket)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return tickets, nil
}

func Next(project string) (int, string, string, int, error) {
	ticket, err := NextTicket(project)
	if err != nil {
		return 0, "", "", 0, err
	}
	return ticket.ID, ticket.Title, ticket.Body, ticket.Priority, nil
}

func NextTicket(project string) (Ticket, error) {
	var ticket Ticket
	err := db.QueryRow(
		`SELECT id, title, body, status, priority, project FROM tickets
			 WHERE status = 'open'
			 AND project = ?
			 ORDER BY priority ASC, order_rank ASC LIMIT 1`,
		project,
	).Scan(&ticket.ID, &ticket.Title, &ticket.Body, &ticket.Status, &ticket.Priority, &ticket.Project)
	if err != nil {
		return Ticket{}, errors.New("no open tickets")
	}
	return ticket, nil
}

func ClaimNext(project string) (Ticket, error) {
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

	res, err := tx.Exec(`UPDATE tickets SET status = 'in_progress' WHERE id = ? AND status = 'open'`, id)
	if err != nil {
		return Ticket{}, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return Ticket{}, errors.New("ticket already claimed")
	}

	var ticket Ticket
	err = tx.QueryRow(
		`SELECT id, title, body, status, priority, project FROM tickets WHERE id = ?`,
		id,
	).Scan(&ticket.ID, &ticket.Title, &ticket.Body, &ticket.Status, &ticket.Priority, &ticket.Project)
	if err != nil {
		return Ticket{}, err
	}

	if err := tx.Commit(); err != nil {
		return Ticket{}, err
	}
	return ticket, nil
}

func Show(id int) (Ticket, error) {
	var ticket Ticket
	err := db.QueryRow(
		`SELECT id, title, body, status, priority, project FROM tickets WHERE id = ?`,
		id,
	).Scan(&ticket.ID, &ticket.Title, &ticket.Body, &ticket.Status, &ticket.Priority, &ticket.Project)
	if err != nil {
		return Ticket{}, fmt.Errorf("no such ticket: %d", id)
	}
	return ticket, nil
}

func AllTickets() ([]Ticket, error) {
	rows, err := db.Query(`SELECT id, title, body, status, priority, project FROM tickets ORDER BY project ASC, priority ASC, order_rank ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	tickets := []Ticket{}
	for rows.Next() {
		var ticket Ticket
		if err := rows.Scan(&ticket.ID, &ticket.Title, &ticket.Body, &ticket.Status, &ticket.Priority, &ticket.Project); err != nil {
			return nil, err
		}
		tickets = append(tickets, ticket)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return tickets, nil
}

func Projects() ([]ProjectSummary, error) {
	rows, err := db.Query(`SELECT project, status, COUNT(*) FROM tickets GROUP BY project, status ORDER BY project ASC, status ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	projects := []ProjectSummary{}
	indexes := map[string]int{}
	for rows.Next() {
		var project, status string
		var count int
		if err := rows.Scan(&project, &status, &count); err != nil {
			return nil, err
		}
		index, ok := indexes[project]
		if !ok {
			index = len(projects)
			indexes[project] = index
			projects = append(projects, ProjectSummary{
				Project:      project,
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
		sets = append(sets, `project = ?`)
		args = append(args, *options.Project)
	}
	if len(sets) == 0 {
		return Ticket{}, errors.New("no fields to update")
	}

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
