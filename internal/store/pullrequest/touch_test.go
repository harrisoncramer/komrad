package pullrequest

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPullRequestWritesTouchTheAgentsOnTheBranch pins that writing a pull request names the
// live agents on its branch, and that a sync write names every live agent on the repository.
func TestPullRequestWritesTouchTheAgentsOnTheBranch(t *testing.T) {
	ctx := t.Context()
	h := open(t)
	flow := h.createWorkflow(t)
	onBranch := h.createAgent(t, flow, "on-branch", weave, "agent/x")
	h.createAgent(t, flow, "elsewhere", weave, "agent/y")
	h.touched.Reset()

	_, err := h.Upsert(ctx, openPullRequest("PR_1", 1, "agent/x"))
	require.NoError(t, err)
	assert.Equal(t, []string{onBranch}, h.touched.Agents(), "an upsert names the agents on the head branch")

	h.touched.Reset()
	require.NoError(t, h.syncs.MarkListed(ctx, weave, time.Now()))
	assert.Len(t, h.touched.Agents(), 2, "a sync write names every live agent on the repository")
	assert.Contains(t, h.touched.Agents(), onBranch)
}
