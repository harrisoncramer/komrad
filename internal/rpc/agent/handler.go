// Package agent serves the agent RPCs, translating each request into a call on the agent
// services and each domain error into the connect code the caller sees.
package agent

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"komrad/internal/agenterr"
	"komrad/internal/agents"
	"komrad/internal/assemble"
	"komrad/internal/caller"
	"komrad/internal/fail"
	"komrad/internal/logging"
	"komrad/internal/rpc/render"
	"komrad/internal/rpcerr"
	"komrad/internal/rpcpage"
	"komrad/lib/xptr"
	"komrad/lib/xslice"
	agentv1 "komrad/proto/gen/agent/v1"
	"komrad/proto/gen/agent/v1/agentv1komrad"

	agentstore "komrad/internal/store/agent"
	rowstore "komrad/internal/store/row"

	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/emptypb"
)

type agentCreator interface {
	NewAgent(ctx context.Context, p agents.NewAgentParams) (*agentstore.Agent, error)
	RequestReview(ctx context.Context, p agents.RequestReviewParams) (*agentstore.Agent, string, error)
	RetryCreate(ctx context.Context, p agents.RetryCreateParams) (*agentstore.Agent, error)
}

type agentAdopter interface {
	Adopt(ctx context.Context, p agents.AdoptParams) (*agents.Adoption, error)
}

// rowReader is what an agent's row says, read for a page, for the agents a write moved, and
// for one agent.
type rowReader interface {
	ListRows(ctx context.Context, page rowstore.Page) ([]rowstore.Row, error)
	ListRowsByID(ctx context.Context, ids []string) ([]rowstore.Row, error)
	GetRow(ctx context.Context, id string) (*rowstore.Row, error)
	ListSurroundings(ctx context.Context, rows []rowstore.Row) (*agents.Surroundings, error)
}

type agentLifecycle interface {
	ArchiveAgents(ctx context.Context, params agents.ArchiveAgentsParams) error
	RestoreAgents(ctx context.Context, ids []string) ([]agentstore.Agent, error)
}

type agentResumer interface {
	ResumeAgent(ctx context.Context, params agents.ResumeAgentParams) (*agentstore.Agent, error)
	RestartAgent(ctx context.Context, params agents.RestartAgentParams) (*agentstore.Agent, error)
}

type agentReader interface {
	GetAgent(ctx context.Context, id string) (*agentstore.Agent, error)
}

// rowWriter changes what an agent's row says.
type rowWriter interface {
	Rename(ctx context.Context, id, name string) error
	MarkRunsRead(ctx context.Context, id string) error
	RecordReport(ctx context.Context, params agents.RecordReportParams) error
}

type turnEndNudgeClaimer interface {
	ClaimTurnEndNudge(ctx context.Context, agentID string) (string, error)
}

type NewHandlerParams struct {
	Agents        agentReader
	Rows          rowReader
	Writer        rowWriter
	Transcripts   transcriptReader
	Screens       screenReader
	Mail          mailer
	Nudges        turnEndNudgeClaimer
	Creator       agentCreator
	Adopter       agentAdopter
	Lifecycle     agentLifecycle
	Resumer       agentResumer
	Review        reviewConversation
	ForgeReplies  forgeReplier
	Documents     documentReadWriter
	DocumentTypes documentTypeKeeper
	Rebaser       worktreeRebaser
	Pusher        worktreePusher
	Aborter       sequenceAborter
	Orchestrator  orchestratorStarter
	Clock         func() time.Time
}

type Handler struct {
	agents        agentReader
	rows          rowReader
	writer        rowWriter
	transcripts   transcriptReader
	screens       screenReader
	mail          mailer
	nudges        turnEndNudgeClaimer
	creator       agentCreator
	adopter       agentAdopter
	lifecycle     agentLifecycle
	resumer       agentResumer
	review        reviewConversation
	forgeReplies  forgeReplier
	documents     documentReadWriter
	documentTypes documentTypeKeeper
	rebaser       worktreeRebaser
	pusher        worktreePusher
	aborter       sequenceAborter
	orchestrator  orchestratorStarter
	now           func() time.Time
}

// NewHandler constructs the handler from the stores it reads and the services that move agents
// through their life.
func NewHandler(params NewHandlerParams) *Handler {
	now := params.Clock
	if now == nil {
		now = time.Now
	}

	return &Handler{
		agents:        params.Agents,
		rows:          params.Rows,
		writer:        params.Writer,
		transcripts:   params.Transcripts,
		screens:       params.Screens,
		mail:          params.Mail,
		nudges:        params.Nudges,
		creator:       params.Creator,
		adopter:       params.Adopter,
		lifecycle:     params.Lifecycle,
		resumer:       params.Resumer,
		review:        params.Review,
		forgeReplies:  params.ForgeReplies,
		documents:     params.Documents,
		documentTypes: params.DocumentTypes,
		rebaser:       params.Rebaser,
		pusher:        params.Pusher,
		aborter:       params.Aborter,
		orchestrator:  params.Orchestrator,
		now:           now,
	}
}

// errCallerHasNoAgent is a report from something that is not an agent, which has no row to
// record it against.
var errCallerHasNoAgent = fail.InvalidArgument("the caller has no agent id of its own")

// errNoIDToFallBackOn is a request naming no agent, made by something that is not one.
var errNoIDToFallBackOn = fail.InvalidArgument("no id given, and the caller has none of its own to fall back to")

// CreateAgent launches a background agent and returns it as stored.
func (s *Handler) CreateAgent(ctx context.Context, req *agentv1komrad.CreateAgentRequest) (*agentv1.CreateAgentResponse, *fail.Error) {
	created, err := s.creator.NewAgent(ctx, agents.NewAgentParams{
		Title:            req.Title,
		Flags:            req.Flags,
		ProvisionEnabled: req.ProvisionEnabled,
		Prompt:           req.Prompt,
		WorkflowID:       req.WorkflowId.String(),
		Harness:          req.Harness,
		IdempotencyKey:   xptr.Deref(req.IdempotencyKey),
		Task:             req.Task,
		TaskSlug:         xptr.Deref(req.NewTaskSlug),
		TaskGoal:         req.NewTaskGoal,
		DependsOn:        req.DependsOn,
	})
	// Create-once is what the key is for, so a key already spent answers with the agent it
	// launched rather than an error.
	if err != nil && !errors.Is(err, agenterr.ErrAlreadyLaunched) {
		return nil, rpcerr.From(err, "failed to create agent")
	}

	rendered, failure := s.renderOneAgent(ctx, created.ID)
	if failure != nil {
		return nil, failure
	}

	return &agentv1.CreateAgentResponse{Agent: rendered}, nil
}

// RequestReview spawns the reviewer for the calling agent's own work and answers with the half
// of the review briefing it was not launched with.
func (s *Handler) RequestReview(ctx context.Context, req *agentv1komrad.RequestReviewRequest) (*agentv1.RequestReviewResponse, *fail.Error) {
	id, failure := scopedAgentID(ctx, req.AgentId)
	if failure != nil {
		return nil, failure
	}

	reviewer, guidance, err := s.creator.RequestReview(ctx, agents.RequestReviewParams{
		AgentID:        id,
		Prompt:         req.Prompt,
		Title:          req.Title,
		Harness:        req.Harness,
		Flags:          req.Flags,
		IdempotencyKey: xptr.Deref(req.IdempotencyKey),
	})
	if err != nil {
		return nil, rpcerr.From(err, "failed to request a review")
	}

	rendered, failure := s.renderOneAgent(ctx, reviewer.ID)
	if failure != nil {
		return nil, failure
	}

	return &agentv1.RequestReviewResponse{Reviewer: rendered, Guidance: guidance}, nil
}

// Adopt puts an agent on a branch or pull request that already exists.
func (s *Handler) Adopt(ctx context.Context, req *agentv1komrad.AdoptRequest) (*agentv1.AdoptResponse, *fail.Error) {
	adoption, err := s.adopter.Adopt(ctx, agents.AdoptParams{
		Ref:            req.Ref,
		Title:          req.Title,
		Flags:          req.Flags,
		Prompt:         req.Prompt,
		WorkflowID:     req.WorkflowId.String(),
		Harness:        req.Harness,
		IdempotencyKey: xptr.Deref(req.IdempotencyKey),
	})
	switch {
	// A workflow with no cwd cannot host an adoption at all.
	case errors.Is(err, agents.ErrWorktreeNeedsCwd):
		return nil, rpcerr.From(err, "failed to adopt agent").Kinded(fail.KindPrecondition)
	case err != nil:
		return nil, rpcerr.From(err, "failed to adopt agent")
	}

	rendered, failure := s.renderOneAgent(ctx, adoption.Agent.ID)
	if failure != nil {
		return nil, failure
	}

	return &agentv1.AdoptResponse{Agent: rendered, Existing: adoption.Existing}, nil
}

// ListAgents returns one page of stored agents, newest first, each assembled with what is
// drawn beside it.
func (s *Handler) ListAgents(ctx context.Context, req *agentv1.ListAgentsRequest) (*agentv1.ListAgentsResponse, *fail.Error) {
	page, failure := rpcpage.List(req.GetPagination(), func(limit int64, cursor *agentstore.Cursor) ([]agentRow, *fail.Error) {
		rows, err := s.rows.ListRows(ctx, rowstore.Page{
			Limit:    limit,
			Cursor:   cursor,
			Archived: req.GetArchived(),
		})
		if err != nil {
			return nil, rpcerr.From(err, "failed to list agents")
		}

		return s.assembleRows(ctx, rows, agentViewOptions(req.GetView()))
	})
	if failure != nil {
		return nil, failure
	}

	response := &agentv1.ListAgentsResponse{NextPageToken: page.Next}
	response.Agents = xslice.Map(page.Resources, func(resource agentRow) *agentv1.AgentRow { return resource.AgentRow })

	return response, nil
}

// renderOneAgent reads one agent's row and renders it.
func (s *Handler) renderOneAgent(ctx context.Context, id string) (*agentv1.Agent, *fail.Error) {
	row, err := s.rows.GetRow(ctx, id)
	if err != nil {
		return nil, rpcerr.From(err, "failed to get agent")
	}

	return render.Agent(*row, render.AgentOptions{}), nil
}

// GetAgent returns one agent by id, archived or not, assembled with what is drawn beside it.
func (s *Handler) GetAgent(ctx context.Context, req *agentv1komrad.GetAgentRequest) (*agentv1.GetAgentResponse, *fail.Error) {
	row, err := s.rows.GetRow(ctx, req.Id.String())
	if err != nil {
		return nil, rpcerr.From(err, "failed to get agent")
	}

	world, failure := s.world(ctx, []rowstore.Row{*row}, render.AgentOptions{})
	if failure != nil {
		return nil, failure
	}

	drawn := assemble.AgentRows(world, s.now())[0]

	return &agentv1.GetAgentResponse{
		Agent:                drawn.GetAgent(),
		UnreadMail:           drawn.GetUnreadMail(),
		PullRequest:          drawn.GetPullRequest(),
		PullRequests:         drawn.GetPullRequests(),
		LocalReviewSnapshots: drawn.GetLocalReviewSnapshots(),
		ReviewerComments:     drawn.ReviewerComments,
		PersonComments:       drawn.PersonComments,
		Task:                 world.Tasks[drawn.GetAgent().GetTaskId()],
	}, nil
}

// MarkRunsRead claims the failed runs an agent is holding, and carries no tool annotation
// because an agent must not move the marks on the user's screen.
func (s *Handler) MarkRunsRead(ctx context.Context, req *agentv1komrad.MarkRunsReadRequest) (*emptypb.Empty, *fail.Error) {
	if err := s.writer.MarkRunsRead(ctx, req.Id.String()); err != nil {
		return nil, rpcerr.From(err, "failed to claim the agent's failed runs")
	}

	return &emptypb.Empty{}, nil
}

// Log writes one line to komrad's log for an agent.
func (s *Handler) Log(ctx context.Context, req *agentv1komrad.LogRequest) (*emptypb.Empty, *fail.Error) {
	id, failure := scopedAgentID(ctx, req.Id)
	if failure != nil {
		return nil, failure
	}

	agent, err := s.agents.GetAgent(ctx, id)
	if err != nil {
		return nil, rpcerr.From(err, "failed to get agent")
	}

	attrs := []slog.Attr{
		slog.String(logging.TargetAgentID, agent.ID),
		slog.String(logging.ScriptAgentCwd, agent.Cwd),
	}
	if script := xptr.Deref(req.Script); script != "" {
		attrs = append(attrs, slog.String(logging.ScriptName, script))
	}

	//nolint:sloglint // this RPC exists to relay a caller's own message, so the message is the argument
	logging.From(ctx).LogAttrs(ctx, slog.LevelInfo, req.Message, attrs...)

	return &emptypb.Empty{}, nil
}

// RecordReport takes what the calling agent's own CLI read out of one of its hook payloads, and
// writes the hook call to the agent's log.
func (s *Handler) RecordReport(ctx context.Context, req *agentv1.RecordReportRequest) (*emptypb.Empty, *fail.Error) {
	id := caller.From(ctx).AgentID
	if id == "" {
		return nil, errCallerHasNoAgent
	}

	report := agents.Report{
		Event:     reportEvents[req.GetEvent()],
		Tool:      req.GetTool(),
		TurnError: req.GetTurnError(),
	}

	if err := s.writer.RecordReport(ctx, agents.RecordReportParams{AgentID: id, Report: report}); err != nil {
		return nil, rpcerr.From(err, "failed to record what the agent reported")
	}

	logging.From(ctx).LogAttrs(ctx, slog.LevelInfo, "an agent's hook reported",
		slog.String(logging.TargetAgentID, id),
		slog.String(logging.HookName, req.GetHook()),
		slog.String(logging.HookEvent, report.Event.String()),
		slog.String(logging.HookTool, report.Tool))

	return &emptypb.Empty{}, nil
}

// ClaimMailNudge hands the calling agent the line telling it about its unread mail as its turn ends,
// empty when there is none to tell it about.
func (s *Handler) ClaimMailNudge(ctx context.Context, _ *emptypb.Empty) (*agentv1.ClaimMailNudgeResponse, *fail.Error) {
	id := caller.From(ctx).AgentID
	if id == "" {
		return nil, errCallerHasNoAgent
	}

	nudge, err := s.nudges.ClaimTurnEndNudge(ctx, id)
	if err != nil {
		return nil, rpcerr.From(err, "failed to claim the mail nudge")
	}

	return &agentv1.ClaimMailNudgeResponse{Nudge: nudge}, nil
}

var reportEvents = map[agentv1.ReportEvent]agents.ReportEvent{
	agentv1.ReportEvent_REPORT_EVENT_TURN_ENDED:      agents.ReportTurnEnded,
	agentv1.ReportEvent_REPORT_EVENT_TURN_FAILED:     agents.ReportTurnFailed,
	agentv1.ReportEvent_REPORT_EVENT_TOOL_STARTED:    agents.ReportToolStarted,
	agentv1.ReportEvent_REPORT_EVENT_TOOL_FINISHED:   agents.ReportToolFinished,
	agentv1.ReportEvent_REPORT_EVENT_APPROVAL_WANTED: agents.ReportApprovalWanted,
	agentv1.ReportEvent_REPORT_EVENT_COMPACTED:       agents.ReportCompacted,
}

// scopedAgentID is the agent a request is about, being the one it names or the calling agent
// when it names none.
func scopedAgentID(ctx context.Context, requested *uuid.UUID) (string, *fail.Error) {
	if requested != nil {
		return requested.String(), nil
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

// ArchiveAgents stops agents and hides them, keeping the records and transcripts.
func (s *Handler) ArchiveAgents(ctx context.Context, req *agentv1komrad.ArchiveAgentsRequest) (*emptypb.Empty, *fail.Error) {
	if err := s.lifecycle.ArchiveAgents(ctx, agents.ArchiveAgentsParams{
		IDs:       xslice.Map(req.Ids, uuid.UUID.String),
		ByAgentID: caller.From(ctx).AgentID,
	}); err != nil {
		return nil, rpcerr.From(err, "failed to archive agents")
	}

	return &emptypb.Empty{}, nil
}

// RestoreAgents takes agents out of the archive and returns them as stored, dormant.
func (s *Handler) RestoreAgents(ctx context.Context, req *agentv1komrad.RestoreAgentsRequest) (*agentv1.RestoreAgentsResponse, *fail.Error) {
	restored, err := s.lifecycle.RestoreAgents(ctx, xslice.Map(req.Ids, uuid.UUID.String))
	if err != nil {
		return nil, rpcerr.From(err, "failed to restore agents")
	}

	rows, err := s.rows.ListRowsByID(ctx, agentstore.IDs(restored))
	if err != nil {
		return nil, rpcerr.From(err, "failed to get the restored agents")
	}

	return &agentv1.RestoreAgentsResponse{
		Agents: xslice.Map(rows, func(row rowstore.Row) *agentv1.Agent { return render.Agent(row, render.AgentOptions{}) }),
	}, nil
}

// ResumeAgent relaunches an agent's CLI on its conversation and returns it as stored.
func (s *Handler) ResumeAgent(ctx context.Context, req *agentv1komrad.ResumeAgentRequest) (*agentv1.ResumeAgentResponse, *fail.Error) {
	resumed, err := s.resumer.ResumeAgent(ctx, agents.ResumeAgentParams{
		ID:        req.Id.String(),
		ByAgentID: caller.From(ctx).AgentID,
	})
	if err != nil {
		return nil, rpcerr.From(err, "failed to resume agent")
	}

	rendered, failure := s.renderOneAgent(ctx, resumed.ID)
	if failure != nil {
		return nil, failure
	}

	return &agentv1.ResumeAgentResponse{Agent: rendered}, nil
}

// RestartAgent relaunches a task's writer or a workflow's orchestrator on a new conversation and returns it as stored.
func (s *Handler) RestartAgent(ctx context.Context, req *agentv1komrad.RestartAgentRequest) (*agentv1.RestartAgentResponse, *fail.Error) {
	restarted, err := s.resumer.RestartAgent(ctx, agents.RestartAgentParams{
		ID:        req.Id.String(),
		Note:      req.Note,
		Harness:   req.Harness,
		Flags:     req.Flags,
		ByAgentID: caller.From(ctx).AgentID,
	})
	if err != nil {
		return nil, rpcerr.From(err, "failed to restart agent")
	}

	rendered, failure := s.renderOneAgent(ctx, restarted.ID)
	if failure != nil {
		return nil, failure
	}

	return &agentv1.RestartAgentResponse{Agent: rendered}, nil
}

// RetryAgent relaunches an agent that failed to create, optionally with corrected flags.
func (s *Handler) RetryAgent(ctx context.Context, req *agentv1komrad.RetryAgentRequest) (*agentv1.RetryAgentResponse, *fail.Error) {
	retried, err := s.creator.RetryCreate(ctx, agents.RetryCreateParams{
		ID:      req.Id.String(),
		Flags:   req.Flags,
		Prompt:  req.Prompt,
		Harness: req.Harness,
	})
	if err != nil {
		return nil, rpcerr.From(err, "failed to retry agent")
	}

	rendered, failure := s.renderOneAgent(ctx, retried.ID)
	if failure != nil {
		return nil, failure
	}

	return &agentv1.RetryAgentResponse{Agent: rendered}, nil
}

// UpdateAgent renames the agent with the given id.
func (s *Handler) UpdateAgent(ctx context.Context, req *agentv1komrad.UpdateAgentRequest) (*emptypb.Empty, *fail.Error) {
	if err := s.writer.Rename(ctx, req.Id.String(), req.Name); err != nil {
		return nil, rpcerr.From(err, "failed to update agent")
	}

	return &emptypb.Empty{}, nil
}

// formatOptionalID returns an optional id as the services take it, nil when unset.
func formatOptionalID(id *uuid.UUID) *string {
	if id == nil {
		return nil
	}

	return xptr.Ptr(id.String())
}
