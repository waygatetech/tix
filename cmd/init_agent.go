/*
Copyright © 2026 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

const (
	tixWorkflowStart = "<!-- tix-workflow:start -->"
	tixWorkflowEnd   = "<!-- tix-workflow:end -->"
)

var agentDBPath string

// initAgentCmd represents the init-agent command
var initAgentCmd = &cobra.Command{
	Use:   "init-agent PROJECT",
	Short: "Add Tix workflow guidance to agent files",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		root, err := agentRoot()
		if err != nil {
			return err
		}
		files := []string{filepath.Join(root, "AGENTS.md"), filepath.Join(root, "CLAUDE.md")}
		section := tixWorkflowSection(args[0], agentDBPath)

		for _, path := range files {
			status, err := appendTixWorkflow(path, section)
			if err != nil {
				return err
			}
			fmt.Printf("%s %s\n", status, path)
		}
		return nil
	},
}

func init() {
	initAgentCmd.Flags().StringVar(&agentDBPath, "db", ".tix/tix.db", "database path to use in generated guidance")
	rootCmd.AddCommand(initAgentCmd)
}

func agentRoot() (string, error) {
	if output, err := exec.Command("git", "rev-parse", "--show-toplevel").Output(); err == nil {
		root := strings.TrimSpace(string(output))
		if root != "" {
			return root, nil
		}
	}

	workingDir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("find current directory failed: %w", err)
	}
	return workingDir, nil
}

func tixWorkflowSection(project, dbPath string) string {
	return fmt.Sprintf(`%s
## Tix Workflow

Use `+"`tix`"+`.

- Start work with `+"`tix claim-next --project %s --json --compact --fields id,title,body`"+`.
- Fetch full details only when needed with `+"`tix show ID --json --compact`"+`.
- List narrow backlogs with `+"`tix list --project %s --status open --limit 10 --json --compact --fields id,title,priority,status`"+`.
- Add follow-up tickets with `+"`tix add --project %s --body-file PATH --priority N TITLE`"+`.
- After verification, close work with `+"`tix done ID`"+`.

%s
`, tixWorkflowStart, project, project, project, tixWorkflowEnd)
}

func appendTixWorkflow(path, section string) (string, error) {
	data, err := os.ReadFile(path)
	existed := err == nil
	if err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("read %s failed: %w", path, err)
	}
	if strings.Contains(string(data), tixWorkflowStart) {
		return "unchanged", nil
	}

	content := strings.TrimRight(string(data), "\n")
	if content != "" {
		content += "\n\n"
	}
	content += section

	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return "", fmt.Errorf("write %s failed: %w", path, err)
	}
	if existed {
		return "updated", nil
	}
	return "created", nil
}
