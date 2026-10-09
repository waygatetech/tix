package tix

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

func TestCheck(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	setupTestDB(t)
	t.Setenv("HOME", t.TempDir())
	repo := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write := func(name, body string) {
		t.Helper()
		full := filepath.Join(repo, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run("init", "-q", "-b", "main")
	run("config", "user.email", "t@example.com")
	run("config", "user.name", "t")
	write("concepts.yaml", "auth: login\n")
	write("README.md", "hi\n")
	run("add", ".")
	run("commit", "-q", "-m", "init")
	run("checkout", "-q", "-b", "work")

	if _, err := CreateProject(Project{Name: "app", Key: "ap", RepoPath: repo}); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	id64, err := Add("scoped", "", 1, "app", nil)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	id := int(id64)
	ref := FormatID("ap", id)
	write("plans/"+ref+".md", "---\nticket: "+ref+"\nnew_concepts: [billing]\npaths: [\"internal/**/*.go\"]\n---\n")
	if _, err := IndexPlan(context.Background(), id, ""); err != nil {
		t.Fatalf("IndexPlan: %v", err)
	}

	tests := []struct {
		name  string
		files map[string]string
		want  CheckResult
	}{
		{"plan file alone", nil, CheckResult{}},
		{"in scope and exempt", map[string]string{"internal/a/b.go": "package b\n", "go.sum": "x\n"}, CheckResult{}},
		{"declared concept", map[string]string{"concepts.yaml": "auth: login\nbilling: money\n"}, CheckResult{}},
		{"out of scope committed", map[string]string{"README.md": "changed\n"}, CheckResult{OutOfScope: []string{"README.md"}}},
		{"undeclared concept", map[string]string{"concepts.yaml": "auth: login\nbilling: money\nsearch: find\n"},
			CheckResult{OutOfScope: []string{"README.md"}, UndeclaredConcepts: []string{"search"}}},
	}
	for i, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			for name, body := range tc.files {
				write(name, body)
			}
			if i == 3 {
				run("commit", "-q", "-am", "readme")
			}
			got, err := Check(context.Background(), id, "main")
			if err != nil {
				t.Fatalf("Check: %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("Check() = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestMatchAny(t *testing.T) {
	tests := []struct {
		glob, file string
		want       bool
	}{
		{"cmd/*.go", "cmd/check.go", true},
		{"cmd/*.go", "cmd/sub/check.go", false},
		{"internal/**", "internal/tix/check.go", true},
		{"**/*_test.go", "check_test.go", true},
		{"internal/**/*.go", "internal/x.go", true},
		{"e2e.sh", "e2e.sh", true},
		{"e2e.sh", "other.sh", false},
	}
	for _, tc := range tests {
		t.Run(tc.glob+" "+tc.file, func(t *testing.T) {
			if got := matchAny([]string{tc.glob}, tc.file); got != tc.want {
				t.Fatalf("matchAny(%q, %q) = %v, want %v", tc.glob, tc.file, got, tc.want)
			}
		})
	}
}

func TestConceptNames(t *testing.T) {
	tests := []struct {
		name, yaml string
		want       []string
	}{
		{"map", "a: x\nb: y\n", []string{"a", "b"}},
		{"list", "- a\n- b\n", []string{"a", "b"}},
		{"named entries under concepts", "concepts:\n  - name: a\n  - name: b\n", []string{"a", "b"}},
		{"empty", "", nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := undeclaredConcepts(nil, []byte(tc.yaml), nil)
			if err != nil {
				t.Fatalf("undeclaredConcepts: %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

// gitRepo makes a repo on main with one commit, plus a worktree for each ref
// at .worktrees/<ref> on a branch of the same name. run runs git in a dir.
func gitRepo(t *testing.T, refs ...string) (repo string, run func(dir string, args ...string)) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	repo = t.TempDir()
	run = func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run(repo, "init", "-q", "-b", "main")
	run(repo, "config", "user.email", "t@example.com")
	run(repo, "config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(repo, "add", ".")
	run(repo, "commit", "-q", "-m", "init")
	for _, ref := range refs {
		run(repo, "worktree", "add", "-q", filepath.Join(worktreesDir, ref), "-b", ref)
	}
	return repo, run
}

func TestTicketDir(t *testing.T) {
	repo, _ := gitRepo(t, "ap-1")
	if err := os.MkdirAll(filepath.Join(repo, worktreesDir, "ap-2"), 0o755); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		repo string
		ref  string
		want string
	}{
		{"git worktree", repo, "ap-1", filepath.Join(repo, worktreesDir, "ap-1")},
		{"stray directory", repo, "ap-2", repo},
		{"no worktree", repo, "ap-3", repo},
		{"not a git repo", t.TempDir(), "ap-1", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			want := tt.want
			if want == "" {
				want = tt.repo
			}
			if got := ticketDir(context.Background(), tt.repo, tt.ref); got != want {
				t.Fatalf("ticketDir() = %q, want %q", got, want)
			}
		})
	}
}

func TestCheckWorktree(t *testing.T) {
	setupTestDB(t)
	t.Setenv("HOME", t.TempDir())
	if _, err := CreateProject(Project{Name: "app", Key: "ap"}); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	id64, err := Add("scoped", "", 1, "app", nil)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	id := int(id64)
	ref := FormatID("ap", id)
	repo, run := gitRepo(t, ref)
	if _, err := db.Exec(`UPDATE projects SET repo_path = ? WHERE name = 'app'`, repo); err != nil {
		t.Fatal(err)
	}
	tree := filepath.Join(repo, worktreesDir, ref)
	files := map[string]string{
		filepath.Join(repo, "junk.txt"):         "untracked in the main checkout\n",
		filepath.Join(tree, "plans", ref+".md"): "---\nticket: " + ref + "\npaths: [\"internal/**\"]\n---\n",
		filepath.Join(tree, "internal", "a.go"): "package a\n",
		filepath.Join(tree, "README.md"):        "changed on the branch\n",
	}
	for path, body := range files {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run(tree, "add", ".")
	run(tree, "commit", "-q", "-m", "work")
	if _, err := IndexPlan(context.Background(), id, filepath.Join(tree, "plans", ref+".md")); err != nil {
		t.Fatalf("IndexPlan: %v", err)
	}

	got, err := Check(context.Background(), id, "main")
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if want := (CheckResult{OutOfScope: []string{"README.md"}}); !reflect.DeepEqual(got, want) {
		t.Fatalf("Check() = %+v, want %+v", got, want)
	}
}
