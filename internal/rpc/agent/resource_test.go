package agent

import (
	"testing"

	agentstore "weave/internal/store/agent"
	"weave/lib/xptr"

	"github.com/stretchr/testify/assert"
)

// TestRenderAgentCarriesWhatTheSyncersWrite covers the branch and the worktree removal flag,
// which a client cannot draw if the response never carries them.
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
