// Package rpc serves the SessionService over ConnectRPC, backed by the store.
package rpc

import (
	"context"
	"fmt"
	"time"

	"claude-agents/internal/store"
	sessionv1 "claude-agents/proto/gen/session/v1"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/emptypb"
)

// SessionService implements sessionv1connect.SessionServiceHandler over the store.
type SessionService struct {
	store *store.Store
}

// NewSessionService constructs the service from the agent store.
func NewSessionService(s *store.Store) *SessionService {
	return &SessionService{store: s}
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

// SetState updates an agent's state.
func (s *SessionService) SetState(ctx context.Context, req *connect.Request[sessionv1.SetStateRequest]) (*connect.Response[emptypb.Empty], error) {
	if err := s.store.SetState(ctx, req.Msg.GetName(), req.Msg.GetState()); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to set state: %w", err))
	}

	return connect.NewResponse(&emptypb.Empty{}), nil
}

// DeleteSession removes an agent by name.
func (s *SessionService) DeleteSession(ctx context.Context, req *connect.Request[sessionv1.DeleteSessionRequest]) (*connect.Response[emptypb.Empty], error) {
	if err := s.store.Delete(ctx, req.Msg.GetName()); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to delete session: %w", err))
	}

	return connect.NewResponse(&emptypb.Empty{}), nil
}

// UpdateSession renames the session with the given id.
func (s *SessionService) UpdateSession(ctx context.Context, req *connect.Request[sessionv1.UpdateSessionRequest]) (*connect.Response[emptypb.Empty], error) {
	if err := s.store.Rename(ctx, req.Msg.GetId(), req.Msg.GetName()); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to update session: %w", err))
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
func (s *SessionService) GetTranscript(ctx context.Context, req *connect.Request[sessionv1.GetTranscriptRequest]) (*connect.Response[sessionv1.GetTranscriptResponse], error) {
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
