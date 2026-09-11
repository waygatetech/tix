package cmd

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/waygatetech/tix/internal/tix"
)

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
