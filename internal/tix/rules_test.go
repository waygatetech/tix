package tix

import (
	"errors"
	"strings"
	"testing"
)

// Every validator check must cite exactly one registered rule, and every
// registered rule must be raised by some check.
func TestRulesCoverValidatorChecks(t *testing.T) {
	ids := map[string]bool{}
	for _, rule := range Rules() {
		if ids[rule.ID] {
			t.Fatalf("duplicate rule id %q", rule.ID)
		}
		if rule.Severity != "error" && rule.Severity != "warning" {
			t.Fatalf("rule %q has severity %q", rule.ID, rule.Severity)
		}
		ids[rule.ID] = true
	}

	tests := []struct {
		name string
		err  func() error
		want string
	}{
		{name: "empty title", err: func() error { return CheckDraft("", "b") }, want: "title-required"},
		{name: "empty body", err: func() error { return CheckDraft("t", "") }, want: "body-required"},
		{name: "priority too high", err: func() error { _, err := NewTicket("t", "b", 6, "p", nil); return err }, want: "priority-range"},
		{name: "priority negative", err: func() error { _, err := NewTicket("t", "b", -1, "p", nil); return err }, want: "priority-range"},
	}
	raised := map[string]bool{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var ruleErr *RuleError
			if err := tt.err(); !errors.As(err, &ruleErr) {
				t.Fatalf("got %v, want a RuleError", err)
			}
			if ruleErr.Rule.ID != tt.want || !ids[ruleErr.Rule.ID] {
				t.Fatalf("got rule %q, want registered %q", ruleErr.Rule.ID, tt.want)
			}
			if !strings.Contains(ruleErr.Error(), "["+tt.want+"]") {
				t.Fatalf("message %q lacks rule id", ruleErr.Error())
			}
			raised[tt.want] = true
		})
	}
	for id := range ids {
		if !raised[id] {
			t.Errorf("rule %q is registered but no check raises it", id)
		}
	}
}

func TestCheckDraftAcceptsValidDraft(t *testing.T) {
	if err := CheckDraft("t", "b"); err != nil {
		t.Fatal(err)
	}
}

func TestTicketTemplateHeadings(t *testing.T) {
	for _, heading := range []string{"## Context", "## Scope", "## Out of scope", "## Acceptance criteria", "## Relevant rules"} {
		if !strings.Contains(TicketTemplate, heading) {
			t.Errorf("template lacks %q", heading)
		}
	}
}
