package ui

import (
	"testing"
	"time"

	"weave/internal/client"
	agentv1 "weave/proto/gen/agent/v1"
	workflowv1 "weave/proto/gen/workflow/v1"

	"github.com/stretchr/testify/assert"
)

// TestLandingGivesUpOnceTheWaitRunsOut covers a row that arrives after its landing stopped waiting.
func TestLandingGivesUpOnceTheWaitRunsOut(t *testing.T) {
	m := watchedPanel()
	m.applyWatch(client.WatchBatch{snapshotEvent(
		workflowRowOf(&workflowv1.Workflow{Id: "w1", Name: "api"}),
		agentRowOf(&agentv1.Agent{
			Id:         "a1",
			Name:       "old",
			WorkflowId: "w1",
		}),
	)})
	hovered := rowKeyAt(m.agents.rows(m.shared()), m.agents.cursor)

	m.landOn(written{row: agentRowKey("a2")}, time.Now().Add(-landingWait-time.Second))
	m.applyWatch(client.WatchBatch{putEvent(agentRowOf(&agentv1.Agent{
		Id:         "a2",
		Name:       "new",
		WorkflowId: "w1",
	}))})

	assert.Equal(t, hovered, rowKeyAt(m.agents.rows(m.shared()), m.agents.cursor))
	assert.Empty(t, m.landing.row)
}
