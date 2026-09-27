package ui

import (
	"testing"
	"time"

	"komrad/internal/ui/panel"
	"komrad/lib/xptr"
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

// TestWorkflowFields covers what the details draw for a workflow: how many agents it holds,
// then the defaults future agents under it inherit.
func TestWorkflowFields(t *testing.T) {
	fields := workflowFields(panel.Workflow{
		ID:      "w1",
		Name:    "api",
		Harness: "codex",
		Cwd:     "/repo/api",
		Flags:   "--model opus",
	}, 3)

	assert.Equal(t, []recordField{
		{label: "name", value: "api"},
		{label: "agents", value: "3"},
		{label: "default agent", value: "codex"},
		{label: "directory", value: "/repo/api"},
		{label: "flags", value: "--model opus"},
		{label: "worktree", value: "off"},
		{label: "workflow_id", value: "w1"},
	}, fields)
}

// TestWorkflowFieldsWorktree pins that a worktree workflow's details draw the worktree
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
		},
	}

	assert.Equal(t, []recordField{
		{label: "name", value: "api"},
		{label: "agents", value: "3"},
		{label: "default agent", value: "codex"},
		{label: "directory", value: "/repo/api"},
		{label: "flags", value: "—"},
		{label: "worktree", value: "on"},
		{label: "base branch", value: "main"},
		{label: "copy", value: ".env, .envrc"},
		{label: "symlink", value: "node_modules"},
		{label: "workflow_id", value: "w1"},
	}, workflowFields(workflow, 3))

	workflow.Worktree.CopyPaths = nil
	workflow.Worktree.SymlinkPaths = nil
	workflow.Worktree.BaseBranch = ""

	fields := workflowFields(workflow, 3)

	assert.Contains(t, fields, recordField{label: "base branch", value: "—"})
	assert.NotContains(t, fields, recordField{label: "copy", value: ""})
	assert.NotContains(t, fields, recordField{label: "worktree", value: "off"}, "a worktree workflow says what it cuts them from, not that it has none")
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
		branch     *string
		failure    string
		wantLabels []string
		wantValues []string
		wantAbsent []string
		wantOnce   []string
	}{
		{
			name:       "what it was launched with, in the order it was launched with",
			wantLabels: []string{"name", "agent", "directory", "flags", "agent_id", "session_id"},
			wantValues: []string{"auth", "—", "/repo", "—", "a1", "abc"},
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
			name:       "an agent off any branch draws none",
			wantAbsent: []string{"branch"},
		},
		{
			name:       "the branch the checkout is on shows without the agent recording it",
			branch:     xptr.Ptr("harry/rate-limits"),
			wantLabels: []string{"branch"},
			wantValues: []string{"harry/rate-limits"},
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
			agent.Branch = tt.branch

			fields := agentFields(agentFieldsParams{agent: agent, schedule: tt.schedule})
			require.NotEmpty(t, fields)
			assert.Equal(t, "name", fields[0].label, "the name it is filed under leads")

			labels := make([]string, 0, len(fields))
			values := make([]string, 0, len(fields))
			for _, field := range fields {
				labels = append(labels, field.label)
				values = append(values, field.value)
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

// TestAgentFieldsReviewingRow pins a reviewer's record drawing whose work it reads exactly
// once.
func TestAgentFieldsReviewingRow(t *testing.T) {
	tests := []struct {
		name       string
		role       panel.Role
		wantLabels []string
		wantOnce   []string
		wantAbsent []string
	}{
		{
			name:       "a reviewer draws whose work it reads once",
			role:       panel.RoleReviewer,
			wantLabels: []string{"reviewing"},
			wantOnce:   []string{"reviewing"},
			wantAbsent: []string{"attached to"},
		},
		{
			name:       "an ordinary child is drawn as attached rather than reviewing",
			role:       panel.RoleImplementer,
			wantLabels: []string{"attached to"},
			wantAbsent: []string{"reviewing"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agent := panel.Agent{
				Name:          "reader",
				Role:          tt.role,
				ParentAgentID: xptr.Ptr("01a0-parent"),
			}

			fields := agentFields(agentFieldsParams{agent: agent, parent: "author"})

			labels := make([]string, 0, len(fields))
			values := make([]string, 0, len(fields))
			for _, field := range fields {
				labels = append(labels, field.label)
				values = append(values, field.value)
			}

			assert.Subset(t, labels, tt.wantLabels)
			for _, absent := range tt.wantAbsent {
				assert.NotContains(t, labels, absent)
				assert.NotContains(t, values, absent)
			}
			for _, once := range tt.wantOnce {
				assert.Equal(t, 1, countLabel(labels, once), "the row is drawn once")
			}
		})
	}
}

func countLabel(labels []string, want string) int {
	return xslice.Count(labels, func(label string) bool {
		return label == want
	})
}
