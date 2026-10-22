package ui

import (
	"testing"
	"time"

	"komrad/internal/ui/panel"
	"komrad/internal/ui/record"
	"komrad/lib/xslice"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWorkflowContextMarkdown covers the document the stage pages for a workflow, which is
// the context its agents inherit.
func TestWorkflowContextMarkdown(t *testing.T) {
	workflow := panel.Workflow{
		ID:      "w1",
		Name:    "api",
		Context: "shared notes",
	}

	assert.Contains(t, workflowContextMarkdown(workflow), "# api")
	assert.Contains(t, workflowContextMarkdown(workflow), "shared notes")

	workflow.Context = ""

	assert.Contains(t, workflowContextMarkdown(workflow), "no shared context set")
}

// TestWorkflowFields covers what the details render for a workflow: how many agents it holds,
// then the defaults future agents under it inherit.
func TestWorkflowFields(t *testing.T) {
	fields := workflowFields(panel.Workflow{
		ID:              "w1",
		Name:            "api",
		Harness:         "codex",
		Cwd:             "/repo/api",
		Flags:           "--model opus",
		ReviewerHarness: "claude",
		ReviewerFlags:   "--model sonnet",
	}, 3)

	assert.Equal(t, []record.Field{
		{Label: "name", Value: "api"},
		{Label: "agents", Value: "3"},
		{Label: "default agent", Value: "codex"},
		{Label: "directory", Value: "/repo/api"},
		{Label: "flags", Value: "--model opus"},
		{Label: "worktree", Value: "off"},
		{Label: "reviewer agent", Value: "claude"},
		{Label: "reviewer flags", Value: "--model sonnet"},
		{Label: "workflow_id", Value: "w1"},
	}, fields)
}

// TestWorkflowFieldsWorktree pins that a worktree workflow's details render the worktree
// settings the user chose.
func TestWorkflowFieldsWorktree(t *testing.T) {
	workflow := panel.Workflow{
		ID:      "w1",
		Name:    "api",
		Harness: "codex",
		Cwd:     "/repo/api",
		Worktree: panel.WorktreeConfig{
			Enabled:      true,
			BaseBranch:   "main",
			CopyPaths:    []string{".env", ".envrc"},
			SymlinkPaths: []string{"node_modules"},
			SetupCommand: "mise install",
		},
	}

	assert.Equal(t, []record.Field{
		{Label: "name", Value: "api"},
		{Label: "agents", Value: "3"},
		{Label: "default agent", Value: "codex"},
		{Label: "directory", Value: "/repo/api"},
		{Label: "flags", Value: "—"},
		{Label: "worktree", Value: "on"},
		{Label: "base branch", Value: "main"},
		{Label: "copy", Value: ".env, .envrc"},
		{Label: "symlink", Value: "node_modules"},
		{Label: "setup", Value: "mise install"},
		{Label: "reviewer agent", Value: "—"},
		{Label: "reviewer flags", Value: "—"},
		{Label: "workflow_id", Value: "w1"},
	}, workflowFields(workflow, 3))

	workflow.Worktree.CopyPaths = nil
	workflow.Worktree.SymlinkPaths = nil
	workflow.Worktree.SetupCommand = ""
	workflow.Worktree.BaseBranch = ""

	fields := workflowFields(workflow, 3)

	assert.Contains(t, fields, record.Field{Label: "base branch", Value: "—"})
	assert.NotContains(t, fields, record.Field{Label: "copy", Value: ""})
	assert.NotContains(t, fields, record.Field{Label: "worktree", Value: "off"}, "a worktree workflow says what it cuts them from, not that it has none")
}

// TestAgentFields covers what an agent shows in the section: what it was launched with and
// the schedule that started it when one did.
func TestAgentFields(t *testing.T) {
	agent := panel.Agent{
		ID:        "a1",
		Name:      "auth",
		Cwd:       "/repo",
		SessionID: "abc",
		CreatedAt: time.Date(2026, 7, 24, 15, 4, 0, 0, time.UTC),
	}

	tests := []struct {
		name       string
		schedule   string
		headerCwd  string
		failure    string
		wantLabels []string
		wantValues []string
		wantAbsent []string
		wantOnce   []string
	}{
		{
			name:       "what it was launched with, in the order it was launched with",
			wantLabels: []string{"agent", "directory", "flags", "agent_id", "session_id"},
			wantValues: []string{"—", "/repo", "—", "a1", "abc"},
			wantAbsent: []string{"name", "workflow"},
		},
		{
			name:       "an agent a schedule launched names the schedule",
			schedule:   "morning-ci",
			wantLabels: []string{"launched by"},
			wantValues: []string{"morning-ci"},
		},
		{
			name:       "one a person started claims no schedule",
			wantAbsent: []string{"launched by"},
		},
		{
			name:       "an agent in its task's directory leaves the directory to the task",
			headerCwd:  "/repo",
			wantAbsent: []string{"directory", "/repo"},
		},
		{
			name:       "an agent working outside its task's directory names its own",
			headerCwd:  "/elsewhere",
			wantLabels: []string{"directory"},
			wantValues: []string{"/repo"},
		},
		{
			name:       "why a create or cleanup failed is left to the failed placeholder, not repeated here",
			failure:    "the provisioner failed: exited 1",
			wantAbsent: []string{"the provisioner failed: exited 1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agent := agent
			agent.FailureReason = tt.failure

			fields := agentFields(agentFieldsParams{
				agent:     agent,
				schedule:  tt.schedule,
				headerCwd: tt.headerCwd,
			})
			require.NotEmpty(t, fields)
			assert.Equal(t, "agent", fields[0].Label, "the harness leads")

			labels := make([]string, 0, len(fields))
			values := make([]string, 0, len(fields))
			for _, field := range fields {
				labels = append(labels, field.Label)
				values = append(values, field.Value)
			}

			assert.Subset(t, labels, tt.wantLabels)
			assert.Subset(t, values, tt.wantValues)
			for _, absent := range tt.wantAbsent {
				assert.NotContains(t, labels, absent)
				assert.NotContains(t, values, absent)
			}
			for _, once := range tt.wantOnce {
				assert.Equal(t, 1, countLabel(labels, once))
			}
		})
	}
}

func countLabel(labels []string, want string) int {
	return xslice.Count(labels, func(label string) bool {
		return label == want
	})
}
