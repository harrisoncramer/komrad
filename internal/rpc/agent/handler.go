// Package agent serves the agent RPCs, translating each request into a call on the agent
// services and each domain error into the connect code the caller sees.
package agent

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"weave/internal/agents"
	"weave/internal/caller"
	"weave/internal/fail"
	"weave/internal/logging"
	"weave/internal/rpc/render"
	"weave/internal/rpcerr"
	"weave/internal/store"
	"weave/lib/pagination"
	"weave/lib/xslice"
	agentv1 "weave/proto/gen/agent/v1"

	agentstore "weave/internal/store/agent"
	markstore "weave/internal/store/mark"
	rowstore "weave/internal/store/row"

	"google.golang.org/protobuf/types/known/emptypb"
)

type agentCreator interface {
	NewAgent(ctx context.Context, p agents.NewAgentParams) (*agentstore.Agent, error)
	RequestReview(ctx context.Context, p agents.RequestReviewParams) (*agentstore.Agent, string, error)
	RetryCreate(ctx context.Context, p agents.RetryCreateParams) (*agentstore.Agent, error)
}

type agentAdopter interface {
	Adopt(ctx context.Context, p agents.AdoptParams) (*agentstore.Agent, error)
}

type agentLiveness interface {
	IsAgentRunning(agent agentstore.Agent) bool
}

// rowReader is what an agent's row says, read for a page, for the agents a write moved, and
// for one agent.
type rowReader interface {
	ListPage(ctx context.Context, page rowstore.Page) ([]rowstore.Row, error)
	ListByKey(ctx context.Context, ids []string) ([]rowstore.Row, error)
	Get(ctx context.Context, id string) (*rowstore.Row, error)
}

type agentLifecycle interface {
	DeleteAgents(ctx context.Context, ids []string) error
	ArchiveAgents(ctx context.Context, ids []string) error
	RestoreAgents(ctx context.Context, ids []string) ([]agentstore.Agent, error)
}

type agentResumer interface {
	ResumeAgent(ctx context.Context, id string) (*agentstore.Agent, error)
}

type agentHolder interface {
	Hold(ctx context.Context, agentID string) (*agentstore.Agent, error)
	Release(ctx context.Context, params agents.ReleaseParams) (*agentstore.Agent, error)
}

type agentReader interface {
	GetByID(ctx context.Context, id string) (*agentstore.Agent, error)
	Exists(ctx context.Context, id string) (bool, error)
}

// rowWriter changes what an agent's row says.
type rowWriter interface {
	SetHidden(ctx context.Context, id string, hidden bool) error
	SetMetadata(ctx context.Context, params agents.SetMetadataParams) error
	Rename(ctx context.Context, id, name string) error
	MarkRunsRead(ctx context.Context, id string) error
	RecordReport(ctx context.Context, params agents.RecordReportParams) error
}

// markReadWriter owns the letters a person types to jump to a row, which span agents and
// workflows and so are written through a transaction rather than through one store.
type markReadWriter interface {
	ListMarks(ctx context.Context) ([]markstore.Mark, error)
	SetMark(ctx context.Context, mark string, target markstore.Target) (bool, error)
	ClearMark(ctx context.Context, target markstore.Target) (bool, error)
}

type archiveEmptier interface {
	EmptyArchive(ctx context.Context) error
}

type NewHandlerParams struct {
	Agents       agentReader
	Rows         rowReader
	Writer       rowWriter
	Transcripts  transcriptReader
	Mail         mailer
	Creator      agentCreator
	Adopter      agentAdopter
	Liveness     agentLiveness
	Lifecycle    agentLifecycle
	Resumer      agentResumer
	Holder       agentHolder
	Archive      archiveEmptier
	Review       reviewConversation
	Marks        markReadWriter
	Workflows    workflowReader
	Unclaimed    unclaimedMailReader
	PullRequests pullRequestReader
	Repositories repositoryReader
	Rebaser      worktreeRebaser
	Orchestrator orchestratorStarter
	Clock        func() time.Time
}

type Handler struct {
	agents       agentReader
	rows         rowReader
	writer       rowWriter
	transcripts  transcriptReader
	mail         mailer
	creator      agentCreator
	adopter      agentAdopter
	liveness     agentLiveness
	lifecycle    agentLifecycle
	resumer      agentResumer
	holder       agentHolder
	archive      archiveEmptier
	review       reviewConversation
	marks        markReadWriter
	workflows    workflowReader
	unclaimed    unclaimedMailReader
	pullRequests pullRequestReader
	repositories repositoryReader
	rebaser      worktreeRebaser
	orchestrator orchestratorStarter
	now          func() time.Time
}

// NewHandler constructs the handler from the stores it reads and the services that move agents
// through their life.
func NewHandler(params NewHandlerParams) *Handler {
	now := params.Clock
	if now == nil {
		now = time.Now
	}

	return &Handler{
		agents:       params.Agents,
		rows:         params.Rows,
		writer:       params.Writer,
		transcripts:  params.Transcripts,
		mail:         params.Mail,
		creator:      params.Creator,
		adopter:      params.Adopter,
		liveness:     params.Liveness,
		lifecycle:    params.Lifecycle,
		resumer:      params.Resumer,
		holder:       params.Holder,
		archive:      params.Archive,
		review:       params.Review,
		marks:        params.Marks,
		workflows:    params.Workflows,
		unclaimed:    params.Unclaimed,
		pullRequests: params.PullRequests,
		repositories: params.Repositories,
		rebaser:      params.Rebaser,
		orchestrator: params.Orchestrator,
		now:          now,
	}
}

// errCallerHasNoAgent is a report from something that is not an agent, which has no row to
// record it against.
var errCallerHasNoAgent = fail.InvalidArgument("the caller has no agent id of its own")

// errNoIDToFallBackOn is a request naming no agent, made by something that is not one.
var errNoIDToFallBackOn = fail.InvalidArgument("no id given, and the caller has none of its own to fall back to")

// CreateAgent launches a background agent and returns it as stored.
func (s *Handler) CreateAgent(ctx context.Context, req *agentv1.CreateAgentRequest) (*agentv1.CreateAgentResponse, *fail.Error) {
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

	rendered, failure := s.renderOneAgent(ctx, created.ID)
	if failure != nil {
		return nil, failure
	}

	return &agentv1.CreateAgentResponse{Agent: rendered}, nil
}

// RequestReview spawns the reviewer for the calling agent's own work and answers with the half
// of the review briefing it was not launched with.
func (s *Handler) RequestReview(ctx context.Context, req *agentv1.RequestReviewRequest) (*agentv1.RequestReviewResponse, *fail.Error) {
	id, failure := scopedAgentID(ctx, req.GetAgentId())
	if failure != nil {
		return nil, failure
	}

	reviewer, guidance, err := s.creator.RequestReview(ctx, agents.RequestReviewParams{
		AgentID:        id,
		Prompt:         req.GetPrompt(),
		Title:          req.GetTitle(),
		Harness:        req.GetHarness(),
		Flags:          req.GetFlags(),
		IdempotencyKey: req.GetIdempotencyKey(),
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
func (s *Handler) Adopt(ctx context.Context, req *agentv1.AdoptRequest) (*agentv1.AdoptResponse, *fail.Error) {
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

	rendered, failure := s.renderOneAgent(ctx, created.ID)
	if failure != nil {
		return nil, failure
	}

	return &agentv1.AdoptResponse{Agent: rendered}, nil
}

// ListAgents returns one page of stored agents, newest first, each assembled with what is
// drawn beside it.
func (s *Handler) ListAgents(ctx context.Context, req *agentv1.ListAgentsRequest) (*agentv1.ListAgentsResponse, *fail.Error) {
	paginator, err := pagination.New[agentstore.Cursor, agentRow](req.GetPagination())
	if err != nil {
		return nil, rpcerr.From(err, "failed to read the pagination")
	}

	rows, err := s.rows.ListPage(ctx, rowstore.Page{
		Limit:    paginator.Limit(),
		Cursor:   paginator.Cursor(),
		Archived: req.GetArchived(),
	})
	if err != nil {
		return nil, rpcerr.From(err, "failed to list agents")
	}

	resources, failure := s.assembleRows(ctx, rows, agentViewOptions(req.GetView()))
	if failure != nil {
		return nil, failure
	}

	next, err := paginator.NextToken(resources)
	if err != nil {
		return nil, rpcerr.From(err, "failed to build the next page token")
	}

	response := &agentv1.ListAgentsResponse{NextPageToken: next}
	response.Agents = xslice.Map(resources, func(resource agentRow) *agentv1.AgentRow { return resource.AgentRow })

	return response, nil
}

// renderOneAgent reads one agent's row and renders it.
func (s *Handler) renderOneAgent(ctx context.Context, id string) (*agentv1.Agent, *fail.Error) {
	row, err := s.rows.Get(ctx, id)
	if err != nil {
		return nil, rpcerr.From(err, "failed to get agent")
	}

	return render.Agent(*row, render.AgentOptions{}), nil
}

// GetAgent returns one agent by id, archived or not, assembled with what is drawn beside it.
func (s *Handler) GetAgent(ctx context.Context, req *agentv1.GetAgentRequest) (*agentv1.GetAgentResponse, *fail.Error) {
	row, err := s.rows.Get(ctx, req.GetId())
	if err != nil {
		return nil, rpcerr.From(err, "failed to get agent")
	}

	resources, failure := s.assembleRows(ctx, []rowstore.Row{*row}, render.AgentOptions{})
	if failure != nil {
		return nil, failure
	}

	return &agentv1.GetAgentResponse{Agent: resources[0].AgentRow}, nil
}

// ListMarks is every mark, by letter.
func (s *Handler) ListMarks(ctx context.Context, _ *emptypb.Empty) (*agentv1.ListMarksResponse, *fail.Error) {
	marks, err := s.marks.ListMarks(ctx)
	if err != nil {
		return nil, rpcerr.From(err, "failed to list the marks")
	}

	return &agentv1.ListMarksResponse{Marks: xslice.Map(marks, render.Mark)}, nil
}

// SetMark puts a letter on an agent or a workflow, taking it off whichever row held it, and
// carries no tool annotation because a mark is the person's own.
func (s *Handler) SetMark(ctx context.Context, req *agentv1.SetMarkRequest) (*emptypb.Empty, *fail.Error) {
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
func (s *Handler) SetHidden(ctx context.Context, req *agentv1.SetHiddenRequest) (*emptypb.Empty, *fail.Error) {
	if err := s.writer.SetHidden(ctx, req.GetId(), req.GetHidden()); err != nil {
		return nil, rpcerr.From(err, "failed to hide the agent")
	}

	return &emptypb.Empty{}, nil
}

// MarkRunsRead claims the failed runs an agent is holding, and carries no tool annotation
// because an agent must not move the marks on the user's screen.
func (s *Handler) MarkRunsRead(ctx context.Context, req *agentv1.MarkRunsReadRequest) (*emptypb.Empty, *fail.Error) {
	if err := s.writer.MarkRunsRead(ctx, req.GetId()); err != nil {
		return nil, rpcerr.From(err, "failed to claim the agent's failed runs")
	}

	return &emptypb.Empty{}, nil
}

// SetMetadata records one fact an agent found out about its own work, or clears the key
// when the value is unset.
func (s *Handler) SetMetadata(ctx context.Context, req *agentv1.SetMetadataRequest) (*emptypb.Empty, *fail.Error) {
	err := s.writer.SetMetadata(ctx, agents.SetMetadataParams{
		AgentID: req.GetId(),
		Key:     req.GetKey(),
		Value:   req.Value,
	})
	if err != nil {
		return nil, rpcerr.From(err, "failed to set the metadata")
	}

	return &emptypb.Empty{}, nil
}

// Log writes one line to weave's log for an agent.
func (s *Handler) Log(ctx context.Context, req *agentv1.LogRequest) (*emptypb.Empty, *fail.Error) {
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

// RecordReport takes what the calling agent's own CLI read out of one of its hook payloads.
func (s *Handler) RecordReport(ctx context.Context, req *agentv1.RecordReportRequest) (*emptypb.Empty, *fail.Error) {
	id := caller.From(ctx).AgentID
	if id == "" {
		return nil, errCallerHasNoAgent
	}

	err := s.writer.RecordReport(ctx, agents.RecordReportParams{
		AgentID:   id,
		Reason:    req.GetReason(),
		TurnEnded: req.GetTurnEnded(),
	})
	if err != nil {
		return nil, rpcerr.From(err, "failed to record what the agent reported")
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
func (s *Handler) DeleteAgents(ctx context.Context, req *agentv1.DeleteAgentsRequest) (*emptypb.Empty, *fail.Error) {
	if err := s.lifecycle.DeleteAgents(ctx, req.GetIds()); err != nil {
		return nil, rpcerr.From(err, "failed to delete agents")
	}

	return &emptypb.Empty{}, nil
}

var errArchiveNotYours = fail.Denied("an agent may archive only its own children")

// ArchiveAgents stops agents and hides them, keeping the records and transcripts.
func (s *Handler) ArchiveAgents(ctx context.Context, req *agentv1.ArchiveAgentsRequest) (*emptypb.Empty, *fail.Error) {
	if err := s.refuseArchivingOthers(ctx, req.GetIds()); err != nil {
		return nil, rpcerr.From(err, "failed to archive agents")
	}

	if err := s.lifecycle.ArchiveAgents(ctx, req.GetIds()); err != nil {
		return nil, rpcerr.From(err, "failed to archive agents")
	}

	return &emptypb.Empty{}, nil
}

// refuseArchivingOthers holds an agent other than an orchestrator to its own children, leaving the panel unrestricted.
func (s *Handler) refuseArchivingOthers(ctx context.Context, ids []string) error {
	from := caller.From(ctx).AgentID
	if from == "" {
		return nil
	}

	me, err := s.agents.GetByID(ctx, from)
	if err != nil {
		return err
	}
	if me.Role == agentstore.RoleOrchestrator {
		return nil
	}

	for _, id := range ids {
		target, err := s.agents.GetByID(ctx, id)
		if errors.Is(err, store.ErrNotFound) {
			continue
		}
		if err != nil {
			return err
		}
		if target.ParentAgentID == nil || *target.ParentAgentID != from {
			return errArchiveNotYours.Saying("%s is not a child of %s", target.Name, me.Name)
		}
	}

	return nil
}

// RestoreAgents takes agents out of the archive and returns them as stored, dormant.
func (s *Handler) RestoreAgents(ctx context.Context, req *agentv1.RestoreAgentsRequest) (*agentv1.RestoreAgentsResponse, *fail.Error) {
	restored, err := s.lifecycle.RestoreAgents(ctx, req.GetIds())
	if err != nil {
		return nil, rpcerr.From(err, "failed to restore agents")
	}

	rows, err := s.rows.ListByKey(ctx, agentstore.IDs(restored))
	if err != nil {
		return nil, rpcerr.From(err, "failed to get the restored agents")
	}

	return &agentv1.RestoreAgentsResponse{
		Agents: xslice.Map(rows, func(row rowstore.Row) *agentv1.Agent { return render.Agent(row, render.AgentOptions{}) }),
	}, nil
}

// EmptyArchive queues the sweep that deletes the whole archive as a background job, every agent
// in it running its own deprovisioner.
func (s *Handler) EmptyArchive(ctx context.Context, _ *emptypb.Empty) (*emptypb.Empty, *fail.Error) {
	if err := s.archive.EmptyArchive(ctx); err != nil {
		return nil, rpcerr.From(err, "failed to empty the archive")
	}

	return &emptypb.Empty{}, nil
}

// ResumeAgent relaunches an agent's CLI on its conversation and returns it as stored.
func (s *Handler) ResumeAgent(ctx context.Context, req *agentv1.ResumeAgentRequest) (*agentv1.ResumeAgentResponse, *fail.Error) {
	resumed, err := s.resumer.ResumeAgent(ctx, req.GetId())
	if err != nil {
		return nil, rpcerr.From(err, "failed to resume agent")
	}

	rendered, failure := s.renderOneAgent(ctx, resumed.ID)
	if failure != nil {
		return nil, failure
	}

	return &agentv1.ResumeAgentResponse{Agent: rendered}, nil
}

// RetryAgent relaunches an agent that failed to create, optionally with corrected flags.
func (s *Handler) RetryAgent(ctx context.Context, req *agentv1.RetryAgentRequest) (*agentv1.RetryAgentResponse, *fail.Error) {
	retried, err := s.creator.RetryCreate(ctx, agents.RetryCreateParams{
		ID:      req.GetId(),
		Flags:   req.GetFlags(),
		Prompt:  req.GetPrompt(),
		Harness: req.GetHarness(),
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

// HoldAgent stops an agent so a person may take its worktree, refusing unless it is seen to come
// to rest and refusing an agent that asks to hold itself.
func (s *Handler) HoldAgent(ctx context.Context, req *agentv1.HoldAgentRequest) (*agentv1.HoldAgentResponse, *fail.Error) {
	if from := caller.From(ctx).AgentID; from != "" && from == req.GetId() {
		return nil, agents.ErrHoldSelf
	}

	held, err := s.holder.Hold(ctx, req.GetId())
	if err != nil {
		return nil, rpcerr.From(err, "failed to hold agent")
	}

	rendered, failure := s.renderOneAgent(ctx, held.ID)
	if failure != nil {
		return nil, failure
	}

	return &agentv1.HoldAgentResponse{Agent: rendered}, nil
}

// ReleaseAgent lets go of a held agent, telling it what changed while it was held.
func (s *Handler) ReleaseAgent(ctx context.Context, req *agentv1.ReleaseAgentRequest) (*agentv1.ReleaseAgentResponse, *fail.Error) {
	released, err := s.holder.Release(ctx, agents.ReleaseParams{
		AgentID: req.GetId(),
		Said:    req.GetSaid(),
	})
	if err != nil {
		return nil, rpcerr.From(err, "failed to release agent")
	}

	rendered, failure := s.renderOneAgent(ctx, released.ID)
	if failure != nil {
		return nil, failure
	}

	return &agentv1.ReleaseAgentResponse{Agent: rendered}, nil
}

// UpdateAgent renames the agent with the given id.
func (s *Handler) UpdateAgent(ctx context.Context, req *agentv1.UpdateAgentRequest) (*emptypb.Empty, *fail.Error) {
	if err := s.writer.Rename(ctx, req.GetId(), req.GetName()); err != nil {
		return nil, rpcerr.From(err, "failed to update agent")
	}

	return &emptypb.Empty{}, nil
}
