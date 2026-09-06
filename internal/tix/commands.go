package tix

import (
	"errors"
	"fmt"
)

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
	query := `SELECT id, title, status, priority FROM tickets WHERE project = ?`
	if !showAll {
		query += ` AND status != 'done'`
	}
	query += ` ORDER BY priority ASC, order_rank ASC`

	rows, err := db.Query(query, project)
	if err != nil {
		fmt.Println("query failed:", err)
		return []string{}
	}
	defer rows.Close()

	if rows.Err() != nil {
		fmt.Println("query failed:", rows.Err())
		return []string{}
	}

	var tickets []string
	for rows.Next() {
		var id, priority int
		var title, status string
		rows.Scan(&id, &title, &status, &priority)
		marker := ""
		if status == "done" {
			marker = "x "
		}
		tickets = append(tickets, fmt.Sprintf("%s#%d [P%d] %s", marker, id, priority, title))
	}
	return tickets
}

func Next(project string) (int, string, string, int, error) {
	var id, priority int
	var title, body string
	err := db.QueryRow(
		`SELECT id, title, body, priority FROM tickets
			 WHERE status != 'done'
			 AND project = ?
			 ORDER BY priority ASC, order_rank ASC LIMIT 1`,
		project,
	).Scan(&id, &title, &body, &priority)
	if err != nil {
		return 0, "", "", 0, errors.New("no open tickets")
	}
	return id, title, body, priority, nil
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
