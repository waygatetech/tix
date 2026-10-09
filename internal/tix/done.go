package tix

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// doneOutputCap bounds how much hook stdout a done_attempt event keeps.
const doneOutputCap = 64 << 10

// doneAttempt is a done_attempt event's data. Output is the hook's stdout,
// kept as JSON when it parses and as a string otherwise; tix reads nothing
// from it but an optional top-level "stage".
type doneAttempt struct {
	Result string `json:"result"`
	Stage  string `json:"stage,omitempty"`
	Output any    `json:"output,omitempty"`
}

// doneCommit is a done event's data: where the repo stood when the ticket
// closed. Dirty means the change was not committed yet, so audit attributes the
// ticket to the commit made on top of SHA.
type doneCommit struct {
	SHA    string `json:"sha"`
	Branch string `json:"branch"`
	Dirty  bool   `json:"dirty"`
}

// headCommit is nil when repo is not a git checkout with a commit: recording
// where a ticket landed is best effort and never blocks done.
func headCommit(ctx context.Context, repo string) *doneCommit {
	if repo == "" {
		return nil
	}
	sha, err := git(ctx, repo, "rev-parse", "HEAD")
	if err != nil {
		return nil
	}
	branch, err := git(ctx, repo, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return nil
	}
	status, err := git(ctx, repo, "status", "--porcelain")
	if err != nil {
		return nil
	}
	return &doneCommit{
		SHA:    strings.TrimSpace(sha),
		Branch: strings.TrimSpace(branch),
		Dirty:  strings.TrimSpace(status) != "",
	}
}

// RunDone runs the project's done hook (.tix/done-hook in its repo, else
// ~/.tix/done-hook) with the ticket ID as its argument, from the repo
// directory. A failing hook records a failed done_attempt and leaves the
// ticket open; a passing one, or no hook, records a pass and closes it, with
// the repo's HEAD on the done event.
func RunDone(ctx context.Context, id int) error {
	ticket, err := Show(id)
	if err != nil {
		return err
	}
	hook, err := hookPath(ticket.Project, "done-hook")
	if err != nil {
		return err
	}
	repo, err := repoPathFor(ticket.Project)
	if err != nil {
		return err
	}
	attempt := doneAttempt{Result: "pass"}
	if hook == "" {
		return closeTicket(id, &attempt, headCommit(ctx, repo))
	}
	cmd := exec.CommandContext(ctx, hook, strconv.Itoa(id))
	cmd.Dir = repo
	cmd.Stderr = os.Stderr
	out, runErr := cmd.Output()
	if len(out) > doneOutputCap {
		out = out[:doneOutputCap]
	}
	attempt.Output = hookOutput(out)
	if runErr == nil {
		return closeTicket(id, &attempt, headCommit(ctx, repo))
	}
	attempt.Result = "fail"
	attempt.Stage = failedStage(hook, out)
	if err := recordEvent(db, id, "done_attempt", attempt); err != nil {
		return err
	}
	return fmt.Errorf("done hook %s failed at %s: %w\n%s", hook, attempt.Stage, runErr, out)
}

// hookOutput keeps JSON stdout as JSON so tix stats can query it, and anything
// else as a string. Empty output is omitted.
func hookOutput(out []byte) any {
	if len(bytes.TrimSpace(out)) == 0 {
		return nil
	}
	if json.Valid(out) {
		return json.RawMessage(out)
	}
	return string(out)
}

// failedStage is the hook's own "stage" when its stdout is a JSON object
// naming one, else the hook's file name.
func failedStage(hook string, out []byte) string {
	var result struct {
		Stage string `json:"stage"`
	}
	if json.Unmarshal(out, &result) == nil && result.Stage != "" {
		return result.Stage
	}
	return filepath.Base(hook)
}
