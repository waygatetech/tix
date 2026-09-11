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
	var b strings.Builder
	for _, r := range strings.ToLower(project) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return fallbackPrefix
	}
	return b.String()
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
	rows, err := db.Query(`SELECT project, prefix FROM project_prefixes`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	overrides := map[string]string{}
	for rows.Next() {
		var project, prefix string
		if err := rows.Scan(&project, &prefix); err != nil {
			return nil, err
		}
		overrides[project] = prefix
	}
	return overrides, rows.Err()
}

// projectNames lists every project that has at least one ticket.
func projectNames() ([]string, error) {
	rows, err := db.Query(`SELECT DISTINCT project FROM tickets`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	names := []string{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	return names, rows.Err()
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

// PrefixFor returns the ID prefix for a single project.
func PrefixFor(project string) (string, error) {
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

// SetPrefix pins a project's prefix, overriding the derived one.
func SetPrefix(project, prefix string) error {
	if project == "" {
		return fmt.Errorf("project is required")
	}
	clean := prefixBase(prefix)
	if clean != strings.ToLower(prefix) || prefix == "" {
		return fmt.Errorf("prefix must be alphanumeric: %s", prefix)
	}
	// A prefix that collides with another project's override would make refs
	// ambiguous, and UNIQUE would reject it with a message nobody can act on.
	overrides, err := prefixOverrides()
	if err != nil {
		return err
	}
	for other, existing := range overrides {
		if existing == clean && other != project {
			return fmt.Errorf("prefix %q is already used by project %q", clean, other)
		}
	}
	_, err = db.Exec(
		`INSERT INTO project_prefixes (project, prefix) VALUES (?, ?)
		 ON CONFLICT(project) DO UPDATE SET prefix = excluded.prefix`,
		project, clean,
	)
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
