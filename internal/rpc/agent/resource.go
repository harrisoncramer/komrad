package agent

import (
	"time"

	agentstore "claude-agents/internal/store/agent"
	transcriptstore "claude-agents/internal/store/transcript"
	agentv1 "claude-agents/proto/gen/agent/v1"
)

// Every resource the service lists is paged the same way. The handler renders
// stored rows into one of the types below, each pairing the proto message the
// caller sees with the cursor locating it, and hands the slice to a paginator. The
// cursor lives beside the proto rather than inside it, since it is the server's
// bookkeeping rather than part of the resource.

// agent is a rendered agent, the proto sent to the caller plus the cursor that
// finds it again in the listing.
type agent struct {
	*agentv1.Agent
	cursor agentstore.Cursor
}

// Cursor returns the keyset cursor locating this agent.
func (s agent) Cursor() (agentstore.Cursor, error) {
	return s.cursor, nil
}

// renderAgent renders a stored agent into the resource the API returns. Liveness
// is observed rather than stored, so it is passed in by whoever can ask tmux. The
// row itself cannot know whether its pane is still there.
func renderAgent(stored agentstore.Agent, alive bool) agent {
	created := ""
	if !stored.CreatedAt.IsZero() {
		created = stored.CreatedAt.Format(time.RFC3339)
	}

	return agent{
		Agent: &agentv1.Agent{
			Id:              stored.ID,
			Name:            stored.Name,
			Cwd:             stored.Cwd,
			Flags:           stored.Flags,
			Prompt:          stored.Prompt,
			PaneId:          stored.Pane,
			State:           stored.State,
			ClaudeSessionId: stored.ClaudeSessionID,
			WorkflowId:      stored.WorkflowID,
			ScheduleId:      stored.ScheduleID,
			CreatedAt:       created,
			Alive:           alive,
			Mark:            stored.Mark,
			Hidden:          stored.Hidden,
		},
		cursor: agentstore.Cursor{ID: stored.ID},
	}
}

// searchHit is a rendered transcript match, the proto sent to the caller plus the
// cursor that finds it again among the results for the same query.
type searchHit struct {
	*agentv1.SearchHit
	cursor transcriptstore.Cursor
}

// Cursor returns the keyset cursor locating this hit.
func (h searchHit) Cursor() (transcriptstore.Cursor, error) {
	return h.cursor, nil
}

// renderSearchHit renders a stored search hit into a searchHit resource.
func renderSearchHit(hit transcriptstore.SearchHit) searchHit {
	return searchHit{
		SearchHit: &agentv1.SearchHit{
			AgentId:     hit.AgentID,
			Name:        hit.Name,
			Deleted:     hit.Deleted,
			Snippet:     hit.Snippet,
			Occurrences: int64(hit.Rank),
		},
		cursor: transcriptstore.Cursor{Rank: hit.Rank, AgentID: hit.AgentID},
	}
}
