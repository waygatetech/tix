package tix

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Plans live in the repo at plans/<key>-<id>.md and are canonical there. tix
// keeps only the parsed frontmatter, re-read on every `tix plan`, so features
// that need a ticket's concepts or paths can answer without the checkout.

// Plan is a plan file's frontmatter plus where it was read from.
type Plan struct {
	Path             string     `json:"path" yaml:"-"`
	Ticket           string     `json:"ticket" yaml:"ticket"`
	Concepts         []string   `json:"concepts" yaml:"concepts"`
	NewConcepts      []string   `json:"new_concepts" yaml:"new_concepts"`
	Paths            []string   `json:"paths" yaml:"paths"`
	ContractsChanged []string   `json:"contracts_changed" yaml:"contracts_changed"`
	Decisions        []Decision `json:"decisions" yaml:"decisions"`
	// Review is the plan hook's ruling, set by tix rather than the file.
	Review *Review `json:"review,omitempty" yaml:"-"`
}

// Review states. A tier 0 plan has neither: it proceeds without a ruling.
const (
	ReviewAwaiting = "awaiting_review"
	ReviewApproved = "approved"
)

// Review is the tier a plan hook assigned and whether a human has ruled on it.
// Tier policy belongs to the hook; tix only records it and blocks on tier 1+.
type Review struct {
	Tier   int    `json:"tier"`
	Reason string `json:"reason,omitempty"`
	State  string `json:"state,omitempty"`
}

// Decision is a choice the plan leaves for a human to rule on.
type Decision struct {
	Q         string   `json:"q" yaml:"q"`
	Options   []string `json:"options" yaml:"options"`
	Recommend string   `json:"recommend" yaml:"recommend"`
	Why       string   `json:"why" yaml:"why"`
}

// ParsePlan reads the YAML frontmatter between the leading "---" lines. Unknown
// keys are rejected, so a misspelled field fails here instead of silently
// dropping out of the index.
func ParsePlan(data []byte) (Plan, error) {
	data = bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))
	rest, ok := bytes.CutPrefix(data, []byte("---\n"))
	if !ok {
		return Plan{}, errors.New("plan has no frontmatter: expected a leading --- line")
	}
	front, _, ok := bytes.Cut(rest, []byte("\n---"))
	if !ok {
		return Plan{}, errors.New("plan frontmatter is not closed with ---")
	}

	var plan Plan
	dec := yaml.NewDecoder(bytes.NewReader(front))
	dec.KnownFields(true)
	if err := dec.Decode(&plan); err != nil {
		return Plan{}, fmt.Errorf("parsing plan frontmatter: %w", err)
	}
	if plan.Ticket == "" {
		return Plan{}, errors.New("plan frontmatter is missing ticket")
	}
	return plan, nil
}

// DefaultPlanPath is where a ticket's plan lives in its project's repo.
func DefaultPlanPath(ticket Ticket) (string, error) {
	repo, err := repoPathFor(ticket.Project)
	if err != nil {
		return "", err
	}
	if repo == "" {
		return "", fmt.Errorf("project %q has no repo_path; pass --file", ticket.Project)
	}
	prefix, err := PrefixFor(ticket.Project)
	if err != nil {
		return "", err
	}
	return filepath.Join(repo, "plans", FormatID(prefix, ticket.ID)+".md"), nil
}

func repoPathFor(project string) (string, error) {
	projects, err := ListProjects()
	if err != nil {
		return "", err
	}
	for _, p := range projects {
		if p.Name == project {
			return p.RepoPath, nil
		}
	}
	return "", nil
}

// planHookPath finds the hook that tiers a project's plans: the repo's own
// .tix/plan-hook first, then ~/.tix/plan-hook. "" means no hook, which is tier
// 0, so review is opt-in.
func planHookPath(project string) (string, error) {
	var candidates []string
	repo, err := repoPathFor(project)
	if err != nil {
		return "", err
	}
	if repo != "" {
		candidates = append(candidates, filepath.Join(repo, tixDirName, "plan-hook"))
	}
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates, filepath.Join(home, tixDirName, "plan-hook"))
	}
	for _, path := range candidates {
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path, nil
		}
	}
	return "", nil
}

// runPlanHook sends the plan as JSON on stdin and reads back
// {"tier": 0-2, "reason": "..."}. A failing or malformed hook is an error
// rather than tier 0: a broken policy should not wave plans through.
func runPlanHook(ctx context.Context, hook string, plan Plan) (Review, error) {
	input, err := json.Marshal(plan)
	if err != nil {
		return Review{}, fmt.Errorf("encoding plan for hook: %w", err)
	}
	cmd := exec.CommandContext(ctx, hook)
	cmd.Stdin = bytes.NewReader(input)
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		return Review{}, fmt.Errorf("running plan hook %s: %w", hook, err)
	}
	var result struct {
		Tier   *int   `json:"tier"`
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal(out, &result); err != nil {
		return Review{}, fmt.Errorf("parsing plan hook %s output: %w", hook, err)
	}
	if result.Tier == nil || *result.Tier < 0 || *result.Tier > 2 {
		return Review{}, fmt.Errorf("plan hook %s must print a tier from 0 to 2", hook)
	}
	return Review{Tier: *result.Tier, Reason: result.Reason}, nil
}

// nextReview carries an approval across a re-plan only while the tier does not
// rise; anything at tier 1+ without one waits for a human.
func nextReview(prev *Review, got Review) *Review {
	if got.Tier == 0 {
		return &got
	}
	got.State = ReviewAwaiting
	if prev != nil && prev.State == ReviewApproved && got.Tier <= prev.Tier {
		got.State = ReviewApproved
	}
	return &got
}

// IndexPlan parses the plan at path (or the default path when empty), runs the
// project's plan hook on it, and replaces the ticket's cached plan. The
// returned plan's Review is awaiting_review when a human must rule first.
func IndexPlan(ctx context.Context, id int, path string) (Plan, error) {
	ticket, err := Show(id)
	if err != nil {
		return Plan{}, err
	}
	if path == "" {
		if path, err = DefaultPlanPath(ticket); err != nil {
			return Plan{}, err
		}
	}
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return Plan{}, fmt.Errorf("reading plan: %w", err)
	}
	plan, err := ParsePlan(data)
	if err != nil {
		return Plan{}, fmt.Errorf("%s: %w", path, err)
	}
	prefix, err := PrefixFor(ticket.Project)
	if err != nil {
		return Plan{}, err
	}
	if want := FormatID(prefix, id); plan.Ticket != want {
		return Plan{}, fmt.Errorf("%s: plan is for ticket %q, not %q", path, plan.Ticket, want)
	}
	plan.Path = path

	hook, err := planHookPath(ticket.Project)
	if err != nil {
		return Plan{}, err
	}
	got := Review{}
	if hook != "" {
		if got, err = runPlanHook(ctx, hook, plan); err != nil {
			return Plan{}, err
		}
	}
	var prev *Review
	if ticket.Plan != nil {
		prev = ticket.Plan.Review
	}
	plan.Review = nextReview(prev, got)

	if err := storePlan(id, plan); err != nil {
		return Plan{}, err
	}
	return plan, nil
}

// Approve records a human's sign-off on a plan awaiting review.
func Approve(id int) (Plan, error) {
	plan, err := planFor(id)
	if err != nil {
		return Plan{}, err
	}
	if plan == nil || plan.Review == nil || plan.Review.State != ReviewAwaiting {
		return Plan{}, fmt.Errorf("ticket %d has no plan awaiting review", id)
	}
	plan.Review.State = ReviewApproved
	if err := storePlan(id, *plan); err != nil {
		return Plan{}, err
	}
	return *plan, nil
}

func storePlan(id int, plan Plan) error {
	encoded, err := json.Marshal(plan)
	if err != nil {
		return fmt.Errorf("encoding plan: %w", err)
	}
	if _, err := db.Exec(`UPDATE tickets SET plan = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, string(encoded), id); err != nil {
		return fmt.Errorf("storing plan: %w", err)
	}
	return nil
}

// planFor returns a ticket's cached plan, or nil when none has been indexed.
func planFor(id int) (*Plan, error) {
	var raw sql.NullString
	if err := db.QueryRow(`SELECT plan FROM tickets WHERE id = ?`, id).Scan(&raw); err != nil {
		return nil, fmt.Errorf("loading plan: %w", err)
	}
	if !raw.Valid {
		return nil, nil
	}
	var plan Plan
	if err := json.Unmarshal([]byte(raw.String), &plan); err != nil {
		return nil, fmt.Errorf("decoding cached plan: %w", err)
	}
	return &plan, nil
}
