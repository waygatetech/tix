package tix

import (
	"context"
	"database/sql"
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
	// Order is the internal rank used to order tickets within a priority. It is
	// hidden from normal JSON output, but the CLI can expose it when a caller
	// explicitly asks for --fields order.
	Order int `json:"-"`
	// ParentID groups this ticket under an epic: the id of another ticket in
	// the same project, or nil when this ticket has no parent.
	ParentID *int `json:"parent_id,omitempty"`
	// DependsOnIDs are tickets that must be done before this ticket is ready.
	DependsOnIDs []int `json:"depends_on,omitempty"`
	// BlockedByIDs is the unfinished subset of DependsOnIDs.
	BlockedByIDs []int `json:"blocked_by,omitempty"`
	// Plan is the cached frontmatter of the ticket's repo plan, loaded by Show
	// only; nil when no plan has been indexed.
	Plan *Plan `json:"plan,omitempty"`
	// Leases are the concepts this ticket holds, loaded by StaleTickets only so
	// a crashed agent's claims are visible where it is recovered.
	Leases []string `json:"leases,omitempty"`
	// Concepts are the concepts and new_concepts the ticket's cached plan
	// declares, loaded by ListTickets only.
	Concepts []string `json:"concepts,omitempty"`
	// CreatedAt and UpdatedAt are stored UTC, and UpdatedAt moves on every
	// write, so how long a ticket has sat in its current status is answerable.
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ticketColumns is the select list every ticket read shares, paired with
// scanTicket below: a column added to one and not the other is a scan error at
// runtime, so they are kept next to each other.
const ticketColumns = `id, title, body, status, priority, order_rank, project, parent_id, created_at, updated_at`

// scanTicket reads one row of ticketColumns. It takes the Scan method rather
// than a row type so both *sql.Row and *sql.Rows can be handed to it.
func scanTicket(scan func(...any) error) (Ticket, error) {
	var ticket Ticket
	var parentID sql.NullInt64
	var updatedAt sql.NullTime
	err := scan(&ticket.ID, &ticket.Title, &ticket.Body, &ticket.Status,
		&ticket.Priority, &ticket.Order, &ticket.Project, &parentID, &ticket.CreatedAt, &updatedAt)
	if err != nil {
		return ticket, err
	}
	if parentID.Valid {
		id := int(parentID.Int64)
		ticket.ParentID = &id
	}
	// updated_at was added after created_at and remains nullable in SQLite.
	// Treat a NULL left by a legacy or direct insert as an untouched ticket.
	if updatedAt.Valid {
		ticket.UpdatedAt = updatedAt.Time
	} else {
		ticket.UpdatedAt = ticket.CreatedAt
	}
	return ticket, nil
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
	// ParentID, when set, keeps only tickets grouped under this parent
	// ticket (its children), instead of every ticket in the project.
	ParentID *int
	// Concepts, when set, keeps only tickets whose cached plan declares any
	// of them in concepts or new_concepts.
	Concepts []string
}

func Add(title, body string, priority int, project string, parentID *int, dependsOnIDs ...int) (int64, error) {
	project, err := ResolveProject(project)
	if err != nil {
		return 0, err
	}
	if parentID != nil {
		if err := checkParent(*parentID, project); err != nil {
			return 0, err
		}
	}
	if err := checkDependencies(0, project, dependsOnIDs); err != nil {
		return 0, err
	}
	status := "open"
	if blocked, err := hasUnfinishedDependencies(dependsOnIDs); err != nil {
		return 0, err
	} else if blocked {
		status = "blocked"
	}

	var maxRank int
	db.QueryRow(`SELECT COALESCE(MAX(order_rank), 0) FROM tickets`).Scan(&maxRank)

	tx, err := db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	res, err := tx.Exec(
		`INSERT INTO tickets (title, body, status, priority, order_rank, project, parent_id, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)`,
		title, body, status, priority, maxRank+1, project, nullableInt(parentID),
	)
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()
	if err := replaceDependencies(tx, int(id), dependsOnIDs); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return id, nil
}

func noSuchTicket(id any) error {
	return fmt.Errorf("no such ticket: %v (database: %s)", id, DBPath())
}

// checkParent reports whether id can be used as a parent for a ticket in
// project: the ticket must exist and belong to the same project, since a
// child grouped under a parent in another project's backlog would be
// invisible to anyone scoped to either one.
func checkParent(id int, project string) error {
	var parentProject string
	if err := db.QueryRow(`SELECT project FROM tickets WHERE id = ?`, id).Scan(&parentProject); err != nil {
		return noSuchTicket(id)
	}
	if parentProject != project {
		return fmt.Errorf("parent ticket %d belongs to project %q, not %q", id, parentProject, project)
	}
	return nil
}

// descendsFrom reports whether candidate is id itself or anywhere in id's
// chain of descendants, walking down through parent_id links. Making a
// ticket's own descendant its parent is what would create a cycle, so Edit
// checks this before accepting a new parent_id. The depth cap keeps a
// pathological chain from recursing forever.
func descendsFrom(candidate, id int) (bool, error) {
	return descendsFromDepth(candidate, id, 0)
}

func descendsFromDepth(candidate, id, depth int) (bool, error) {
	if candidate == id {
		return true, nil
	}
	if depth > 1000 {
		return false, fmt.Errorf("ticket %d's parent chain is too deep to check", id)
	}
	rows, err := db.Query(`SELECT id FROM tickets WHERE parent_id = ?`, id)
	if err != nil {
		return false, err
	}
	var children []int
	for rows.Next() {
		var child int
		if err := rows.Scan(&child); err != nil {
			rows.Close()
			return false, err
		}
		children = append(children, child)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return false, err
	}
	for _, child := range children {
		if ok, err := descendsFromDepth(candidate, child, depth+1); err != nil {
			return false, err
		} else if ok {
			return true, nil
		}
	}
	return false, nil
}

func checkDependencies(id int, project string, dependsOnIDs []int) error {
	seen := map[int]bool{}
	for _, depID := range dependsOnIDs {
		if depID == id && id != 0 {
			return errors.New("a ticket cannot depend on itself")
		}
		if seen[depID] {
			return fmt.Errorf("duplicate dependency: %d", depID)
		}
		seen[depID] = true

		var depProject string
		if err := db.QueryRow(`SELECT project FROM tickets WHERE id = ?`, depID).Scan(&depProject); err != nil {
			return noSuchTicket(depID)
		}
		if depProject != project {
			return fmt.Errorf("dependency ticket %d belongs to project %q, not %q", depID, depProject, project)
		}
		if id != 0 {
			if cyclic, err := dependsOn(depID, id); err != nil {
				return err
			} else if cyclic {
				return fmt.Errorf("depending on %d would create a cycle", depID)
			}
		}
	}
	return nil
}

func dependsOn(ticketID, targetID int) (bool, error) {
	return dependsOnDepth(ticketID, targetID, 0)
}

func dependsOnDepth(ticketID, targetID, depth int) (bool, error) {
	if ticketID == targetID {
		return true, nil
	}
	if depth > 1000 {
		return false, fmt.Errorf("ticket %d's dependency chain is too deep to check", ticketID)
	}
	rows, err := db.Query(`SELECT depends_on_id FROM ticket_dependencies WHERE ticket_id = ?`, ticketID)
	if err != nil {
		return false, err
	}
	var dependencies []int
	for rows.Next() {
		var depID int
		if err := rows.Scan(&depID); err != nil {
			rows.Close()
			return false, err
		}
		dependencies = append(dependencies, depID)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return false, err
	}
	for _, depID := range dependencies {
		if ok, err := dependsOnDepth(depID, targetID, depth+1); err != nil {
			return false, err
		} else if ok {
			return true, nil
		}
	}
	return false, nil
}

func hasUnfinishedDependencies(dependsOnIDs []int) (bool, error) {
	for _, depID := range dependsOnIDs {
		var status string
		if err := db.QueryRow(`SELECT status FROM tickets WHERE id = ?`, depID).Scan(&status); err != nil {
			return false, noSuchTicket(depID)
		}
		if status != "done" {
			return true, nil
		}
	}
	return false, nil
}

type sqlExecer interface {
	Exec(query string, args ...any) (sql.Result, error)
}

func replaceDependencies(exec sqlExecer, id int, dependsOnIDs []int) error {
	if _, err := exec.Exec(`DELETE FROM ticket_dependencies WHERE ticket_id = ?`, id); err != nil {
		return err
	}
	for _, depID := range dependsOnIDs {
		if _, err := exec.Exec(
			`INSERT INTO ticket_dependencies (ticket_id, depends_on_id) VALUES (?, ?)`,
			id, depID,
		); err != nil {
			return err
		}
	}
	return nil
}

// nullableInt turns a possibly-nil *int into the value database/sql sends for
// a NULL column.
func nullableInt(v *int) any {
	if v == nil {
		return nil
	}
	return *v
}

func Bump(id, priority int) (int, int, error) {
	res, err := db.Exec(`UPDATE tickets SET priority = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, priority, id)
	if err != nil {
		return 0, 0, fmt.Errorf("update failed: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return 0, 0, noSuchTicket(id)
	}
	return id, priority, nil
}

// Reorder assigns fresh, consecutive order_rank values to the given tickets in
// the order listed, placing them after every existing order_rank so the
// sequence is unambiguous without disturbing tickets left out of the call.
// Tickets still tie-break by priority first, so a custom sequence is only
// visible among tickets that share a priority.
func Reorder(ids []int) error {
	if len(ids) == 0 {
		return errors.New("no tickets given")
	}
	seen := map[int]bool{}
	for _, id := range ids {
		if seen[id] {
			return fmt.Errorf("duplicate ticket: %d", id)
		}
		seen[id] = true
	}

	values := make([]string, 0, len(ids))
	args := make([]any, 0, len(ids)*2+1)
	for i, id := range ids {
		values = append(values, `(?, ?)`)
		args = append(args, id, i+1)
	}
	args = append(args, len(ids))

	query := `WITH input(id, seq) AS (VALUES ` + strings.Join(values, `, `) + `),
		base(rank) AS (SELECT COALESCE(MAX(order_rank), 0) FROM tickets),
		matched(count) AS (SELECT COUNT(*) FROM tickets WHERE id IN (SELECT id FROM input))
	UPDATE tickets
	SET order_rank = (SELECT rank FROM base) + (SELECT seq FROM input WHERE input.id = tickets.id),
		updated_at = CURRENT_TIMESTAMP
	WHERE id IN (SELECT id FROM input)
		AND (SELECT count FROM matched) = ?`
	res, err := db.Exec(query, args...)
	if err != nil {
		return fmt.Errorf("update failed: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == int64(len(ids)) {
		return nil
	}
	for _, id := range ids {
		var exists int
		if err := db.QueryRow(`SELECT 1 FROM tickets WHERE id = ?`, id).Scan(&exists); err != nil {
			return noSuchTicket(id)
		}
	}
	return fmt.Errorf("reorder changed %d tickets, want %d", n, len(ids))
}

func Done(id int) error {
	return closeTicket(id, nil)
}

// closeTicket marks id done, recording attempt alongside when it came through
// the done hook.
func closeTicket(id int, attempt *doneAttempt) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	res, err := tx.Exec(`UPDATE tickets SET status = 'done', updated_at = CURRENT_TIMESTAMP WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("update failed: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return noSuchTicket(id)
	}
	if attempt != nil {
		if err := recordEvent(tx, id, "done_attempt", attempt); err != nil {
			return err
		}
	}
	if err := recordEvent(tx, id, "done", nil); err != nil {
		return err
	}
	// Closing an epic's last unfinished child closes the epic, and so on up.
	for doneID := id; ; {
		if err := unblockDependents(tx, doneID); err != nil {
			return err
		}
		var parentID int
		err := tx.QueryRow(`
			SELECT parent.id FROM tickets child
			JOIN tickets parent ON parent.id = child.parent_id
			WHERE child.id = ?
				AND parent.status != 'done'
				AND NOT EXISTS (
					SELECT 1 FROM tickets sibling
					WHERE sibling.parent_id = parent.id AND sibling.status != 'done'
				)`, doneID).Scan(&parentID)
		if errors.Is(err, sql.ErrNoRows) {
			break
		}
		if err != nil {
			return fmt.Errorf("close parent failed: %w", err)
		}
		if _, err := tx.Exec(`UPDATE tickets SET status = 'done', updated_at = CURRENT_TIMESTAMP WHERE id = ?`, parentID); err != nil {
			return fmt.Errorf("close parent failed: %w", err)
		}
		if err := recordEvent(tx, parentID, "done", map[string]int{"closed_by": doneID}); err != nil {
			return err
		}
		doneID = parentID
	}
	return tx.Commit()
}

// unblockDependents reopens blocked tickets whose last unfinished dependency
// was id.
func unblockDependents(exec sqlExecer, id int) error {
	if _, err := exec.Exec(`
		UPDATE tickets
		SET status = 'open', updated_at = CURRENT_TIMESTAMP
		WHERE status = 'blocked'
			AND id IN (SELECT ticket_id FROM ticket_dependencies WHERE depends_on_id = ?)
			AND NOT EXISTS (
				SELECT 1
				FROM ticket_dependencies td
				JOIN tickets dep ON dep.id = td.depends_on_id
				WHERE td.ticket_id = tickets.id
					AND dep.status != 'done'
			)`, id); err != nil {
		return fmt.Errorf("unblock dependents failed: %w", err)
	}
	return nil
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
	if options.ParentID != nil {
		where = append(where, `parent_id = ?`)
		args = append(args, *options.ParentID)
	}
	if len(options.Concepts) > 0 {
		where = append(where, `EXISTS (SELECT 1 FROM `+planConcepts+` WHERE c.value IN (?`+strings.Repeat(`, ?`, len(options.Concepts)-1)+`))`)
		for _, concept := range options.Concepts {
			args = append(args, concept)
		}
	}
	query := `SELECT ` + ticketColumns + ` FROM tickets WHERE ` + strings.Join(where, ` AND `) + ` ORDER BY priority ASC, order_rank ASC`
	if options.Limit > 0 {
		query += ` LIMIT ?`
		args = append(args, options.Limit)
	}

	tickets, err := queryTickets(query, args...)
	if err != nil {
		return nil, err
	}
	if err := hydrateConcepts(project, tickets); err != nil {
		return nil, err
	}
	return tickets, nil
}

// planConcepts expands the outer tickets row's cached plan into one row per
// declared concept, as c.value.
const planConcepts = `(VALUES ('$.concepts'), ('$.new_concepts')) p, json_each(tickets.plan, p.column1) c`

// hydrateConcepts fills each ticket's Concepts from its cached plan.
func hydrateConcepts(project string, tickets []Ticket) error {
	rows, err := db.Query(`SELECT tickets.id, c.value FROM tickets, `+planConcepts+`
		WHERE tickets.project = ? AND c.type = 'text' ORDER BY tickets.id, p.column1, c.key`, project)
	if err != nil {
		return fmt.Errorf("loading plan concepts: %w", err)
	}
	defer rows.Close()
	declared := map[int][]string{}
	for rows.Next() {
		var id int
		var concept string
		if err := rows.Scan(&id, &concept); err != nil {
			return fmt.Errorf("loading plan concepts: %w", err)
		}
		declared[id] = append(declared[id], concept)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("loading plan concepts: %w", err)
	}
	for i := range tickets {
		tickets[i].Concepts = declared[tickets[i].ID]
	}
	return nil
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
	if err := hydrateDependencies(tickets); err != nil {
		return nil, err
	}
	return tickets, nil
}

func hydrateDependencies(tickets []Ticket) error {
	for i := range tickets {
		dependsOn, blockedBy, err := dependenciesFor(tickets[i].ID)
		if err != nil {
			return err
		}
		tickets[i].DependsOnIDs = dependsOn
		tickets[i].BlockedByIDs = blockedBy
	}
	return nil
}

func dependenciesFor(id int) ([]int, []int, error) {
	rows, err := db.Query(`
		SELECT dep.id, dep.status
		FROM ticket_dependencies td
		JOIN tickets dep ON dep.id = td.depends_on_id
		WHERE td.ticket_id = ?
		ORDER BY dep.id ASC`, id)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	var dependsOn []int
	var blockedBy []int
	for rows.Next() {
		var depID int
		var status string
		if err := rows.Scan(&depID, &status); err != nil {
			return nil, nil, err
		}
		dependsOn = append(dependsOn, depID)
		if status != "done" {
			blockedBy = append(blockedBy, depID)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	return dependsOn, blockedBy, nil
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
		where += ` AND datetime(COALESCE(updated_at, created_at)) <= datetime(?)`
		args = append(args, time.Now().UTC().Add(-options.Older).Format(sqlTimeLayout))
	}
	query := `SELECT ` + ticketColumns + ` FROM tickets
			 WHERE ` + where + `
			 ORDER BY datetime(COALESCE(updated_at, created_at)) ASC, id ASC`
	if options.Limit > 0 {
		query += ` LIMIT ?`
		args = append(args, options.Limit)
	}
	tickets, err := queryTickets(query, args...)
	if err != nil {
		return nil, err
	}
	leases, err := queryLeases(db, project)
	if err != nil {
		return nil, err
	}
	held := map[int][]string{}
	for _, lease := range leases {
		held[lease.TicketID] = append(held[lease.TicketID], lease.Concept)
	}
	for i := range tickets {
		tickets[i].Leases = held[tickets[i].ID]
	}
	return tickets, nil
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

type sqlQueryer interface {
	Query(query string, args ...any) (*sql.Rows, error)
}

// leaseConflict matches when another in_progress ticket in the project leases
// a concept the outer tickets row's cached plan declares. A ticket reopened
// with its plan still cached would otherwise take its leases back on claim
// without the check tix plan makes.
const leaseConflict = `SELECT 1 FROM tickets holder,
		(VALUES ('$.concepts'), ('$.new_concepts')) hp, json_each(holder.plan, hp.column1) held,
		(VALUES ('$.concepts'), ('$.new_concepts')) wp, json_each(tickets.plan, wp.column1) wanted
	WHERE holder.project = tickets.project AND holder.status = 'in_progress' AND holder.id != tickets.id
		AND held.type = 'text' AND wanted.type = 'text' AND held.value = wanted.value`

// nextClaimable picks the next ticket to work on. A ticket with unfinished
// children is an epic: it is never claimed itself, and when it comes up next
// its own children are searched instead, in the same order. An epic with
// nothing claimable left is skipped. Top-level candidates are tickets with no
// parent, or whose parent is done, so children of a closed epic are not lost.
func nextClaimable(q sqlQueryer, project string, parentID *int, depth int) (int, bool, error) {
	if depth > 1000 {
		return 0, false, errors.New("parent chain is too deep to search")
	}
	scope := `(parent_id IS NULL OR parent_id IN (SELECT id FROM tickets WHERE status = 'done'))`
	args := []any{project}
	if parentID != nil {
		scope = `parent_id = ?`
		args = append(args, *parentID)
	}
	rows, err := q.Query(
		`SELECT id, status, EXISTS (
				SELECT 1 FROM tickets child WHERE child.parent_id = tickets.id AND child.status != 'done'
			 ) FROM tickets
			 WHERE status != 'done'
			 AND project = ?
			 AND `+scope+`
			 AND NOT EXISTS (
				SELECT 1
				FROM ticket_dependencies td
				JOIN tickets dep ON dep.id = td.depends_on_id
				WHERE td.ticket_id = tickets.id
					AND dep.status != 'done'
			 )
			 AND NOT EXISTS (`+leaseConflict+`)
			 ORDER BY priority ASC, order_rank ASC`,
		args...,
	)
	if err != nil {
		return 0, false, err
	}
	type candidate struct {
		id     int
		status string
		epic   bool
	}
	// Collected before recursing, since a transaction's connection cannot
	// run the next level's query while these rows are still open.
	var candidates []candidate
	for rows.Next() {
		var c candidate
		if err := rows.Scan(&c.id, &c.status, &c.epic); err != nil {
			rows.Close()
			return 0, false, err
		}
		candidates = append(candidates, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, false, err
	}

	for _, c := range candidates {
		if !c.epic {
			if c.status == "open" {
				return c.id, true, nil
			}
			continue
		}
		id, ok, err := nextClaimable(q, project, &c.id, depth+1)
		if err != nil || ok {
			return id, ok, err
		}
	}
	return 0, false, nil
}

func NextTicket(project string) (Ticket, error) {
	project, err := ResolveProject(project)
	if err != nil {
		return Ticket{}, err
	}

	id, ok, err := nextClaimable(db, project, nil, 0)
	if err != nil {
		return Ticket{}, err
	}
	if !ok {
		return Ticket{}, errors.New("no open tickets")
	}
	ticket, err := scanTicket(db.QueryRow(`SELECT `+ticketColumns+` FROM tickets WHERE id = ?`, id).Scan)
	if err != nil {
		return Ticket{}, err
	}
	dependsOn, blockedBy, err := dependenciesFor(ticket.ID)
	if err != nil {
		return Ticket{}, err
	}
	ticket.DependsOnIDs = dependsOn
	ticket.BlockedByIDs = blockedBy
	return ticket, nil
}

func ClaimNext(project string) (Ticket, error) {
	ticket, _, err := ClaimNextEvaluated(context.Background(), project)
	return ticket, err
}

// ClaimNextEvaluated applies the optional Jev judgment layer immediately
// before the existing claim transition. The returned evaluation is nil when
// Jev is off, preserving both behavior and network isolation by default.
func ClaimNextEvaluated(ctx context.Context, project string) (Ticket, *JevEvaluation, error) {
	cfg, err := loadJevConfig()
	if err != nil {
		return Ticket{}, nil, err
	}
	if cfg.Mode == JevOff {
		ticket, err := claimNext(project)
		return ticket, nil, err
	}

	candidate, err := NextTicket(project)
	if err != nil {
		return Ticket{}, nil, err
	}
	prefix, err := PrefixFor(candidate.Project)
	if err != nil {
		return Ticket{}, nil, err
	}
	evaluation := evaluateJev(ctx, cfg, candidate, prefix)
	status := "in_progress"
	if cfg.Mode == JevEnforce && evaluation.Err == nil && evaluation.PolicyDecision == "require_human_input" {
		status = "blocked"
	}
	ticket, err := transitionOpenTicket(candidate.ID, status)
	if err != nil {
		return Ticket{}, &evaluation, err
	}
	// Telemetry must not turn Jev into a claim-time failure. The schema is
	// migrated with the rest of tix, so this can fail only on a database error
	// that the next normal write will also expose.
	_ = recordJevEvaluation(ticket.ID, evaluation)
	return ticket, &evaluation, nil
}

func claimNext(project string) (Ticket, error) {
	project, err := ResolveProject(project)
	if err != nil {
		return Ticket{}, err
	}

	tx, err := db.Begin()
	if err != nil {
		return Ticket{}, err
	}
	defer tx.Rollback()

	id, ok, err := nextClaimable(tx, project, nil, 0)
	if err != nil {
		return Ticket{}, err
	}
	if !ok {
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
	if err := recordEvent(tx, id, "claim", map[string]string{"status": "in_progress"}); err != nil {
		return Ticket{}, err
	}

	ticket, err := scanTicket(tx.QueryRow(
		`SELECT `+ticketColumns+` FROM tickets WHERE id = ?`, id).Scan)
	if err != nil {
		return Ticket{}, err
	}

	if err := tx.Commit(); err != nil {
		return Ticket{}, err
	}
	dependsOn, blockedBy, err := dependenciesFor(ticket.ID)
	if err != nil {
		return Ticket{}, err
	}
	ticket.DependsOnIDs = dependsOn
	ticket.BlockedByIDs = blockedBy
	return ticket, nil
}

func transitionOpenTicket(id int, status string) (Ticket, error) {
	tx, err := db.Begin()
	if err != nil {
		return Ticket{}, err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`UPDATE tickets SET status = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ? AND status = 'open'`, status, id)
	if err != nil {
		return Ticket{}, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return Ticket{}, errors.New("ticket already claimed")
	}
	if err := recordEvent(tx, id, "claim", map[string]string{"status": status}); err != nil {
		return Ticket{}, err
	}
	ticket, err := scanTicket(tx.QueryRow(`SELECT `+ticketColumns+` FROM tickets WHERE id = ?`, id).Scan)
	if err != nil {
		return Ticket{}, err
	}
	if err := tx.Commit(); err != nil {
		return Ticket{}, err
	}
	dependsOn, blockedBy, err := dependenciesFor(ticket.ID)
	if err != nil {
		return Ticket{}, err
	}
	ticket.DependsOnIDs, ticket.BlockedByIDs = dependsOn, blockedBy
	return ticket, nil
}

func Show(id int) (Ticket, error) {
	ticket, err := scanTicket(db.QueryRow(
		`SELECT `+ticketColumns+` FROM tickets WHERE id = ?`, id).Scan)
	if err != nil {
		return Ticket{}, noSuchTicket(id)
	}
	dependsOn, blockedBy, err := dependenciesFor(ticket.ID)
	if err != nil {
		return Ticket{}, err
	}
	ticket.DependsOnIDs = dependsOn
	ticket.BlockedByIDs = blockedBy
	if ticket.Plan, err = planFor(ticket.ID); err != nil {
		return Ticket{}, err
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
	// ParentID sets the ticket's parent when non-nil; ClearParent removes it
	// regardless of ParentID. Both unset leaves the parent untouched, the way
	// a nil field leaves any other column untouched.
	ParentID          *int
	ClearParent       bool
	DependsOnIDs      *[]int
	ClearDependencies bool
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
	var newProject string
	if options.Project != nil {
		// Moving a ticket to a project nobody created would hide it just as
		// thoroughly as adding it there in the first place.
		project, err := ResolveProject(*options.Project)
		if err != nil {
			return Ticket{}, err
		}
		newProject = project
		sets = append(sets, `project = ?`)
		args = append(args, project)
	}
	if options.ClearParent {
		sets = append(sets, `parent_id = NULL`)
	} else if options.ParentID != nil {
		if *options.ParentID == id {
			return Ticket{}, errors.New("a ticket cannot be its own parent")
		}
		project := newProject
		if project == "" {
			current, err := Show(id)
			if err != nil {
				return Ticket{}, err
			}
			project = current.Project
		}
		if err := checkParent(*options.ParentID, project); err != nil {
			return Ticket{}, err
		}
		if cyclic, err := descendsFrom(*options.ParentID, id); err != nil {
			return Ticket{}, err
		} else if cyclic {
			return Ticket{}, fmt.Errorf("setting parent to %d would create a cycle", *options.ParentID)
		}
		sets = append(sets, `parent_id = ?`)
		args = append(args, *options.ParentID)
	}
	dependencyEdit := options.DependsOnIDs != nil || options.ClearDependencies
	if len(sets) == 0 && !dependencyEdit {
		return Ticket{}, errors.New("no fields to update")
	}
	if dependencyEdit {
		if options.DependsOnIDs != nil && options.ClearDependencies {
			return Ticket{}, errors.New("use either dependencies or clear dependencies, not both")
		}
		current, err := Show(id)
		if err != nil {
			return Ticket{}, err
		}
		project := newProject
		if project == "" {
			project = current.Project
		}
		dependencies := []int{}
		if options.DependsOnIDs != nil {
			dependencies = *options.DependsOnIDs
		}
		if err := checkDependencies(id, project, dependencies); err != nil {
			return Ticket{}, err
		}
		if blocked, err := hasUnfinishedDependencies(dependencies); err != nil {
			return Ticket{}, err
		} else if blocked && options.Status == nil {
			sets = append(sets, `status = 'blocked'`)
		} else if !blocked && options.Status == nil && current.Status == "blocked" {
			sets = append(sets, `status = 'open'`)
		}
	}
	// Appended after the empty check so an edit that changes nothing does not
	// make the ticket look freshly worked on.
	sets = append(sets, `updated_at = CURRENT_TIMESTAMP`)

	tx, err := db.Begin()
	if err != nil {
		return Ticket{}, err
	}
	defer tx.Rollback()

	args = append(args, id)
	if len(sets) > 0 {
		query := `UPDATE tickets SET ` + strings.Join(sets, `, `) + ` WHERE id = ?`
		res, err := tx.Exec(query, args...)
		if err != nil {
			return Ticket{}, fmt.Errorf("update failed: %w", err)
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			return Ticket{}, noSuchTicket(id)
		}
	}
	if dependencyEdit {
		dependencies := []int{}
		if options.DependsOnIDs != nil {
			dependencies = *options.DependsOnIDs
		}
		if err := replaceDependencies(tx, id, dependencies); err != nil {
			return Ticket{}, fmt.Errorf("update dependencies failed: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return Ticket{}, err
	}

	return Show(id)
}

func Remove(id int, project string) error {
	project, err := ResolveProject(project)
	if err != nil {
		return err
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM ticket_dependencies WHERE ticket_id = ? OR depends_on_id = ?`, id, id); err != nil {
		return fmt.Errorf("delete dependencies failed: %w", err)
	}
	res, err := tx.Exec(`DELETE FROM tickets WHERE id = ? AND project = ?`, id, project)
	if err != nil {
		return errors.New("delete failed")
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return noSuchTicket(id)
	}
	return tx.Commit()
}
