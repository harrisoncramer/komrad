// Package rpc serves the SessionService over ConnectRPC, backed by the store.
package rpc

import (
	"context"
	"errors"
	"fmt"
	"time"

	"claude-agents/internal/agents"
	"claude-agents/internal/store"
	sessionv1 "claude-agents/proto/gen/session/v1"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/emptypb"
)

// sessionLauncher is the part of the agent manager the service drives. Creating
// a chat is more than a row: it opens a tmux window and types claude into it, so
// the service delegates rather than writing the record itself.
type sessionLauncher interface {
	NewSession(p agents.NewSessionParams) (*store.Agent, error)
}

// SessionService implements sessionv1connect.SessionServiceHandler over the store.
type SessionService struct {
	store    *store.Store
	launcher sessionLauncher
}

// NewSessionService constructs the service from the agent store and the manager
// that launches chats.
func NewSessionService(s *store.Store, launcher sessionLauncher) *SessionService {
	return &SessionService{store: s, launcher: launcher}
}

// CreateSession launches a background agent and returns it as stored. The
// request's own constraints decide what is well-formed; by the time a request
// reaches here it has already passed them.
func (s *SessionService) CreateSession(_ context.Context, req *connect.Request[sessionv1.CreateSessionRequest]) (*connect.Response[sessionv1.CreateSessionResponse], error) {
	created, err := s.launcher.NewSession(agents.NewSessionParams{
		Title:      req.Msg.GetTitle(),
		Dir:        req.Msg.GetDir(),
		Flags:      req.Msg.GetFlags(),
		Prompt:     req.Msg.GetPrompt(),
		WorkflowID: req.Msg.WorkflowId,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to create session: %w", err))
	}

	return connect.NewResponse(&sessionv1.CreateSessionResponse{Session: toProto(*created)}), nil
}

// ListSessions returns every stored agent.
func (s *SessionService) ListSessions(ctx context.Context, _ *connect.Request[emptypb.Empty]) (*connect.Response[sessionv1.ListSessionsResponse], error) {
	agents, err := s.store.List(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to list sessions: %w", err))
	}

	response := &sessionv1.ListSessionsResponse{}
	for _, agent := range agents {
		response.Sessions = append(response.Sessions, toProto(agent))
	}

	return connect.NewResponse(response), nil
}

// GetSession returns one agent by name.
func (s *SessionService) GetSession(ctx context.Context, req *connect.Request[sessionv1.GetSessionRequest]) (*connect.Response[sessionv1.GetSessionResponse], error) {
	agent, err := s.store.Get(ctx, req.Msg.GetName())
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to get session: %w", err))
	}
	if agent == nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("session %q not found", req.Msg.GetName()))
	}

	return connect.NewResponse(&sessionv1.GetSessionResponse{Session: toProto(*agent)}), nil
}

// SetState updates an agent's state. Naming a chat that does not exist is an
// error rather than a quiet success, so a caller that mistyped a name learns it
// here instead of wondering why nothing changed.
func (s *SessionService) SetState(ctx context.Context, req *connect.Request[sessionv1.SetStateRequest]) (*connect.Response[emptypb.Empty], error) {
	changed, err := s.store.SetState(ctx, req.Msg.GetName(), req.Msg.GetState())
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to set state: %w", err))
	}
	if !changed {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("session %q not found", req.Msg.GetName()))
	}

	return connect.NewResponse(&emptypb.Empty{}), nil
}

// DeleteSession removes an agent by name, along with its config and transcript.
func (s *SessionService) DeleteSession(ctx context.Context, req *connect.Request[sessionv1.DeleteSessionRequest]) (*connect.Response[emptypb.Empty], error) {
	deleted, err := s.store.Delete(ctx, req.Msg.GetName())
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to delete session: %w", err))
	}
	if !deleted {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("session %q not found", req.Msg.GetName()))
	}

	return connect.NewResponse(&emptypb.Empty{}), nil
}

// UpdateSession renames the session with the given id. A name already taken by
// a live chat is a conflict, not a server fault: the store's unique index is
// what refuses it, and reporting that as internal would leak a SQLite message
// to a caller who simply picked a taken name.
func (s *SessionService) UpdateSession(ctx context.Context, req *connect.Request[sessionv1.UpdateSessionRequest]) (*connect.Response[emptypb.Empty], error) {
	renamed, err := s.store.Rename(ctx, req.Msg.GetId(), req.Msg.GetName())
	switch {
	case errors.Is(err, store.ErrNameTaken):
		return nil, connect.NewError(connect.CodeAlreadyExists, fmt.Errorf("a chat named %q already exists", req.Msg.GetName()))
	case err != nil:
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to update session: %w", err))
	case !renamed:
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("no chat with id %q", req.Msg.GetId()))
	}

	return connect.NewResponse(&emptypb.Empty{}), nil
}

// SearchTranscripts runs a full-text search over stored transcripts and returns
// the matching sessions with a marked snippet, best match first.
func (s *SessionService) SearchTranscripts(ctx context.Context, req *connect.Request[sessionv1.SearchTranscriptsRequest]) (*connect.Response[sessionv1.SearchTranscriptsResponse], error) {
	hits, err := s.store.SearchTranscripts(ctx, req.Msg.GetQuery(), int(req.Msg.GetLimit()))
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to search transcripts: %w", err))
	}

	response := &sessionv1.SearchTranscriptsResponse{}
	for _, hit := range hits {
		response.Hits = append(response.Hits, &sessionv1.SearchHit{
			SessionId: hit.SessionID,
			Name:      hit.Name,
			Deleted:   hit.Deleted,
			Snippet:   hit.Snippet,
			Rank:      hit.Rank,
		})
	}

	return connect.NewResponse(response), nil
}

// GetTranscript returns a session's full stored transcript by id, so the search
// preview can show a match in its surrounding conversation.
// An id that matches no chat is not_found rather than empty content: the two
// were indistinguishable, and since a listed chat carries both an id and a
// claudeSessionId, reaching for the wrong one failed silently.
func (s *SessionService) GetTranscript(ctx context.Context, req *connect.Request[sessionv1.GetTranscriptRequest]) (*connect.Response[sessionv1.GetTranscriptResponse], error) {
	exists, err := s.store.SessionExists(ctx, req.Msg.GetSessionId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to look up session: %w", err))
	}
	if !exists {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("no chat with id %q", req.Msg.GetSessionId()))
	}

	tr, err := s.store.GetTranscript(ctx, req.Msg.GetSessionId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to get transcript: %w", err))
	}

	return connect.NewResponse(&sessionv1.GetTranscriptResponse{Content: tr.Content}), nil
}

func toProto(agent store.Agent) *sessionv1.Session {
	created := ""
	if !agent.CreatedAt.IsZero() {
		created = agent.CreatedAt.Format(time.RFC3339)
	}

	return &sessionv1.Session{
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
	}
}
