package cmd

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/waygatetech/tix/internal/tix"
)

// requireProjectMatch rejects a ticket that belongs to a project other than the
// one the caller named. Commands that take a bare id act on whatever ticket
// carries that number, in whichever repo's backlog; passing --project states
// the intended target out loud and turns a cross-project hit into an error
// instead of a silent mutation. An empty name means the caller said nothing,
// so nothing is checked.
func requireProjectMatch(ticket tix.Ticket, named string) error {
	if strings.TrimSpace(named) == "" {
		return nil
	}
	want, err := tix.ResolveProject(named)
	if err != nil {
		return err
	}
	if ticket.Project == want {
		return nil
	}
	prefix, err := tix.PrefixFor(ticket.Project)
	if err != nil {
		return err
	}
	return fmt.Errorf(
		"ticket %d belongs to project %q (%s), not %q",
		ticket.ID, ticket.Project, tix.FormatID(prefix, ticket.ID), want,
	)
}

// ticketContext names the project and title a mutation touched, so a one-line
// confirmation says which backlog just changed rather than leaving the caller
// to infer it from a bare number.
func ticketContext(ticket tix.Ticket) string {
	return fmt.Sprintf("(%s: %s)", ticket.Project, ticket.Title)
}

// projectScope answers which project a command operates on. An explicit
// --project always wins, so filing into another repo's backlog stays something
// the caller says out loud; without one the working directory decides, which is
// what lets the flag disappear while you are sitting in a registered repo.
func projectScope() (string, error) {
	if strings.TrimSpace(project) != "" {
		return project, nil
	}

	dir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("--project is required: working directory unavailable: %w", err)
	}
	resolved, err := tix.ProjectForDir(dir)
	if errors.Is(err, tix.ErrNoProjectForDir) {
		return "", fmt.Errorf(
			"--project is required: no project's repo path contains %s"+
				" (register this one with `tix projects set-repo-path NAME`)",
			dir,
		)
	}
	if err != nil {
		return "", err
	}
	return resolved, nil
}
