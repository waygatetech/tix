package tix

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"time"
)

// LatestVersionURL is where the Go module proxy reports tix's newest tagged
// release.
const LatestVersionURL = "https://proxy.golang.org/github.com/waygatetech/tix/@latest"

// UpdateCheckInterval is how long a check attempt counts before another runs.
const UpdateCheckInterval = 24 * time.Hour

// UpdateState is what ~/.tix/update.json remembers between commands.
type UpdateState struct {
	// CheckedAt is when the last check was started, successful or not.
	CheckedAt time.Time `json:"checked_at"`
	// Latest is the newest release the last successful check found.
	Latest string `json:"latest,omitempty"`
	// Notified is the version the agent was last told about.
	Notified string `json:"notified,omitempty"`
}

// UpdateStatePath is ~/.tix/update.json.
func UpdateStatePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("finding home directory: %w", err)
	}
	return filepath.Join(home, tixDirName, "update.json"), nil
}

// ReadUpdateState reads the state at path. A missing file is the zero state.
func ReadUpdateState(path string) (UpdateState, error) {
	var state UpdateState
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return state, nil
	}
	if err != nil {
		return state, fmt.Errorf("reading update state: %w", err)
	}
	if err := json.Unmarshal(data, &state); err != nil {
		return state, fmt.Errorf("parsing update state: %w", err)
	}
	return state, nil
}

// WriteUpdateState writes the state to path, creating its directory.
func WriteUpdateState(path string, state UpdateState) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating update state directory: %w", err)
	}
	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("encoding update state: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("writing update state: %w", err)
	}
	return nil
}

var releaseVersion = regexp.MustCompile(`^v(\d+)\.(\d+)\.(\d+)$`)

// ValidRelease reports whether v is a plain vMAJOR.MINOR.PATCH release.
// Prereleases and pseudo-versions don't count, so dev builds stay quiet.
func ValidRelease(v string) bool {
	return releaseVersion.MatchString(v)
}

// NewerRelease reports whether latest is a newer release than running. Either
// one not being a valid release means no.
func NewerRelease(latest, running string) bool {
	l := releaseVersion.FindStringSubmatch(latest)
	r := releaseVersion.FindStringSubmatch(running)
	if l == nil || r == nil {
		return false
	}
	for i := 1; i <= 3; i++ {
		ln, _ := strconv.Atoi(l[i])
		rn, _ := strconv.Atoi(r[i])
		if ln != rn {
			return ln > rn
		}
	}
	return false
}

// FetchLatest asks the module proxy at url for the newest release and rejects
// anything that is not a valid release, since the answer ends up in a command.
func FetchLatest(ctx context.Context, url string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("building latest version request: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetching latest version: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("fetching latest version: %s", resp.Status)
	}
	var body struct{ Version string }
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", fmt.Errorf("parsing latest version: %w", err)
	}
	if !ValidRelease(body.Version) {
		return "", fmt.Errorf("proxy reported %q, not a release version", body.Version)
	}
	return body.Version, nil
}
