package tix

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// ErrUnknownProject reports a project name that is not in the table. It is the
// one error EnsureProject is allowed to answer by creating the project.
var ErrUnknownProject = errors.New("unknown project")

// Projects are rows, not whatever string a caller happened to type. An agent
// with no shell history and no autocomplete will write "frontend", "front-end",
// "Frontend" and "frontend " on four different days; before this table each of
// those silently created a project and hid the other three's tickets.
//
// Two rules keep that from happening:
//
//   - A project name that is not in the table is rejected, so a typo fails
//     loudly instead of forking the backlog. Creating one is explicit.
//   - Lookup folds away case and punctuation, so the four spellings above all
//     resolve to the one project that exists. Commands then use the stored
//     spelling, which is what ends up on the ticket.

// Project is a row of the projects table. Key is the ticket ID prefix; empty
// means the prefix is derived from the name rather than pinned. RepoPath is
// where the project's code lives, recorded for commands that want to infer the
// project from the working directory.
type Project struct {
	Name     string `json:"name"`
	Key      string `json:"key,omitempty"`
	RepoPath string `json:"repo_path,omitempty"`
}

// foldProject reduces a name to the lowercase alphanumerics it shares with its
// variant spellings: "Front-End", "front end" and "frontend " all fold to
// "frontend". A name with no alphanumerics folds to "", which never matches.
func foldProject(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// ListProjects returns every project, ordered by name.
func ListProjects() ([]Project, error) {
	rows, err := db.Query(`SELECT name, key, repo_path FROM projects ORDER BY name ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	projects := []Project{}
	for rows.Next() {
		var project Project
		if err := rows.Scan(&project.Name, &project.Key, &project.RepoPath); err != nil {
			return nil, err
		}
		projects = append(projects, project)
	}
	return projects, rows.Err()
}

// ResolveProject turns a caller-supplied project name into the stored one,
// matching exactly first and by folded spelling second. An unknown name is an
// error naming the projects that do exist, so a mistyped --project tells the
// caller what to type instead.
func ResolveProject(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("project is required")
	}

	projects, err := ListProjects()
	if err != nil {
		return "", err
	}

	folded := foldProject(name)
	var matches []string
	for _, project := range projects {
		if project.Name == name {
			return project.Name, nil
		}
		if folded != "" && foldProject(project.Name) == folded {
			matches = append(matches, project.Name)
		}
	}
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return "", unknownProjectError(name, projects)
	default:
		// Only a database that predates this table can hold two projects that
		// fold together; picking one of them would close the wrong ticket.
		return "", fmt.Errorf(
			"project %q is ambiguous: %s — use the exact name",
			name, strings.Join(matches, ", "),
		)
	}
}

// EnsureProject resolves a project name, creating the project when nothing
// matches. It reports whether a new project was created.
func EnsureProject(name string) (string, bool, error) {
	resolved, err := ResolveProject(name)
	if err == nil {
		return resolved, false, nil
	}
	if !errors.Is(err, ErrUnknownProject) {
		return "", false, err
	}

	project, err := CreateProject(Project{Name: name})
	if err != nil {
		// Parallel agents all passing --create race here, and the loser's
		// INSERT hits the primary key. The project it wanted now exists, which
		// is all it was asking for.
		if resolved, resolveErr := ResolveProject(name); resolveErr == nil {
			return resolved, false, nil
		}
		return "", false, err
	}
	return project.Name, true, nil
}

// CreateProject adds a project. A name that folds onto an existing project is
// refused rather than stored: accepting it would recreate the fragmentation the
// table exists to prevent.
func CreateProject(project Project) (Project, error) {
	project.Name = strings.TrimSpace(project.Name)
	if project.Name == "" {
		return Project{}, fmt.Errorf("project name is required")
	}
	if foldProject(project.Name) == "" {
		return Project{}, fmt.Errorf("project name must contain letters or digits: %q", project.Name)
	}
	if project.Key != "" {
		clean, err := cleanPrefix(project.Key)
		if err != nil {
			return Project{}, err
		}
		project.Key = clean
	}

	existing, err := ResolveProject(project.Name)
	switch {
	case err == nil && existing == project.Name:
		return Project{}, fmt.Errorf("project %q already exists", project.Name)
	case err == nil:
		return Project{}, fmt.Errorf("project %q already exists as %q", project.Name, existing)
	case !errors.Is(err, ErrUnknownProject):
		// An ambiguous name means two spellings are already in the table;
		// adding a third is the opposite of what this table is for.
		return Project{}, err
	}

	if project.Key != "" {
		if err := checkPrefixAvailable(project.Name, project.Key); err != nil {
			return Project{}, err
		}
	}
	if _, err := db.Exec(
		`INSERT INTO projects (name, key, repo_path) VALUES (?, ?, ?)`,
		project.Name, project.Key, project.RepoPath,
	); err != nil {
		return Project{}, fmt.Errorf("create project failed: %w", err)
	}
	return project, nil
}

// SetRepoPath records where a project's code lives.
func SetRepoPath(name, repoPath string) (string, error) {
	resolved, err := ResolveProject(name)
	if err != nil {
		return "", err
	}
	if _, err := db.Exec(`UPDATE projects SET repo_path = ? WHERE name = ?`, repoPath, resolved); err != nil {
		return "", fmt.Errorf("update project failed: %w", err)
	}
	return resolved, nil
}

// unknownProjectError explains what to type instead of the name that missed.
func unknownProjectError(name string, projects []Project) error {
	// The message names the escape hatch on both surfaces, because the caller
	// getting it is as often an agent speaking MCP as a person at a shell.
	if len(projects) == 0 {
		return fmt.Errorf(
			"%w %q: no projects exist yet; pass --create (or create=true) to make it",
			ErrUnknownProject, name,
		)
	}
	names := make([]string, 0, len(projects))
	for _, project := range projects {
		names = append(names, project.Name)
	}
	sort.Strings(names)
	return fmt.Errorf(
		"%w %q: known projects are %s; pass --create (or create=true) to make it a new one",
		ErrUnknownProject, name, strings.Join(names, ", "),
	)
}
