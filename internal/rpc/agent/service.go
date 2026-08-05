// Package agent serves AgentService over ConnectRPC, backed by the store and the
// service that launches agents.
package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"weave/internal/agents"
	"weave/internal/caller"
	"weave/internal/harness"
	"weave/internal/logging"
	"weave/internal/store"
	"weave/lib/pagination"
	"weave/lib/xslice"
	agentv1 "weave/proto/gen/agent/v1"

	agentstore "weave/internal/store/agent"
	messagestore "weave/internal/store/message"
	transcriptstore "weave/internal/store/transcript"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/emptypb"
)

// agentLauncher is the part of the agent manager the service drives. Creating an agent
// opens a tmux window and types a command line into it, so the service delegates rather
// than writing the record itself.
type agentLauncher interface {
	NewAgent(ctx context.Context, p agents.NewAgentParams) (*agentstore.Agent, error)
	IsAgentRunning(agent agentstore.Agent) bool
	GetRunningAgents(ctx context.Context, agents []agentstore.Agent) map[string]bool
	DeleteAgents(ctx context.Context, ids []string) error
	ArchiveAgents(ctx context.Context, ids []string) error
	RestoreAgents(ctx context.Context, ids []string) ([]agentstore.Agent, error)
	ResumeAgent(ctx context.Context, id string) (*agentstore.Agent, error)
}

type agentReadWriter interface {
	ListPage(ctx context.Context, limit int64, cursor *agentstore.Cursor, archived bool) ([]agentstore.Agent, error)
	GetByID(ctx context.Context, id string) (*agentstore.Agent, error)
	Exists(ctx context.Context, id string) (bool, error)
	SetMarks(ctx context.Context, ids []string, mark *string) (int, error)
	SetHidden(ctx context.Context, id string, hidden bool) (bool, error)
	SetMetadata(ctx context.Context, params agentstore.SetMetadataParams) (bool, error)
	Rename(ctx context.Context, id, name string) (bool, error)
}

// transcriptReader is the part of the transcript store this handler reads. One RPC surface
// covers both domains, but each store is held separately.
type transcriptReader interface {
	Get(ctx context.Context, agentID string) (transcriptstore.Transcript, error)
	Search(ctx context.Context, params transcriptstore.SearchParams) ([]transcriptstore.SearchHit, error)
}

type messenger interface {
	SendMessage(ctx context.Context, params agents.SendMessageParams) (*messagestore.Message, error)
	ClaimMessages(ctx context.Context, agentID string) ([]messagestore.Message, error)
}

// Service implements agentv1connect.AgentServiceHandler.
type Service struct {
	agents      agentReadWriter
	transcripts transcriptReader
	messages    messenger
	launcher    agentLauncher
}

type NewServiceParams struct {
	Agents      agentReadWriter
	Transcripts transcriptReader
	Messages    messenger
	Launcher    agentLauncher
}

// NewService constructs the handler from the stores it reads and the manager that launches
// agents.
func NewService(p NewServiceParams) *Service {
	return &Service{
		agents:      p.Agents,
		transcripts: p.Transcripts,
		messages:    p.Messages,
		launcher:    p.Launcher,
	}
}

// CreateAgent launches a background agent and returns it as stored. A request reaching
// here has already passed the constraints declared in the proto.
func (s *Service) CreateAgent(ctx context.Context, req *connect.Request[agentv1.CreateAgentRequest]) (*connect.Response[agentv1.CreateAgentResponse], error) {
	created, err := s.launcher.NewAgent(ctx, agents.NewAgentParams{
		Title:          req.Msg.GetTitle(),
		Dir:            req.Msg.GetDir(),
		Flags:          req.Msg.GetFlags(),
		SetupAction:    req.Msg.SetupAction,
		CleanupAction:  req.Msg.CleanupAction,
		Prompt:         req.Msg.GetPrompt(),
		WorkflowID:     req.Msg.GetWorkflowId(),
		Harness:        req.Msg.GetHarness(),
		IdempotencyKey: req.Msg.GetIdempotencyKey(),
	})
	switch {
	case errors.Is(err, agents.ErrWorkflowNotFound):
		return nil, connect.NewError(connect.CodeNotFound, err)
	case errors.Is(err, agents.ErrSetupFailed):
		return nil, connect.NewError(connect.CodeFailedPrecondition, err)
	case errors.Is(err, agents.ErrBadDirectory), errors.Is(err, harness.ErrNotRegistered):
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	case errors.Is(err, agents.ErrTooManyAgents):
		return nil, connect.NewError(connect.CodeResourceExhausted, err)
	case err != nil:
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to create agent: %w", err))
	}

	return connect.NewResponse(&agentv1.CreateAgentResponse{Agent: renderAgent(*created, s.launcher.IsAgentRunning(*created)).Agent}), nil
}

// ListAgents returns one page of stored agents, newest first. The request picks which set
// is listed, the live agents or the archived ones.
func (s *Service) ListAgents(ctx context.Context, req *connect.Request[agentv1.ListAgentsRequest]) (*connect.Response[agentv1.ListAgentsResponse], error) {
	paginator, err := pagination.New[agentstore.Cursor, agent](req.Msg.GetPagination())
	if err != nil {
		return nil, err
	}

	agents, err := s.agents.ListPage(ctx, paginator.Limit(), paginator.Cursor(), req.Msg.GetArchived())
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to list agents: %w", err))
	}

	running := s.launcher.GetRunningAgents(ctx, agents)

	resources := xslice.Map(agents, func(a agentstore.Agent) agent { return renderAgent(a, running[a.ID]) })

	next, err := paginator.NextToken(resources)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to build the next page token: %w", err))
	}

	response := &agentv1.ListAgentsResponse{NextPageToken: next}
	response.Agents = xslice.Map(resources, func(resource agent) *agentv1.Agent { return resource.Agent })

	return connect.NewResponse(response), nil
}

// GetAgent returns one agent by id, archived or not.
func (s *Service) GetAgent(ctx context.Context, req *connect.Request[agentv1.GetAgentRequest]) (*connect.Response[agentv1.GetAgentResponse], error) {
	agent, err := s.agents.GetByID(ctx, req.Msg.GetId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to get agent: %w", err))
	}
	if agent == nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("no agent with id %q", req.Msg.GetId()))
	}

	return connect.NewResponse(&agentv1.GetAgentResponse{Agent: renderAgent(*agent, s.launcher.IsAgentRunning(*agent)).Agent}), nil
}

// SetMarks marks every agent named, and an unset mark clears the ones they carry. A
// request that reached nothing at all is an error rather than a quiet success, which would
// leave the caller believing in a mark it never made.
//
// One that reached some of its ids is not an error. A mark is drawn on a row and nothing
// more, and an agent archived between the caller choosing it and the write landing has no
// row left to draw one on.
func (s *Service) SetMarks(ctx context.Context, req *connect.Request[agentv1.SetMarksRequest]) (*connect.Response[emptypb.Empty], error) {
	marked, err := s.agents.SetMarks(ctx, req.Msg.GetIds(), req.Msg.Mark)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to set the mark: %w", err))
	}
	if marked == 0 {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("none of the %d ids given is a live agent", len(req.Msg.GetIds())))
	}

	return connect.NewResponse(&emptypb.Empty{}), nil
}

// SetHidden hides an agent from the panel's list or brings it back. An id matching nothing
// is an error, for the reason it is on a mark.
func (s *Service) SetHidden(ctx context.Context, req *connect.Request[agentv1.SetHiddenRequest]) (*connect.Response[emptypb.Empty], error) {
	changed, err := s.agents.SetHidden(ctx, req.Msg.GetId(), req.Msg.GetHidden())
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to hide the agent: %w", err))
	}
	if !changed {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("no agent with id %q", req.Msg.GetId()))
	}

	return connect.NewResponse(&emptypb.Empty{}), nil
}

// SetMetadata records one fact an agent found out about its own work, or clears the key
// when the value is unset. An id matching nothing is an error, as it is on a mark, but
// clearing a key nothing held is not: the agent asked for that key to be gone and it is.
func (s *Service) SetMetadata(ctx context.Context, req *connect.Request[agentv1.SetMetadataRequest]) (*connect.Response[emptypb.Empty], error) {
	changed, err := s.agents.SetMetadata(ctx, agentstore.SetMetadataParams{
		ID:    req.Msg.GetId(),
		Key:   req.Msg.GetKey(),
		Value: req.Msg.Value,
	})
	switch {
	case errors.Is(err, agentstore.ErrTooManyMetadataKeys):
		return nil, connect.NewError(connect.CodeResourceExhausted, fmt.Errorf("agent %q already holds as much metadata as it may", req.Msg.GetId()))
	case err != nil:
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to set the metadata: %w", err))
	}
	if !changed && req.Msg.Value != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("no agent with id %q", req.Msg.GetId()))
	}

	return connect.NewResponse(&emptypb.Empty{}), nil
}

// Log writes one line to weave's log, tagged to the given agent. It is the channel a
// running action, or anything else acting on an agent's behalf, uses to say what it did on
// purpose, since an action's own stdout goes nowhere and its stderr is read back only when
// it fails.
func (s *Service) Log(ctx context.Context, req *connect.Request[agentv1.LogRequest]) (*connect.Response[emptypb.Empty], error) {
	id := callerScope(ctx, req.Msg.GetId())
	if id == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("no id given, and the caller has none of its own to fall back to"))
	}

	agent, err := s.agents.GetByID(ctx, id)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to get agent: %w", err))
	}
	if agent == nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("no agent with id %q", id))
	}

	attrs := []slog.Attr{
		slog.String(logging.TargetAgentID, agent.ID),
		slog.String(logging.ActionAgentCwd, agent.Cwd),
	}
	if action := req.Msg.GetAction(); action != "" {
		attrs = append(attrs, slog.String(logging.ActionName, action))
	}

	logging.From(ctx).LogAttrs(ctx, slog.LevelInfo, req.Msg.GetMessage(), attrs...)

	return connect.NewResponse(&emptypb.Empty{}), nil
}

// callerScope is the agent a call is about: the one named, or the caller's own when none is,
// the same fallback searchScope gives a search naming nobody.
func callerScope(ctx context.Context, requested string) string {
	if requested != "" {
		return requested
	}

	return caller.From(ctx).AgentID
}

func (s *Service) SendMessage(ctx context.Context, req *connect.Request[agentv1.SendMessageRequest]) (*connect.Response[agentv1.SendMessageResponse], error) {
	sent, err := s.messages.SendMessage(ctx, agents.SendMessageParams{
		From:      caller.From(ctx).AgentID,
		ToAgentID: req.Msg.GetToAgentId(),
		Body:      req.Msg.GetBody(),
		ReplyTo:   req.Msg.ReplyTo,
	})
	switch {
	case errors.Is(err, agents.ErrRecipientNotFound):
		return nil, connect.NewError(connect.CodeNotFound, err)
	case errors.Is(err, agents.ErrAgentArchived):
		return nil, connect.NewError(connect.CodeFailedPrecondition, err)
	case errors.Is(err, agents.ErrSelfAddressed), errors.Is(err, agents.ErrSenderUnknown):
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	case errors.Is(err, agents.ErrMailboxFull):
		return nil, connect.NewError(connect.CodeResourceExhausted, err)
	case err != nil:
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to send the message: %w", err))
	}

	return connect.NewResponse(&agentv1.SendMessageResponse{Message: renderMessage(*sent)}), nil
}

func (s *Service) ReadMessages(ctx context.Context, req *connect.Request[agentv1.ReadMessagesRequest]) (*connect.Response[agentv1.ReadMessagesResponse], error) {
	id := callerScope(ctx, req.Msg.GetId())
	if id == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("no id given, and the caller has none of its own to fall back to"))
	}

	logCrossAgentRead(ctx, id, "agent read another agent's mail")

	claimed, err := s.messages.ClaimMessages(ctx, id)
	switch {
	case errors.Is(err, agents.ErrAgentNotFound):
		return nil, connect.NewError(connect.CodeNotFound, err)
	case err != nil:
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to read the mail: %w", err))
	}

	return connect.NewResponse(&agentv1.ReadMessagesResponse{
		Messages: xslice.Map(claimed, renderMessage),
	}), nil
}

// DeleteAgents stops agents and removes them with their configs and transcripts. The
// manager owns this rather than the store, since stopping an agent means killing its pane.
func (s *Service) DeleteAgents(ctx context.Context, req *connect.Request[agentv1.DeleteAgentsRequest]) (*connect.Response[emptypb.Empty], error) {
	err := s.launcher.DeleteAgents(ctx, req.Msg.GetIds())
	switch {
	case errors.Is(err, agents.ErrAgentNotFound):
		return nil, connect.NewError(connect.CodeNotFound, err)
	case err != nil:
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to delete agents: %w", err))
	}

	return connect.NewResponse(&emptypb.Empty{}), nil
}

// ArchiveAgents stops agents and hides them, keeping the records and transcripts. It is the
// reversible half of removing agents, and DeleteAgents the other.
func (s *Service) ArchiveAgents(ctx context.Context, req *connect.Request[agentv1.ArchiveAgentsRequest]) (*connect.Response[emptypb.Empty], error) {
	err := s.launcher.ArchiveAgents(ctx, req.Msg.GetIds())
	switch {
	case errors.Is(err, agents.ErrAgentNotFound):
		return nil, connect.NewError(connect.CodeNotFound, err)
	case err != nil:
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to archive agents: %w", err))
	}

	return connect.NewResponse(&emptypb.Empty{}), nil
}

// RestoreAgents takes agents out of the archive and returns them as stored, dormant.
func (s *Service) RestoreAgents(ctx context.Context, req *connect.Request[agentv1.RestoreAgentsRequest]) (*connect.Response[agentv1.RestoreAgentsResponse], error) {
	restored, err := s.launcher.RestoreAgents(ctx, req.Msg.GetIds())
	switch {
	case errors.Is(err, agents.ErrAgentNotFound):
		return nil, connect.NewError(connect.CodeNotFound, err)
	case errors.Is(err, agents.ErrTooManyAgents):
		return nil, connect.NewError(connect.CodeResourceExhausted, err)
	case errors.Is(err, store.ErrNameTaken):
		return nil, connect.NewError(connect.CodeAlreadyExists, err)
	case err != nil:
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to restore agents: %w", err))
	}

	running := s.launcher.GetRunningAgents(ctx, restored)

	response := &agentv1.RestoreAgentsResponse{
		Agents: xslice.Map(restored, func(a agentstore.Agent) *agentv1.Agent { return renderAgent(a, running[a.ID]).Agent }),
	}

	return connect.NewResponse(response), nil
}

// ResumeAgent relaunches an agent's CLI on its conversation and returns it as stored.
func (s *Service) ResumeAgent(ctx context.Context, req *connect.Request[agentv1.ResumeAgentRequest]) (*connect.Response[agentv1.ResumeAgentResponse], error) {
	resumed, err := s.launcher.ResumeAgent(ctx, req.Msg.GetId())
	switch {
	case errors.Is(err, agents.ErrAgentNotFound):
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("no agent with id %q", req.Msg.GetId()))
	case errors.Is(err, agents.ErrAgentArchived):
		return nil, connect.NewError(connect.CodeFailedPrecondition, err)
	case err != nil:
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to resume agent: %w", err))
	}

	return connect.NewResponse(&agentv1.ResumeAgentResponse{Agent: renderAgent(*resumed, s.launcher.IsAgentRunning(*resumed)).Agent}), nil
}

// UpdateAgent renames the agent with the given id. A name a live agent already holds is a
// conflict rather than a server fault, since reporting the unique index's refusal as
// internal would leak a database message to a caller who picked a taken name.
func (s *Service) UpdateAgent(ctx context.Context, req *connect.Request[agentv1.UpdateAgentRequest]) (*connect.Response[emptypb.Empty], error) {
	renamed, err := s.agents.Rename(ctx, req.Msg.GetId(), req.Msg.GetName())
	switch {
	case errors.Is(err, store.ErrNameTaken):
		return nil, connect.NewError(connect.CodeAlreadyExists, fmt.Errorf("an agent named %q already exists", req.Msg.GetName()))
	case err != nil:
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to update agent: %w", err))
	case !renamed:
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("no agent with id %q", req.Msg.GetId()))
	}

	return connect.NewResponse(&emptypb.Empty{}), nil
}

// SearchTranscripts runs a full-text search over stored transcripts and returns one page
// of matching agents with a marked snippet, best match first. The page token encodes a
// position in this query's ranking, so replaying it against another query means nothing.
//
// Scope is worked out per page rather than carried in the token, so a replayed cursor
// cannot widen a search.
func (s *Service) SearchTranscripts(ctx context.Context, req *connect.Request[agentv1.SearchTranscriptsRequest]) (*connect.Response[agentv1.SearchTranscriptsResponse], error) {
	paginator, err := pagination.New[transcriptstore.Cursor, searchHit](req.Msg.GetPagination())
	if err != nil {
		return nil, err
	}

	scope := searchScope(ctx, req.Msg.GetAgentId())
	if scope != nil {
		logCrossAgentRead(ctx, *scope, "agent read another agent's transcript")
	}

	hits, err := s.transcripts.Search(ctx, transcriptstore.SearchParams{
		Query:   req.Msg.GetQuery(),
		Limit:   paginator.Limit(),
		Cursor:  paginator.Cursor(),
		AgentID: scope,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to search transcripts: %w", err))
	}

	resources := xslice.Map(hits, renderSearchHit)

	next, err := paginator.NextToken(resources)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to build the next page token: %w", err))
	}

	response := &agentv1.SearchTranscriptsResponse{NextPageToken: next}
	response.Hits = xslice.Map(resources, func(resource searchHit) *agentv1.SearchHit { return resource.SearchHit })

	return connect.NewResponse(response), nil
}

// GetTranscript returns an agent's full stored transcript by id, so the search preview can
// show a match in its surrounding conversation. An id that matches no agent is not_found
// rather than empty content, which would read as an agent with nothing stored yet.
func (s *Service) GetTranscript(ctx context.Context, req *connect.Request[agentv1.GetTranscriptRequest]) (*connect.Response[agentv1.GetTranscriptResponse], error) {
	logCrossAgentRead(ctx, req.Msg.GetAgentId(), "agent read another agent's transcript")

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

// searchScope is the one agent a search is confined to, nil for a search across all of
// them, so an agent that named nobody reads its own conversation and the panel reads
// everyone.
func searchScope(ctx context.Context, requested string) *string {
	if requested != "" {
		return &requested
	}

	from := caller.From(ctx)
	if from.AgentID == "" {
		return nil
	}

	return &from.AgentID
}

// logCrossAgentRead records an agent reading a conversation other than its own, which is
// what a scoping keyed on a header a caller sets about itself leaves worth having.
func logCrossAgentRead(ctx context.Context, target, message string) {
	from := caller.From(ctx)
	if from.AgentID == "" || from.AgentID == target {
		return
	}

	logging.From(ctx).LogAttrs(ctx, slog.LevelInfo, message,
		slog.String(logging.TargetAgentID, target))
}
