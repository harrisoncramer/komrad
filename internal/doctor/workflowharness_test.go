package doctor

import (
	"context"
	"errors"
	"strings"
	"testing"

	"komrad/config"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func workflowHarnessEnv(workflows []WorkflowHarness, readErr error, broken map[string]bool, ran *[]string) Env {
	return Env{
		Settings: Settings{
			DatabaseURL: writableURL,
			TmuxSocket:  "komrad",
			Harnesses:   []HarnessInfo{{ID: "claude", Binary: "claude"}, {ID: "codex", Binary: "codex"}},
		},
		LoadConfig: func() (*config.Config, error) {
			return &config.Config{DefaultHarness: "claude"}, nil
		},
		ReadWorkflowHarnesses: func(context.Context, string) ([]WorkflowHarness, error) {
			return workflows, readErr
		},
		Output: func(_ context.Context, name string, args ...string) (string, error) {
			if name == "tmux" {
				return "/bin/bash", nil
			}

			script := args[len(args)-1]
			*ran = append(*ran, name+" "+strings.Join(args[:len(args)-1], " ")+" "+script)
			for dir := range broken {
				if strings.Contains(script, dir) {
					return "", errors.New("mise ERROR No version is set for shim")
				}
			}

			return "1.0.0", nil
		},
	}
}

// TestWorkflowHarnessCheckNamesTheWorkflowWhoseCLIWillNotStart is the case the check exists for:
// a CLI on PATH everywhere else that the project's pinned tool versions hide.
func TestWorkflowHarnessCheckNamesTheWorkflowWhoseCLIWillNotStart(t *testing.T) {
	workflows := []WorkflowHarness{
		{Name: "komrad", Cwd: "/repo/komrad"},
		{Name: "notes", Cwd: "/repo/notes", Harness: "codex"},
		{Name: "plain"},
		{Name: "unknown", Cwd: "/repo/unknown", Harness: "someone-elses-cli"},
	}
	var ran []string

	result := newWorkflowHarnessCheck().Run(t.Context(),
		workflowHarnessEnv(workflows, nil, map[string]bool{"/repo/komrad": true}, &ran))

	assert.Equal(t, StatusWarn, result.Status)
	assert.Equal(t, "workflow_harness_unrunnable", codeOf(result.Err))
	assert.Contains(t, result.Detail, "komrad cannot run claude")
	assert.NotContains(t, result.Detail, "notes")
	require.Len(t, ran, 2, "a workflow with no directory or a CLI this build lacks is skipped")
	assert.Equal(t, "/bin/bash -l -i -c cd '/repo/komrad' && 'claude' --version", ran[0],
		"the CLI is started the way a pane starts it, from the workflow's directory")
	assert.Contains(t, ran[1], "'codex' --version", "a workflow naming its own harness runs that one")
}

// TestWorkflowHarnessCheckPassesWhenEveryCLIStarts covers the ordinary engine.
func TestWorkflowHarnessCheckPassesWhenEveryCLIStarts(t *testing.T) {
	var ran []string

	result := newWorkflowHarnessCheck().Run(t.Context(),
		workflowHarnessEnv([]WorkflowHarness{{Name: "komrad", Cwd: "/repo/komrad"}}, nil, nil, &ran))

	assert.Equal(t, StatusOK, result.Status)
	assert.Equal(t, "1 workflow checked", result.Detail)
}

// TestWorkflowHarnessCheckWarnsWhenWorkflowsCannotBeRead covers a database that answers the
// ping but not the query.
func TestWorkflowHarnessCheckWarnsWhenWorkflowsCannotBeRead(t *testing.T) {
	var ran []string

	result := newWorkflowHarnessCheck().Run(t.Context(),
		workflowHarnessEnv(nil, errors.New("permission denied"), nil, &ran))

	assert.Equal(t, StatusWarn, result.Status)
	assert.Equal(t, "workflow_harnesses_unreadable", codeOf(result.Err))
	assert.Empty(t, ran)
}
