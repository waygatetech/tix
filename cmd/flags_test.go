package cmd

import "testing"

// Defining a flag writes its default into the variable it binds, and several
// commands bind the same variables. A command that defaults a shared filter to
// something non-empty therefore changes what every other command does before
// any of them run: giving stale a --status default of in_progress once left
// `tix list` and `tix search` quietly filtering to in_progress and printing
// nothing.
func TestSharedFilterDefaultsAreEmpty(t *testing.T) {
	if statusFilter != "" {
		t.Errorf("statusFilter = %q before any command runs, want empty", statusFilter)
	}
	if limitFilter != 0 {
		t.Errorf("limitFilter = %d before any command runs, want 0", limitFilter)
	}
	if priorityFilter != "" {
		t.Errorf("priorityFilter = %q before any command runs, want empty", priorityFilter)
	}

	for _, name := range []string{"status", "priority", "limit"} {
		for _, cmd := range []string{"list", "search"} {
			command, _, err := rootCmd.Find([]string{cmd})
			if err != nil {
				t.Fatalf("find %s: %v", cmd, err)
			}
			flag := command.Flags().Lookup(name)
			if flag == nil {
				t.Fatalf("%s has no --%s flag", cmd, name)
			}
			if flag.DefValue != "" && flag.DefValue != "0" {
				t.Errorf("%s --%s defaults to %q, want no filtering", cmd, name, flag.DefValue)
			}
		}
	}
}
