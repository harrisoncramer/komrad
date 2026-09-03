package ui

import (
	"strings"
	"testing"
	"time"

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
	converted := protoToAgent(&agentv1.Agent{
		Id:              "a1",
		Branch:          &branch,
		WorktreeRemoved: true,
	})

	require.NotNil(t, converted.Branch)
	assert.Equal(t, branch, *converted.Branch)
	assert.True(t, converted.WorktreeRemoved)
}

// TestProtoToAgentCarriesTheStateChange pins the timestamp the ghost slot counts from, which
// is the only field on the row that has to survive the wire for a span to read at all.
func TestProtoToAgentCarriesTheStateChange(t *testing.T) {
	changed := time.Date(2026, time.September, 1, 9, 9, 0, 0, time.UTC)
	converted := protoToAgent(&agentv1.Agent{
		Id:             "a1",
		StateChangedAt: changed.Format(time.RFC3339),
	})

	assert.True(t, converted.StateChangedAt.Equal(changed))
}

// TestProtoToAgentWithoutAStateChange covers a server that sent no timestamp, which leaves the
// zero time rather than an instant the slot would count a span from.
func TestProtoToAgentWithoutAStateChange(t *testing.T) {
	assert.True(t, protoToAgent(&agentv1.Agent{Id: "a1"}).StateChangedAt.IsZero())
}

// TestAgentsViewDrawsWorktreeRemovedFromTheWire covers the way from the proto to the printed
// row, since the glyph's own test starts from a store agent.
func TestAgentsViewDrawsWorktreeRemovedFromTheWire(t *testing.T) {
	m := model{
		frame: layout.New(&recordingTmux{}, config.DefaultLayout()),
		keys:  config.DefaultKeybindings(),
		listing: listing{
			workflows: []workflowstore.Workflow{{ID: "w1", Name: "acmecorp"}},
			agents: agentItemsFrom([]*agentv1.Agent{
				{
					Id:              "a1",
					Name:            "worker",
					WorkflowId:      "w1",
					WorktreeRemoved: true,
				},
				{
					Id:         "a2",
					Name:       "reviewer",
					WorkflowId: "w1",
				},
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
		ArchiveOnPrClosed: true,
		Worktree: &workflowv1.WorktreeConfig{
			Enabled:      true,
			BaseBranch:   "develop",
			CopyPaths:    []string{".env"},
			SymlinkPaths: []string{"node_modules"},
			DiffExclude:  []string{"proto/gen"},
		},
		Hooks: []*workflowv1.WorkflowHook{
			{Event: "pre-launch", Script: "install-deps"},
			{Event: "post-launch", Script: "announce"},
		},
	}})

	require.Len(t, got, 1)
	assert.Equal(t, workflowstore.Workflow{
		ID:      "w1",
		Name:    "api",
		Context: "shared notes",
		Harness: "claude",
		Cwd:     "/work/api",
		Flags:   "--model opus",
		Hooks: []workflowstore.Hook{
			{Event: "pre-launch", Script: "install-deps"},
			{Event: "post-launch", Script: "announce"},
		},
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
