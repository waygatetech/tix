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

// RunDone runs the project's done hook (.tix/done-hook in its repo, else
// ~/.tix/done-hook) with the ticket ID as its argument, from the repo
// directory. A failing hook records a failed done_attempt and leaves the
// ticket open; a passing one, or no hook, records a pass and closes it.
func RunDone(ctx context.Context, id int) error {
	ticket, err := Show(id)
	if err != nil {
		return err
	}
	hook, err := hookPath(ticket.Project, "done-hook")
	if err != nil {
		return err
	}
	attempt := doneAttempt{Result: "pass"}
	if hook == "" {
		return closeTicket(id, &attempt)
	}
	repo, err := repoPathFor(ticket.Project)
	if err != nil {
		return err
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
		return closeTicket(id, &attempt)
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
