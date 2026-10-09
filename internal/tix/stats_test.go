package tix

import (
	"maps"
	"slices"
	"testing"
	"time"
)

func TestBuildStats(t *testing.T) {
	setupTestDB(t)
	newProjects(t, "acme", "zen")

	add := func(project string) int {
		id, err := Add("t", "", 1, project, nil)
		if err != nil {
			t.Fatalf("Add: %v", err)
		}
		return int(id)
	}
	now := time.Now().UTC()
	event := func(id int, name string, ago time.Duration, data string) {
		var d any
		if data != "" {
			d = data
		}
		if _, err := db.Exec(`INSERT INTO ticket_events (ticket_id, event, at, data) VALUES (?, ?, ?, ?)`,
			id, name, now.Add(-ago).Format("2006-01-02 15:04:05"), d); err != nil {
			t.Fatalf("insert %s: %v", name, err)
		}
	}

	a, b, waiting, old, other := add("acme"), add("acme"), add("acme"), add("acme"), add("zen")
	event(a, "claim", 50*time.Hour, "")
	event(a, "plan", 49*time.Hour, `{"tier":1,"state":"awaiting_review"}`)
	event(a, "approve", 45*time.Hour, `{"tier":1}`)
	event(a, "done_attempt", 42*time.Hour, `{"result":"fail","stage":"test","output":{"findings":[{"reviewer":"r1","blocking":true},{"reviewer":"r1","blocking":false}]}}`)
	event(a, "done_attempt", 41*time.Hour, `{"result":"pass"}`)
	event(a, "done", 40*time.Hour, "")
	event(a, "reject", 39*time.Hour, "")
	event(b, "claim", 30*time.Hour, "")
	event(b, "plan", 29*time.Hour, `{"tier":0}`)
	event(b, "done_attempt", 11*time.Hour, `{"result":"pass","output":"not json"}`)
	event(b, "done", 10*time.Hour, "")
	event(b, "reopen", 5*time.Hour, "")
	event(waiting, "plan", time.Hour, `{"tier":2,"state":"awaiting_review"}`)
	event(old, "claim", 60*24*time.Hour, "")
	event(old, "done", 59*24*time.Hour, "")
	event(other, "claim", 2*time.Hour, "")
	event(other, "done", time.Hour, "")

	since := now.Add(-30 * 24 * time.Hour)
	stats, err := BuildStats(StatsQuery{Project: "acme", Since: since})
	if err != nil {
		t.Fatalf("BuildStats: %v", err)
	}
	total := stats.Total
	intEqual := func(t *testing.T, name string, got, want int) {
		t.Helper()
		if got != want {
			t.Errorf("%s = %d, want %d", name, got, want)
		}
	}
	tests := []struct {
		name  string
		check func(t *testing.T)
	}{
		{"window and project filter tickets", func(t *testing.T) { intEqual(t, "tickets", total.Tickets, 3) }},
		{"done counts in-window closes", func(t *testing.T) { intEqual(t, "done", total.Done, 2) }},
		{"cycle time percentiles", func(t *testing.T) {
			if total.CycleP50Hours == nil || *total.CycleP50Hours != 10 || *total.CycleP90Hours != 20 {
				t.Errorf("cycle p50/p90 = %v/%v, want 10/20", total.CycleP50Hours, total.CycleP90Hours)
			}
		}},
		{"failures by stage", func(t *testing.T) {
			intEqual(t, "failures", total.DoneFailures, 1)
			intEqual(t, "test stage", total.FailuresByStage["test"], 1)
		}},
		{"blocking findings by reviewer", func(t *testing.T) {
			if total.BlockingFindings == nil || *total.BlockingFindings != 1 {
				t.Fatalf("blocking = %v, want 1", total.BlockingFindings)
			}
			intEqual(t, "r1", total.BlockingByReviewer["r1"], 1)
		}},
		{"rejects and reopens", func(t *testing.T) {
			intEqual(t, "rejects", total.Rejects, 1)
			intEqual(t, "reopens", total.Reopens, 1)
			if total.RejectRate != 0.5 {
				t.Errorf("reject rate = %v, want 0.5", total.RejectRate)
			}
		}},
		{"awaiting review time", func(t *testing.T) {
			if total.AwaitingReviewHours != 4 {
				t.Errorf("awaiting = %v, want 4", total.AwaitingReviewHours)
			}
		}},
		{"tier distribution", func(t *testing.T) {
			if want := map[string]int{"0": 1, "1": 1, "2": 1}; !maps.Equal(total.Tiers, want) {
				t.Errorf("tiers = %v, want %v", total.Tiers, want)
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, tt.check)
	}

	t.Run("by tier keeps findings absent where none were reported", func(t *testing.T) {
		stats, err := BuildStats(StatsQuery{Project: "acme", Since: since, By: StatsByTier})
		if err != nil {
			t.Fatalf("BuildStats: %v", err)
		}
		keys := []string{}
		for _, g := range stats.Groups {
			keys = append(keys, g.Key)
		}
		if !slices.Equal(keys, []string{"0", "1", "2"}) {
			t.Fatalf("keys = %v", keys)
		}
		if stats.Groups[0].BlockingFindings != nil {
			t.Errorf("tier 0 blocking = %v, want absent", *stats.Groups[0].BlockingFindings)
		}
	})

	t.Run("by week fills weeks and tracks the review queue", func(t *testing.T) {
		stats, err := BuildStats(StatsQuery{Project: "acme", Since: since, By: StatsByWeek})
		if err != nil {
			t.Fatalf("BuildStats: %v", err)
		}
		if len(stats.Groups) < 5 {
			t.Fatalf("got %d weeks, want every week since the window opened", len(stats.Groups))
		}
		last := stats.Groups[len(stats.Groups)-1]
		if last.QueueDepth == nil || *last.QueueDepth != 1 {
			t.Errorf("current queue depth = %v, want 1", last.QueueDepth)
		}
	})

	t.Run("rejects an unknown grouping", func(t *testing.T) {
		if _, err := BuildStats(StatsQuery{By: "month"}); err == nil {
			t.Error("BuildStats(by month) succeeded")
		}
	})
}

func TestBuildStatsInherited(t *testing.T) {
	setupTestDB(t)
	newProjects(t, "acme")
	ids := map[string]int{}
	for _, name := range []string{"inherited", "reviewed"} {
		id, err := Add(name, "", 1, "acme", nil)
		if err != nil {
			t.Fatalf("Add: %v", err)
		}
		ids[name] = int(id)
	}
	for _, e := range []struct {
		ticket, event, data string
	}{
		{"inherited", "plan", `{"tier":0,"inherited_from":"ac-9"}`},
		{"inherited", "done", ""},
		{"inherited", "reopen", ""},
		{"reviewed", "plan", `{"tier":0}`},
		{"reviewed", "done", ""},
	} {
		var data any
		if e.data != "" {
			data = e.data
		}
		if _, err := db.Exec(`INSERT INTO ticket_events (ticket_id, event, data) VALUES (?, ?, ?)`, ids[e.ticket], e.event, data); err != nil {
			t.Fatalf("insert %s: %v", e.event, err)
		}
	}

	stats, err := BuildStats(StatsQuery{Project: "acme", By: StatsByTier})
	if err != nil {
		t.Fatalf("BuildStats: %v", err)
	}
	reopens := map[string]float64{}
	for _, g := range stats.Groups {
		reopens[g.Key] = g.ReopenRate
	}
	if want := map[string]float64{"0": 0, "inherited": 1}; !maps.Equal(reopens, want) {
		t.Fatalf("reopen rate by tier = %v, want %v", reopens, want)
	}
}
