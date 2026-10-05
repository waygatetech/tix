package tix

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
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
	projects, err := ListProjects()
	if err != nil {
		return "", err
	}
	repo := ""
	for _, p := range projects {
		if p.Name == ticket.Project {
			repo = p.RepoPath
		}
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

// IndexPlan parses the plan at path (or the default path when empty) and
// replaces the ticket's cached plan with it.
func IndexPlan(id int, path string) (Plan, error) {
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

	encoded, err := json.Marshal(plan)
	if err != nil {
		return Plan{}, fmt.Errorf("encoding plan: %w", err)
	}
	if _, err := db.Exec(`UPDATE tickets SET plan = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, string(encoded), id); err != nil {
		return Plan{}, fmt.Errorf("storing plan: %w", err)
	}
	return plan, nil
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
