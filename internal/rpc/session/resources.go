package session

import (
	"time"

	"claude-agents/internal/store"
	sessionv1 "claude-agents/proto/gen/session/v1"
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

// renderSession renders a stored agent into a session resource. Liveness is
// observed rather than stored, so it is passed in by whoever can ask tmux: the
// row itself cannot know whether its pane is still there.
func renderSession(agent store.Agent, alive bool) session {
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
			Alive:           alive,
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
