package tix

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"time"
)

// Stats groupings tix stats accepts for --by.
const (
	StatsByTier    = "tier"
	StatsByConcept = "concept"
	StatsByWeek    = "week"
)

// StatsQuery selects which events tix stats folds. Since bounds the events
// counted; a ticket's earlier history still sets its claim time and tier.
type StatsQuery struct {
	Project string
	Since   time.Time
	By      string
}

// Stats is the workflow outcome report: Total over the whole window, plus one
// row per group when By is set.
type Stats struct {
	Project string       `json:"project,omitempty"`
	Since   time.Time    `json:"since"`
	By      string       `json:"by,omitempty"`
	Total   StatsGroup   `json:"total"`
	Groups  []StatsGroup `json:"groups,omitempty"`
}

// StatsGroup is one row of outcomes. Rates are per ticket done in the row.
// Blocking findings are nil when no done hook in the row reported findings,
// which is different from reporting none. QueueDepth is set for week rows only:
// tickets awaiting review when the week ended.
type StatsGroup struct {
	Key                 string         `json:"key"`
	Tickets             int            `json:"tickets"`
	Done                int            `json:"done"`
	CycleP50Hours       *float64       `json:"cycle_p50_hours,omitempty"`
	CycleP90Hours       *float64       `json:"cycle_p90_hours,omitempty"`
	DoneFailures        int            `json:"done_failures"`
	FailuresPerTicket   float64        `json:"failures_per_ticket"`
	FailuresByStage     map[string]int `json:"failures_by_stage,omitempty"`
	BlockingFindings    *int           `json:"blocking_findings,omitempty"`
	BlockingPerTicket   *float64       `json:"blocking_per_ticket,omitempty"`
	BlockingByReviewer  map[string]int `json:"blocking_by_reviewer,omitempty"`
	Rejects             int            `json:"rejects"`
	RejectRate          float64        `json:"reject_rate"`
	Reopens             int            `json:"reopens"`
	ReopenRate          float64        `json:"reopen_rate"`
	Reverts             int            `json:"reverts"`
	RevertRate          float64        `json:"revert_rate"`
	Tiers               map[string]int `json:"tiers,omitempty"`
	AwaitingReviewHours float64        `json:"awaiting_review_hours"`
	QueueDepth          *int           `json:"queue_depth,omitempty"`

	tickets map[int]bool
	cycles  []float64
}

type statsEvent struct {
	ticketID int
	event    string
	at       time.Time
	data     []byte
}

// statsTicket is what earlier events say about a ticket when a later one is
// folded.
type statsTicket struct {
	firstClaim    time.Time
	awaitingSince time.Time
	tier          string
	awaiting      []interval
}

type interval struct{ start, end time.Time }

// findingsOutput is the part of a done hook's JSON stdout stats reads. A nil
// Findings means the hook did not report any.
type findingsOutput struct {
	Findings []struct {
		Reviewer string `json:"reviewer"`
		Blocking bool   `json:"blocking"`
	} `json:"findings"`
}

// BuildStats folds ticket_events into workflow outcomes.
func BuildStats(q StatsQuery) (Stats, error) {
	switch q.By {
	case "", StatsByTier, StatsByConcept, StatsByWeek:
	default:
		return Stats{}, fmt.Errorf("unknown --by %q: use tier, concept, or week", q.By)
	}
	if q.Project != "" {
		resolved, err := ResolveProject(q.Project)
		if err != nil {
			return Stats{}, err
		}
		q.Project = resolved
	}
	events, err := loadStatsEvents(q.Project)
	if err != nil {
		return Stats{}, err
	}
	var concepts map[int][]string
	if q.By == StatsByConcept {
		if concepts, err = loadStatsConcepts(q.Project); err != nil {
			return Stats{}, err
		}
	}

	// Tier is the latest plan's, so every event of a ticket lands in one tier.
	// Plans that rode on an epic's approval get their own row.
	states := map[int]*statsTicket{}
	for _, e := range events {
		if states[e.ticketID] == nil {
			states[e.ticketID] = &statsTicket{tier: "none"}
		}
		if e.event == "plan" {
			var r Review
			if json.Unmarshal(e.data, &r) == nil {
				states[e.ticketID].tier = strconv.Itoa(r.Tier)
				if r.InheritedFrom != "" {
					states[e.ticketID].tier = "inherited"
				}
			}
		}
	}

	now := time.Now().UTC()
	total := newStatsGroup("all")
	groups := map[string]*StatsGroup{}
	// Empty weeks get rows too, so a trend line shows the gap.
	if q.By == StatsByWeek && !q.Since.IsZero() {
		for w := weekStart(q.Since); !w.After(now); w = w.AddDate(0, 0, 7) {
			key := w.Format("2006-01-02")
			groups[key] = newStatsGroup(key)
		}
	}
	for _, e := range events {
		st := states[e.ticketID]
		var cycle, awaited float64 = -1, -1
		switch e.event {
		case "claim":
			if st.firstClaim.IsZero() {
				st.firstClaim = e.at
			}
		case "done":
			if !st.firstClaim.IsZero() {
				cycle = e.at.Sub(st.firstClaim).Hours()
			}
		case "plan":
			var r Review
			if json.Unmarshal(e.data, &r) == nil && r.State == ReviewAwaiting && st.awaitingSince.IsZero() {
				st.awaitingSince = e.at
			}
		case "approve":
			if !st.awaitingSince.IsZero() {
				awaited = e.at.Sub(st.awaitingSince).Hours()
				st.awaiting = append(st.awaiting, interval{st.awaitingSince, e.at})
				st.awaitingSince = time.Time{}
			}
		}
		if e.at.Before(q.Since) {
			continue
		}
		var keys []string
		switch q.By {
		case StatsByTier:
			keys = []string{st.tier}
		case StatsByConcept:
			keys = concepts[e.ticketID]
			if len(keys) == 0 {
				keys = []string{"(none)"}
			}
		case StatsByWeek:
			keys = []string{weekStart(e.at).Format("2006-01-02")}
		}
		total.add(e, st, cycle, awaited)
		for _, k := range keys {
			if groups[k] == nil {
				groups[k] = newStatsGroup(k)
			}
			groups[k].add(e, st, cycle, awaited)
		}
	}

	stats := Stats{Project: q.Project, Since: q.Since, By: q.By, Total: total.finish(states)}
	keys := make([]string, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		g := groups[k].finish(states)
		if q.By == StatsByWeek {
			end := mustParseWeek(k).AddDate(0, 0, 7)
			if end.After(now) {
				end = now
			}
			depth := queueDepth(states, end)
			g.QueueDepth = &depth
		}
		stats.Groups = append(stats.Groups, g)
	}
	return stats, nil
}

func newStatsGroup(key string) *StatsGroup {
	return &StatsGroup{Key: key, tickets: map[int]bool{}}
}

// add counts one in-window event. cycle and awaited are hours, or -1 when the
// event closes no claim or review wait.
func (g *StatsGroup) add(e statsEvent, st *statsTicket, cycle, awaited float64) {
	g.tickets[e.ticketID] = true
	switch e.event {
	case "done":
		g.Done++
		if cycle >= 0 {
			g.cycles = append(g.cycles, cycle)
		}
	case "done_attempt":
		var attempt struct {
			Result string          `json:"result"`
			Stage  string          `json:"stage"`
			Output json.RawMessage `json:"output"`
		}
		if json.Unmarshal(e.data, &attempt) != nil {
			return
		}
		if attempt.Result == "fail" {
			g.DoneFailures++
			if g.FailuresByStage == nil {
				g.FailuresByStage = map[string]int{}
			}
			g.FailuresByStage[attempt.Stage]++
		}
		var out findingsOutput
		if json.Unmarshal(attempt.Output, &out) != nil || out.Findings == nil {
			return
		}
		if g.BlockingFindings == nil {
			g.BlockingFindings = new(int)
			g.BlockingByReviewer = map[string]int{}
		}
		for _, f := range out.Findings {
			if f.Blocking {
				*g.BlockingFindings++
				g.BlockingByReviewer[f.Reviewer]++
			}
		}
	case "reject":
		g.Rejects++
	case "reopen":
		g.Reopens++
	case "reverted":
		g.Reverts++
	case "approve":
		if awaited >= 0 {
			g.AwaitingReviewHours += awaited
		}
	}
}

func (g *StatsGroup) finish(states map[int]*statsTicket) StatsGroup {
	g.Tickets = len(g.tickets)
	for id := range g.tickets {
		if g.Tiers == nil {
			g.Tiers = map[string]int{}
		}
		g.Tiers[states[id].tier]++
	}
	sort.Float64s(g.cycles)
	g.CycleP50Hours = percentile(g.cycles, 0.5)
	g.CycleP90Hours = percentile(g.cycles, 0.9)
	g.FailuresPerTicket = ratio(g.DoneFailures, g.Tickets)
	g.RejectRate = ratio(g.Rejects, g.Done)
	g.ReopenRate = ratio(g.Reopens, g.Done)
	g.RevertRate = ratio(g.Reverts, g.Done)
	if g.BlockingFindings != nil {
		per := ratio(*g.BlockingFindings, g.Tickets)
		g.BlockingPerTicket = &per
	}
	return *g
}

// percentile is nearest-rank over sorted values, nil when there are none.
func percentile(sorted []float64, p float64) *float64 {
	if len(sorted) == 0 {
		return nil
	}
	v := sorted[int(math.Ceil(p*float64(len(sorted))))-1]
	return &v
}

func ratio(n, d int) float64 {
	if d == 0 {
		return 0
	}
	return float64(n) / float64(d)
}

// weekStart is the Monday 00:00 UTC that begins t's week.
func weekStart(t time.Time) time.Time {
	day := t.UTC().Truncate(24 * time.Hour)
	return day.AddDate(0, 0, -((int(day.Weekday()) + 6) % 7))
}

func mustParseWeek(key string) time.Time {
	t, _ := time.Parse("2006-01-02", key)
	return t
}

// queueDepth counts tickets waiting on a human review at instant at.
func queueDepth(states map[int]*statsTicket, at time.Time) int {
	n := 0
	for _, st := range states {
		waits := st.awaiting
		if !st.awaitingSince.IsZero() {
			waits = append(waits, interval{start: st.awaitingSince})
		}
		for _, w := range waits {
			if w.start.Before(at) && (w.end.IsZero() || !w.end.Before(at)) {
				n++
				break
			}
		}
	}
	return n
}

// loadStatsEvents reads every event, oldest first. Without a project filter a
// removed ticket's history still counts; with one, it has no project to match.
// ponytail: loads the whole table, push the window into SQL if it grows large.
func loadStatsEvents(project string) ([]statsEvent, error) {
	rows, err := db.Query(`SELECT e.ticket_id, e.event, e.at, COALESCE(e.data, '')
		FROM ticket_events e LEFT JOIN tickets t ON t.id = e.ticket_id
		WHERE ? = '' OR t.project = ? ORDER BY e.id`, project, project)
	if err != nil {
		return nil, fmt.Errorf("loading events: %w", err)
	}
	defer rows.Close()
	var events []statsEvent
	for rows.Next() {
		var e statsEvent
		var data string
		if err := rows.Scan(&e.ticketID, &e.event, &e.at, &data); err != nil {
			return nil, fmt.Errorf("scanning event: %w", err)
		}
		e.at = e.at.UTC()
		e.data = []byte(data)
		events = append(events, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("loading events: %w", err)
	}
	return events, nil
}

// loadStatsConcepts maps tickets to the concepts their current plan declares.
// ponytail: a reopened ticket's plan is cleared, so it groups as (none) until
// replanned; record concepts on plan events if that skews the numbers.
func loadStatsConcepts(project string) (map[int][]string, error) {
	rows, err := db.Query(`SELECT tickets.id, c.value FROM tickets, `+planConcepts+`
		WHERE (? = '' OR tickets.project = ?) AND c.type = 'text' ORDER BY tickets.id, p.column1, c.key`, project, project)
	if err != nil {
		return nil, fmt.Errorf("loading plan concepts: %w", err)
	}
	defer rows.Close()
	concepts := map[int][]string{}
	for rows.Next() {
		var id int
		var concept string
		if err := rows.Scan(&id, &concept); err != nil {
			return nil, fmt.Errorf("loading plan concepts: %w", err)
		}
		concepts[id] = append(concepts[id], concept)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("loading plan concepts: %w", err)
	}
	return concepts, nil
}
