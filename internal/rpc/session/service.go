// Package session serves SessionService over ConnectRPC, backed by the store and
// the service that launches chats.
package session

import (
	"context"
	"errors"
	"fmt"

	"claude-agents/internal/agents"
	"claude-agents/internal/store"
	"claude-agents/lib/pagination"
	sessionv1 "claude-agents/proto/gen/session/v1"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/emptypb"
)

// sessionLauncher is the part of the agent manager the service drives. Creating
// a chat is more than a row: it opens a tmux window and types claude into it, so
// the service delegates rather than writing the record itself.
type sessionLauncher interface {
	NewSession(p agents.NewSessionParams) (*store.Agent, error)
	// PaneAlive answers whether a chat is still running, which is read at
	// response time rather than stored so a client needs no tmux of its own.
	PaneAlive(pane string) bool
	DeleteSession(ctx context.Context, name string) error
	ArchiveSession(ctx context.Context, name string) error
	RestoreSession(ctx context.Context, id string) (*store.Agent, error)
}

// Service implements sessionv1connect.SessionServiceHandler over the store.
type Service struct {
	store    *store.Store
	launcher sessionLauncher
}

// NewService constructs the handler from the agent store and the manager that
// launches chats.
func NewService(s *store.Store, launcher sessionLauncher) *Service {
	return &Service{store: s, launcher: launcher}
}

// CreateSession launches a background agent and returns it as stored. The
// request's own constraints decide what is well-formed; by the time a request
// reaches here it has already passed them.
func (s *Service) CreateSession(_ context.Context, req *connect.Request[sessionv1.CreateSessionRequest]) (*connect.Response[sessionv1.CreateSessionResponse], error) {
	created, err := s.launcher.NewSession(agents.NewSessionParams{
		Title:      req.Msg.GetTitle(),
		Dir:        req.Msg.GetDir(),
		Flags:      req.Msg.GetFlags(),
		Prompt:     req.Msg.GetPrompt(),
		WorkflowID: req.Msg.WorkflowId,
	})
	switch {
	case errors.Is(err, agents.ErrWorkflowNotFound):
		return nil, connect.NewError(connect.CodeNotFound, err)
	case errors.Is(err, agents.ErrBadDirectory):
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	case err != nil:
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to create session: %w", err))
	}

	return connect.NewResponse(&sessionv1.CreateSessionResponse{Session: renderSession(*created, s.launcher.PaneAlive(created.Pane)).Session}), nil
}

// ListSessions returns one page of stored agents, most recently updated first.
func (s *Service) ListSessions(ctx context.Context, req *connect.Request[sessionv1.ListSessionsRequest]) (*connect.Response[sessionv1.ListSessionsResponse], error) {
	paginator, err := pagination.New[store.SessionCursor, session](req.Msg.GetPagination())
	if err != nil {
		return nil, err
	}

	agents, err := s.store.ListPage(ctx, paginator.Limit(), paginator.Cursor())
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to list sessions: %w", err))
	}

	resources := make([]session, 0, len(agents))
	for _, agent := range agents {
		resources = append(resources, renderSession(agent, s.launcher.PaneAlive(agent.Pane)))
	}

	next, err := paginator.NextToken(resources)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to build the next page token: %w", err))
	}

	response := &sessionv1.ListSessionsResponse{NextPageToken: next}
	for _, resource := range resources {
		response.Sessions = append(response.Sessions, resource.Session)
	}

	return connect.NewResponse(response), nil
}

// GetSession returns one agent by name.
func (s *Service) GetSession(ctx context.Context, req *connect.Request[sessionv1.GetSessionRequest]) (*connect.Response[sessionv1.GetSessionResponse], error) {
	agent, err := s.store.Get(ctx, req.Msg.GetName())
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to get session: %w", err))
	}
	if agent == nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("session %q not found", req.Msg.GetName()))
	}

	return connect.NewResponse(&sessionv1.GetSessionResponse{Session: renderSession(*agent, s.launcher.PaneAlive(agent.Pane)).Session}), nil
}

// SetState updates an agent's state. Naming a chat that does not exist is an
// error rather than a quiet success, so a caller that mistyped a name learns it
// here instead of wondering why nothing changed.
func (s *Service) SetState(ctx context.Context, req *connect.Request[sessionv1.SetStateRequest]) (*connect.Response[emptypb.Empty], error) {
	changed, err := s.store.SetState(ctx, req.Msg.GetName(), req.Msg.GetState())
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to set state: %w", err))
	}
	if !changed {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("session %q not found", req.Msg.GetName()))
	}

	return connect.NewResponse(&emptypb.Empty{}), nil
}

// DeleteSession stops an agent and removes it, along with its config and
// transcript. The manager owns this rather than the store, since stopping the
// agent means killing its pane.
func (s *Service) DeleteSession(ctx context.Context, req *connect.Request[sessionv1.DeleteSessionRequest]) (*connect.Response[emptypb.Empty], error) {
	err := s.launcher.DeleteSession(ctx, req.Msg.GetName())
	switch {
	case errors.Is(err, agents.ErrSessionNotFound):
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("session %q not found", req.Msg.GetName()))
	case err != nil:
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to delete session: %w", err))
	}

	return connect.NewResponse(&emptypb.Empty{}), nil
}

// ArchiveSession stops a chat and hides it, keeping the record and transcript.
// This is the reversible half of removing a chat; DeleteSession is the other.
func (s *Service) ArchiveSession(ctx context.Context, req *connect.Request[sessionv1.ArchiveSessionRequest]) (*connect.Response[emptypb.Empty], error) {
	err := s.launcher.ArchiveSession(ctx, req.Msg.GetName())
	switch {
	case errors.Is(err, agents.ErrSessionNotFound):
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("session %q not found", req.Msg.GetName()))
	case err != nil:
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to archive session: %w", err))
	}

	return connect.NewResponse(&emptypb.Empty{}), nil
}

// RestoreSession brings a chat back and returns it as stored.
func (s *Service) RestoreSession(ctx context.Context, req *connect.Request[sessionv1.RestoreSessionRequest]) (*connect.Response[sessionv1.RestoreSessionResponse], error) {
	restored, err := s.launcher.RestoreSession(ctx, req.Msg.GetId())
	switch {
	case errors.Is(err, agents.ErrSessionNotFound):
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("no chat with id %q", req.Msg.GetId()))
	case err != nil:
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to restore session: %w", err))
	}

	return connect.NewResponse(&sessionv1.RestoreSessionResponse{Session: renderSession(*restored, s.launcher.PaneAlive(restored.Pane)).Session}), nil
}

// UpdateSession renames the session with the given id. A name already taken by
// a live chat is a conflict, not a server fault: the store's unique index is
// what refuses it, and reporting that as internal would leak a SQLite message
// to a caller who simply picked a taken name.
func (s *Service) UpdateSession(ctx context.Context, req *connect.Request[sessionv1.UpdateSessionRequest]) (*connect.Response[emptypb.Empty], error) {
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
// one page of matching sessions with a marked snippet, best match first. The
// page token encodes a position in this query's ranking, so replaying it against
// a different query resumes from a rank that means nothing there.
func (s *Service) SearchTranscripts(ctx context.Context, req *connect.Request[sessionv1.SearchTranscriptsRequest]) (*connect.Response[sessionv1.SearchTranscriptsResponse], error) {
	paginator, err := pagination.New[store.TranscriptCursor, searchHit](req.Msg.GetPagination())
	if err != nil {
		return nil, err
	}

	hits, err := s.store.SearchTranscripts(ctx, store.SearchTranscriptsParams{
		Query:  req.Msg.GetQuery(),
		Limit:  paginator.Limit(),
		Cursor: paginator.Cursor(),
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to search transcripts: %w", err))
	}

	resources := make([]searchHit, 0, len(hits))
	for _, hit := range hits {
		resources = append(resources, renderSearchHit(hit))
	}

	next, err := paginator.NextToken(resources)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to build the next page token: %w", err))
	}

	response := &sessionv1.SearchTranscriptsResponse{NextPageToken: next}
	for _, resource := range resources {
		response.Hits = append(response.Hits, resource.SearchHit)
	}

	return connect.NewResponse(response), nil
}

// GetTranscript returns a session's full stored transcript by id, so the search
// preview can show a match in its surrounding conversation.
// An id that matches no chat is not_found rather than empty content: the two
// were indistinguishable, and since a listed chat carries both an id and a
// claudeSessionId, reaching for the wrong one failed silently.
func (s *Service) GetTranscript(ctx context.Context, req *connect.Request[sessionv1.GetTranscriptRequest]) (*connect.Response[sessionv1.GetTranscriptResponse], error) {
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
