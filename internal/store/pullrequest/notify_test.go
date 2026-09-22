package pullrequest

import (
	"testing"
	"time"

	"weave/internal/dbtest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPullRequestWritesNotifyTheAgentsOnTheBranch pins that writing a pull request names the
// live agents on its branch, and that a sync write names every live agent on the repository.
func TestPullRequestWritesNotifyTheAgentsOnTheBranch(t *testing.T) {
	ctx := t.Context()
	h := open(t)
	flow := h.createWorkflow(t)
	onBranch := h.createAgent(t, flow, "on-branch", weave, "agent/x")
	elsewhere := h.createAgent(t, flow, "elsewhere", weave, "agent/y")
	raised := dbtest.Listen(t, h.url)

	_, err := h.Upsert(ctx, openPullRequest("PR_1", 1, "agent/x"))
	require.NoError(t, err)
	assert.Equal(t, "agents "+onBranch, raised.Next(t), "an upsert names the agents on the head branch")

	require.NoError(t, h.syncs.MarkListed(ctx, weave, time.Now()))
	assert.ElementsMatch(t, []string{"agents " + onBranch, "agents " + elsewhere}, []string{raised.Next(t), raised.Next(t)}, "a sync write names every live agent on the repository")
}
