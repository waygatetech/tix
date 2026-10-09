package tix

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// DefaultAuditDays is how far back tix audit looks for reverts.
const DefaultAuditDays = 14

// Revert is a reverted event audit recorded: SHA is the ticket's commit and
// RevertSHA the commit that undid it.
type Revert struct {
	TicketID  int    `json:"ticket_id"`
	Project   string `json:"project"`
	SHA       string `json:"sha"`
	RevertSHA string `json:"revert_sha"`
}

// revertsCommit is the line git revert writes into the message body.
var revertsCommit = regexp.MustCompile(`This reverts commit ([0-9a-f]{7,40})`)

// Audit scans the last days of git history in each project's repo_path (or
// only project's, when given) for reverts of commits recorded on done events,
// and records a reverted event for each one not already recorded. It returns
// the events it recorded, so running it again records nothing new.
func Audit(ctx context.Context, project string, days int) ([]Revert, error) {
	if days <= 0 {
		return nil, fmt.Errorf("days must be > 0")
	}
	if project != "" {
		resolved, err := ResolveProject(project)
		if err != nil {
			return nil, err
		}
		project = resolved
	}
	projects, err := ListProjects()
	if err != nil {
		return nil, err
	}
	recorded := []Revert{}
	for _, p := range projects {
		if (project != "" && p.Name != project) || p.RepoPath == "" {
			continue
		}
		found, err := auditProject(ctx, p, days)
		if err != nil {
			return nil, fmt.Errorf("auditing %s: %w", p.Name, err)
		}
		recorded = append(recorded, found...)
	}
	return recorded, nil
}

func auditProject(ctx context.Context, p Project, days int) ([]Revert, error) {
	log, err := git(ctx, p.RepoPath, "log", "--all", fmt.Sprintf("--since=%d days ago", days), "--format=%H%x00%B%x1e")
	if err != nil {
		return nil, err
	}
	// revertOf maps a reverted commit (as the message spells it, maybe
	// abbreviated) to the commit that reverted it.
	revertOf := map[string]string{}
	for _, entry := range strings.Split(log, "\x1e") {
		sha, body, ok := strings.Cut(strings.TrimSpace(entry), "\x00")
		if !ok {
			continue
		}
		for _, m := range revertsCommit.FindAllStringSubmatch(body, -1) {
			revertOf[m[1]] = sha
		}
	}
	if len(revertOf) == 0 {
		return nil, nil
	}

	dones, err := doneCommits(p.Name)
	if err != nil {
		return nil, err
	}
	seen, err := recordedReverts()
	if err != nil {
		return nil, err
	}
	var recorded []Revert
	for _, d := range dones {
		sha := d.commit.SHA
		if d.commit.Dirty {
			if sha, err = commitOnTop(ctx, p.RepoPath, d.commit); err != nil {
				return nil, err
			}
		}
		if sha == "" {
			continue
		}
		for target, revertSHA := range revertOf {
			if !strings.HasPrefix(sha, target) || seen[revertKey{d.ticketID, revertSHA}] {
				continue
			}
			r := Revert{TicketID: d.ticketID, Project: p.Name, SHA: sha, RevertSHA: revertSHA}
			if err := recordEvent(db, d.ticketID, "reverted", map[string]string{"sha": r.SHA, "revert_sha": r.RevertSHA}); err != nil {
				return nil, err
			}
			seen[revertKey{d.ticketID, revertSHA}] = true
			recorded = append(recorded, r)
		}
	}
	return recorded, nil
}

type recordedDone struct {
	ticketID int
	commit   doneCommit
}

// doneCommits lists the project's done events that recorded a commit. A ticket
// reopened and closed again has one per close.
func doneCommits(project string) ([]recordedDone, error) {
	rows, err := db.Query(`
		SELECT e.ticket_id, e.data FROM ticket_events e
		JOIN tickets t ON t.id = e.ticket_id
		WHERE t.project = ? AND e.event = 'done' AND e.data IS NOT NULL
		ORDER BY e.id`, project)
	if err != nil {
		return nil, fmt.Errorf("loading done events: %w", err)
	}
	defer rows.Close()
	var dones []recordedDone
	for rows.Next() {
		var d recordedDone
		var data string
		if err := rows.Scan(&d.ticketID, &data); err != nil {
			return nil, fmt.Errorf("scanning done event: %w", err)
		}
		if err := json.Unmarshal([]byte(data), &d.commit); err != nil {
			return nil, fmt.Errorf("parsing done event for ticket %d: %w", d.ticketID, err)
		}
		if d.commit.SHA != "" {
			dones = append(dones, d)
		}
	}
	return dones, rows.Err()
}

type revertKey struct {
	ticketID  int
	revertSHA string
}

func recordedReverts() (map[revertKey]bool, error) {
	rows, err := db.Query(`SELECT ticket_id, json_extract(data, '$.revert_sha') FROM ticket_events WHERE event = 'reverted'`)
	if err != nil {
		return nil, fmt.Errorf("loading reverted events: %w", err)
	}
	defer rows.Close()
	seen := map[revertKey]bool{}
	for rows.Next() {
		var k revertKey
		var sha sql.NullString
		if err := rows.Scan(&k.ticketID, &sha); err != nil {
			return nil, fmt.Errorf("scanning reverted event: %w", err)
		}
		k.revertSHA = sha.String
		seen[k] = true
	}
	return seen, rows.Err()
}

// commitOnTop is the ticket's commit for a done recorded before it was
// committed: the first commit on the recorded branch whose parent is the
// recorded HEAD. "" means none yet, or the branch or HEAD no longer exists.
// ponytail: two tickets committed on the same parent both claim the first
// one; record the commit at commit time if that starts happening.
func commitOnTop(ctx context.Context, repo string, c doneCommit) (string, error) {
	for _, ref := range []string{c.SHA, c.Branch} {
		if _, err := git(ctx, repo, "rev-parse", "--verify", "--quiet", ref+"^{commit}"); err != nil {
			return "", nil
		}
	}
	out, err := git(ctx, repo, "rev-list", "--reverse", "--parents", c.SHA+".."+c.Branch)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(out, "\n") {
		if fields := strings.Fields(line); len(fields) > 1 && fields[1] == c.SHA {
			return fields[0], nil
		}
	}
	return "", nil
}
