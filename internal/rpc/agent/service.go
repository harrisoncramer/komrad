// Package agent serves the agent RPCs, translating each request into a call on the agent
// services and each domain error into the connect code the caller sees.
package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"weave/internal/agents"
	"weave/internal/caller"
	"weave/internal/logging"
	"weave/internal/rpcerr"
	"weave/internal/store"
	"weave/lib/pagination"
	"weave/lib/xslice"
	agentv1 "weave/proto/gen/agent/v1"

	agentstore "weave/internal/store/agent"
	markstore "weave/internal/store/mark"
	reviewstore "weave/internal/store/review"
	runstore "weave/internal/store/run"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/emptypb"
)

// reportRecorder is where what an agent's own CLI reports about itself reaches the reconcile
// loop, which keeps the report beside what it read off the pane.
type reportRecorder interface {
	RecordTurnBoundary(agentID string)
	RecordReportedReason(agentID, reason string)
}

type agentCreator interface {
	NewAgent(ctx context.Context, p agents.NewAgentParams) (*agentstore.Agent, error)
	RequestReview(ctx context.Context, p agents.RequestReviewParams) (*agentstore.Agent, string, error)
}

type agentAdopter interface {
	Adopt(ctx context.Context, p agents.AdoptParams) (*agentstore.Agent, error)
}

type agentLiveness interface {
	IsAgentRunning(agent agentstore.Agent) bool
	GetRunningAgents(ctx context.Context, agents []agentstore.Agent) map[string]bool
}

type agentLifecycle interface {
	DeleteAgents(ctx context.Context, ids []string) error
	ArchiveAgents(ctx context.Context, ids []string) error
	RestoreAgents(ctx context.Context, ids []string) ([]agentstore.Agent, error)
}

type agentResumer interface {
	ResumeAgent(ctx context.Context, id string) (*agentstore.Agent, error)
}

type agentReadWriter interface {
	ListPage(ctx context.Context, limit int64, cursor *agentstore.Cursor, archived bool) ([]agentstore.Agent, error)
	GetByID(ctx context.Context, id string) (*agentstore.Agent, error)
	Exists(ctx context.Context, id string) (bool, error)
	SetHidden(ctx context.Context, id string, hidden bool) (bool, error)
	SetViewed(ctx context.Context, id string, viewed bool) error
	SetTurnEnded(ctx context.Context, id string) error
	SetReportedReason(ctx context.Context, id, reason string) error
	ListUnseenTurns(ctx context.Context) ([]string, error)
	SetMetadata(ctx context.Context, params agentstore.SetMetadataParams) (bool, error)
	Rename(ctx context.Context, id, name string) (bool, error)
}

// markReadWriter owns the letters a person addresses rows by, which span agents and workflows and
// so are written through a transaction rather than through one store.
type markReadWriter interface {
	ListMarks(ctx context.Context) ([]markstore.Mark, error)
	SetMark(ctx context.Context, mark string, target markstore.Target) (bool, error)
	ClearMark(ctx context.Context, target markstore.Target) (bool, error)
}

// runReadWriter is the record of the scripts weave fired, which the listing reads to say whether
// an agent is holding a failure nobody has looked at.
type runReadWriter interface {
	ListUnreadFailures(ctx context.Context) ([]runstore.Failure, error)
	MarkRead(ctx context.Context, agentID string) error
}

type archiveEmptier interface {
	EmptyArchive(ctx context.Context) error
}

type Service struct {
	agents      agentReadWriter
	transcripts transcriptReader
	mail        mailer
	creator     agentCreator
	adopter     agentAdopter
	liveness    agentLiveness
	lifecycle   agentLifecycle
	resumer     agentResumer
	reconciler  reportRecorder
	archive     archiveEmptier
	review      reviewConversation
	runs        runReadWriter
	marks       markReadWriter
	refresher   worktreeRefresher
}

type NewServiceParams struct {
	Agents      agentReadWriter
	Transcripts transcriptReader
	Mail        mailer
	Creator     agentCreator
	Adopter     agentAdopter
	Liveness    agentLiveness
	Lifecycle   agentLifecycle
	Resumer     agentResumer
	Reconciler  reportRecorder
	Archive     archiveEmptier
	Review      reviewConversation
	Runs        runReadWriter
	Marks       markReadWriter
	Refresher   worktreeRefresher
}

// NewService constructs the handler from the stores it reads and the services that move agents
// through their life.
func NewService(params NewServiceParams) *Service {
	return &Service{
		agents:      params.Agents,
		transcripts: params.Transcripts,
		mail:        params.Mail,
		creator:     params.Creator,
		adopter:     params.Adopter,
		liveness:    params.Liveness,
		lifecycle:   params.Lifecycle,
		resumer:     params.Resumer,
		reconciler:  params.Reconciler,
		archive:     params.Archive,
		review:      params.Review,
		runs:        params.Runs,
		marks:       params.Marks,
		refresher:   params.Refresher,
	}
}

// CreateAgent launches a background agent and returns it as stored.
func (s *Service) CreateAgent(ctx context.Context, req *connect.Request[agentv1.CreateAgentRequest]) (*connect.Response[agentv1.CreateAgentResponse], error) {
	created, err := s.creator.NewAgent(ctx, agents.NewAgentParams{
		Title:            req.Msg.GetTitle(),
		Dir:              req.Msg.GetDir(),
		Flags:            req.Msg.GetFlags(),
		ProvisionEnabled: req.Msg.ProvisionEnabled,
		Prompt:           req.Msg.GetPrompt(),
		WorkflowID:       req.Msg.GetWorkflowId(),
		Harness:          req.Msg.GetHarness(),
		IdempotencyKey:   req.Msg.GetIdempotencyKey(),
		ParentAgentID:    req.Msg.GetParentAgentId(),
	})
	// Create-once is what the key is for, so a key already spent answers with the agent it
	// launched rather than an error.
	if err != nil && !errors.Is(err, agents.ErrAlreadyLaunched) {
		return nil, rpcerr.Wrap(err, "failed to create agent")
	}

	return connect.NewResponse(&agentv1.CreateAgentResponse{Agent: renderAgent(*created, agentFlags{alive: s.liveness.IsAgentRunning(*created)}).Agent}), nil
}

// RequestReview spawns the reviewer for the calling agent's own work and answers with the rest
// of the loop that agent is in.
func (s *Service) RequestReview(ctx context.Context, req *connect.Request[agentv1.RequestReviewRequest]) (*connect.Response[agentv1.RequestReviewResponse], error) {
	id, err := scopedAgentID(ctx, req.Msg.GetAgentId())
	if err != nil {
		return nil, err
	}

	reviewer, guidance, err := s.creator.RequestReview(ctx, agents.RequestReviewParams{
		AgentID:        id,
		Prompt:         req.Msg.GetPrompt(),
		Title:          req.Msg.GetTitle(),
		IdempotencyKey: req.Msg.GetIdempotencyKey(),
	})
	if err != nil {
		return nil, rpcerr.Wrap(err, "failed to request a review",
			rpcerr.Saying(agents.ErrAgentNotFound, "no agent with id %q", id))
	}

	return connect.NewResponse(&agentv1.RequestReviewResponse{
		Reviewer: renderAgent(*reviewer, agentFlags{alive: s.liveness.IsAgentRunning(*reviewer)}).Agent,
		Guidance: guidance,
	}), nil
}

// Adopt puts an agent on a branch or pull request that already exists.
func (s *Service) Adopt(ctx context.Context, req *connect.Request[agentv1.AdoptRequest]) (*connect.Response[agentv1.AdoptResponse], error) {
	created, err := s.adopter.Adopt(ctx, agents.AdoptParams{
		Ref:            req.Msg.GetRef(),
		Title:          req.Msg.GetTitle(),
		Flags:          req.Msg.GetFlags(),
		Prompt:         req.Msg.GetPrompt(),
		WorkflowID:     req.Msg.GetWorkflowId(),
		Harness:        req.Msg.GetHarness(),
		IdempotencyKey: req.Msg.GetIdempotencyKey(),
	})
	switch {
	// A workflow with no cwd cannot host an adoption at all.
	case errors.Is(err, agents.ErrWorktreeNeedsCwd):
		return nil, connect.NewError(connect.CodeFailedPrecondition, err)
	case err != nil:
		return nil, rpcerr.Wrap(err, "failed to adopt agent")
	}

	return connect.NewResponse(&agentv1.AdoptResponse{Agent: renderAgent(*created, agentFlags{alive: s.liveness.IsAgentRunning(*created)}).Agent}), nil
}

// agentFlagsFor gathers the facts about a set of agents that are not on their own rows, so a
// listing asks for each of them once rather than once per agent.
func (s *Service) agentFlagsFor(ctx context.Context, listed []agentstore.Agent) (func(agentstore.Agent) agentFlags, error) {
	running := s.liveness.GetRunningAgents(ctx, listed)

	mailboxes, err := s.mail.ListUnreadMailboxes(ctx)
	if err != nil {
		return nil, rpcerr.Wrap(err, "failed to list the mailboxes holding unread mail")
	}
	waiting := xslice.KeyBy(mailboxes, func(id string) string { return id })

	unseen, err := s.agents.ListUnseenTurns(ctx)
	if err != nil {
		return nil, rpcerr.Wrap(err, "failed to list the agents holding unseen turns")
	}
	unviewed := xslice.KeyBy(unseen, func(id string) string { return id })

	tallied, err := s.review.ListTallies(ctx)
	if err != nil {
		return nil, rpcerr.Wrap(err, "failed to count the rounds of review the agents have been through")
	}
	tallies := xslice.KeyBy(tallied, func(tally reviewstore.Tally) string { return tally.AgentID })

	live, err := s.review.ListLiveReviewers(ctx)
	if err != nil {
		return nil, rpcerr.Wrap(err, "failed to list the agents whose work is being reviewed")
	}
	reading := xslice.KeyBy(live, func(reviewer reviewstore.LiveReviewer) string { return reviewer.AgentID })

	failures, err := s.runs.ListUnreadFailures(ctx)
	if err != nil {
		return nil, rpcerr.Wrap(err, "failed to list the agents holding a failed run")
	}
	failed := xslice.KeyBy(failures, func(failure runstore.Failure) string { return failure.AgentID })

	listedMarks, err := s.marks.ListMarks(ctx)
	if err != nil {
		return nil, rpcerr.Wrap(err, "failed to list the marks")
	}
	marks := make(map[string]*string, len(listedMarks))
	for _, mark := range listedMarks {
		if mark.AgentID != nil {
			marks[*mark.AgentID] = &mark.Mark
		}
	}

	return func(a agentstore.Agent) agentFlags {
		_, hasMail := waiting[a.ID]
		_, hasOutput := unviewed[a.ID]
		failure, hasFailure := failed[a.ID]

		return agentFlags{
			mark:          marks[a.ID],
			alive:         running[a.ID],
			unreadMail:    hasMail,
			unviewed:      hasOutput,
			failedRun:     hasFailure,
			failedRunLog:  failure.LogNumber,
			localRounds:   tallies[a.ID].Rounds,
			localVerdict:  localVerdictOf(tallies[a.ID]),
			inLocalReview: reading[a.ID].ReviewerID != "",
		}
	}, nil
}

// localVerdictOf is the tally's verdict as the wire carries it, which is the empty string for the
// agent whose work no review has concluded on.
func localVerdictOf(tally reviewstore.Tally) string {
	if tally.Verdict == nil {
		return ""
	}

	return string(*tally.Verdict)
}

// ListAgents returns one page of stored agents, newest first.
func (s *Service) ListAgents(ctx context.Context, req *connect.Request[agentv1.ListAgentsRequest]) (*connect.Response[agentv1.ListAgentsResponse], error) {
	paginator, err := pagination.New[agentstore.Cursor, agent](req.Msg.GetPagination())
	if err != nil {
		return nil, err
	}

	agents, err := s.agents.ListPage(ctx, paginator.Limit(), paginator.Cursor(), req.Msg.GetArchived())
	if err != nil {
		return nil, rpcerr.Wrap(err, "failed to list agents")
	}

	flags, err := s.agentFlagsFor(ctx, agents)
	if err != nil {
		return nil, err
	}

	view := agentViewOptions(req.Msg.GetView())
	resources := xslice.Map(agents, func(a agentstore.Agent) agent { return renderAgent(a, flags(a), view...) })

	next, err := paginator.NextToken(resources)
	if err != nil {
		return nil, rpcerr.Wrap(err, "failed to build the next page token")
	}

	response := &agentv1.ListAgentsResponse{NextPageToken: next}
	response.Agents = xslice.Map(resources, func(resource agent) *agentv1.Agent { return resource.Agent })

	return connect.NewResponse(response), nil
}

// renderOneAgent gathers the facts an agent's row does not hold and renders it, which is what a
// call answering with a single agent wants.
func (s *Service) renderOneAgent(ctx context.Context, stored agentstore.Agent) (*agentv1.Agent, error) {
	flags, err := s.agentFlagsFor(ctx, []agentstore.Agent{stored})
	if err != nil {
		return nil, err
	}

	return renderAgent(stored, flags(stored)).Agent, nil
}

// GetAgent returns one agent by id, archived or not.
func (s *Service) GetAgent(ctx context.Context, req *connect.Request[agentv1.GetAgentRequest]) (*connect.Response[agentv1.GetAgentResponse], error) {
	agent, err := s.agents.GetByID(ctx, req.Msg.GetId())
	if err != nil {
		return nil, rpcerr.Wrap(err, "failed to get agent",
			rpcerr.Saying(store.ErrNotFound, "no agent with id %q", req.Msg.GetId()))
	}

	rendered, err := s.renderOneAgent(ctx, *agent)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(&agentv1.GetAgentResponse{Agent: rendered}), nil
}

// SetMark puts a letter on an agent or a workflow, taking it off whichever row held it, and
// carries no tool annotation because a mark is the person's own address for a row rather than
// anything an agent should move.
func (s *Service) SetMark(ctx context.Context, req *connect.Request[agentv1.SetMarkRequest]) (*connect.Response[emptypb.Empty], error) {
	target := markstore.Target{}
	switch {
	case req.Msg.GetAgentId() != "":
		target = markstore.OnAgent(req.Msg.GetAgentId())
	case req.Msg.GetWorkflowId() != "":
		target = markstore.OnWorkflow(req.Msg.GetWorkflowId())
	}

	if req.Msg.Mark == nil {
		if _, err := s.marks.ClearMark(ctx, target); err != nil {
			return nil, rpcerr.Wrap(err, "failed to clear the mark")
		}

		return connect.NewResponse(&emptypb.Empty{}), nil
	}

	marked, err := s.marks.SetMark(ctx, req.Msg.GetMark(), target)
	if err != nil {
		return nil, rpcerr.Wrap(err, "failed to set the mark")
	}
	if !marked {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("%s is not a live row to mark", target))
	}

	return connect.NewResponse(&emptypb.Empty{}), nil
}

// SetHidden hides an agent from the panel's sidebar or brings it back.
func (s *Service) SetHidden(ctx context.Context, req *connect.Request[agentv1.SetHiddenRequest]) (*connect.Response[emptypb.Empty], error) {
	changed, err := s.agents.SetHidden(ctx, req.Msg.GetId(), req.Msg.GetHidden())
	if err != nil {
		return nil, rpcerr.Wrap(err, "failed to hide the agent")
	}
	if !changed {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("no agent with id %q", req.Msg.GetId()))
	}

	return connect.NewResponse(&emptypb.Empty{}), nil
}

// SetAgentViewed claims whatever the agent has finished up to now, or puts the mark back, and
// carries no tool annotation because an agent moving the marks on the user's screen is the one
// caller it must not have.
func (s *Service) SetAgentViewed(ctx context.Context, req *connect.Request[agentv1.SetAgentViewedRequest]) (*connect.Response[emptypb.Empty], error) {
	if err := s.agents.SetViewed(ctx, req.Msg.GetId(), req.Msg.GetViewed()); err != nil {
		return nil, rpcerr.Wrap(err, "failed to set whether the agent was viewed")
	}

	return connect.NewResponse(&emptypb.Empty{}), nil
}

// MarkRunsRead claims the failed runs an agent is holding, which is what looking at its run log
// does, and carries no tool annotation for the same reason SetAgentViewed does not.
func (s *Service) MarkRunsRead(ctx context.Context, req *connect.Request[agentv1.MarkRunsReadRequest]) (*connect.Response[emptypb.Empty], error) {
	if err := s.runs.MarkRead(ctx, req.Msg.GetId()); err != nil {
		return nil, rpcerr.Wrap(err, "failed to claim the agent's failed runs")
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
	if err != nil {
		return nil, rpcerr.Wrap(err, "failed to set the metadata",
			rpcerr.Saying(agentstore.ErrTooManyMetadataKeys, "agent %q already holds as much metadata as it may", req.Msg.GetId()))
	}
	// Clearing a key nothing held is not an error.
	if !changed && req.Msg.Value != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("no agent with id %q", req.Msg.GetId()))
	}

	return connect.NewResponse(&emptypb.Empty{}), nil
}

// Log writes one line to weave's log for an agent, which is how a running script whose stdout
// goes nowhere says what it did.
func (s *Service) Log(ctx context.Context, req *connect.Request[agentv1.LogRequest]) (*connect.Response[emptypb.Empty], error) {
	id, err := scopedAgentID(ctx, req.Msg.GetId())
	if err != nil {
		return nil, err
	}

	agent, err := s.agents.GetByID(ctx, id)
	if err != nil {
		return nil, rpcerr.Wrap(err, "failed to get agent",
			rpcerr.Saying(store.ErrNotFound, "no agent with id %q", id))
	}

	attrs := []slog.Attr{
		slog.String(logging.TargetAgentID, agent.ID),
		slog.String(logging.ScriptAgentCwd, agent.Cwd),
	}
	if script := req.Msg.GetScript(); script != "" {
		attrs = append(attrs, slog.String(logging.ScriptName, script))
	}

	//nolint:sloglint // this RPC exists to relay a caller's own message, so the message is the argument
	logging.From(ctx).LogAttrs(ctx, slog.LevelInfo, req.Msg.GetMessage(), attrs...)

	return connect.NewResponse(&emptypb.Empty{}), nil
}

// RecordReport takes what the calling agent's own CLI read out of one of its hook payloads: the
// reason goes on the row beside the state the pane decided, and a turn it reports ending lets
// the reconciler reach an idle verdict sooner and stamps the row the unread mark is measured
// against, ahead of the reconciler doing the same off the pane.
func (s *Service) RecordReport(ctx context.Context, req *connect.Request[agentv1.RecordReportRequest]) (*connect.Response[emptypb.Empty], error) {
	id := caller.From(ctx).AgentID
	if id == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("the caller has no agent id of its own"))
	}

	// Both stamps are taken before either write, so a store that is down costs the flags it
	// would have raised rather than also costing the reconciler its shortcut to idle.
	reason := req.Msg.GetReason()
	s.reconciler.RecordReportedReason(id, reason)
	turnEnded := req.Msg.GetTurnEnded()
	if turnEnded {
		s.reconciler.RecordTurnBoundary(id)
	}

	if err := s.agents.SetReportedReason(ctx, id, reason); err != nil {
		return nil, rpcerr.Wrap(err, "failed to record what the agent reported it was doing")
	}

	if !turnEnded {
		return connect.NewResponse(&emptypb.Empty{}), nil
	}

	if err := s.agents.SetTurnEnded(ctx, id); err != nil {
		return nil, rpcerr.Wrap(err, "failed to record the turn end")
	}

	return connect.NewResponse(&emptypb.Empty{}), nil
}

// scopedAgentID is the agent a request is about: the one it names, or the calling agent when it
// names none.
func scopedAgentID(ctx context.Context, requested string) (string, error) {
	if requested != "" {
		return requested, nil
	}

	from := caller.From(ctx).AgentID
	if from == "" {
		return "", connect.NewError(connect.CodeInvalidArgument,
			errors.New("no id given, and the caller has none of its own to fall back to"))
	}

	return from, nil
}

func logCrossAgentRead(ctx context.Context, target, message string) {
	from := caller.From(ctx)
	if from.AgentID == "" || from.AgentID == target {
		return
	}

	//nolint:sloglint // every caller passes a literal, and the helper is the indirection the check cannot see through
	logging.From(ctx).LogAttrs(ctx, slog.LevelInfo, message,
		slog.String(logging.TargetAgentID, target))
}

// DeleteAgents stops agents and removes them with their configs and transcripts.
func (s *Service) DeleteAgents(ctx context.Context, req *connect.Request[agentv1.DeleteAgentsRequest]) (*connect.Response[emptypb.Empty], error) {
	if err := s.lifecycle.DeleteAgents(ctx, req.Msg.GetIds()); err != nil {
		return nil, rpcerr.Wrap(err, "failed to delete agents")
	}

	return connect.NewResponse(&emptypb.Empty{}), nil
}

// ArchiveAgents stops agents and hides them, keeping the records and transcripts.
func (s *Service) ArchiveAgents(ctx context.Context, req *connect.Request[agentv1.ArchiveAgentsRequest]) (*connect.Response[emptypb.Empty], error) {
	if err := s.lifecycle.ArchiveAgents(ctx, req.Msg.GetIds()); err != nil {
		return nil, rpcerr.Wrap(err, "failed to archive agents")
	}

	return connect.NewResponse(&emptypb.Empty{}), nil
}

// RestoreAgents takes agents out of the archive and returns them as stored, dormant.
func (s *Service) RestoreAgents(ctx context.Context, req *connect.Request[agentv1.RestoreAgentsRequest]) (*connect.Response[agentv1.RestoreAgentsResponse], error) {
	restored, err := s.lifecycle.RestoreAgents(ctx, req.Msg.GetIds())
	if err != nil {
		return nil, rpcerr.Wrap(err, "failed to restore agents")
	}

	flags, err := s.agentFlagsFor(ctx, restored)
	if err != nil {
		return nil, err
	}

	response := &agentv1.RestoreAgentsResponse{
		Agents: xslice.Map(restored, func(a agentstore.Agent) *agentv1.Agent {
			return renderAgent(a, flags(a)).Agent
		}),
	}

	return connect.NewResponse(response), nil
}

// EmptyArchive queues the sweep that deletes the whole archive as a background job, every agent
// in it running its own deprovisioner.
func (s *Service) EmptyArchive(ctx context.Context, _ *connect.Request[emptypb.Empty]) (*connect.Response[emptypb.Empty], error) {
	if err := s.archive.EmptyArchive(ctx); err != nil {
		return nil, rpcerr.Wrap(err, "failed to empty the archive")
	}

	return connect.NewResponse(&emptypb.Empty{}), nil
}

// ResumeAgent relaunches an agent's CLI on its conversation and returns it as stored.
func (s *Service) ResumeAgent(ctx context.Context, req *connect.Request[agentv1.ResumeAgentRequest]) (*connect.Response[agentv1.ResumeAgentResponse], error) {
	resumed, err := s.resumer.ResumeAgent(ctx, req.Msg.GetId())
	if err != nil {
		return nil, rpcerr.Wrap(err, "failed to resume agent",
			rpcerr.Saying(agents.ErrAgentNotFound, "no agent with id %q", req.Msg.GetId()))
	}

	rendered, err := s.renderOneAgent(ctx, *resumed)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(&agentv1.ResumeAgentResponse{Agent: rendered}), nil
}

// UpdateAgent renames the agent with the given id, reporting a name another agent holds as
// a conflict rather than leaking the database's own refusal.
func (s *Service) UpdateAgent(ctx context.Context, req *connect.Request[agentv1.UpdateAgentRequest]) (*connect.Response[emptypb.Empty], error) {
	renamed, err := s.agents.Rename(ctx, req.Msg.GetId(), req.Msg.GetName())
	if err != nil {
		return nil, rpcerr.Wrap(err, "failed to update agent",
			rpcerr.Saying(store.ErrNameTaken, "an agent named %q already exists", req.Msg.GetName()))
	}
	if !renamed {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("no agent with id %q", req.Msg.GetId()))
	}

	return connect.NewResponse(&emptypb.Empty{}), nil
}
