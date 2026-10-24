package tix

import "fmt"

// TicketTemplate is the canonical shape of a ticket body.
const TicketTemplate = `## Context
Why this work exists: the problem, who has it, and what it blocks.

## Scope
- What this ticket changes, one item per line.

## Out of scope
- What it deliberately leaves alone, so reviewers can hold the line.

## Acceptance criteria
- [ ] One observable, testable statement of done per item.

## Relevant rules
Repo or team rules that apply, or "None."
`

// Rule is one check tix runs on a ticket draft.
type Rule struct {
	ID          string `json:"id"`
	Field       string `json:"field"`
	Description string `json:"description"`
	Severity    string `json:"severity"` // error or warning
}

// Rules is the registry every draft check cites. tix rules prints it.
func Rules() []Rule {
	return []Rule{
		{ID: "title-required", Field: "title", Description: "A ticket needs a non-empty title.", Severity: "error"},
		{ID: "body-required", Field: "body", Description: "A ticket needs a non-empty body.", Severity: "error"},
		{ID: "priority-range", Field: "priority", Description: "Priority is an integer from 0 (most urgent) to 5.", Severity: "error"},
	}
}

// RuleError is a draft that breaks a registered rule.
type RuleError struct {
	Rule Rule
	Msg  string
}

func (e *RuleError) Error() string {
	return fmt.Sprintf("%s [%s]", e.Msg, e.Rule.ID)
}

// violation reports a broken rule. An id missing from Rules is a bug in tix,
// not in the draft, so it panics.
func violation(id, format string, args ...any) error {
	for _, rule := range Rules() {
		if rule.ID == id {
			return &RuleError{Rule: rule, Msg: fmt.Sprintf(format, args...)}
		}
	}
	panic(fmt.Sprintf("tix: unregistered rule %q", id))
}

// CheckDraft checks a draft's title and body before anything resolves its
// project, parent, or dependencies.
func CheckDraft(title, body string) error {
	if title == "" {
		return violation("title-required", "ticket title is required")
	}
	if body == "" {
		return violation("body-required", "ticket body is required")
	}
	return nil
}
