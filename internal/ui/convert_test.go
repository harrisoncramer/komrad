package ui

import (
	"strings"
	"testing"

	"weave/config"
	workflowstore "weave/internal/store/workflow"
	"weave/internal/ui/layout"
	agentv1 "weave/proto/gen/agent/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestProtoToAgentCarriesTheSyncedColumns covers what the panel keeps of a listed agent.
// The branch and the worktree removal flag are both written by a server-side pass and
// drawn as glyphs, so a conversion that drops either leaves the row looking healthy.
func TestProtoToAgentCarriesTheSyncedColumns(t *testing.T) {
	branch := "fix/thing"
	converted := protoToAgent(&agentv1.Agent{Id: "a1", Branch: &branch, WorktreeRemoved: true})

	require.NotNil(t, converted.Branch)
	assert.Equal(t, branch, *converted.Branch)
	assert.True(t, converted.WorktreeRemoved)
}

// TestAgentsViewDrawsWorktreeRemovedFromTheWire covers the whole way in, from the proto a
// listing returns to the row the panel prints. The glyph's own test builds a store agent by
// hand, which is what let a field missing from the wire still pass it.
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
