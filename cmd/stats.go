package cmd

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"github.com/waygatetech/tix/internal/tix"
)

var (
	statsSince string
	statsBy    string
)

var statsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Report workflow outcomes from ticket history",
	Long: `Fold ticket_events from the last --since into workflow outcomes: cycle
time from claim to done, done-hook failures by stage, blocking review findings
by reviewer, rejects, reopens, reverts, plan tiers, and time awaiting review.

Blocking findings come from a done hook that prints a JSON object with a
top-level "findings" list of {"reviewer", "blocking"}; without one they are
shown as "-", not zero.

--by tier|concept|week repeats the row per group. Weekly rows also show the
human review queue depth at the end of each week.`,
	PersistentPreRunE: initDB,
	RunE: func(cmd *cobra.Command, args []string) error {
		window, err := parseSince(statsSince)
		if err != nil {
			return err
		}
		stats, err := tix.BuildStats(tix.StatsQuery{Project: project, Since: time.Now().UTC().Add(-window), By: statsBy})
		if err != nil {
			return err
		}
		if jsonOutput {
			return printJSON(stats, compactOutput, fieldsOutput)
		}
		return printStats(os.Stdout, stats)
	},
}

// parseSince reads a window as Nd, Nw, or a Go duration.
func parseSince(s string) (time.Duration, error) {
	unit := map[byte]time.Duration{'d': 24 * time.Hour, 'w': 7 * 24 * time.Hour}
	if len(s) > 1 {
		if u, ok := unit[s[len(s)-1]]; ok {
			n, err := strconv.Atoi(s[:len(s)-1])
			if err == nil && n > 0 {
				return time.Duration(n) * u, nil
			}
		}
	}
	d, err := time.ParseDuration(s)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("invalid --since %q: use a positive window like 30d, 4w, or 72h", s)
	}
	return d, nil
}

func printStats(w io.Writer, stats tix.Stats) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	header := "GROUP\tTICKETS\tDONE\tCYCLE P50\tCYCLE P90\tFAILS/TICKET\tBLOCKING/TICKET\tREJECTS\tREOPENS\tREVERTS\tAWAITING REVIEW"
	if stats.By == tix.StatsByWeek {
		header += "\tQUEUE"
	}
	fmt.Fprintln(tw, header)
	for _, g := range append([]tix.StatsGroup{stats.Total}, stats.Groups...) {
		line := fmt.Sprintf("%s\t%d\t%d\t%s\t%s\t%.2f\t%s\t%s\t%s\t%s\t%.1fh",
			g.Key, g.Tickets, g.Done, hours(g.CycleP50Hours), hours(g.CycleP90Hours), g.FailuresPerTicket,
			perTicket(g.BlockingPerTicket), countRate(g.Rejects, g.RejectRate), countRate(g.Reopens, g.ReopenRate),
			countRate(g.Reverts, g.RevertRate), g.AwaitingReviewHours)
		if g.QueueDepth != nil {
			line += fmt.Sprintf("\t%d", *g.QueueDepth)
		} else if stats.By == tix.StatsByWeek {
			line += "\t-"
		}
		fmt.Fprintln(tw, line)
	}
	if err := tw.Flush(); err != nil {
		return fmt.Errorf("writing stats: %w", err)
	}
	for _, breakdown := range []struct {
		label  string
		counts map[string]int
	}{
		{"failures by stage", stats.Total.FailuresByStage},
		{"blocking by reviewer", stats.Total.BlockingByReviewer},
		{"tiers", stats.Total.Tiers},
	} {
		if len(breakdown.counts) > 0 {
			fmt.Fprintf(w, "%s: %s\n", breakdown.label, joinCounts(breakdown.counts))
		}
	}
	return nil
}

func hours(h *float64) string {
	if h == nil {
		return "-"
	}
	return fmt.Sprintf("%.1fh", *h)
}

func perTicket(v *float64) string {
	if v == nil {
		return "-"
	}
	return fmt.Sprintf("%.2f", *v)
}

func countRate(n int, rate float64) string {
	return fmt.Sprintf("%d (%.0f%%)", n, rate*100)
}

func joinCounts(counts map[string]int) string {
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = fmt.Sprintf("%s=%d", k, counts[k])
	}
	return strings.Join(parts, " ")
}

func init() {
	statsCmd.Flags().StringVarP(&project, "project", "p", "", "only count this project's tickets (default: every project)")
	statsCmd.Flags().StringVar(&statsSince, "since", "30d", "window to report, like 30d, 4w, or 72h")
	statsCmd.Flags().StringVar(&statsBy, "by", "", "group rows by tier, concept, or week")
	statsCmd.Flags().BoolVar(&jsonOutput, "json", false, "output stats as JSON")
	statsCmd.Flags().BoolVar(&compactOutput, "compact", false, "minify JSON output")
	statsCmd.Flags().StringVar(&fieldsOutput, "fields", "", "comma-separated JSON fields to include")
	rootCmd.AddCommand(statsCmd)
}
