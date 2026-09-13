// Package agent serves the agent RPCs, translating each request into a call on the agent
// services and each domain error into the connect code the caller sees.
package agent

import (
	"context"
	"errors"
	"log/slog"

	"weave/internal/agents"
	"weave/internal/caller"
	"weave/internal/fail"
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

	"google.golang.org/protobuf/types/known/emptypb"
)

// reportRecorder is where what an agent's own CLI reports about itself reaches the reconcile
// loop.
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

// markReadWriter owns the letters a person types to jump to a row, which span agents and
// workflows and so are written through a transaction rather than through one store.
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

// errCallerHasNoAgent is a report from something that is not an agent, which has no row to
// record it against.
var errCallerHasNoAgent = fail.InvalidArgument("the caller has no agent id of its own")

// errNoIDToFallBackOn is a request naming no agent, made by something that is not one.
var errNoIDToFallBackOn = fail.InvalidArgument("no id given, and the caller has none of its own to fall back to")

// CreateAgent launches a background agent and returns it as stored.
func (s *Service) CreateAgent(ctx context.Context, req *agentv1.CreateAgentRequest) (*agentv1.CreateAgentResponse, *fail.Error) {
	created, err := s.creator.NewAgent(ctx, agents.NewAgentParams{
		Title:            req.GetTitle(),
		Dir:              req.GetDir(),
		Flags:            req.GetFlags(),
		ProvisionEnabled: req.ProvisionEnabled,
		Prompt:           req.GetPrompt(),
		WorkflowID:       req.GetWorkflowId(),
		Harness:          req.GetHarness(),
		IdempotencyKey:   req.GetIdempotencyKey(),
		ParentAgentID:    req.GetParentAgentId(),
	})
	// Create-once is what the key is for, so a key already spent answers with the agent it
	// launched rather than an error.
	if err != nil && !errors.Is(err, agents.ErrAlreadyLaunched) {
		return nil, rpcerr.From(err, "failed to create agent")
	}

	return &agentv1.CreateAgentResponse{Agent: renderAgent(*created, agentFlags{alive: s.liveness.IsAgentRunning(*created)}).Agent}, nil
}

// RequestReview spawns the reviewer for the calling agent's own work and answers with the half
// of the review briefing it was not launched with.
func (s *Service) RequestReview(ctx context.Context, req *agentv1.RequestReviewRequest) (*agentv1.RequestReviewResponse, *fail.Error) {
	id, failure := scopedAgentID(ctx, req.GetAgentId())
	if failure != nil {
		return nil, failure
	}

	reviewer, guidance, err := s.creator.RequestReview(ctx, agents.RequestReviewParams{
		AgentID:        id,
		Prompt:         req.GetPrompt(),
		Title:          req.GetTitle(),
		IdempotencyKey: req.GetIdempotencyKey(),
	})
	if err != nil {
		return nil, rpcerr.From(err, "failed to request a review")
	}

	return &agentv1.RequestReviewResponse{
		Reviewer: renderAgent(*reviewer, agentFlags{alive: s.liveness.IsAgentRunning(*reviewer)}).Agent,
		Guidance: guidance,
	}, nil
}

// Adopt puts an agent on a branch or pull request that already exists.
func (s *Service) Adopt(ctx context.Context, req *agentv1.AdoptRequest) (*agentv1.AdoptResponse, *fail.Error) {
	created, err := s.adopter.Adopt(ctx, agents.AdoptParams{
		Ref:            req.GetRef(),
		Title:          req.GetTitle(),
		Flags:          req.GetFlags(),
		Prompt:         req.GetPrompt(),
		WorkflowID:     req.GetWorkflowId(),
		Harness:        req.GetHarness(),
		IdempotencyKey: req.GetIdempotencyKey(),
	})
	switch {
	// A workflow with no cwd cannot host an adoption at all.
	case errors.Is(err, agents.ErrWorktreeNeedsCwd):
		return nil, rpcerr.From(err, "failed to adopt agent").Kinded(fail.KindPrecondition)
	case err != nil:
		return nil, rpcerr.From(err, "failed to adopt agent")
	}

	return &agentv1.AdoptResponse{Agent: renderAgent(*created, agentFlags{alive: s.liveness.IsAgentRunning(*created)}).Agent}, nil
}

// agentFlagsFor gathers the facts about a set of agents that are not on their own rows, asking
// for each once rather than once per agent.
func (s *Service) agentFlagsFor(ctx context.Context, listed []agentstore.Agent) (func(agentstore.Agent) agentFlags, *fail.Error) {
	running := s.liveness.GetRunningAgents(ctx, listed)

	mailboxes, err := s.mail.ListUnreadMailboxes(ctx)
	if err != nil {
		return nil, rpcerr.From(err, "failed to list the mailboxes holding unread mail")
	}
	waiting := xslice.KeyBy(mailboxes, func(id string) string { return id })

	unseen, err := s.agents.ListUnseenTurns(ctx)
	if err != nil {
		return nil, rpcerr.From(err, "failed to list the agents holding unseen turns")
	}
	unviewed := xslice.KeyBy(unseen, func(id string) string { return id })

	tallied, err := s.review.ListTallies(ctx)
	if err != nil {
		return nil, rpcerr.From(err, "failed to count the rounds of review the agents have been through")
	}
	tallies := xslice.KeyBy(tallied, func(tally reviewstore.Tally) string { return tally.AgentID })

	live, err := s.review.ListLiveReviewers(ctx)
	if err != nil {
		return nil, rpcerr.From(err, "failed to list the agents whose work is being reviewed")
	}
	reading := xslice.KeyBy(live, func(reviewer reviewstore.LiveReviewer) string { return reviewer.AgentID })

	failures, err := s.runs.ListUnreadFailures(ctx)
	if err != nil {
		return nil, rpcerr.From(err, "failed to list the agents holding a failed run")
	}
	failed := xslice.KeyBy(failures, func(failure runstore.Failure) string { return failure.AgentID })

	listedMarks, err := s.marks.ListMarks(ctx)
	if err != nil {
		return nil, rpcerr.From(err, "failed to list the marks")
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

// localVerdictOf is the tally's verdict as the wire carries it, empty for an agent whose work no
// review has concluded on.
func localVerdictOf(tally reviewstore.Tally) string {
	if tally.Verdict == nil {
		return ""
	}

	return string(*tally.Verdict)
}

// ListAgents returns one page of stored agents, newest first.
func (s *Service) ListAgents(ctx context.Context, req *agentv1.ListAgentsRequest) (*agentv1.ListAgentsResponse, *fail.Error) {
	paginator, err := pagination.New[agentstore.Cursor, agent](req.GetPagination())
	if err != nil {
		return nil, rpcerr.From(err, "failed to read the pagination")
	}

	agents, err := s.agents.ListPage(ctx, paginator.Limit(), paginator.Cursor(), req.GetArchived())
	if err != nil {
		return nil, rpcerr.From(err, "failed to list agents")
	}

	flags, failure := s.agentFlagsFor(ctx, agents)
	if failure != nil {
		return nil, failure
	}

	view := agentViewOptions(req.GetView())
	resources := xslice.Map(agents, func(a agentstore.Agent) agent { return renderAgent(a, flags(a), view...) })

	next, err := paginator.NextToken(resources)
	if err != nil {
		return nil, rpcerr.From(err, "failed to build the next page token")
	}

	response := &agentv1.ListAgentsResponse{NextPageToken: next}
	response.Agents = xslice.Map(resources, func(resource agent) *agentv1.Agent { return resource.Agent })

	return response, nil
}

// renderOneAgent gathers the facts an agent's row does not hold and renders it.
func (s *Service) renderOneAgent(ctx context.Context, stored agentstore.Agent) (*agentv1.Agent, *fail.Error) {
	flags, failure := s.agentFlagsFor(ctx, []agentstore.Agent{stored})
	if failure != nil {
		return nil, failure
	}

	return renderAgent(stored, flags(stored)).Agent, nil
}

// GetAgent returns one agent by id, archived or not.
func (s *Service) GetAgent(ctx context.Context, req *agentv1.GetAgentRequest) (*agentv1.GetAgentResponse, *fail.Error) {
	agent, err := s.agents.GetByID(ctx, req.GetId())
	if err != nil {
		return nil, rpcerr.From(err, "failed to get agent")
	}

	rendered, failure := s.renderOneAgent(ctx, *agent)
	if failure != nil {
		return nil, failure
	}

	return &agentv1.GetAgentResponse{Agent: rendered}, nil
}

// SetMark puts a letter on an agent or a workflow, taking it off whichever row held it, and
// carries no tool annotation because a mark is the person's own.
func (s *Service) SetMark(ctx context.Context, req *agentv1.SetMarkRequest) (*emptypb.Empty, *fail.Error) {
	target := markstore.Target{}
	switch {
	case req.GetAgentId() != "":
		target = markstore.OnAgent(req.GetAgentId())
	case req.GetWorkflowId() != "":
		target = markstore.OnWorkflow(req.GetWorkflowId())
	}

	if req.Mark == nil {
		if _, err := s.marks.ClearMark(ctx, target); err != nil {
			return nil, rpcerr.From(err, "failed to clear the mark")
		}

		return &emptypb.Empty{}, nil
	}

	marked, err := s.marks.SetMark(ctx, req.GetMark(), target)
	if err != nil {
		return nil, rpcerr.From(err, "failed to set the mark")
	}
	if !marked {
		return nil, store.ErrNotFound.Saying("%s is not a live row to mark", target)
	}

	return &emptypb.Empty{}, nil
}

// SetHidden hides an agent from the panel's sidebar or brings it back.
func (s *Service) SetHidden(ctx context.Context, req *agentv1.SetHiddenRequest) (*emptypb.Empty, *fail.Error) {
	changed, err := s.agents.SetHidden(ctx, req.GetId(), req.GetHidden())
	if err != nil {
		return nil, rpcerr.From(err, "failed to hide the agent")
	}
	if !changed {
		return nil, agents.ErrAgentNotFound.Saying("no agent with id %q", req.GetId())
	}

	return &emptypb.Empty{}, nil
}

// SetAgentViewed claims whatever the agent has finished up to now, or puts the mark back, and
// carries no tool annotation because an agent must not move the marks on the user's screen.
func (s *Service) SetAgentViewed(ctx context.Context, req *agentv1.SetAgentViewedRequest) (*emptypb.Empty, *fail.Error) {
	if err := s.agents.SetViewed(ctx, req.GetId(), req.GetViewed()); err != nil {
		return nil, rpcerr.From(err, "failed to set whether the agent was viewed")
	}

	return &emptypb.Empty{}, nil
}

// MarkRunsRead claims the failed runs an agent is holding, and carries no tool annotation
// because an agent must not move the marks on the user's screen.
func (s *Service) MarkRunsRead(ctx context.Context, req *agentv1.MarkRunsReadRequest) (*emptypb.Empty, *fail.Error) {
	if err := s.runs.MarkRead(ctx, req.GetId()); err != nil {
		return nil, rpcerr.From(err, "failed to claim the agent's failed runs")
	}

	return &emptypb.Empty{}, nil
}

// SetMetadata records one fact an agent found out about its own work, or clears the key
// when the value is unset.
func (s *Service) SetMetadata(ctx context.Context, req *agentv1.SetMetadataRequest) (*emptypb.Empty, *fail.Error) {
	changed, err := s.agents.SetMetadata(ctx, agentstore.SetMetadataParams{
		ID:    req.GetId(),
		Key:   req.GetKey(),
		Value: req.Value,
	})
	if err != nil {
		return nil, rpcerr.From(err, "failed to set the metadata")
	}
	// Clearing a key nothing held is not an error.
	if !changed && req.Value != nil {
		return nil, agents.ErrAgentNotFound.Saying("no agent with id %q", req.GetId())
	}

	return &emptypb.Empty{}, nil
}

// Log writes one line to weave's log for an agent, which is how a running script whose stdout
// goes nowhere says what it did.
func (s *Service) Log(ctx context.Context, req *agentv1.LogRequest) (*emptypb.Empty, *fail.Error) {
	id, failure := scopedAgentID(ctx, req.GetId())
	if failure != nil {
		return nil, failure
	}

	agent, err := s.agents.GetByID(ctx, id)
	if err != nil {
		return nil, rpcerr.From(err, "failed to get agent")
	}

	attrs := []slog.Attr{
		slog.String(logging.TargetAgentID, agent.ID),
		slog.String(logging.ScriptAgentCwd, agent.Cwd),
	}
	if script := req.GetScript(); script != "" {
		attrs = append(attrs, slog.String(logging.ScriptName, script))
	}

	//nolint:sloglint // this RPC exists to relay a caller's own message, so the message is the argument
	logging.From(ctx).LogAttrs(ctx, slog.LevelInfo, req.GetMessage(), attrs...)

	return &emptypb.Empty{}, nil
}

// RecordReport takes what the calling agent's own CLI read out of one of its hook payloads,
// putting the reason beside the state the pane decided and stamping a reported turn end.
func (s *Service) RecordReport(ctx context.Context, req *agentv1.RecordReportRequest) (*emptypb.Empty, *fail.Error) {
	id := caller.From(ctx).AgentID
	if id == "" {
		return nil, errCallerHasNoAgent
	}

	// Both stamps are taken before either write, so a store that is down costs the flags it
	// would have raised rather than also costing the reconciler its shortcut to idle.
	reason := req.GetReason()
	s.reconciler.RecordReportedReason(id, reason)
	turnEnded := req.GetTurnEnded()
	if turnEnded {
		s.reconciler.RecordTurnBoundary(id)
	}

	if err := s.agents.SetReportedReason(ctx, id, reason); err != nil {
		return nil, rpcerr.From(err, "failed to record what the agent reported it was doing")
	}

	if !turnEnded {
		return &emptypb.Empty{}, nil
	}

	if err := s.agents.SetTurnEnded(ctx, id); err != nil {
		return nil, rpcerr.From(err, "failed to record the turn end")
	}

	return &emptypb.Empty{}, nil
}

// scopedAgentID is the agent a request is about, being the one it names or the calling agent
// when it names none.
func scopedAgentID(ctx context.Context, requested string) (string, *fail.Error) {
	if requested != "" {
		return requested, nil
	}

	from := caller.From(ctx).AgentID
	if from == "" {
		return "", errNoIDToFallBackOn
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
func (s *Service) DeleteAgents(ctx context.Context, req *agentv1.DeleteAgentsRequest) (*emptypb.Empty, *fail.Error) {
	if err := s.lifecycle.DeleteAgents(ctx, req.GetIds()); err != nil {
		return nil, rpcerr.From(err, "failed to delete agents")
	}

	return &emptypb.Empty{}, nil
}

// ArchiveAgents stops agents and hides them, keeping the records and transcripts.
func (s *Service) ArchiveAgents(ctx context.Context, req *agentv1.ArchiveAgentsRequest) (*emptypb.Empty, *fail.Error) {
	if err := s.lifecycle.ArchiveAgents(ctx, req.GetIds()); err != nil {
		return nil, rpcerr.From(err, "failed to archive agents")
	}

	return &emptypb.Empty{}, nil
}

// RestoreAgents takes agents out of the archive and returns them as stored, dormant.
func (s *Service) RestoreAgents(ctx context.Context, req *agentv1.RestoreAgentsRequest) (*agentv1.RestoreAgentsResponse, *fail.Error) {
	restored, err := s.lifecycle.RestoreAgents(ctx, req.GetIds())
	if err != nil {
		return nil, rpcerr.From(err, "failed to restore agents")
	}

	flags, failure := s.agentFlagsFor(ctx, restored)
	if failure != nil {
		return nil, failure
	}

	return &agentv1.RestoreAgentsResponse{
		Agents: xslice.Map(restored, func(a agentstore.Agent) *agentv1.Agent {
			return renderAgent(a, flags(a)).Agent
		}),
	}, nil
}

// EmptyArchive queues the sweep that deletes the whole archive as a background job, every agent
// in it running its own deprovisioner.
func (s *Service) EmptyArchive(ctx context.Context, _ *emptypb.Empty) (*emptypb.Empty, *fail.Error) {
	if err := s.archive.EmptyArchive(ctx); err != nil {
		return nil, rpcerr.From(err, "failed to empty the archive")
	}

	return &emptypb.Empty{}, nil
}

// ResumeAgent relaunches an agent's CLI on its conversation and returns it as stored.
func (s *Service) ResumeAgent(ctx context.Context, req *agentv1.ResumeAgentRequest) (*agentv1.ResumeAgentResponse, *fail.Error) {
	resumed, err := s.resumer.ResumeAgent(ctx, req.GetId())
	if err != nil {
		return nil, rpcerr.From(err, "failed to resume agent")
	}

	rendered, failure := s.renderOneAgent(ctx, *resumed)
	if failure != nil {
		return nil, failure
	}

	return &agentv1.ResumeAgentResponse{Agent: rendered}, nil
}

// UpdateAgent renames the agent with the given id, reporting a name another agent holds as a
// conflict rather than leaking the database's own refusal.
func (s *Service) UpdateAgent(ctx context.Context, req *agentv1.UpdateAgentRequest) (*emptypb.Empty, *fail.Error) {
	renamed, err := s.agents.Rename(ctx, req.GetId(), req.GetName())
	if err != nil {
		return nil, rpcerr.From(err, "failed to update agent")
	}
	if !renamed {
		return nil, agents.ErrAgentNotFound.Saying("no agent with id %q", req.GetId())
	}

	return &emptypb.Empty{}, nil
}
