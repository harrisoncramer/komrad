package rpc

import (
	"time"

	"claude-agents/internal/store"
	sessionv1 "claude-agents/proto/gen/session/v1"
	workflowv1 "claude-agents/proto/gen/workflow/v1"
)

// Every resource the service lists is paged the same way: the handler renders
// stored rows into one of the types below, each of which pairs the proto message
// the caller sees with the cursor locating it, and hands the slice to a
// paginator. The cursor lives beside the proto rather than inside it because it
// is the server's bookkeeping, not part of the resource.

// session is a rendered chat: the proto sent to the caller and the cursor that
// finds it again in the listing.
type session struct {
	*sessionv1.Session
	cursor store.SessionCursor
}

// Cursor returns the keyset cursor locating this chat.
func (s session) Cursor() (store.SessionCursor, error) {
	return s.cursor, nil
}

// renderSession renders a stored agent into a session resource.
func renderSession(agent store.Agent) session {
	created := ""
	if !agent.CreatedAt.IsZero() {
		created = agent.CreatedAt.Format(time.RFC3339)
	}

	return session{
		Session: &sessionv1.Session{
			Id:              agent.ID,
			Name:            agent.Name,
			Cwd:             agent.Cwd,
			Flags:           agent.Flags,
			Prompt:          agent.Prompt,
			PaneId:          agent.Pane,
			State:           agent.State,
			ClaudeSessionId: agent.ClaudeSessionID,
			WorkflowId:      agent.WorkflowID,
			CreatedAt:       created,
		},
		cursor: store.SessionCursor{UpdatedAt: agent.UpdatedAt, ID: agent.ID},
	}
}

// searchHit is a rendered transcript match: the proto sent to the caller and the
// cursor that finds it again among the results for the same query.
type searchHit struct {
	*sessionv1.SearchHit
	cursor store.TranscriptCursor
}

// Cursor returns the keyset cursor locating this hit.
func (h searchHit) Cursor() (store.TranscriptCursor, error) {
	return h.cursor, nil
}

// renderSearchHit renders a stored search hit into a searchHit resource.
func renderSearchHit(hit store.SearchHit) searchHit {
	return searchHit{
		SearchHit: &sessionv1.SearchHit{
			SessionId:   hit.SessionID,
			Name:        hit.Name,
			Deleted:     hit.Deleted,
			Snippet:     hit.Snippet,
			Occurrences: int64(hit.Rank),
		},
		cursor: store.TranscriptCursor{Rank: hit.Rank, SessionID: hit.SessionID},
	}
}

// workflow is a rendered workflow: the proto sent to the caller and the cursor
// that finds it again in the listing.
type workflow struct {
	*workflowv1.Workflow
	cursor store.WorkflowCursor
}

// Cursor returns the keyset cursor locating this workflow.
func (w workflow) Cursor() (store.WorkflowCursor, error) {
	return w.cursor, nil
}

// renderWorkflow renders a stored workflow into a workflow resource.
func renderWorkflow(w store.Workflow) workflow {
	return workflow{
		Workflow: &workflowv1.Workflow{
			Id:      w.ID,
			Name:    w.Name,
			Context: w.Context,
			Cwd:     w.Cwd,
			Flags:   w.Flags,
		},
		cursor: store.WorkflowCursor{UpdatedAt: w.UpdatedAt, ID: w.ID},
	}
}
