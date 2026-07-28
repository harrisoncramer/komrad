// Package agent serves AgentService over ConnectRPC, backed by the store and the
// service that launches agents.
package agent

import (
	"context"
	"errors"
	"fmt"

	"claude-agents/internal/agents"
	"claude-agents/lib/pagination"
	agentv1 "claude-agents/proto/gen/agent/v1"

	agentstore "claude-agents/internal/store/agent"
	transcriptstore "claude-agents/internal/store/transcript"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/emptypb"
)

// agentLauncher is the part of the agent manager the service drives. Creating an
// agent is more than a row. It opens a tmux window and types claude into it, so the
// service delegates rather than writing the record itself.
type agentLauncher interface {
	NewAgent(ctx context.Context, p agents.NewAgentParams) (*agentstore.Agent, error)
	// PaneAlive answers whether an agent is still running, which is read at
	// response time rather than stored so a client needs no tmux of its own.
	PaneAlive(pane string) bool
	DeleteAgent(ctx context.Context, id string) error
	ArchiveAgent(ctx context.Context, name string) error
	RestoreAgent(ctx context.Context, id string) (*agentstore.Agent, error)
}

// agentStore is the part of the agent store this handler reads.
type agentStore interface {
	ListPage(ctx context.Context, limit int64, cursor *agentstore.Cursor, deleted bool) ([]agentstore.Agent, error)
	Get(ctx context.Context, name string) (*agentstore.Agent, error)
	Exists(ctx context.Context, id string) (bool, error)
	SetState(ctx context.Context, name, state string) (bool, error)
	SetMarked(ctx context.Context, id string, marked bool) (bool, error)
	Rename(ctx context.Context, id, name string) (bool, error)
}

// transcriptStore is the part of the transcript store this handler reads. One RPC
// surface covers both domains, but each store is held separately rather than one
// object reaching across both.
type transcriptStore interface {
	Get(ctx context.Context, agentID string) (transcriptstore.Transcript, error)
	Search(ctx context.Context, params transcriptstore.SearchParams) ([]transcriptstore.SearchHit, error)
}

// Service implements agentv1connect.AgentServiceHandler.
type Service struct {
	agents      agentStore
	transcripts transcriptStore
	launcher    agentLauncher
}

// NewService constructs the handler from the stores it reads and the manager
// that launches agents.
func NewService(agents agentStore, transcripts transcriptStore, launcher agentLauncher) *Service {
	return &Service{agents: agents, transcripts: transcripts, launcher: launcher}
}

// CreateAgent launches a background agent and returns it as stored. The request's
// own constraints decide what is well-formed, and a request reaching here has
// already passed them.
func (s *Service) CreateAgent(ctx context.Context, req *connect.Request[agentv1.CreateAgentRequest]) (*connect.Response[agentv1.CreateAgentResponse], error) {
	created, err := s.launcher.NewAgent(ctx, agents.NewAgentParams{
		Title:          req.Msg.GetTitle(),
		Dir:            req.Msg.GetDir(),
		Flags:          req.Msg.GetFlags(),
		Prompt:         req.Msg.GetPrompt(),
		WorkflowID:     req.Msg.GetWorkflowId(),
		IdempotencyKey: req.Msg.GetIdempotencyKey(),
	})
	switch {
	case errors.Is(err, agents.ErrWorkflowNotFound):
		return nil, connect.NewError(connect.CodeNotFound, err)
	case errors.Is(err, agents.ErrBadDirectory):
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	case err != nil:
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to create agent: %w", err))
	}

	return connect.NewResponse(&agentv1.CreateAgentResponse{Agent: renderAgent(*created, s.launcher.PaneAlive(created.Pane)).Agent}), nil
}

// ListAgents returns one page of stored agents, newest first. The request picks
// which set is listed, the live agents or the soft-deleted ones.
func (s *Service) ListAgents(ctx context.Context, req *connect.Request[agentv1.ListAgentsRequest]) (*connect.Response[agentv1.ListAgentsResponse], error) {
	paginator, err := pagination.New[agentstore.Cursor, agent](req.Msg.GetPagination())
	if err != nil {
		return nil, err
	}

	agents, err := s.agents.ListPage(ctx, paginator.Limit(), paginator.Cursor(), req.Msg.GetDeleted())
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to list agents: %w", err))
	}

	resources := make([]agent, 0, len(agents))
	for _, agent := range agents {
		resources = append(resources, renderAgent(agent, s.launcher.PaneAlive(agent.Pane)))
	}

	next, err := paginator.NextToken(resources)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to build the next page token: %w", err))
	}

	response := &agentv1.ListAgentsResponse{NextPageToken: next}
	for _, resource := range resources {
		response.Agents = append(response.Agents, resource.Agent)
	}

	return connect.NewResponse(response), nil
}

// GetAgent returns one agent by name.
func (s *Service) GetAgent(ctx context.Context, req *connect.Request[agentv1.GetAgentRequest]) (*connect.Response[agentv1.GetAgentResponse], error) {
	agent, err := s.agents.Get(ctx, req.Msg.GetName())
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to get agent: %w", err))
	}
	if agent == nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("agent %q not found", req.Msg.GetName()))
	}

	return connect.NewResponse(&agentv1.GetAgentResponse{Agent: renderAgent(*agent, s.launcher.PaneAlive(agent.Pane)).Agent}), nil
}

// SetState updates an agent's state. Naming an agent that does not exist is an
// error rather than a quiet success, so a caller that mistyped a name learns it
// here rather than wondering why nothing changed.
func (s *Service) SetState(ctx context.Context, req *connect.Request[agentv1.SetStateRequest]) (*connect.Response[emptypb.Empty], error) {
	changed, err := s.agents.SetState(ctx, req.Msg.GetName(), req.Msg.GetState())
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to set state: %w", err))
	}
	if !changed {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("agent %q not found", req.Msg.GetName()))
	}

	return connect.NewResponse(&emptypb.Empty{}), nil
}

// SetMarked marks or unmarks an agent. An id matching nothing is an error rather
// than a quiet success, which would leave the caller believing in a mark it never
// made.
func (s *Service) SetMarked(ctx context.Context, req *connect.Request[agentv1.SetMarkedRequest]) (*connect.Response[emptypb.Empty], error) {
	changed, err := s.agents.SetMarked(ctx, req.Msg.GetId(), req.Msg.GetMarked())
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to set the mark: %w", err))
	}
	if !changed {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("no agent with id %q", req.Msg.GetId()))
	}

	return connect.NewResponse(&emptypb.Empty{}), nil
}

// DeleteAgent stops an agent and removes it, along with its config and transcript.
// The manager owns this rather than the store, since stopping the agent means
// killing its pane.
func (s *Service) DeleteAgent(ctx context.Context, req *connect.Request[agentv1.DeleteAgentRequest]) (*connect.Response[emptypb.Empty], error) {
	err := s.launcher.DeleteAgent(ctx, req.Msg.GetId())
	switch {
	case errors.Is(err, agents.ErrAgentNotFound):
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("no agent with id %q", req.Msg.GetId()))
	case err != nil:
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to delete agent: %w", err))
	}

	return connect.NewResponse(&emptypb.Empty{}), nil
}

// ArchiveAgent stops an agent and hides it, keeping the record and transcript.
// This is the reversible half of removing an agent. DeleteAgent is the other.
func (s *Service) ArchiveAgent(ctx context.Context, req *connect.Request[agentv1.ArchiveAgentRequest]) (*connect.Response[emptypb.Empty], error) {
	err := s.launcher.ArchiveAgent(ctx, req.Msg.GetName())
	switch {
	case errors.Is(err, agents.ErrAgentNotFound):
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("agent %q not found", req.Msg.GetName()))
	case err != nil:
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to archive agent: %w", err))
	}

	return connect.NewResponse(&emptypb.Empty{}), nil
}

// RestoreAgent brings an agent back and returns it as stored.
func (s *Service) RestoreAgent(ctx context.Context, req *connect.Request[agentv1.RestoreAgentRequest]) (*connect.Response[agentv1.RestoreAgentResponse], error) {
	restored, err := s.launcher.RestoreAgent(ctx, req.Msg.GetId())
	switch {
	case errors.Is(err, agents.ErrAgentNotFound):
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("no agent with id %q", req.Msg.GetId()))
	case err != nil:
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to restore agent: %w", err))
	}

	return connect.NewResponse(&agentv1.RestoreAgentResponse{Agent: renderAgent(*restored, s.launcher.PaneAlive(restored.Pane)).Agent}), nil
}

// UpdateAgent renames the agent with the given id. A name a live agent already
// holds is a conflict rather than a server fault. The store's unique index refuses
// it, and reporting that as internal would leak a database message to a caller who
// picked a taken name.
func (s *Service) UpdateAgent(ctx context.Context, req *connect.Request[agentv1.UpdateAgentRequest]) (*connect.Response[emptypb.Empty], error) {
	renamed, err := s.agents.Rename(ctx, req.Msg.GetId(), req.Msg.GetName())
	switch {
	case errors.Is(err, agentstore.ErrNameTaken):
		return nil, connect.NewError(connect.CodeAlreadyExists, fmt.Errorf("an agent named %q already exists", req.Msg.GetName()))
	case err != nil:
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to update agent: %w", err))
	case !renamed:
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("no agent with id %q", req.Msg.GetId()))
	}

	return connect.NewResponse(&emptypb.Empty{}), nil
}

// SearchTranscripts runs a full-text search over stored transcripts and returns
// one page of matching agents with a marked snippet, best match first. The page
// token encodes a position in this query's ranking, so replaying it against a
// different query resumes from a rank that means nothing there.
func (s *Service) SearchTranscripts(ctx context.Context, req *connect.Request[agentv1.SearchTranscriptsRequest]) (*connect.Response[agentv1.SearchTranscriptsResponse], error) {
	paginator, err := pagination.New[transcriptstore.Cursor, searchHit](req.Msg.GetPagination())
	if err != nil {
		return nil, err
	}

	hits, err := s.transcripts.Search(ctx, transcriptstore.SearchParams{
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

	response := &agentv1.SearchTranscriptsResponse{NextPageToken: next}
	for _, resource := range resources {
		response.Hits = append(response.Hits, resource.SearchHit)
	}

	return connect.NewResponse(response), nil
}

// GetTranscript returns an agent's full stored transcript by id, so the search
// preview can show a match in its surrounding conversation. An id that matches no
// agent is not_found rather than empty content, which would be indistinguishable
// from an agent that has nothing stored yet.
func (s *Service) GetTranscript(ctx context.Context, req *connect.Request[agentv1.GetTranscriptRequest]) (*connect.Response[agentv1.GetTranscriptResponse], error) {
	exists, err := s.agents.Exists(ctx, req.Msg.GetAgentId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to look up agent: %w", err))
	}
	if !exists {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("no agent with id %q", req.Msg.GetAgentId()))
	}

	tr, err := s.transcripts.Get(ctx, req.Msg.GetAgentId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to get transcript: %w", err))
	}

	return connect.NewResponse(&agentv1.GetTranscriptResponse{Content: tr.Content}), nil
}
