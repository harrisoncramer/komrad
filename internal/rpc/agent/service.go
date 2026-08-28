package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"

	"weave/internal/agents"
	"weave/internal/caller"
	"weave/internal/logging"
	"weave/internal/store"
	"weave/lib/pagination"
	"weave/lib/xslice"
	agentv1 "weave/proto/gen/agent/v1"

	agentstore "weave/internal/store/agent"
	mailstore "weave/internal/store/mail"
	reviewstore "weave/internal/store/review"
	transcriptstore "weave/internal/store/transcript"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/emptypb"
)

type turnBoundaryRecorder interface {
	RecordTurnBoundary(agentID string)
}

type agentLauncher interface {
	NewAgent(ctx context.Context, p agents.NewAgentParams) (*agentstore.Agent, error)
	Adopt(ctx context.Context, p agents.AdoptParams) (*agentstore.Agent, error)
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
	SetTags(ctx context.Context, ids []string, tag *string) (int, error)
	SetHidden(ctx context.Context, id string, hidden bool) (bool, error)
	SetMetadata(ctx context.Context, params agentstore.SetMetadataParams) (bool, error)
	Rename(ctx context.Context, id, name string) (bool, error)
}

type archiveEmptier interface {
	EmptyArchive(ctx context.Context) error
}

type transcriptReader interface {
	Get(ctx context.Context, agentID string) (transcriptstore.Transcript, error)
	Search(ctx context.Context, params transcriptstore.SearchParams) ([]transcriptstore.SearchHit, error)
}

type mailer interface {
	SendMail(ctx context.Context, params agents.SendMailParams) (*mailstore.Mail, error)
	ClaimMail(ctx context.Context, agentID string) ([]mailstore.Mail, error)
	ListMail(ctx context.Context, agentID string, limit int32, cursor *mailstore.Cursor) ([]mailstore.Mail, error)
	DeleteMail(ctx context.Context, ids []string) error
	ListUnreadMailboxes(ctx context.Context) ([]string, error)
}

type Service struct {
	agents      agentReadWriter
	transcripts transcriptReader
	mail        mailer
	launcher    agentLauncher
	reconciler  turnBoundaryRecorder
	archive     archiveEmptier
	review      reviewConversation
}

type reviewConversation interface {
	ListReviewThreads(ctx context.Context, params agents.ListReviewThreadsParams) ([]agents.ReviewThreadWithMessages, error)
	CreateReviewThread(ctx context.Context, params agents.CreateReviewThreadParams) (*agents.ReviewThreadWithMessages, error)
	AddReviewMessage(ctx context.Context, params agents.AddReviewMessageParams) (*reviewstore.Message, error)
	EditReviewMessage(ctx context.Context, params agents.EditReviewMessageParams) (*reviewstore.Message, error)
	MarkReviewThreadRead(ctx context.Context, params agents.MarkReviewThreadReadParams) (*reviewstore.Thread, error)
	SetReviewThreadResolved(ctx context.Context, params agents.SetReviewThreadResolvedParams) (*reviewstore.Thread, error)
	DeleteReviewThread(ctx context.Context, params agents.DeleteReviewThreadParams) error
	DeleteReviewMessage(ctx context.Context, params agents.DeleteReviewMessageParams) error
	NotifyReviewThreads(ctx context.Context, params agents.NotifyReviewThreadsParams) (*agents.NotifyReviewThreadsResult, error)
}

type NewServiceParams struct {
	Agents      agentReadWriter
	Transcripts transcriptReader
	Mail        mailer
	Launcher    agentLauncher
	Reconciler  turnBoundaryRecorder
	Archive     archiveEmptier
	Review      reviewConversation
}

// NewService constructs the handler from the stores it reads and the manager that launches
// agents.
func NewService(p NewServiceParams) *Service {
	return &Service{
		agents:      p.Agents,
		transcripts: p.Transcripts,
		mail:        p.Mail,
		launcher:    p.Launcher,
		reconciler:  p.Reconciler,
		archive:     p.Archive,
		review:      p.Review,
	}
}

// CreateAgent launches a background agent and returns it as stored.
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
		SetupPane:      req.Msg.GetSetupPane(),
	})
	switch {
	// Create-once is what the key is for, so a key already spent answers with the agent it
	// launched rather than an error.
	case errors.Is(err, agents.ErrAlreadyLaunched):
	case errors.Is(err, agents.ErrWorkflowNotFound):
		return nil, connect.NewError(connect.CodeNotFound, err)
	case errors.Is(err, agents.ErrSetupFailed):
		return nil, connect.NewError(connect.CodeFailedPrecondition, err)
	case errors.Is(err, agents.ErrBadDirectory), errors.Is(err, agents.ErrHarnessUnknown), errors.Is(err, agents.ErrSpawnerUnknown):
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	case errors.Is(err, agents.ErrTooManyAgents):
		return nil, connect.NewError(connect.CodeResourceExhausted, err)
	case err != nil:
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to create agent: %w", err))
	}

	return connect.NewResponse(&agentv1.CreateAgentResponse{Agent: renderAgent(*created, s.launcher.IsAgentRunning(*created), false).Agent}), nil
}

// Adopt puts an agent on a branch or pull request that already exists.
func (s *Service) Adopt(ctx context.Context, req *connect.Request[agentv1.AdoptRequest]) (*connect.Response[agentv1.AdoptResponse], error) {
	created, err := s.launcher.Adopt(ctx, agents.AdoptParams{
		Ref:            req.Msg.GetRef(),
		Title:          req.Msg.GetTitle(),
		Flags:          req.Msg.GetFlags(),
		Prompt:         req.Msg.GetPrompt(),
		WorkflowID:     req.Msg.GetWorkflowId(),
		Harness:        req.Msg.GetHarness(),
		IdempotencyKey: req.Msg.GetIdempotencyKey(),
		SetupPane:      req.Msg.GetSetupPane(),
	})
	switch {
	case errors.Is(err, agents.ErrWorkflowNotFound):
		return nil, connect.NewError(connect.CodeNotFound, err)
	case errors.Is(err, agents.ErrSetupFailed):
		return nil, connect.NewError(connect.CodeFailedPrecondition, err)
	case errors.Is(err, agents.ErrAdoptNeedsWorktree), errors.Is(err, agents.ErrAdoptBranchInUse), errors.Is(err, agents.ErrWorktreeNeedsCwd):
		return nil, connect.NewError(connect.CodeFailedPrecondition, err)
	case errors.Is(err, agents.ErrAdoptRepoMismatch), errors.Is(err, agents.ErrAdoptForkBranch), errors.Is(err, agents.ErrAdoptNoResolver),
		errors.Is(err, agents.ErrBadDirectory), errors.Is(err, agents.ErrHarnessUnknown), errors.Is(err, agents.ErrSpawnerUnknown):
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	case errors.Is(err, agents.ErrTooManyAgents):
		return nil, connect.NewError(connect.CodeResourceExhausted, err)
	case err != nil:
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to adopt agent: %w", err))
	}

	return connect.NewResponse(&agentv1.AdoptResponse{Agent: renderAgent(*created, s.launcher.IsAgentRunning(*created), false).Agent}), nil
}

// ListAgents returns one page of stored agents, newest first.
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

	unreadMailboxes, err := s.mail.ListUnreadMailboxes(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to list the mailboxes holding unread mail: %w", err))
	}
	unread := xslice.KeyBy(unreadMailboxes, func(id string) string { return id })

	resources := xslice.Map(agents, func(a agentstore.Agent) agent {
		_, hasUnread := unread[a.ID]

		return renderAgent(a, running[a.ID], hasUnread)
	})

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

	unreadMailboxes, err := s.mail.ListUnreadMailboxes(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to list the mailboxes holding unread mail: %w", err))
	}
	hasUnread := slices.Contains(unreadMailboxes, agent.ID)

	return connect.NewResponse(&agentv1.GetAgentResponse{Agent: renderAgent(*agent, s.launcher.IsAgentRunning(*agent), hasUnread).Agent}), nil
}

// SetTags tags every agent named, and an unset tag clears the ones they carry.
func (s *Service) SetTags(ctx context.Context, req *connect.Request[agentv1.SetTagsRequest]) (*connect.Response[emptypb.Empty], error) {
	tagged, err := s.agents.SetTags(ctx, req.Msg.GetIds(), req.Msg.Tag)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to set the tag: %w", err))
	}
	// Reaching only some of the ids is fine, since an agent archived in the meantime has no
	// row left to draw a tag on.
	if tagged == 0 {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("none of the %d ids given is a live agent", len(req.Msg.GetIds())))
	}

	return connect.NewResponse(&emptypb.Empty{}), nil
}

// SetHidden hides an agent from the panel's sidebar or brings it back.
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
// when the value is unset.
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
	// Clearing a key nothing held is not an error, since the agent asked for it to be gone.
	if !changed && req.Msg.Value != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("no agent with id %q", req.Msg.GetId()))
	}

	return connect.NewResponse(&emptypb.Empty{}), nil
}

// Log writes one line to weave's log for an agent, which is how a running action says what
// it did, since an action's own stdout goes nowhere.
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

// RecordTurnBoundary notes that the calling agent's own CLI reported its turn ending.
func (s *Service) RecordTurnBoundary(ctx context.Context, _ *connect.Request[emptypb.Empty]) (*connect.Response[emptypb.Empty], error) {
	id := caller.From(ctx).AgentID
	if id == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("the caller has no agent id of its own"))
	}

	s.reconciler.RecordTurnBoundary(id)

	return connect.NewResponse(&emptypb.Empty{}), nil
}

func callerScope(ctx context.Context, requested string) string {
	if requested != "" {
		return requested
	}

	return caller.From(ctx).AgentID
}

func (s *Service) SendMail(ctx context.Context, req *connect.Request[agentv1.SendMailRequest]) (*connect.Response[agentv1.SendMailResponse], error) {
	sent, err := s.mail.SendMail(ctx, agents.SendMailParams{
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
	case errors.Is(err, agents.ErrSelfAddressed), errors.Is(err, agents.ErrSenderUnknown), errors.Is(err, agents.ErrReplyToNotFound):
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	case errors.Is(err, agents.ErrMailboxFull), errors.Is(err, agents.ErrThreadCapped):
		return nil, connect.NewError(connect.CodeResourceExhausted, err)
	case err != nil:
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to send the mail: %w", err))
	}

	return connect.NewResponse(&agentv1.SendMailResponse{Mail: renderMail(*sent)}), nil
}

func (s *Service) ReadMail(ctx context.Context, req *connect.Request[agentv1.ReadMailRequest]) (*connect.Response[agentv1.ReadMailResponse], error) {
	id := callerScope(ctx, req.Msg.GetId())
	if id == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("no id given, and the caller has none of its own to fall back to"))
	}

	logCrossAgentRead(ctx, id, "agent read another agent's mail")

	claimed, err := s.mail.ClaimMail(ctx, id)
	switch {
	case errors.Is(err, agents.ErrAgentNotFound):
		return nil, connect.NewError(connect.CodeNotFound, err)
	case err != nil:
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to read the mail: %w", err))
	}

	return connect.NewResponse(&agentv1.ReadMailResponse{
		Mail: xslice.Map(claimed, renderMail),
	}), nil
}

func (s *Service) ListMail(ctx context.Context, req *connect.Request[agentv1.ListMailRequest]) (*connect.Response[agentv1.ListMailResponse], error) {
	id := callerScope(ctx, req.Msg.GetId())
	if id == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("no id given, and the caller has none of its own to fall back to"))
	}

	logCrossAgentRead(ctx, id, "agent listed another agent's mail")

	paginator, err := pagination.New[mailstore.Cursor, mail](req.Msg.GetPagination())
	if err != nil {
		return nil, err
	}

	listed, err := s.mail.ListMail(ctx, id, int32(paginator.Limit()), paginator.Cursor())
	switch {
	case errors.Is(err, agents.ErrAgentNotFound):
		return nil, connect.NewError(connect.CodeNotFound, err)
	case err != nil:
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to list the mail: %w", err))
	}

	resources := xslice.Map(listed, renderMailPage)

	next, err := paginator.NextToken(resources)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to build the next page token: %w", err))
	}

	response := &agentv1.ListMailResponse{NextPageToken: next}
	response.Mail = xslice.Map(resources, func(resource mail) *agentv1.Mail { return resource.Mail })

	return connect.NewResponse(response), nil
}

// DeleteMail permanently removes mail by id, so the recipient never sees it rather than
// having it marked as read.
func (s *Service) DeleteMail(ctx context.Context, req *connect.Request[agentv1.DeleteMailRequest]) (*connect.Response[emptypb.Empty], error) {
	err := s.mail.DeleteMail(ctx, req.Msg.GetIds())
	switch {
	case errors.Is(err, agents.ErrMailNotFound):
		return nil, connect.NewError(connect.CodeNotFound, err)
	case err != nil:
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to delete mail: %w", err))
	}

	return connect.NewResponse(&emptypb.Empty{}), nil
}

// DeleteAgents stops agents and removes them with their configs and transcripts.
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

// EmptyArchive queues the sweep that deletes the whole archive as a background job, since
// every agent in it runs its own cleanup action and the request would be held open too long.
func (s *Service) EmptyArchive(ctx context.Context, _ *connect.Request[emptypb.Empty]) (*connect.Response[emptypb.Empty], error) {
	if err := s.archive.EmptyArchive(ctx); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to empty the archive: %w", err))
	}

	return connect.NewResponse(&emptypb.Empty{}), nil
}

// ArchiveAgents stops agents and hides them, keeping the records and transcripts.
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

	unreadMailboxes, err := s.mail.ListUnreadMailboxes(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to list the mailboxes holding unread mail: %w", err))
	}
	unread := xslice.KeyBy(unreadMailboxes, func(id string) string { return id })

	response := &agentv1.RestoreAgentsResponse{
		Agents: xslice.Map(restored, func(a agentstore.Agent) *agentv1.Agent {
			_, hasUnread := unread[a.ID]

			return renderAgent(a, running[a.ID], hasUnread).Agent
		}),
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

	unreadMailboxes, err := s.mail.ListUnreadMailboxes(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to list the mailboxes holding unread mail: %w", err))
	}
	hasUnread := slices.Contains(unreadMailboxes, resumed.ID)

	return connect.NewResponse(&agentv1.ResumeAgentResponse{Agent: renderAgent(*resumed, s.launcher.IsAgentRunning(*resumed), hasUnread).Agent}), nil
}

// UpdateAgent renames the agent with the given id, reporting a name another agent holds as
// a conflict rather than leaking the database's own refusal.
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

// SearchTranscripts runs a full-text search over stored transcripts and returns one page of
// matching agents with a marked snippet, best match first.
func (s *Service) SearchTranscripts(ctx context.Context, req *connect.Request[agentv1.SearchTranscriptsRequest]) (*connect.Response[agentv1.SearchTranscriptsResponse], error) {
	paginator, err := pagination.New[transcriptstore.Cursor, searchHit](req.Msg.GetPagination())
	if err != nil {
		return nil, err
	}

	// Scope is worked out per page rather than carried in the token, so a replayed cursor
	// cannot widen a search.
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
// show a match in its surrounding conversation.
func (s *Service) GetTranscript(ctx context.Context, req *connect.Request[agentv1.GetTranscriptRequest]) (*connect.Response[agentv1.GetTranscriptResponse], error) {
	logCrossAgentRead(ctx, req.Msg.GetAgentId(), "agent read another agent's transcript")

	// Empty content would read as an agent with nothing stored yet, so a missing one is
	// not_found instead.
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

func logCrossAgentRead(ctx context.Context, target, message string) {
	from := caller.From(ctx)
	if from.AgentID == "" || from.AgentID == target {
		return
	}

	logging.From(ctx).LogAttrs(ctx, slog.LevelInfo, message,
		slog.String(logging.TargetAgentID, target))
}
