package agent

import (
	"testing"

	agentstore "weave/internal/store/agent"
	"weave/lib/xptr"

	"github.com/stretchr/testify/assert"
)

// TestRenderAgentCarriesWhatTheSyncersWrite covers the two columns no reconcile pass owns:
// the branch syncer's branch and the worktree syncer's removal flag. Both are stored, so
// both have to reach the caller, and a client that never sees them cannot draw them.
func TestRenderAgentCarriesWhatTheSyncersWrite(t *testing.T) {
	rendered := renderAgent(agentstore.Agent{
		ID:              "a1",
		Name:            "worker",
		Branch:          xptr.Ptr("fix/thing"),
		WorktreeRemoved: true,
	}, false, false)

	assert.Equal(t, "fix/thing", rendered.GetBranch())
	assert.True(t, rendered.GetWorktreeRemoved())
}

// TestRenderAgentLeavesAPresentWorktreeAlone covers the other side of the flag: an agent
// whose cwd the syncer still finds is rendered without it.
func TestRenderAgentLeavesAPresentWorktreeAlone(t *testing.T) {
	rendered := renderAgent(agentstore.Agent{ID: "a2", Name: "reviewer"}, false, false)

	assert.False(t, rendered.GetWorktreeRemoved())
}
