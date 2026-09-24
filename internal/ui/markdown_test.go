package ui

import (
	"testing"
	"time"

	"weave/internal/ui/panel"
	"weave/lib/xptr"
	"weave/lib/xslice"

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
		{"name", "api"},
		{"agents", "3"},
		{"default agent", "codex"},
		{"directory", "/repo/api"},
		{"flags", "--model opus"},
		{"worktree", "off"},
		{"workflow_id", "w1"},
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
		{"name", "api"},
		{"agents", "3"},
		{"default agent", "codex"},
		{"directory", "/repo/api"},
		{"flags", "—"},
		{"worktree", "on"},
		{"base branch", "main"},
		{"copy", ".env, .envrc"},
		{"symlink", "node_modules"},
		{"workflow_id", "w1"},
	}, workflowFields(workflow, 3))

	workflow.Worktree.CopyPaths = nil
	workflow.Worktree.SymlinkPaths = nil
	workflow.Worktree.BaseBranch = ""

	fields := workflowFields(workflow, 3)

	assert.Contains(t, fields, recordField{"base branch", "—"})
	assert.NotContains(t, fields, recordField{"copy", ""})
	assert.NotContains(t, fields, recordField{"worktree", "off"}, "a worktree workflow says what it cuts them from, not that it has none")
}

// TestAgentFields covers what an agent shows in the section: what it was launched with,
// the schedule that started it when one did, and whatever it has recorded about its own work.
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
		metadata   []panel.Metadata
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
			name:       "an agent that recorded nothing adds nothing for it",
			wantAbsent: []string{"pr", "branch"},
		},
		{
			name: "what the agent recorded comes after what it was launched with",
			metadata: []panel.Metadata{
				{Key: "branch", Value: "harry/rate-limits"},
				{Key: "pr", Value: "https://github.com/o/r/pull/312"},
			},
			wantLabels: []string{"branch", "pr"},
			wantValues: []string{"harry/rate-limits", "https://github.com/o/r/pull/312"},
		},
		{
			name:       "the branch the checkout is on shows without the agent recording it",
			branch:     xptr.Ptr("harry/rate-limits"),
			wantLabels: []string{"branch"},
			wantValues: []string{"harry/rate-limits"},
		},
		{
			name:       "the branch read from the checkout wins over one the agent recorded itself",
			branch:     xptr.Ptr("harry/rate-limits"),
			metadata:   []panel.Metadata{{Key: "branch", Value: "harry/stale"}},
			wantValues: []string{"harry/rate-limits"},
			wantAbsent: []string{"harry/stale"},
			wantOnce:   []string{"branch"},
		},
		{
			name: "why a create or cleanup failed is left to the failed placeholder, not repeated here",
			metadata: []panel.Metadata{
				{Key: "error", Value: "the provisioner failed: exited 1"},
				{Key: "branch", Value: "harry/rate-limits"},
			},
			wantLabels: []string{"branch"},
			wantAbsent: []string{"error"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agent := agent
			agent.Metadata = tt.metadata
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
		metadata   []panel.Metadata
		wantLabels []string
		wantOnce   []string
		wantAbsent []string
	}{
		{
			name:       "a reviewer that recorded the key too draws one row, not two",
			role:       panel.RoleReviewer,
			metadata:   []panel.Metadata{{Key: "reviewing", Value: "01a0-stale"}},
			wantLabels: []string{"reviewing"},
			wantOnce:   []string{"reviewing"},
			wantAbsent: []string{"01a0-stale", "attached to"},
		},
		{
			name:       "an ordinary child is drawn as attached rather than reviewing",
			role:       panel.RoleImplementer,
			wantLabels: []string{"attached to"},
			wantAbsent: []string{"reviewing"},
		},
		{
			name:       "a child that recorded the key without the role keeps what it recorded",
			role:       panel.RoleImplementer,
			metadata:   []panel.Metadata{{Key: "reviewing", Value: "01a0-someone"}},
			wantLabels: []string{"attached to", "reviewing"},
			wantOnce:   []string{"reviewing"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agent := panel.Agent{
				Name:          "reader",
				Role:          tt.role,
				ParentAgentID: xptr.Ptr("01a0-parent"),
				Metadata:      tt.metadata,
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
