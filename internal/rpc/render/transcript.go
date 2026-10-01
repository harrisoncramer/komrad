package render

import (
	"komrad/internal/transcripts"
	agentv1 "komrad/proto/gen/agent/v1"
)

// Turn draws one turn of a conversation as the wire carries it.
func Turn(turn transcripts.Turn) *agentv1.Turn {
	return &agentv1.Turn{
		Role:      turn.Role,
		Seq:       turn.Seq,
		Content:   turn.Content,
		Truncated: turn.Truncated,
	}
}
