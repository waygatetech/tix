package tix

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Ticket IDs are integers in the database and in every JSON payload. The
// prefixed form (fe-47) exists only at the edges: it is what humans read in
// list/show output and what they may type back in. Keeping the integer
// canonical means the MCP tool schemas, the --json output, and the primary key
// are all untouched by prefixes, so agents that already speak integer ids keep
// working.
//
// A project's prefix is derived from its name rather than stored, so a
// database needs no backfill to start showing prefixes. Derivation is a pure
// function of the sorted set of project names plus any overrides, which makes
// it deterministic: every process computes the same prefix for the same data.
// The cost is that introducing a project whose name collides with an existing
// one can shift the shorter project's prefix; setting an override freezes it.

// defaultPrefixLen is the number of characters a derived prefix uses before
// collisions force it longer.
const defaultPrefixLen = 2

// fallbackPrefix is used for a project whose name has no alphanumerics.
const fallbackPrefix = "tx"

// prefixBase reduces a project name to the lowercase alphanumerics a prefix is
// drawn from, so "Front-End!" and "frontend" yield the same base.
func prefixBase(project string) string {
	base := foldProject(project)
	if base == "" {
		return fallbackPrefix
	}
	return base
}

// derivePrefixes assigns every project a unique prefix. Overrides are honored
// first and claim their prefix outright; the rest are processed in sorted order
// so the result depends only on the inputs, never on insertion order. A project
// whose short prefix is taken lengthens it one character at a time, then falls
// back to a numeric suffix once its name is exhausted.
func derivePrefixes(names []string, overrides map[string]string) map[string]string {
	prefixes := make(map[string]string, len(names))
	taken := make(map[string]string, len(names))

	sorted := append([]string(nil), names...)
	sort.Strings(sorted)

	// Overrides win, and an override for a project with no tickets still
	// reserves its prefix so a derived one cannot collide with it later.
	for project, prefix := range overrides {
		prefixes[project] = prefix
		taken[prefix] = project
	}

	for _, project := range sorted {
		if _, ok := prefixes[project]; ok {
			continue
		}
		base := prefixBase(project)
		prefix := ""
		for n := defaultPrefixLen; n <= len(base); n++ {
			candidate := base[:n]
			if _, clash := taken[candidate]; !clash {
				prefix = candidate
				break
			}
		}
		if prefix == "" {
			// The whole name is already claimed by another project, so
			// disambiguate with a counter: fe, fe2, fe3...
			for n := 2; ; n++ {
				candidate := base + strconv.Itoa(n)
				if _, clash := taken[candidate]; !clash {
					prefix = candidate
					break
				}
			}
		}
		prefixes[project] = prefix
		taken[prefix] = project
	}
	return prefixes
}

// prefixOverrides reads the manually assigned prefixes.
func prefixOverrides() (map[string]string, error) {
	projects, err := ListProjects()
	if err != nil {
		return nil, err
	}
	overrides := map[string]string{}
	for _, project := range projects {
		if project.Key != "" {
			overrides[project.Name] = project.Key
		}
	}
	return overrides, nil
}

// projectNames lists every known project, whether or not it has tickets yet.
func projectNames() ([]string, error) {
	projects, err := ListProjects()
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(projects))
	for _, project := range projects {
		names = append(names, project.Name)
	}
	return names, nil
}

// Prefixes maps every known project to its ticket ID prefix.
func Prefixes() (map[string]string, error) {
	names, err := projectNames()
	if err != nil {
		return nil, err
	}
	overrides, err := prefixOverrides()
	if err != nil {
		return nil, err
	}
	return derivePrefixes(names, overrides), nil
}

// PrefixFor returns the ID prefix for a single project. A variant spelling is
// resolved first, so the prefix printed after a command is the one the ticket
// actually got rather than one derived from what the caller typed.
func PrefixFor(project string) (string, error) {
	if resolved, err := ResolveProject(project); err == nil {
		project = resolved
	}
	prefixes, err := Prefixes()
	if err != nil {
		return "", err
	}
	if prefix, ok := prefixes[project]; ok {
		return prefix, nil
	}
	// A project with no tickets yet still needs a prefix to display, so derive
	// one against the projects that do exist.
	names, err := projectNames()
	if err != nil {
		return "", err
	}
	overrides, err := prefixOverrides()
	if err != nil {
		return "", err
	}
	return derivePrefixes(append(names, project), overrides)[project], nil
}

// cleanPrefix validates a prefix a person typed and returns it normalized.
func cleanPrefix(prefix string) (string, error) {
	clean := foldProject(prefix)
	if clean == "" || clean != strings.ToLower(prefix) {
		return "", fmt.Errorf("prefix must be alphanumeric: %s", prefix)
	}
	return clean, nil
}

// checkPrefixAvailable rejects a prefix another project has already pinned. The
// UNIQUE index would catch it too, but with a message nobody can act on.
func checkPrefixAvailable(project, prefix string) error {
	overrides, err := prefixOverrides()
	if err != nil {
		return err
	}
	for other, existing := range overrides {
		if existing == prefix && other != project {
			return fmt.Errorf("prefix %q is already used by project %q", prefix, other)
		}
	}
	return nil
}

// SetPrefix pins a project's prefix, overriding the derived one.
func SetPrefix(project, prefix string) error {
	resolved, err := ResolveProject(project)
	if err != nil {
		return err
	}
	clean, err := cleanPrefix(prefix)
	if err != nil {
		return err
	}
	if err := checkPrefixAvailable(resolved, clean); err != nil {
		return err
	}
	_, err = db.Exec(`UPDATE projects SET key = ? WHERE name = ?`, clean, resolved)
	return err
}

// FormatID renders a ticket ID the way humans read it.
func FormatID(prefix string, id int) string {
	if prefix == "" {
		return strconv.Itoa(id)
	}
	return fmt.Sprintf("%s-%d", prefix, id)
}

// ParseRef splits a ticket reference into its numeric id and optional prefix.
// Both "47" and "fe-47" are accepted; the prefix, when present, is returned for
// the caller to verify against the ticket's actual project.
func ParseRef(ref string) (id int, prefix string, err error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return 0, "", fmt.Errorf("invalid id: %s", ref)
	}
	// Leading '#' is how list output used to render ids, so accept it rather
	// than making users strip it out of copied text.
	trimmed := strings.TrimPrefix(ref, "#")
	if i := strings.LastIndex(trimmed, "-"); i >= 0 {
		prefix = trimmed[:i]
		trimmed = trimmed[i+1:]
		if prefix == "" {
			return 0, "", fmt.Errorf("invalid id: %s", ref)
		}
	}
	id, convErr := strconv.Atoi(trimmed)
	if convErr != nil || id <= 0 {
		return 0, "", fmt.Errorf("invalid id: %s", ref)
	}
	return id, prefix, nil
}

// ResolveRef turns a user-supplied reference into a ticket ID, rejecting a
// prefix that names a different project than the ticket actually belongs to.
// That check is the point of prefixes: "tix done fe-47" cannot silently close a
// backend ticket that happens to be numbered 47.
func ResolveRef(ref string) (int, error) {
	id, prefix, err := ParseRef(ref)
	if err != nil {
		return 0, err
	}
	if prefix == "" {
		return id, nil
	}

	var project string
	if err := db.QueryRow(`SELECT project FROM tickets WHERE id = ?`, id).Scan(&project); err != nil {
		return 0, fmt.Errorf("no such ticket: %s", ref)
	}
	actual, err := PrefixFor(project)
	if err != nil {
		return 0, err
	}
	if actual != prefix {
		return 0, fmt.Errorf(
			"ticket %d belongs to project %q (%s-%d), not %q",
			id, project, actual, id, prefix,
		)
	}
	return id, nil
}
