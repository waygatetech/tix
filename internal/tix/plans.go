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
	"strings"

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
	if err := validateConcepts(ticket.Project, plan); err != nil {
		return Plan{}, fmt.Errorf("%s: %w", path, err)
	}

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

	// The lease check and the store share a transaction, so two agents
	// planning the same concept cannot both pass the check.
	tx, err := db.Begin()
	if err != nil {
		return Plan{}, fmt.Errorf("beginning plan transaction: %w", err)
	}
	defer tx.Rollback()
	if err := checkLeases(tx, ticket, prefix, plan); err != nil {
		return Plan{}, err
	}
	if err := storePlan(tx, id, plan); err != nil {
		return Plan{}, err
	}
	if err := recordEvent(tx, id, "plan", plan.Review); err != nil {
		return Plan{}, err
	}
	if err := tx.Commit(); err != nil {
		return Plan{}, fmt.Errorf("committing plan: %w", err)
	}
	return plan, nil
}

// Lease is a concept an in_progress ticket's plan claims within its project.
type Lease struct {
	Project  string `json:"project"`
	Concept  string `json:"concept"`
	TicketID int    `json:"ticket_id"`
}

// Leases are derived from cached plans rather than stored: a ticket holds its
// plan's concepts and new_concepts exactly while it is in_progress, so done,
// rm, and a return to open release them with nothing to clean up, and a
// crashed agent's leases surface through its stale ticket.
const leasesQuery = `
	SELECT t.project, c.value, t.id FROM tickets t, json_each(t.plan, '$.concepts') c
	 WHERE t.status = 'in_progress' AND t.plan IS NOT NULL AND c.value IS NOT NULL AND t.project = ?
	UNION
	SELECT t.project, c.value, t.id FROM tickets t, json_each(t.plan, '$.new_concepts') c
	 WHERE t.status = 'in_progress' AND t.plan IS NOT NULL AND c.value IS NOT NULL AND t.project = ?
	ORDER BY 2, 3`

// Leases lists the project's active concept leases, by concept.
func Leases(project string) ([]Lease, error) {
	project, err := ResolveProject(project)
	if err != nil {
		return nil, err
	}
	return queryLeases(db, project)
}

func queryLeases(q sqlQueryer, project string) ([]Lease, error) {
	rows, err := q.Query(leasesQuery, project, project)
	if err != nil {
		return nil, fmt.Errorf("listing leases: %w", err)
	}
	defer rows.Close()
	var leases []Lease
	for rows.Next() {
		var lease Lease
		if err := rows.Scan(&lease.Project, &lease.Concept, &lease.TicketID); err != nil {
			return nil, fmt.Errorf("scanning lease: %w", err)
		}
		leases = append(leases, lease)
	}
	return leases, rows.Err()
}

// checkLeases fails when another in_progress ticket in the project already
// leases a concept the plan declares.
// prefix is the project's, resolved by the caller: the transaction q belongs
// to holds the only connection.
func checkLeases(q sqlQueryer, ticket Ticket, prefix string, plan Plan) error {
	wanted := map[string]bool{}
	for _, concepts := range [][]string{plan.Concepts, plan.NewConcepts} {
		for _, concept := range concepts {
			wanted[concept] = true
		}
	}
	if len(wanted) == 0 {
		return nil
	}
	leases, err := queryLeases(q, ticket.Project)
	if err != nil {
		return err
	}
	for _, lease := range leases {
		if lease.TicketID != ticket.ID && wanted[lease.Concept] {
			return fmt.Errorf("concept %q is leased by %s in project %q; wait for it or coordinate",
				lease.Concept, FormatID(prefix, lease.TicketID), lease.Project)
		}
	}
	return nil
}

// Concept is an entry in the repo's concepts.yaml registry. LeasedBy lists the
// in_progress tickets whose plans hold it.
type Concept struct {
	Name     string `json:"name" yaml:"name"`
	Owner    string `json:"owner,omitempty" yaml:"owner"`
	Module   string `json:"module,omitempty" yaml:"module"`
	LeasedBy []int  `json:"leased_by,omitempty" yaml:"-"`
}

// ErrNoRegistry means the project's repo has no concepts.yaml, so concepts go
// unvalidated: repos adopt the registry when they are ready.
var ErrNoRegistry = errors.New("no " + conceptsFile + " registry")

// readRegistry loads concepts.yaml from the project's repo_path.
func readRegistry(project string) ([]Concept, error) {
	repo, err := repoPathFor(project)
	if err != nil {
		return nil, err
	}
	if repo == "" {
		return nil, fmt.Errorf("project %q has no repo_path: %w", project, ErrNoRegistry)
	}
	path := filepath.Join(repo, conceptsFile)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("%s: %w", repo, ErrNoRegistry)
	}
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	concepts, err := parseConcepts(data)
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	return concepts, nil
}

// validateConcepts rejects plan concepts the registry does not define, which
// catches a plan naming an existing thing differently. Names in new_concepts
// are exempt: the plan is adding them.
func validateConcepts(project string, plan Plan) error {
	registry, err := readRegistry(project)
	if errors.Is(err, ErrNoRegistry) {
		fmt.Fprintf(os.Stderr, "warning: %v; skipping concept validation\n", err)
		return nil
	}
	if err != nil {
		return err
	}
	known := map[string]bool{}
	for _, c := range registry {
		known[c.Name] = true
	}
	for _, c := range plan.NewConcepts {
		known[c] = true
	}
	var unknown []string
	for _, c := range plan.Concepts {
		if !known[c] {
			unknown = append(unknown, c)
		}
	}
	if len(unknown) > 0 {
		return fmt.Errorf("concepts not in %s: %s; use a registered name (tix concepts) or list it in new_concepts",
			conceptsFile, strings.Join(unknown, ", "))
	}
	return nil
}

// InitRegistry creates an empty concepts.yaml in dir, refusing to overwrite
// one that exists, and returns its path.
func InitRegistry(dir string) (string, error) {
	path := filepath.Join(dir, conceptsFile)
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if errors.Is(err, os.ErrExist) {
		return "", fmt.Errorf("%s already exists", path)
	}
	if err != nil {
		return "", fmt.Errorf("creating %s: %w", path, err)
	}
	_, err = f.WriteString(registryTemplate)
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return "", fmt.Errorf("writing %s: %w", path, err)
	}
	return path, nil
}

const registryTemplate = `# Concepts this repo defines. tix plan rejects plan concepts missing here
# unless the plan lists them in new_concepts. Entries look like:
#
# auth:
#   owner: team-a
#   module: internal/auth
`

// Concepts lists the project's registry with each concept's active leases.
func Concepts(project string) ([]Concept, error) {
	project, err := ResolveProject(project)
	if err != nil {
		return nil, err
	}
	concepts, err := readRegistry(project)
	if err != nil {
		return nil, err
	}
	leases, err := queryLeases(db, project)
	if err != nil {
		return nil, err
	}
	for i := range concepts {
		for _, lease := range leases {
			if lease.Concept == concepts[i].Name {
				concepts[i].LeasedBy = append(concepts[i].LeasedBy, lease.TicketID)
			}
		}
	}
	return concepts, nil
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
	tx, err := db.Begin()
	if err != nil {
		return Plan{}, fmt.Errorf("beginning approve transaction: %w", err)
	}
	defer tx.Rollback()
	if err := storePlan(tx, id, *plan); err != nil {
		return Plan{}, err
	}
	if err := recordEvent(tx, id, "approve", map[string]int{"tier": plan.Review.Tier}); err != nil {
		return Plan{}, err
	}
	if err := tx.Commit(); err != nil {
		return Plan{}, fmt.Errorf("committing approval: %w", err)
	}
	return *plan, nil
}

func storePlan(exec sqlExecer, id int, plan Plan) error {
	encoded, err := json.Marshal(plan)
	if err != nil {
		return fmt.Errorf("encoding plan: %w", err)
	}
	if _, err := exec.Exec(`UPDATE tickets SET plan = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, string(encoded), id); err != nil {
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
