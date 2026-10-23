package tasks

import (
	"testing"

	agentstore "komrad/internal/store/agent"
	agentv1 "komrad/proto/gen/agent/v1"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

// TestIsOpening covers which agents starting on a task take it from no live agent to one.
func TestIsOpening(t *testing.T) {
	taskID := uuid.New()
	starting := agentstore.Agent{
		ID:     startingAgentID,
		TaskID: &taskID,
		Role:   agentv1.AgentRole_AGENT_ROLE_WRITER,
	}

	tests := []struct {
		name  string
		agent agentstore.Agent
		live  []agentstore.Agent
		want  bool
	}{
		{
			name:  "an agent on no task opens none",
			agent: agentstore.Agent{ID: startingAgentID},
		},
		{
			name: "a reviewer opens nothing",
			agent: agentstore.Agent{
				ID:     startingAgentID,
				TaskID: &taskID,
				Role:   agentv1.AgentRole_AGENT_ROLE_REVIEWER,
			},
		},
		{
			name:  "another live agent holds the task open",
			agent: starting,
			live:  []agentstore.Agent{starting, {ID: otherAgentID, State: agentv1.AgentState_AGENT_STATE_IDLE}},
		},
		{
			name:  "an agent that failed to create holds nothing open",
			agent: starting,
			live:  []agentstore.Agent{starting, {ID: otherAgentID, State: agentv1.AgentState_AGENT_STATE_FAILED_TO_CREATE}},
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

var (
	otherAgentID    = uuid.New()
	startingAgentID = uuid.New()
)
