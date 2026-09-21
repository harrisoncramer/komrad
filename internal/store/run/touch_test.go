package run

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRunWritesTouchTheAgent pins that opening, finishing and reading a run each name the
// agent, whose row carries the failed-run mark.
func TestRunWritesTouchTheAgent(t *testing.T) {
	ctx := t.Context()
	f := open(t)
	agentID := f.seedAgent(t)
	f.touched.Reset()

	runID, err := f.Begin(ctx, BeginParams{AgentID: agentID, Trigger: "post-launch", Script: "hook"})
	require.NoError(t, err)
	assert.Equal(t, []string{agentID}, f.touched.Agents(), "a begun run names the agent")

	f.touched.Reset()
	require.NoError(t, f.Finish(ctx, FinishParams{ID: runID, Failure: "exit 1"}))
	assert.Equal(t, []string{agentID}, f.touched.Agents(), "a finished run names the agent through the run's id")

	f.touched.Reset()
	require.NoError(t, f.MarkRead(ctx, agentID))
	assert.Equal(t, []string{agentID}, f.touched.Agents())
}
