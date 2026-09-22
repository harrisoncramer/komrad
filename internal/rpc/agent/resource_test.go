package agent

import (
	"testing"
	"time"

	agentstore "weave/internal/store/agent"
	rowstore "weave/internal/store/row"
	"weave/lib/xptr"

	"github.com/stretchr/testify/assert"
)

// TestRenderAgentCarriesWhatTheSyncersWrite covers the branch and the worktree removal flag,
// which a client cannot draw if the response never carries them.
func TestRenderAgentCarriesWhatTheSyncersWrite(t *testing.T) {
	rendered := renderAgent(rowOf(agentstore.Agent{
		ID:              "a1",
		Name:            "worker",
		Branch:          xptr.Ptr("fix/thing"),
		WorktreeRemoved: true,
	}))

	assert.Equal(t, "fix/thing", rendered.GetBranch())
	assert.True(t, rendered.GetWorktreeRemoved())
}

// TestRenderAgentLeavesAPresentWorktreeAlone covers the other side of the flag: an agent
// whose cwd the syncer still finds is rendered without it.
func TestRenderAgentLeavesAPresentWorktreeAlone(t *testing.T) {
	rendered := renderAgent(rowOf(agentstore.Agent{ID: "a2", Name: "reviewer"}))

	assert.False(t, rendered.GetWorktreeRemoved())
}

// TestRenderAgentCarriesTheStateChange pins the timestamp the panel counts a span from, which
// is written nowhere but this response.
func TestRenderAgentCarriesTheStateChange(t *testing.T) {
	changed := time.Date(2026, time.September, 1, 9, 9, 0, 0, time.UTC)
	rendered := renderAgent(rowOf(agentstore.Agent{ID: "a1", StateChangedAt: changed}))

	assert.Equal(t, changed.Format(time.RFC3339), rendered.GetStateChangedAt())
}

// TestRenderAgentWithoutAStateChange covers an agent that has never been given one, which
// sends nothing rather than a formatted zero time.
func TestRenderAgentWithoutAStateChange(t *testing.T) {
	assert.Empty(t, renderAgent(rowOf(agentstore.Agent{ID: "a2"})).GetStateChangedAt())
}

// rowOf is an agent's row with nothing beside it.
func rowOf(stored agentstore.Agent) rowstore.Row {
	return rowstore.Row{Agent: stored}
}
