package render

import (
	markstore "komrade/internal/store/mark"
	agentv1 "komrade/proto/gen/agent/v1"
)

// Mark draws one mark as the wire carries it.
func Mark(stored markstore.Mark) *agentv1.Mark {
	return &agentv1.Mark{
		Id:         stored.ID,
		Mark:       stored.Mark,
		AgentId:    stored.AgentID,
		WorkflowId: stored.WorkflowID,
	}
}
