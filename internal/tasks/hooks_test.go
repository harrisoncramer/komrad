package tasks

import (
	"testing"

	agentstore "komrad/internal/store/agent"

	"github.com/stretchr/testify/assert"
)

// TestIsOpening covers which agents starting on a task take it from no live agent to one.
func TestIsOpening(t *testing.T) {
	taskID := "task"
	parentID := "parent"
	starting := agentstore.Agent{ID: "starting", TaskID: &taskID}

	tests := []struct {
		name  string
		agent agentstore.Agent
		live  []agentstore.Agent
		want  bool
	}{
		{
			name:  "an agent on no task opens none",
			agent: agentstore.Agent{ID: "starting"},
		},
		{
			name: "a child opens nothing",
			agent: agentstore.Agent{
				ID:            "starting",
				TaskID:        &taskID,
				ParentAgentID: &parentID,
			},
		},
		{
			name:  "another live agent holds the task open",
			agent: starting,
			live:  []agentstore.Agent{starting, {ID: "other", State: agentstore.StateIdle}},
		},
		{
			name:  "an agent that failed to create holds nothing open",
			agent: starting,
			live:  []agentstore.Agent{starting, {ID: "other", State: agentstore.StateFailedToCreate}},
			want:  true,
		},
		{
			name:  "the only agent on the task opens it",
			agent: starting,
			live:  []agentstore.Agent{starting},
			want:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, isOpening(tt.agent, tt.live))
		})
	}
}
