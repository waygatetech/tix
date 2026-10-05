package tix

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// conceptsFile is the repo's concepts registry. Its edits are judged against
// the plan's new_concepts rather than its paths.
const conceptsFile = "concepts.yaml"

// defaultCheckExempt are toolchain-managed files a plan never has to declare.
// A repo replaces the list with .tix/check-exempt, one glob per line.
var defaultCheckExempt = []string{"go.mod", "go.sum"}

// CheckResult is how a ticket's diff strays from its plan. Empty means it
// conforms.
type CheckResult struct {
	OutOfScope         []string `json:"out_of_scope"`
	UndeclaredConcepts []string `json:"undeclared_concepts"`
}

// OK reports whether the diff conforms to the plan.
func (r CheckResult) OK() bool {
	return len(r.OutOfScope) == 0 && len(r.UndeclaredConcepts) == 0
}

// Check compares the files changed since the merge base with base (committed,
// uncommitted, and untracked) to the ticket's indexed plan.
func Check(ctx context.Context, id int, base string) (CheckResult, error) {
	ticket, err := Show(id)
	if err != nil {
		return CheckResult{}, err
	}
	plan, err := planFor(id)
	if err != nil {
		return CheckResult{}, err
	}
	if plan == nil {
		return CheckResult{}, fmt.Errorf("ticket %d has no indexed plan; run tix plan first", id)
	}
	repo, err := repoPathFor(ticket.Project)
	if err != nil {
		return CheckResult{}, err
	}
	if repo == "" {
		return CheckResult{}, fmt.Errorf("project %q has no repo_path", ticket.Project)
	}

	mergeBase, err := git(ctx, repo, "merge-base", "HEAD", base)
	if err != nil {
		return CheckResult{}, err
	}
	mergeBase = strings.TrimSpace(mergeBase)
	diffed, err := git(ctx, repo, "diff", "--name-only", "--no-renames", "-z", mergeBase)
	if err != nil {
		return CheckResult{}, err
	}
	untracked, err := git(ctx, repo, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return CheckResult{}, err
	}

	exempt, err := checkExempt(repo)
	if err != nil {
		return CheckResult{}, err
	}
	exempt = append(exempt, conceptsFile)
	if rel, err := filepath.Rel(repo, plan.Path); err == nil && plan.Path != "" {
		exempt = append(exempt, filepath.ToSlash(rel))
	}

	var result CheckResult
	seen := map[string]bool{}
	for _, file := range strings.Split(diffed+untracked, "\x00") {
		if file == "" || seen[file] {
			continue
		}
		seen[file] = true
		if !matchAny(exempt, file) && !matchAny(plan.Paths, file) {
			result.OutOfScope = append(result.OutOfScope, file)
		}
	}
	sort.Strings(result.OutOfScope)

	if seen[conceptsFile] {
		// A missing file at the merge base just means every concept is new.
		before, _ := git(ctx, repo, "show", mergeBase+":"+conceptsFile)
		after, err := os.ReadFile(filepath.Join(repo, conceptsFile))
		if err != nil && !os.IsNotExist(err) {
			return CheckResult{}, fmt.Errorf("reading %s: %w", conceptsFile, err)
		}
		if result.UndeclaredConcepts, err = undeclaredConcepts([]byte(before), after, plan.NewConcepts); err != nil {
			return CheckResult{}, err
		}
	}
	return result, nil
}

func git(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s in %s: %w: %s", strings.Join(args, " "), dir, err, strings.TrimSpace(stderr.String()))
	}
	return string(out), nil
}

func checkExempt(repo string) ([]string, error) {
	f, err := os.Open(filepath.Join(repo, tixDirName, "check-exempt"))
	if os.IsNotExist(err) {
		return append([]string(nil), defaultCheckExempt...), nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading check-exempt: %w", err)
	}
	defer f.Close()
	var globs []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		if line := strings.TrimSpace(scanner.Text()); line != "" && !strings.HasPrefix(line, "#") {
			globs = append(globs, line)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading check-exempt: %w", err)
	}
	return globs, nil
}

func matchAny(globs []string, file string) bool {
	for _, glob := range globs {
		if matchGlob(strings.Split(glob, "/"), strings.Split(file, "/")) {
			return true
		}
	}
	return false
}

// matchGlob matches slash-separated segments with path.Match, plus "**" for
// any number of whole segments.
func matchGlob(glob, file []string) bool {
	if len(glob) == 0 {
		return len(file) == 0
	}
	if glob[0] == "**" {
		for i := 0; i <= len(file); i++ {
			if matchGlob(glob[1:], file[i:]) {
				return true
			}
		}
		return false
	}
	if len(file) == 0 {
		return false
	}
	ok, err := path.Match(glob[0], file[0])
	return err == nil && ok && matchGlob(glob[1:], file[1:])
}

// undeclaredConcepts lists concepts the registry gained that the plan did not
// declare as new.
func undeclaredConcepts(before, after []byte, declared []string) ([]string, error) {
	old, err := conceptNames(before)
	if err != nil {
		return nil, fmt.Errorf("parsing %s at merge base: %w", conceptsFile, err)
	}
	cur, err := conceptNames(after)
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", conceptsFile, err)
	}
	allowed := map[string]bool{}
	for _, c := range declared {
		allowed[c] = true
	}
	var undeclared []string
	for c := range cur {
		if !old[c] && !allowed[c] {
			undeclared = append(undeclared, c)
		}
	}
	sort.Strings(undeclared)
	return undeclared, nil
}

// conceptNames is the set of names a registry defines.
func conceptNames(data []byte) (map[string]bool, error) {
	concepts, err := parseConcepts(data)
	if err != nil {
		return nil, err
	}
	names := map[string]bool{}
	for _, c := range concepts {
		names[c.Name] = true
	}
	return names, nil
}

// parseConcepts reads a registry that is a map keyed by concept, or a list of
// names or of entries with a name field, optionally under a top-level
// "concepts" key. Entries keep their file order.
func parseConcepts(data []byte) ([]Concept, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	if len(doc.Content) == 0 {
		return nil, nil
	}
	node := doc.Content[0]
	if node.Kind == yaml.MappingNode && len(node.Content) == 2 && node.Content[0].Value == "concepts" {
		node = node.Content[1]
	}
	var concepts []Concept
	switch node.Kind {
	case yaml.MappingNode:
		for i := 0; i < len(node.Content); i += 2 {
			var c Concept
			if value := node.Content[i+1]; value.Kind == yaml.MappingNode {
				if err := value.Decode(&c); err != nil {
					return nil, fmt.Errorf("line %d: %w", value.Line, err)
				}
			}
			c.Name = node.Content[i].Value
			concepts = append(concepts, c)
		}
	case yaml.SequenceNode:
		for _, item := range node.Content {
			if item.Kind == yaml.ScalarNode {
				concepts = append(concepts, Concept{Name: item.Value})
				continue
			}
			var c Concept
			if err := item.Decode(&c); err != nil || c.Name == "" {
				return nil, fmt.Errorf("line %d: concept entry has no name", item.Line)
			}
			concepts = append(concepts, c)
		}
	}
	return concepts, nil
}
