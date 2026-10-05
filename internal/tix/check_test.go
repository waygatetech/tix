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
