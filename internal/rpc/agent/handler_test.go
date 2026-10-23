package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"komrad/internal/agenterr"
	"komrad/internal/agents"
	"komrad/internal/caller"
	"komrad/internal/checkouts"
	"komrad/internal/git"
	"komrad/internal/interceptors"
	"komrad/internal/logging"
	"komrad/internal/logtest"
	"komrad/internal/rpc/render"
	"komrad/internal/store"
	agentstore "komrad/internal/store/agent"
	mailstore "komrad/internal/store/mail"
	pullrequeststore "komrad/internal/store/pullrequest"
	reviewstore "komrad/internal/store/review"
	rowstore "komrad/internal/store/row"
	taskstore "komrad/internal/store/task"
	workflowstore "komrad/internal/store/workflow"
	"komrad/lib/xptr"
	"komrad/lib/xslice"
	agentv1 "komrad/proto/gen/agent/v1"
	"komrad/proto/gen/agent/v1/agentv1connect"
	agentv1komrad "komrad/proto/gen/agent/v1/agentv1komrad"
	"komrad/proto/gen/oneofs"
	paginationv1 "komrad/proto/gen/pagination/v1"
	taskv1 "komrad/proto/gen/task/v1"
	viewv1 "komrad/proto/gen/view/v1"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	missingID  = uuid.New()
	workflowID = uuid.MustParse("01a0d1d7-0000-7000-8000-0000000000f1")
)

const komradRepository = "https://github.com/harrisoncramer/komrad"

// fakeRowReader answers from the rows and surroundings a test seeds and records each page asked for.
type fakeRowReader struct {
	rows         []rowstore.Row
	surroundings agents.Surroundings
	err          error
	pages        []rowstore.Page
}

// seed adds an agent's row, giving it an id when it has none.
func (f *fakeRowReader) seed(agent agentstore.Agent) agentstore.Agent {
	if agent.ID == uuid.Nil {
		agent.ID = uuid.New()
	}
	f.rows = append(f.rows, rowstore.Row{Agent: agent})

	return agent
}

// GetAgent answers the seeded agent with the id, or not found.
func (f *fakeRowReader) GetAgent(_ context.Context, id uuid.UUID) (*agentstore.Agent, error) {
	row, err := f.find(id)
	if err != nil {
		return nil, err
	}

	return &row.Agent, nil
}

// ListRows records the page and answers every seeded row unless err refuses it.
func (f *fakeRowReader) ListRows(_ context.Context, page rowstore.Page) ([]rowstore.Row, error) {
	f.pages = append(f.pages, page)
	if f.err != nil {
		return nil, f.err
	}

	return f.rows, nil
}

// ListRowsByID answers the seeded rows among the ids.
func (f *fakeRowReader) ListRowsByID(_ context.Context, ids []uuid.UUID) ([]rowstore.Row, error) {
	return xslice.Filter(f.rows, func(row rowstore.Row) bool {
		return slices.Contains(ids, row.ID)
	}), nil
}

// GetRow answers the seeded row with the id, or not found.
func (f *fakeRowReader) GetRow(_ context.Context, id uuid.UUID) (*rowstore.Row, error) {
	return f.find(id)
}

// ListSurroundings answers the surroundings unless err refuses it.
func (f *fakeRowReader) ListSurroundings(context.Context, []rowstore.Row) (*agents.Surroundings, error) {
	if f.err != nil {
		return nil, f.err
	}

	return &f.surroundings, nil
}

func (f *fakeRowReader) find(id uuid.UUID) (*rowstore.Row, error) {
	for _, row := range f.rows {
		if row.ID == id {
			return &row, nil
		}
	}

	return nil, fmt.Errorf("%w: agent %q", store.ErrNotFound, id)
}

// TestLifecycleRefusalsMapToCodes pins the code each refusal of the agent services reaches the
// caller as.
func TestLifecycleRefusalsMapToCodes(t *testing.T) {
	tests := []struct {
		name     string
		call     func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error
		err      error
		wantCode connect.Code
	}{
		{
			name: "a launch that cannot start",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error {
				_, err := svc.CreateAgent(ctx, connect.NewRequest(&agentv1.CreateAgentRequest{Prompt: "go", WorkflowId: workflowID.String()}))

				return err
			},
			err:      errors.New("tmux is not running"),
			wantCode: connect.CodeInternal,
		},
		{
			name:     "an adoption into a workflow with no cwd reads as a precondition",
			call:     adoptMain,
			err:      agents.ErrWorktreeNeedsCwd,
			wantCode: connect.CodeFailedPrecondition,
		},
		{
			name:     "an adoption into a workflow that makes no worktrees",
			call:     adoptMain,
			err:      agents.ErrAdoptNeedsWorktree,
			wantCode: connect.CodeFailedPrecondition,
		},
		{
			name: "a review requested by nothing that is an agent",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error {
				_, err := svc.RequestReview(ctx, connect.NewRequest(&agentv1.RequestReviewRequest{Prompt: "read it"}))

				return err
			},
			wantCode: connect.CodeInvalidArgument,
		},
		{
			name: "a reviewer past the task's ceiling",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error {
				_, err := svc.RequestReview(ctx, connect.NewRequest(&agentv1.RequestReviewRequest{
					Prompt:  "read it",
					AgentId: xptr.Ptr(alphaID.String()),
				}))

				return err
			},
			err:      agenterr.ErrTooManyReviewers,
			wantCode: connect.CodeResourceExhausted,
		},
		{
			name: "a retry of an agent that did not fail",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error {
				_, err := svc.RetryAgent(ctx, connect.NewRequest(&agentv1.RetryAgentRequest{Id: alphaID.String()}))

				return err
			},
			err:      agents.ErrAgentNotFailed,
			wantCode: connect.CodeFailedPrecondition,
		},
		{
			name: "a resume of an archived agent",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error {
				_, err := svc.ResumeAgent(ctx, connect.NewRequest(&agentv1.ResumeAgentRequest{Id: alphaID.String()}))

				return err
			},
			err:      agenterr.ErrAgentArchived,
			wantCode: connect.CodeFailedPrecondition,
		},
		{
			name: "an archive naming an agent nobody holds",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error {
				_, err := svc.ArchiveAgents(ctx, connect.NewRequest(&agentv1.ArchiveAgentsRequest{Ids: []string{alphaID.String()}}))

				return err
			},
			err:      fmt.Errorf("%w: %s", agenterr.ErrAgentNotFound, alphaID),
			wantCode: connect.CodeNotFound,
		},
		{
			name:     "a restore past the panel's ceiling",
			call:     restoreAlpha,
			err:      agenterr.ErrTooManyAgents,
			wantCode: connect.CodeResourceExhausted,
		},
		{
			name:     "a restore onto a name a live agent has taken",
			call:     restoreAlpha,
			err:      fmt.Errorf("%w: auth", store.ErrNameTaken),
			wantCode: connect.CodeAlreadyExists,
		},
		{
			name:     "a restore of a reviewer before its task",
			call:     restoreAlpha,
			err:      agenterr.ErrTaskArchived,
			wantCode: connect.CodeFailedPrecondition,
		},
		{
			name: "an orchestrator for a workflow that is not there",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error {
				_, err := svc.StartOrchestrator(ctx, connect.NewRequest(&agentv1.StartOrchestratorRequest{WorkflowId: missingID.String()}))

				return err
			},
			err:      agenterr.ErrWorkflowNotFound,
			wantCode: connect.CodeNotFound,
		},
		{
			name: "planning a rebase of a task with no worktree",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error {
				_, err := svc.PlanRebase(ctx, connect.NewRequest(&agentv1.PlanRebaseRequest{Task: &agentv1.PlanRebaseRequest_TaskId{TaskId: alphaID.String()}}))

				return err
			},
			err:      checkouts.ErrNoWorktree,
			wantCode: connect.CodeFailedPrecondition,
		},
		{
			name: "a rebase of a dirty worktree",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error {
				_, err := svc.RebaseWorktree(ctx, connect.NewRequest(&agentv1.RebaseWorktreeRequest{Task: &agentv1.RebaseWorktreeRequest_TaskId{TaskId: alphaID.String()}}))

				return err
			},
			err:      checkouts.ErrRebaseDirty,
			wantCode: connect.CodeFailedPrecondition,
		},
		{
			name: "a push origin turned away because it moved on",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error {
				_, err := svc.PushWorktree(ctx, connect.NewRequest(&agentv1.PushWorktreeRequest{Task: &agentv1.PushWorktreeRequest_TaskId{TaskId: alphaID.String()}}))

				return err
			},
			err:      git.ErrPushRejected,
			wantCode: connect.CodeFailedPrecondition,
		},
		{
			name: "an abort of a worktree with nothing in progress",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error {
				_, err := svc.AbortSequence(ctx, connect.NewRequest(&agentv1.AbortSequenceRequest{Task: &agentv1.AbortSequenceRequest_TaskId{TaskId: alphaID.String()}}))

				return err
			},
			err:      checkouts.ErrAbortNothing,
			wantCode: connect.CodeFailedPrecondition,
		},
		{
			name: "a rename onto a name another agent holds",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error {
				_, err := svc.UpdateAgent(ctx, connect.NewRequest(&agentv1.UpdateAgentRequest{
					Id:   alphaID.String(),
					Name: "beta",
				}))

				return err
			},
			err:      fmt.Errorf("%w: beta", store.ErrNameTaken),
			wantCode: connect.CodeAlreadyExists,
		},
		{
			name: "a report from nothing that is an agent",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error {
				_, err := svc.RecordReport(ctx, connect.NewRequest(&agentv1.RecordReportRequest{Event: agentv1.ReportEvent_REPORT_EVENT_TURN_ENDED, Hook: "Stop"}))

				return err
			},
			wantCode: connect.CodeInvalidArgument,
		},
		{
			name: "a report from an agent komrad does not hold",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error {
				ctx = caller.With(ctx, caller.Caller{AgentID: missingID})
				_, err := svc.RecordReport(ctx, connect.NewRequest(&agentv1.RecordReportRequest{Event: agentv1.ReportEvent_REPORT_EVENT_TURN_ENDED, Hook: "Stop"}))

				return err
			},
			err:      store.ErrNotFound,
			wantCode: connect.CodeNotFound,
		},
		{
			name: "a page token holding an id no row can have",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error {
				_, err := svc.ListAgents(ctx, connect.NewRequest(&agentv1.ListAgentsRequest{
					Pagination: &paginationv1.Pagination{Cursor: xptr.Ptr("eyJpIjoibm90LWFuLWlkIn0=")},
				}))

				return err
			},
			wantCode: connect.CodeInvalidArgument,
		},
		{
			name: "reading an agent nobody holds",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error {
				_, err := svc.GetAgent(ctx, connect.NewRequest(&agentv1.GetAgentRequest{Id: missingID.String()}))

				return err
			},
			wantCode: connect.CodeNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, services := newRecordedService(t)
			services.err = tt.err

			err := tt.call(t.Context(), svc)

			assert.Equal(t, tt.wantCode, connect.CodeOf(err))
			if tt.err == nil {
				assert.Equal(t, &recordingAgentServices{rows: &fakeRowReader{}}, services, "a request the handler refuses reaches no service")
			}
		})
	}
}

// TestLifecycleRequestsReachTheServices pins what each request hands the agent services and what
// it answers with.
func TestLifecycleRequestsReachTheServices(t *testing.T) {
	tests := []struct {
		name       string
		call       func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter, ids []uuid.UUID) (string, error)
		err        error
		wantAnswer string
		check      func(t *testing.T, services *recordingAgentServices, ids []uuid.UUID)
	}{
		{
			name: "a spent idempotency key answers with the agent it launched",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter, _ []uuid.UUID) (string, error) {
				created, err := svc.CreateAgent(ctx, connect.NewRequest(&agentv1.CreateAgentRequest{
					Prompt:         "go",
					WorkflowId:     workflowID.String(),
					IdempotencyKey: xptr.Ptr("triage:ENG-5260"),
				}))
				if err != nil {
					return "", err
				}

				return created.Msg.GetAgent().GetName(), nil
			},
			err:        agenterr.ErrAlreadyLaunched,
			wantAnswer: "spawned",
			check: func(t *testing.T, services *recordingAgentServices, _ []uuid.UUID) {
				assert.Equal(t, "triage:ENG-5260", services.created.IdempotencyKey)
			},
		},
		{
			name: "creating",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter, ids []uuid.UUID) (string, error) {
				created, err := svc.CreateAgent(ctx, connect.NewRequest(&agentv1.CreateAgentRequest{
					Prompt:           "audit the migrations",
					Title:            "audit",
					Flags:            "--model opus",
					ProvisionEnabled: xptr.Ptr(false),
					WorkflowId:       workflowID.String(),
					Harness:          "codex",
					IdempotencyKey:   xptr.Ptr("triage:ENG-5260"),
					Task:             &agentv1.CreateAgentRequest_TaskSlug{TaskSlug: "fix-login"},
					NewTaskSlug:      xptr.Ptr("login"),
					NewTaskGoal:      "Logins work.",
				}))
				if err != nil {
					return "", err
				}

				return created.Msg.GetAgent().GetName(), nil
			},
			wantAnswer: "spawned",
			check: func(t *testing.T, services *recordingAgentServices, ids []uuid.UUID) {
				assert.Equal(t, agents.NewAgentParams{
					Title:            "audit",
					Flags:            "--model opus",
					ProvisionEnabled: xptr.Ptr(false),
					Prompt:           "audit the migrations",
					WorkflowID:       workflowID,
					Harness:          "codex",
					IdempotencyKey:   "triage:ENG-5260",
					Task:             oneofs.Task{TaskSlug: xptr.Ptr("fix-login")},
					TaskSlug:         "login",
					TaskGoal:         "Logins work.",
				}, services.created)
			},
		},
		{
			name: "requesting a review for the calling agent",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter, ids []uuid.UUID) (string, error) {
				ctx = caller.With(ctx, caller.Caller{AgentID: ids[0]})
				requested, err := svc.RequestReview(ctx, connect.NewRequest(&agentv1.RequestReviewRequest{
					Prompt:         "read the parser change",
					Title:          "second-reader",
					Harness:        "codex",
					Flags:          "--sandbox read-only",
					IdempotencyKey: xptr.Ptr("review-1"),
				}))
				if err != nil {
					return "", err
				}

				return requested.Msg.GetReviewer().GetName(), nil
			},
			wantAnswer: "spawned",
			check: func(t *testing.T, services *recordingAgentServices, ids []uuid.UUID) {
				assert.Equal(t, agents.RequestReviewParams{
					AgentID:        ids[0],
					Prompt:         "read the parser change",
					Title:          "second-reader",
					Harness:        "codex",
					Flags:          "--sandbox read-only",
					IdempotencyKey: "review-1",
				}, services.reviewed)
			},
		},
		{
			name: "adopting",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter, _ []uuid.UUID) (string, error) {
				adopted, err := svc.Adopt(ctx, connect.NewRequest(&agentv1.AdoptRequest{
					Ref:            "https://github.com/o/r/pull/1069",
					Prompt:         "pick it up",
					Title:          "logo",
					Flags:          "--model opus",
					Harness:        "claude",
					WorkflowId:     workflowID.String(),
					IdempotencyKey: xptr.Ptr("adopt-1"),
				}))
				if err != nil {
					return "", err
				}

				return adopted.Msg.GetAgent().GetName(), nil
			},
			wantAnswer: "spawned",
			check: func(t *testing.T, services *recordingAgentServices, _ []uuid.UUID) {
				assert.Equal(t, agents.AdoptParams{
					Ref:            "https://github.com/o/r/pull/1069",
					Title:          "logo",
					Flags:          "--model opus",
					Prompt:         "pick it up",
					WorkflowID:     workflowID,
					Harness:        "claude",
					IdempotencyKey: "adopt-1",
				}, services.adopted)
			},
		},
		{
			name: "retrying",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter, ids []uuid.UUID) (string, error) {
				retried, err := svc.RetryAgent(ctx, connect.NewRequest(&agentv1.RetryAgentRequest{
					Id:      ids[0].String(),
					Flags:   "--model opus",
					Prompt:  "try again",
					Harness: "codex",
				}))
				if err != nil {
					return "", err
				}

				return retried.Msg.GetAgent().GetName(), nil
			},
			wantAnswer: "spawned",
			check: func(t *testing.T, services *recordingAgentServices, ids []uuid.UUID) {
				assert.Equal(t, agents.RetryCreateParams{
					ID:      ids[0],
					Flags:   "--model opus",
					Prompt:  "try again",
					Harness: "codex",
				}, services.retried)
			},
		},
		{
			name: "resuming",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter, ids []uuid.UUID) (string, error) {
				resumed, err := svc.ResumeAgent(ctx, connect.NewRequest(&agentv1.ResumeAgentRequest{Id: ids[0].String()}))
				if err != nil {
					return "", err
				}

				return resumed.Msg.GetAgent().GetName(), nil
			},
			wantAnswer: "spawned",
			check: func(t *testing.T, services *recordingAgentServices, ids []uuid.UUID) {
				assert.Equal(t, []agents.ResumeAgentParams{{ID: ids[0]}}, services.resumed)
			},
		},
		{
			name: "resuming from an agent says who asked",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter, ids []uuid.UUID) (string, error) {
				ctx = caller.With(ctx, caller.Caller{AgentID: betaID})
				_, err := svc.ResumeAgent(ctx, connect.NewRequest(&agentv1.ResumeAgentRequest{Id: ids[0].String()}))

				return "", err
			},
			check: func(t *testing.T, services *recordingAgentServices, ids []uuid.UUID) {
				assert.Equal(t, []agents.ResumeAgentParams{{ID: ids[0], ByAgentID: betaID}}, services.resumed)
			},
		},
		{
			name: "restarting",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter, ids []uuid.UUID) (string, error) {
				restarted, err := svc.RestartAgent(ctx, connect.NewRequest(&agentv1.RestartAgentRequest{
					Id:      ids[0].String(),
					Note:    "pick up the review",
					Harness: "codex",
					Flags:   "--model o3",
				}))
				if err != nil {
					return "", err
				}

				return restarted.Msg.GetAgent().GetName(), nil
			},
			wantAnswer: "spawned",
			check: func(t *testing.T, services *recordingAgentServices, ids []uuid.UUID) {
				assert.Equal(t, []agents.RestartAgentParams{{
					ID:      ids[0],
					Note:    "pick up the review",
					Harness: "codex",
					Flags:   "--model o3",
				}}, services.restarted)
			},
		},
		{
			name: "restarting from an agent says who asked",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter, ids []uuid.UUID) (string, error) {
				ctx = caller.With(ctx, caller.Caller{AgentID: betaID})
				_, err := svc.RestartAgent(ctx, connect.NewRequest(&agentv1.RestartAgentRequest{Id: ids[0].String()}))

				return "", err
			},
			check: func(t *testing.T, services *recordingAgentServices, ids []uuid.UUID) {
				assert.Equal(t, []agents.RestartAgentParams{{ID: ids[0], ByAgentID: betaID}}, services.restarted)
			},
		},
		{
			name: "starting a workflow's orchestrator",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter, _ []uuid.UUID) (string, error) {
				started, err := svc.StartOrchestrator(ctx, connect.NewRequest(&agentv1.StartOrchestratorRequest{WorkflowId: workflowID.String()}))
				if err != nil {
					return "", err
				}

				return started.Msg.GetAgent().GetName(), nil
			},
			wantAnswer: "spawned",
			check: func(t *testing.T, services *recordingAgentServices, _ []uuid.UUID) {
				assert.Equal(t, []string{workflowID.String()}, services.orchestrated)
			},
		},
		{
			name: "planning a rebase",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter, ids []uuid.UUID) (string, error) {
				planned, err := svc.PlanRebase(ctx, connect.NewRequest(&agentv1.PlanRebaseRequest{Task: &agentv1.PlanRebaseRequest_TaskId{TaskId: ids[0].String()}}))
				if err != nil {
					return "", err
				}

				return fmt.Sprintf("%s onto %s, %d of %d to replay", planned.Msg.GetBranch(), planned.Msg.GetBaseRef(), planned.Msg.GetReplayCommits(), planned.Msg.GetCommits()), nil
			},
			wantAnswer: "agent/alpha onto origin/main, 2 of 3 to replay",
			check: func(t *testing.T, services *recordingAgentServices, ids []uuid.UUID) {
				assert.Equal(t, []oneofs.Task{{TaskId: xptr.Ptr(ids[0])}}, services.planned)
			},
		},
		{
			name: "rebasing",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter, ids []uuid.UUID) (string, error) {
				rebased, err := svc.RebaseWorktree(ctx, connect.NewRequest(&agentv1.RebaseWorktreeRequest{Task: &agentv1.RebaseWorktreeRequest_TaskId{TaskId: ids[0].String()}}))
				if err != nil {
					return "", err
				}

				return fmt.Sprintf("%s onto %s, conflicted %t", rebased.Msg.GetBranch(), rebased.Msg.GetBaseRef(), rebased.Msg.GetConflicted()), nil
			},
			wantAnswer: "agent/alpha onto origin/main, conflicted true",
			check: func(t *testing.T, services *recordingAgentServices, ids []uuid.UUID) {
				assert.Equal(t, []oneofs.Task{{TaskId: xptr.Ptr(ids[0])}}, services.rebased)
			},
		},
		{
			name: "planning a push",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter, ids []uuid.UUID) (string, error) {
				planned, err := svc.PlanPush(ctx, connect.NewRequest(&agentv1.PlanPushRequest{Task: &agentv1.PlanPushRequest_TaskId{TaskId: ids[0].String()}}))
				if err != nil {
					return "", err
				}

				return fmt.Sprintf("%s, %d up, forced %t, %d left behind", planned.Msg.GetBranch(), planned.Msg.GetCommits(), planned.Msg.GetForced(), planned.Msg.GetUncommittedFiles()), nil
			},
			wantAnswer: "agent/alpha, 2 up, forced true, 1 left behind",
			check: func(t *testing.T, services *recordingAgentServices, ids []uuid.UUID) {
				assert.Equal(t, []oneofs.Task{{TaskId: xptr.Ptr(ids[0])}}, services.pushPlanned)
			},
		},
		{
			name: "pushing",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter, ids []uuid.UUID) (string, error) {
				pushed, err := svc.PushWorktree(ctx, connect.NewRequest(&agentv1.PushWorktreeRequest{Task: &agentv1.PushWorktreeRequest_TaskId{TaskId: ids[0].String()}}))
				if err != nil {
					return "", err
				}

				return fmt.Sprintf("%s at %s, forced %t", pushed.Msg.GetBranch(), pushed.Msg.GetSha(), pushed.Msg.GetForced()), nil
			},
			wantAnswer: "agent/alpha at pushedsha, forced true",
			check: func(t *testing.T, services *recordingAgentServices, ids []uuid.UUID) {
				assert.Equal(t, []oneofs.Task{{TaskId: xptr.Ptr(ids[0])}}, services.pushed)
			},
		},
		{
			name: "aborting",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter, ids []uuid.UUID) (string, error) {
				aborted, err := svc.AbortSequence(ctx, connect.NewRequest(&agentv1.AbortSequenceRequest{Task: &agentv1.AbortSequenceRequest_TaskId{TaskId: ids[0].String()}}))
				if err != nil {
					return "", err
				}

				return fmt.Sprintf("%s on %s, parked at %s", aborted.Msg.GetSequence(), aborted.Msg.GetBranch(), aborted.Msg.GetParkedRef()), nil
			},
			wantAnswer: "SEQUENCE_CHERRY_PICK on agent/alpha, parked at refs/komrad/aborted/alpha/1",
			check: func(t *testing.T, services *recordingAgentServices, ids []uuid.UUID) {
				assert.Equal(t, []oneofs.Task{{TaskId: xptr.Ptr(ids[0])}}, services.aborted)
			},
		},
		{
			name: "archiving from the panel takes the whole batch",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter, ids []uuid.UUID) (string, error) {
				_, err := svc.ArchiveAgents(ctx, connect.NewRequest(&agentv1.ArchiveAgentsRequest{Ids: xslice.Map(ids, uuid.UUID.String)}))

				return "", err
			},
			check: func(t *testing.T, services *recordingAgentServices, ids []uuid.UUID) {
				assert.Equal(t, []agents.ArchiveAgentsParams{{IDs: ids}}, services.archived)
			},
		},
		{
			name: "archiving from an agent says who asked",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter, ids []uuid.UUID) (string, error) {
				ctx = caller.With(ctx, caller.Caller{AgentID: betaID})
				_, err := svc.ArchiveAgents(ctx, connect.NewRequest(&agentv1.ArchiveAgentsRequest{Ids: xslice.Map(ids, uuid.UUID.String)}))

				return "", err
			},
			check: func(t *testing.T, services *recordingAgentServices, ids []uuid.UUID) {
				assert.Equal(t, []agents.ArchiveAgentsParams{{IDs: ids, ByAgentID: betaID}}, services.archived)
			},
		},
		{
			name: "restoring answers with every agent restored",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter, ids []uuid.UUID) (string, error) {
				restored, err := svc.RestoreAgents(ctx, connect.NewRequest(&agentv1.RestoreAgentsRequest{Ids: xslice.Map(ids, uuid.UUID.String)}))
				if err != nil {
					return "", err
				}

				names := xslice.Map(restored.Msg.GetAgents(), func(agent *agentv1.Agent) string {
					return agent.GetName()
				})
				slices.Sort(names)

				return strings.Join(names, " "), nil
			},
			wantAnswer: "alpha beta",
			check: func(t *testing.T, services *recordingAgentServices, ids []uuid.UUID) {
				assert.Equal(t, ids, services.restored)
			},
		},
		{
			name: "renaming",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter, ids []uuid.UUID) (string, error) {
				_, err := svc.UpdateAgent(ctx, connect.NewRequest(&agentv1.UpdateAgentRequest{
					Id:   ids[0].String(),
					Name: "renamed",
				}))

				return "", err
			},
			check: func(t *testing.T, services *recordingAgentServices, ids []uuid.UUID) {
				assert.Equal(t, map[uuid.UUID]string{ids[0]: "renamed"}, services.renamed)
			},
		},
		{
			name: "claiming the failed runs",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter, ids []uuid.UUID) (string, error) {
				_, err := svc.MarkRunsRead(ctx, connect.NewRequest(&agentv1.MarkRunsReadRequest{Id: ids[0].String()}))

				return "", err
			},
			check: func(t *testing.T, services *recordingAgentServices, ids []uuid.UUID) {
				assert.Equal(t, ids[:1], services.runsRead)
			},
		},
		{
			name: "a report from the calling agent's own CLI",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter, ids []uuid.UUID) (string, error) {
				ctx = caller.With(ctx, caller.Caller{AgentID: ids[0]})
				_, err := svc.RecordReport(ctx, connect.NewRequest(&agentv1.RecordReportRequest{
					Event:     agentv1.ReportEvent_REPORT_EVENT_TURN_FAILED,
					Hook:      "StopFailure",
					TurnError: "Overloaded",
				}))

				return "", err
			},
			check: func(t *testing.T, services *recordingAgentServices, ids []uuid.UUID) {
				assert.Equal(t, agents.RecordReportParams{
					AgentID: ids[0],
					Report:  agents.Report{Event: agents.ReportTurnFailed, TurnError: "Overloaded"},
				}, services.reported)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, services := newRecordedService(t)
			services.err = tt.err
			ids := []uuid.UUID{
				services.rows.seed(agentstore.Agent{Name: "alpha"}).ID,
				services.rows.seed(agentstore.Agent{Name: "beta"}).ID,
			}

			answer, err := tt.call(t.Context(), svc, ids)
			require.NoError(t, err)

			assert.Equal(t, tt.wantAnswer, answer)
			tt.check(t, services, ids)
		})
	}
}

// TestRecordReportLogsTheHookCall pins the line a hook call leaves only once komrad recorded it.
func TestRecordReportLogsTheHookCall(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{
			name: "a report from an agent komrad does not hold",
			err:  store.ErrNotFound,
		},
		{
			name: "a report the agent's row took",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, services := newRecordedService(t)
			services.err = tt.err
			id := services.rows.seed(agentstore.Agent{Name: "alpha"}).ID

			var written strings.Builder
			ctx := logging.WithLogger(caller.With(t.Context(), caller.Caller{AgentID: id}), logging.New(&written, slog.LevelInfo))

			_, err := svc.RecordReport(ctx, connect.NewRequest(&agentv1.RecordReportRequest{
				Event: agentv1.ReportEvent_REPORT_EVENT_TOOL_STARTED,
				Hook:  "PreToolUse",
				Tool:  "Bash",
			}))
			if tt.err != nil {
				require.Error(t, err)
				assert.Empty(t, written.String())

				return
			}

			require.NoError(t, err)

			record := logtest.Record(t, written.String())
			assert.Equal(t, id.String(), record[logging.TargetAgentID])
			assert.Equal(t, "PreToolUse", record[logging.HookName])
			assert.Equal(t, "tool started", record[logging.HookEvent])
			assert.Equal(t, "Bash", record[logging.HookTool])
		})
	}
}

// TestAdoptAnswersWithTheAgentAlreadyOnTheBranch covers the service's existing flag reaching the response.
func TestAdoptAnswersWithTheAgentAlreadyOnTheBranch(t *testing.T) {
	svc, services := newRecordedService(t)
	services.adoptExisting = true

	adopted, err := svc.Adopt(t.Context(), connect.NewRequest(&agentv1.AdoptRequest{
		Ref:        "someones/work",
		WorkflowId: workflowID.String(),
	}))
	require.NoError(t, err)

	assert.True(t, adopted.Msg.GetExisting())
	assert.Equal(t, "spawned", adopted.Msg.GetAgent().GetName())
}

// TestListAgents pins what a listed row carries from the surroundings beside it, and what the
// request's view and archive flag change.
func TestListAgents(t *testing.T) {
	prompt := strings.Repeat("a", render.SummaryHead+50)
	archivedAt := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name  string
		req   *agentv1.ListAgentsRequest
		check func(t *testing.T, rows map[string]*agentv1.AgentRow, pages []rowstore.Page)
	}{
		{
			name: "the default listing asks for the live agents and cuts their prompts to the head",
			req:  &agentv1.ListAgentsRequest{},
			check: func(t *testing.T, rows map[string]*agentv1.AgentRow, pages []rowstore.Page) {
				require.Len(t, pages, 1)
				assert.False(t, pages[0].Archived)
				alpha := rows["alpha"].GetAgent()
				assert.Equal(t, prompt[:render.SummaryHead], alpha.GetPrompt())
				assert.True(t, alpha.GetPromptTruncated())
				assert.True(t, rows["alpha"].GetUnreadMail(), "the mail sent to it is read from the surroundings")
				assert.True(t, alpha.GetFailedRun())
				assert.Nil(t, alpha.ArchivedAt)
			},
		},
		{
			name: "the full view keeps the whole prompt",
			req:  &agentv1.ListAgentsRequest{View: viewv1.View_VIEW_FULL},
			check: func(t *testing.T, rows map[string]*agentv1.AgentRow, _ []rowstore.Page) {
				assert.Equal(t, prompt, rows["alpha"].GetAgent().GetPrompt())
				assert.False(t, rows["alpha"].GetAgent().GetPromptTruncated())
			},
		},
		{
			name: "asking for the archived agents asks for those and says when each was archived",
			req:  &agentv1.ListAgentsRequest{Archived: true},
			check: func(t *testing.T, rows map[string]*agentv1.AgentRow, pages []rowstore.Page) {
				require.Len(t, pages, 1)
				assert.True(t, pages[0].Archived)
				assert.Equal(t, time.Date(2026, time.September, 20, 12, 0, 0, 0, time.UTC), rows["archived"].GetAgent().GetArchivedAt().AsTime())
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rows := &fakeRowReader{
				rows: []rowstore.Row{
					{
						Agent: agentstore.Agent{
							ID:     alphaID,
							Name:   "alpha",
							Prompt: prompt,
						},
						FailedRun: true,
					},
					{
						Agent: agentstore.Agent{
							ID:         uuid.New(),
							Name:       "archived",
							ArchivedAt: &archivedAt,
						},
					},
				},
				surroundings: agents.Surroundings{Mail: []mailstore.Mail{
					{
						ID:        mailID,
						FromName:  "beta",
						ToAgentID: alphaID,
						Body:      "refunded_cents",
						ExpiresAt: time.Now().Add(time.Hour),
					},
				}},
			}

			listed, err := newRowHandler(rows).ListAgents(t.Context(), connect.NewRequest(tt.req))
			require.NoError(t, err)

			tt.check(t, xslice.KeyBy(listed.Msg.GetAgents(), func(row *agentv1.AgentRow) string {
				return row.GetAgent().GetName()
			}), rows.pages)
		})
	}
}

// TestListAgentsRefusalsMapToCodes pins the code a read the listing depends on reaches the caller as.
func TestListAgentsRefusalsMapToCodes(t *testing.T) {
	tests := []struct {
		name     string
		call     func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error
		wantCode connect.Code
	}{
		{
			name: "a listing that cannot be read",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error {
				_, err := svc.ListAgents(ctx, connect.NewRequest(&agentv1.ListAgentsRequest{}))

				return err
			},
			wantCode: connect.CodeInternal,
		},
		{
			name: "an agent whose surroundings cannot be read",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error {
				_, err := svc.GetAgent(ctx, connect.NewRequest(&agentv1.GetAgentRequest{Id: alphaID.String()}))

				return err
			},
			wantCode: connect.CodeInternal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rows := &fakeRowReader{err: errors.New("connection refused")}
			rows.seed(agentstore.Agent{
				ID:   alphaID,
				Name: "alpha",
			})

			assert.Equal(t, tt.wantCode, connect.CodeOf(tt.call(t.Context(), newRowHandler(rows))))
		})
	}
}

// TestGetAgent pins that one agent is read whole and drawn beside its surroundings, its liveness
// written even when false.
func TestGetAgent(t *testing.T) {
	prompt := strings.Repeat("a", render.SummaryHead+50)
	created := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	rows := &fakeRowReader{
		rows: []rowstore.Row{
			{
				Agent: agentstore.Agent{
					ID:         alphaID,
					Name:       "alpha",
					Cwd:        "/repo",
					Prompt:     prompt,
					State:      agentv1.AgentState_AGENT_STATE_THINKING,
					CreatedAt:  created,
					WorkflowID: workflowID,
					TaskID:     xptr.Ptr(taskID),
				},
				FailedRun:    true,
				FailedRunLog: xptr.Ptr(int32(4)),
			},
		},
		surroundings: agents.Surroundings{
			Mail: []mailstore.Mail{
				{
					ID:        mailID,
					ToAgentID: alphaID,
					ExpiresAt: time.Now().Add(time.Hour),
				},
			},
			PullRequests: []pullrequeststore.PullRequest{
				{
					ID:                uuid.New(),
					Number:            740,
					RepositoryURL:     komradRepository,
					HeadRepositoryURL: komradRepository,
					HeadBranch:        "agent/x",
					State:             "open",
					OpenedAt:          created,
				},
			},
			Repositories: []pullrequeststore.Sync{{RepositoryURL: komradRepository}},
			Reviews: []reviewstore.Review{
				{
					ID:              reviewID,
					TaskID:          taskID,
					SnapshotID:      uuid.New(),
					SnapshotOrdinal: 1,
					SnapshotSHA:     "snapshot-1-sha",
					ReleasedAt:      xptr.Ptr(created),
				},
			},
			Workflows: []workflowstore.Workflow{{ID: workflowID}},
			Tasks: []taskstore.Task{{
				ID: taskID,
				Checkout: taskstore.Checkout{
					RepositoryURL: xptr.Ptr(komradRepository),
					Branch:        xptr.Ptr("agent/x"),
				},
			}},
		},
	}

	got, err := newRowHandler(rows).GetAgent(t.Context(), connect.NewRequest(&agentv1.GetAgentRequest{Id: alphaID.String()}))
	require.NoError(t, err)

	assert.True(t, got.Msg.GetUnreadMail(), "the agent is drawn beside its surroundings")
	require.Len(t, got.Msg.GetPullRequests(), 1)
	assert.Equal(t, int32(740), got.Msg.GetPullRequests()[0].GetNumber(), "with every pull request on its branch")
	assert.Equal(t, int32(740), got.Msg.GetPullRequest().GetNumber(), "and the current one's standing")
	assert.Equal(t, int32(1), got.Msg.GetLocalReviewSnapshots())
	assert.Equal(t, "agent/x", got.Msg.GetTask().GetBranch(), "and the task whose checkout it works in")

	agent := got.Msg.GetAgent()
	assert.Equal(t, "/repo", agent.GetCwd())
	assert.Equal(t, agentv1.AgentState_AGENT_STATE_THINKING, agent.GetState())
	assert.Equal(t, prompt, agent.GetPrompt(), "no view narrows a single agent")
	assert.Equal(t, int32(4), agent.GetFailedRunLog())
	require.NotNil(t, agent.Alive, "a dormant agent says so rather than leaving it out")
	assert.False(t, agent.GetAlive())
	assert.Equal(t, time.Date(2026, time.September, 20, 12, 0, 0, 0, time.UTC), agent.GetCreatedAt().AsTime())
}

// TestGetAgentResponseMirrorsAgentRow pins that every field AgentRow draws beside an agent is
// declared on GetAgentResponse the same way, which adds only the task.
func TestGetAgentResponseMirrorsAgentRow(t *testing.T) {
	t.Parallel()

	rowFields := (&agentv1.AgentRow{}).ProtoReflect().Descriptor().Fields()
	responseFields := (&agentv1.GetAgentResponse{}).ProtoReflect().Descriptor().Fields()

	assert.Equal(t, rowFields.Len()+1, responseFields.Len())
	assert.NotNil(t, responseFields.ByName("task"), "GetAgentResponse carries the task")

	for i := range rowFields.Len() {
		want := rowFields.Get(i)

		got := responseFields.ByName(want.Name())
		if !assert.NotNil(t, got, "GetAgentResponse declares no field %q", want.Name()) {
			continue
		}

		assert.Equal(t, want.Number(), got.Number(), "%q is numbered differently", want.Name())
		assert.Equal(t, want.Kind(), got.Kind(), "%q is of a different kind", want.Name())
		assert.Equal(t, want.Cardinality(), got.Cardinality(), "%q has a different cardinality", want.Name())
		assert.Equal(t, want.HasPresence(), got.HasPresence(), "%q differs on presence", want.Name())

		if want.Message() != nil {
			assert.Equal(t, want.Message().FullName(), got.Message().FullName(), "%q is of a different message", want.Name())
		}

		if want.Enum() != nil {
			assert.Equal(t, want.Enum().FullName(), got.Enum().FullName(), "%q is of a different enum", want.Name())
		}
	}
}

// TestLog covers the channel a running script uses to say what it did, and the refusal of an
// id nobody holds.
func TestLog(t *testing.T) {
	tests := []struct {
		name     string
		seed     bool
		explicit bool
		callerIs bool
		script   string
		wantCode connect.Code
	}{
		{
			name:     "an id nobody holds is not a quiet success",
			explicit: true,
			wantCode: connect.CodeNotFound,
		},
		{
			name:     "an unset id with no caller identity is refused",
			seed:     true,
			wantCode: connect.CodeInvalidArgument,
		},
		{
			name:     "an explicit id is used as given",
			seed:     true,
			explicit: true,
		},
		{
			name:     "an unset id falls back to the caller's own",
			seed:     true,
			callerIs: true,
		},
		{
			name:     "a script names itself on the line",
			seed:     true,
			explicit: true,
			script:   "create-worktree",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rows := &fakeRowReader{}
			svc := newRowHandler(rows)

			id := uuid.New()
			if tt.seed {
				id = rows.seed(agentstore.Agent{
					Name:  "alpha",
					Cwd:   "/repo",
					State: agentv1.AgentState_AGENT_STATE_THINKING,
				}).ID
			}

			ctx := t.Context()
			if tt.callerIs {
				ctx = caller.With(ctx, caller.Caller{AgentID: id})
			}

			var written strings.Builder
			ctx = logging.WithLogger(ctx, logging.New(&written, slog.LevelInfo))

			req := &agentv1.LogRequest{Message: "fetched origin/main"}
			if tt.script != "" {
				req.Script = xptr.Ptr(tt.script)
			}
			if tt.explicit {
				req.Id = xptr.Ptr(id.String())
			}

			_, err := svc.Log(ctx, connect.NewRequest(req))
			if tt.wantCode != 0 {
				assert.Equal(t, tt.wantCode, connect.CodeOf(err))
				return
			}

			require.NoError(t, err)

			record := logtest.Record(t, written.String())
			assert.Equal(t, "fetched origin/main", record["body"])
			assert.Equal(t, id.String(), record[logging.TargetAgentID])
			assert.Equal(t, "/repo", record[logging.ScriptAgentCwd])
			if tt.script == "" {
				assert.NotContains(t, record, logging.ScriptName)

				return
			}

			assert.Equal(t, tt.script, record[logging.ScriptName])
		})
	}
}

// recordingAgentServices stands in for every agent service the handler calls, recording each request.
type recordingAgentServices struct {
	rows          *fakeRowReader
	err           error
	spawnedAgent  *agentstore.Agent
	created       agents.NewAgentParams
	reviewed      agents.RequestReviewParams
	adopted       agents.AdoptParams
	adoptExisting bool
	retried       agents.RetryCreateParams
	reported      agents.RecordReportParams
	resumed       []agents.ResumeAgentParams
	restarted     []agents.RestartAgentParams
	orchestrated  []string
	planned       []oneofs.Task
	rebased       []oneofs.Task
	pushPlanned   []oneofs.Task
	pushed        []oneofs.Task
	aborted       []oneofs.Task
	archived      []agents.ArchiveAgentsParams
	restored      []uuid.UUID
	runsRead      []uuid.UUID
	renamed       map[uuid.UUID]string
}

// spawn seeds the agent the fake answers with among the rows.
func (f *recordingAgentServices) spawn() *agentstore.Agent {
	if f.spawnedAgent == nil {
		spawned := f.rows.seed(agentstore.Agent{
			Name:  "spawned",
			Cwd:   "/repo",
			State: agentv1.AgentState_AGENT_STATE_IDLE,
		})
		f.spawnedAgent = &spawned
	}

	return f.spawnedAgent
}

// answer returns the spawned agent, or err in its place unless err is a spent idempotency key.
func (f *recordingAgentServices) answer() (*agentstore.Agent, error) {
	if f.err != nil && !errors.Is(f.err, agenterr.ErrAlreadyLaunched) {
		return nil, f.err
	}

	return f.spawn(), f.err
}

func (f *recordingAgentServices) NewAgent(_ context.Context, params agents.NewAgentParams) (*agentstore.Agent, error) {
	f.created = params

	return f.answer()
}

func (f *recordingAgentServices) RequestReview(_ context.Context, params agents.RequestReviewParams) (*agentstore.Agent, string, error) {
	f.reviewed = params
	reviewer, err := f.answer()

	return reviewer, "", err
}

func (f *recordingAgentServices) RetryCreate(_ context.Context, params agents.RetryCreateParams) (*agentstore.Agent, error) {
	f.retried = params

	return f.answer()
}

func (f *recordingAgentServices) Adopt(_ context.Context, params agents.AdoptParams) (*agents.Adoption, error) {
	f.adopted = params
	agent, err := f.answer()
	if agent == nil {
		return nil, err
	}

	return &agents.Adoption{Agent: agent, Existing: f.adoptExisting}, err
}

func (f *recordingAgentServices) ResumeAgent(_ context.Context, params agents.ResumeAgentParams) (*agentstore.Agent, error) {
	f.resumed = append(f.resumed, params)

	return f.answer()
}

func (f *recordingAgentServices) RestartAgent(_ context.Context, params agents.RestartAgentParams) (*agentstore.Agent, error) {
	f.restarted = append(f.restarted, params)

	return f.answer()
}

func (f *recordingAgentServices) Start(_ context.Context, workflowID uuid.UUID, _ string) (*agentstore.Agent, error) {
	f.orchestrated = append(f.orchestrated, workflowID.String())

	return f.answer()
}

func (f *recordingAgentServices) PlanRebase(_ context.Context, params checkouts.RebaseParams) (*checkouts.RebasePlan, error) {
	f.planned = append(f.planned, params.Task)
	if f.err != nil {
		return nil, f.err
	}

	return &checkouts.RebasePlan{
		BaseRef: "origin/main",
		Branch:  "agent/alpha",
		Commits: 3,
		Replay:  2,
	}, nil
}

func (f *recordingAgentServices) Rebase(_ context.Context, params checkouts.RebaseParams) (*checkouts.RebaseResult, error) {
	f.rebased = append(f.rebased, params.Task)
	if f.err != nil {
		return nil, f.err
	}

	return &checkouts.RebaseResult{
		RebasePlan: checkouts.RebasePlan{
			BaseRef: "origin/main",
			Branch:  "agent/alpha",
		},
		Conflicted: true,
	}, nil
}

func (f *recordingAgentServices) PlanPush(_ context.Context, task oneofs.Task) (*checkouts.PushPlan, error) {
	f.pushPlanned = append(f.pushPlanned, task)
	if f.err != nil {
		return nil, f.err
	}

	return &checkouts.PushPlan{
		Branch:      "agent/alpha",
		Commits:     2,
		Forced:      true,
		Uncommitted: 1,
	}, nil
}

func (f *recordingAgentServices) Push(_ context.Context, task oneofs.Task) (*checkouts.PushResult, error) {
	f.pushed = append(f.pushed, task)
	if f.err != nil {
		return nil, f.err
	}

	return &checkouts.PushResult{
		PushPlan: checkouts.PushPlan{
			Branch:  "agent/alpha",
			Commits: 2,
			Forced:  true,
		},
		SHA: "pushedsha",
	}, nil
}

func (f *recordingAgentServices) ArchiveAgents(_ context.Context, params agents.ArchiveAgentsParams) error {
	f.archived = append(f.archived, params)

	return f.err
}

// RestoreAgents answers with an agent for every id.
func (f *recordingAgentServices) RestoreAgents(_ context.Context, ids []uuid.UUID) ([]agentstore.Agent, error) {
	f.restored = append(f.restored, ids...)
	if f.err != nil {
		return nil, f.err
	}

	return xslice.Map(ids, func(id uuid.UUID) agentstore.Agent {
		return agentstore.Agent{ID: id}
	}), nil
}

func (f *recordingAgentServices) Rename(_ context.Context, id uuid.UUID, name string) error {
	if f.renamed == nil {
		f.renamed = map[uuid.UUID]string{}
	}
	f.renamed[id] = name

	return f.err
}

func (f *recordingAgentServices) MarkRunsRead(_ context.Context, id uuid.UUID) error {
	f.runsRead = append(f.runsRead, id)

	return f.err
}

func (f *recordingAgentServices) RecordReport(_ context.Context, params agents.RecordReportParams) error {
	f.reported = params

	return f.err
}

func adoptMain(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error {
	_, err := svc.Adopt(ctx, connect.NewRequest(&agentv1.AdoptRequest{
		Ref:        "main",
		WorkflowId: workflowID.String(),
	}))

	return err
}

func restoreAlpha(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error {
	_, err := svc.RestoreAgents(ctx, connect.NewRequest(&agentv1.RestoreAgentsRequest{Ids: []string{alphaID.String()}}))

	return err
}

// newRowHandler is the handler over a fake of the rows.
func newRowHandler(rows *fakeRowReader) agentv1komrad.AgentServiceAdapter {
	return agentv1komrad.AgentServiceAdapter{Impl: NewHandler(NewHandlerParams{
		Agents: rows,
		Rows:   rows,
	})}
}

// newRecordedService is the handler over fakes of every agent service, recording what each one received.
func newRecordedService(t *testing.T) (agentv1komrad.AgentServiceAdapter, *recordingAgentServices) {
	t.Helper()

	services := &recordingAgentServices{rows: &fakeRowReader{}}

	return agentv1komrad.AgentServiceAdapter{Impl: NewHandler(NewHandlerParams{
		Agents:       services.rows,
		Rows:         services.rows,
		Writer:       services,
		Transcripts:  &fakeTranscriptReader{},
		Mail:         &fakeMailer{},
		Creator:      services,
		Adopter:      services,
		Lifecycle:    services,
		Resumer:      services,
		Orchestrator: services,
		Rebaser:      services,
		Pusher:       services,
		Aborter:      services,
	})}, services
}

// TestCreateAgentValidation pins which create requests the schema accepts and which it refuses
// before a handler ever sees them.
func TestCreateAgentValidation(t *testing.T) {
	tests := []struct {
		name    string
		req     *agentv1.CreateAgentRequest
		wantErr bool
	}{
		{
			name: "a prompt and a workflow are enough",
			req:  &agentv1.CreateAgentRequest{Prompt: "go", WorkflowId: workflowID.String()},
		},
		{
			name:    "no prompt",
			req:     &agentv1.CreateAgentRequest{WorkflowId: workflowID.String()},
			wantErr: true,
		},
		{
			name:    "no workflow, which every agent needs",
			req:     &agentv1.CreateAgentRequest{Prompt: "go"},
			wantErr: true,
		},
		{
			name: "flags carrying a shell metacharacter are refused rather than quoted and run",
			req: &agentv1.CreateAgentRequest{
				Prompt:     "go",
				WorkflowId: workflowID.String(),
				Flags:      "--model opus; rm -rf /",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, services := validatedClient(t)

			_, err := client.CreateAgent(t.Context(), connect.NewRequest(tt.req))
			if tt.wantErr {
				assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
				assert.Empty(t, services.created.Prompt, "a rejected request should never reach the services")

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.req.GetPrompt(), services.created.Prompt)
		})
	}
}

// TestRequestValidation covers the proto constraints on the agent requests.
func TestRequestValidation(t *testing.T) {
	tests := []struct {
		name    string
		call    func(t *testing.T, c agentv1connect.AgentServiceClient) error
		wantErr bool
	}{
		{
			name: "a rename to a name with a space is refused",
			call: func(t *testing.T, c agentv1connect.AgentServiceClient) error {
				_, err := c.UpdateAgent(t.Context(), connect.NewRequest(&agentv1.UpdateAgentRequest{Id: "id-1", Name: "my agent"}))

				return err
			},
			wantErr: true,
		},
		{
			name: "so is one carrying a colon, which tmux reads as a target separator",
			call: func(t *testing.T, c agentv1connect.AgentServiceClient) error {
				_, err := c.UpdateAgent(t.Context(), connect.NewRequest(&agentv1.UpdateAgentRequest{Id: "id-1", Name: "a:b"}))

				return err
			},
			wantErr: true,
		},
		{
			name: "a rename needs an id to key on",
			call: func(t *testing.T, c agentv1connect.AgentServiceClient) error {
				_, err := c.UpdateAgent(t.Context(), connect.NewRequest(&agentv1.UpdateAgentRequest{Id: "", Name: "fine"}))

				return err
			},
			wantErr: true,
		},
		{
			name: "a get with no id is refused",
			call: func(t *testing.T, c agentv1connect.AgentServiceClient) error {
				_, err := c.GetAgent(t.Context(), connect.NewRequest(&agentv1.GetAgentRequest{Id: ""}))

				return err
			},
			wantErr: true,
		},
		{
			name: "an archive naming the same agent twice is refused, since it says nothing twice",
			call: func(t *testing.T, c agentv1connect.AgentServiceClient) error {
				id := uuid.NewString()
				_, err := c.ArchiveAgents(t.Context(), connect.NewRequest(&agentv1.ArchiveAgentsRequest{
					Ids: []string{id, id},
				}))

				return err
			},
			wantErr: true,
		},
		{
			name: "a restore of however many agents is taken",
			call: func(t *testing.T, c agentv1connect.AgentServiceClient) error {
				ids := make([]string, 0, 500)
				for range 500 {
					ids = append(ids, uuid.NewString())
				}

				_, err := c.RestoreAgents(t.Context(), connect.NewRequest(&agentv1.RestoreAgentsRequest{Ids: ids}))

				return err
			},
		},
		{
			name: "a log script carrying a control character is refused",
			call: func(t *testing.T, c agentv1connect.AgentServiceClient) error {
				script := "run\x1b[31mred"
				_, err := c.Log(t.Context(), connect.NewRequest(&agentv1.LogRequest{
					Id:      xptr.Ptr(uuid.NewString()),
					Message: "starting",
					Script:  &script,
				}))

				return err
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, _ := validatedClient(t)

			err := tt.call(t, client)
			if tt.wantErr {
				assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
				return
			}

			require.NoError(t, err)
		})
	}
}

func validatedClient(t *testing.T) (agentv1connect.AgentServiceClient, *recordingAgentServices) {
	t.Helper()

	svc, services := newRecordedService(t)

	validation, err := interceptors.NewValidation()
	require.NoError(t, err)

	mux := http.NewServeMux()
	mux.Handle(agentv1connect.NewAgentServiceHandler(svc, connect.WithInterceptors(validation)))

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	return agentv1connect.NewAgentServiceClient(server.Client(), server.URL), services
}

func (f *recordingAgentServices) Abort(_ context.Context, task oneofs.Task) (*checkouts.AbortResult, error) {
	f.aborted = append(f.aborted, task)
	if f.err != nil {
		return nil, f.err
	}

	return &checkouts.AbortResult{
		Sequence:  taskv1.Sequence_SEQUENCE_CHERRY_PICK,
		Branch:    "agent/alpha",
		ParkedRef: "refs/komrad/aborted/alpha/1",
	}, nil
}

func (f *recordingAgentServices) PickForkPoint(_ context.Context, params checkouts.PickForkPointParams) (string, error) {
	if f.err != nil {
		return "", f.err
	}

	return params.Commit, nil
}

var (
	taskID = uuid.New()
)
