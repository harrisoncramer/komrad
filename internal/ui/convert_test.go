package ui

import (
	"strings"
	"testing"

	"weave/config"
	workflowstore "weave/internal/store/workflow"
	"weave/internal/ui/layout"
	agentv1 "weave/proto/gen/agent/v1"
	workflowv1 "weave/proto/gen/workflow/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestProtoToAgentCarriesTheSyncedColumns pins the branch and the worktree removal flag,
// since a conversion that drops either leaves the row looking healthy.
func TestProtoToAgentCarriesTheSyncedColumns(t *testing.T) {
	branch := "fix/thing"
	converted := protoToAgent(&agentv1.Agent{Id: "a1", Branch: &branch, WorktreeRemoved: true})

	require.NotNil(t, converted.Branch)
	assert.Equal(t, branch, *converted.Branch)
	assert.True(t, converted.WorktreeRemoved)
}

// TestAgentsViewDrawsWorktreeRemovedFromTheWire covers the way from the proto to the printed
// row, since the glyph's own test starts from a store agent.
func TestAgentsViewDrawsWorktreeRemovedFromTheWire(t *testing.T) {
	m := model{
		frame: layout.New(&recordingTmux{}, config.DefaultLayout()),
		keys:  config.DefaultKeybindings(),
		listing: listing{
			workflows: []workflowstore.Workflow{{ID: "w1", Name: "chariot"}},
			agents: agentItemsFrom([]*agentv1.Agent{
				{Id: "a1", Name: "worker", WorkflowId: "w1", WorktreeRemoved: true},
				{Id: "a2", Name: "reviewer", WorkflowId: "w1"},
			}),
		},
	}

	rows := strings.Split(m.agentsView(), "\n")
	worker, ok := rowNamed(rows, "worker")
	require.True(t, ok)
	assert.Contains(t, worker, worktreeRemovedIcon)

	reviewer, ok := rowNamed(rows, "reviewer")
	require.True(t, ok)
	assert.NotContains(t, reviewer, worktreeRemovedIcon)
}

// TestWorkflowsFromCarriesTheWholeWorkflow covers the listing conversion field by field,
// since every screen reads a workflow out of the listing and writes it back whole, which
// makes a field the conversion drops a field the next rename clears.
func TestWorkflowsFromCarriesTheWholeWorkflow(t *testing.T) {
	got := workflowsFrom([]*workflowv1.Workflow{{
		Id:                "w1",
		Name:              "api",
		Context:           "shared notes",
		Harness:           "claude",
		Cwd:               "/work/api",
		Flags:             "--model opus",
		SetupAction:       "make-worktree",
		CleanupAction:     "drop-worktree",
		ArchiveOnPrClosed: true,
		Worktree: &workflowv1.WorktreeConfig{
			Enabled:      true,
			BaseBranch:   "develop",
			CopyPaths:    []string{".env"},
			SymlinkPaths: []string{"node_modules"},
			DiffExclude:  []string{"proto/gen"},
		},
	}})

	require.Len(t, got, 1)
	assert.Equal(t, workflowstore.Workflow{
		ID:                "w1",
		Name:              "api",
		Context:           "shared notes",
		Harness:           "claude",
		Cwd:               "/work/api",
		Flags:             "--model opus",
		SetupAction:       "make-worktree",
		CleanupAction:     "drop-worktree",
		ArchiveOnPRClosed: true,
		Worktree: workflowstore.WorktreeConfig{
			Enabled:      true,
			BaseBranch:   "develop",
			CopyPaths:    []string{".env"},
			SymlinkPaths: []string{"node_modules"},
			DiffExclude:  []string{"proto/gen"},
		},
	}, got[0])
}
