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

	agentv1komrad "komrad/proto/gen/agent/v1/agentv1komrad"

	"komrad/internal/agents"
	"komrad/internal/caller"
	"komrad/internal/checkouts"
	"komrad/internal/interceptors"
	"komrad/internal/logging"
	"komrad/internal/logtest"
	"komrad/internal/rpc/render"
	"komrad/internal/store"
	"komrad/lib/xptr"
	"komrad/lib/xslice"
	agentv1 "komrad/proto/gen/agent/v1"
	"komrad/proto/gen/agent/v1/agentv1connect"
	paginationv1 "komrad/proto/gen/pagination/v1"
	viewv1 "komrad/proto/gen/view/v1"

	agentstore "komrad/internal/store/agent"
	mailstore "komrad/internal/store/mail"
	pullrequeststore "komrad/internal/store/pullrequest"
	reviewstore "komrad/internal/store/review"
	rowstore "komrad/internal/store/row"
	workflowstore "komrad/internal/store/workflow"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const missingID = "00000000-0000-0000-0000-000000000000"

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
	if agent.ID == "" {
		agent.ID = uuid.NewString()
	}
	f.rows = append(f.rows, rowstore.Row{Agent: agent})

	return agent
}

// GetAgent answers the seeded agent with the id, or not found.
func (f *fakeRowReader) GetAgent(_ context.Context, id string) (*agentstore.Agent, error) {
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
func (f *fakeRowReader) ListRowsByID(_ context.Context, ids []string) ([]rowstore.Row, error) {
	return xslice.Filter(f.rows, func(row rowstore.Row) bool {
		return slices.Contains(ids, row.ID)
	}), nil
}

// GetRow answers the seeded row with the id, or not found.
func (f *fakeRowReader) GetRow(_ context.Context, id string) (*rowstore.Row, error) {
	return f.find(id)
}

// ListSurroundings answers the surroundings unless err refuses it.
func (f *fakeRowReader) ListSurroundings(context.Context, []rowstore.Row) (*agents.Surroundings, error) {
	if f.err != nil {
		return nil, f.err
	}

	return &f.surroundings, nil
}

func (f *fakeRowReader) find(id string) (*rowstore.Row, error) {
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
				_, err := svc.CreateAgent(ctx, connect.NewRequest(&agentv1.CreateAgentRequest{Prompt: "go"}))

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
			name: "a second reviewer",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error {
				_, err := svc.RequestReview(ctx, connect.NewRequest(&agentv1.RequestReviewRequest{
					Prompt:  "read it",
					AgentId: xptr.Ptr(alphaID),
				}))

				return err
			},
			err:      agents.ErrReviewerAlreadyRunning,
			wantCode: connect.CodeAlreadyExists,
		},
		{
			name: "a retry of an agent that did not fail",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error {
				_, err := svc.RetryAgent(ctx, connect.NewRequest(&agentv1.RetryAgentRequest{Id: alphaID}))

				return err
			},
			err:      agents.ErrAgentNotFailed,
			wantCode: connect.CodeFailedPrecondition,
		},
		{
			name: "a resume of an archived agent",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error {
				_, err := svc.ResumeAgent(ctx, connect.NewRequest(&agentv1.ResumeAgentRequest{Id: alphaID}))

				return err
			},
			err:      agents.ErrAgentArchived,
			wantCode: connect.CodeFailedPrecondition,
		},
		{
			name: "an agent holding itself",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error {
				ctx = caller.With(ctx, caller.Caller{AgentID: alphaID})
				_, err := svc.HoldAgent(ctx, connect.NewRequest(&agentv1.HoldAgentRequest{Id: alphaID}))

				return err
			},
			wantCode: connect.CodeInvalidArgument,
		},
		{
			name: "a hold on an agent that will not stop",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error {
				_, err := svc.HoldAgent(ctx, connect.NewRequest(&agentv1.HoldAgentRequest{Id: alphaID}))

				return err
			},
			err:      agents.ErrUnsettled,
			wantCode: connect.CodeFailedPrecondition,
		},
		{
			name: "a release of an agent nobody holds",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error {
				_, err := svc.ReleaseAgent(ctx, connect.NewRequest(&agentv1.ReleaseAgentRequest{Id: alphaID}))

				return err
			},
			err:      agents.ErrNotHeld,
			wantCode: connect.CodeFailedPrecondition,
		},
		{
			name: "a delete naming an agent nobody holds",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error {
				_, err := svc.DeleteAgents(ctx, connect.NewRequest(&agentv1.DeleteAgentsRequest{Ids: []string{alphaID}}))

				return err
			},
			err:      fmt.Errorf("%w: %s", agents.ErrAgentNotFound, alphaID),
			wantCode: connect.CodeNotFound,
		},
		{
			name: "an archive naming an agent nobody holds",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error {
				_, err := svc.ArchiveAgents(ctx, connect.NewRequest(&agentv1.ArchiveAgentsRequest{Ids: []string{alphaID}}))

				return err
			},
			err:      fmt.Errorf("%w: %s", agents.ErrAgentNotFound, alphaID),
			wantCode: connect.CodeNotFound,
		},
		{
			name:     "a restore past the panel's ceiling",
			call:     restoreAlpha,
			err:      agents.ErrTooManyAgents,
			wantCode: connect.CodeResourceExhausted,
		},
		{
			name:     "a restore onto a name a live agent has taken",
			call:     restoreAlpha,
			err:      fmt.Errorf("%w: auth", store.ErrNameTaken),
			wantCode: connect.CodeAlreadyExists,
		},
		{
			name:     "a restore of a child before its parent",
			call:     restoreAlpha,
			err:      agents.ErrParentStillArchived,
			wantCode: connect.CodeFailedPrecondition,
		},
		{
			name: "an orchestrator for a workflow that is not there",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error {
				_, err := svc.StartOrchestrator(ctx, connect.NewRequest(&agentv1.StartOrchestratorRequest{WorkflowId: missingID}))

				return err
			},
			err:      agents.ErrWorkflowNotFound,
			wantCode: connect.CodeNotFound,
		},
		{
			name: "planning a rebase of an agent with no worktree",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error {
				_, err := svc.PlanRebase(ctx, connect.NewRequest(&agentv1.PlanRebaseRequest{AgentId: alphaID}))

				return err
			},
			err:      checkouts.ErrRebaseNoWorktree,
			wantCode: connect.CodeFailedPrecondition,
		},
		{
			name: "a rebase of a dirty worktree",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error {
				_, err := svc.RebaseWorktree(ctx, connect.NewRequest(&agentv1.RebaseWorktreeRequest{AgentId: alphaID}))

				return err
			},
			err:      checkouts.ErrRebaseDirty,
			wantCode: connect.CodeFailedPrecondition,
		},
		{
			name: "a rename onto a name another agent holds",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error {
				_, err := svc.UpdateAgent(ctx, connect.NewRequest(&agentv1.UpdateAgentRequest{
					Id:   alphaID,
					Name: "beta",
				}))

				return err
			},
			err:      fmt.Errorf("%w: beta", store.ErrNameTaken),
			wantCode: connect.CodeAlreadyExists,
		},
		{
			name: "hiding an agent nobody holds",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error {
				_, err := svc.SetHidden(ctx, connect.NewRequest(&agentv1.SetHiddenRequest{
					Id:     alphaID,
					Hidden: true,
				}))

				return err
			},
			err:      agents.ErrAgentNotFound,
			wantCode: connect.CodeNotFound,
		},
		{
			name: "a report from nothing that is an agent",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error {
				_, err := svc.RecordReport(ctx, connect.NewRequest(&agentv1.RecordReportRequest{TurnEnded: true}))

				return err
			},
			wantCode: connect.CodeInvalidArgument,
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
				_, err := svc.GetAgent(ctx, connect.NewRequest(&agentv1.GetAgentRequest{Id: missingID}))

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
		call       func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter, ids []string) (string, error)
		err        error
		wantAnswer string
		check      func(t *testing.T, services *recordingAgentServices, ids []string)
	}{
		{
			name: "a spent idempotency key answers with the agent it launched",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter, _ []string) (string, error) {
				created, err := svc.CreateAgent(ctx, connect.NewRequest(&agentv1.CreateAgentRequest{
					Prompt:         "go",
					IdempotencyKey: xptr.Ptr("triage:ENG-5260"),
				}))
				if err != nil {
					return "", err
				}

				return created.Msg.GetAgent().GetName(), nil
			},
			err:        agents.ErrAlreadyLaunched,
			wantAnswer: "spawned",
			check: func(t *testing.T, services *recordingAgentServices, _ []string) {
				assert.Equal(t, "triage:ENG-5260", services.created.IdempotencyKey)
			},
		},
		{
			name: "creating",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter, ids []string) (string, error) {
				created, err := svc.CreateAgent(ctx, connect.NewRequest(&agentv1.CreateAgentRequest{
					Prompt:           "audit the migrations",
					Title:            "audit",
					Dir:              "/repo",
					Flags:            "--model opus",
					ProvisionEnabled: xptr.Ptr(false),
					WorkflowId:       "wf-1",
					Harness:          "codex",
					IdempotencyKey:   xptr.Ptr("triage:ENG-5260"),
					ParentAgentId:    xptr.Ptr(ids[0]),
				}))
				if err != nil {
					return "", err
				}

				return created.Msg.GetAgent().GetName(), nil
			},
			wantAnswer: "spawned",
			check: func(t *testing.T, services *recordingAgentServices, ids []string) {
				assert.Equal(t, agents.NewAgentParams{
					Title:            "audit",
					Dir:              "/repo",
					Flags:            "--model opus",
					ProvisionEnabled: xptr.Ptr(false),
					Prompt:           "audit the migrations",
					WorkflowID:       "wf-1",
					Harness:          "codex",
					IdempotencyKey:   "triage:ENG-5260",
					ParentAgentID:    ids[0],
				}, services.created)
			},
		},
		{
			name: "requesting a review for the calling agent",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter, ids []string) (string, error) {
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
			check: func(t *testing.T, services *recordingAgentServices, ids []string) {
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
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter, _ []string) (string, error) {
				adopted, err := svc.Adopt(ctx, connect.NewRequest(&agentv1.AdoptRequest{
					Ref:            "https://github.com/o/r/pull/1069",
					Prompt:         "pick it up",
					Title:          "logo",
					Flags:          "--model opus",
					Harness:        "claude",
					WorkflowId:     "wf-1",
					IdempotencyKey: xptr.Ptr("adopt-1"),
				}))
				if err != nil {
					return "", err
				}

				return adopted.Msg.GetAgent().GetName(), nil
			},
			wantAnswer: "spawned",
			check: func(t *testing.T, services *recordingAgentServices, _ []string) {
				assert.Equal(t, agents.AdoptParams{
					Ref:            "https://github.com/o/r/pull/1069",
					Title:          "logo",
					Flags:          "--model opus",
					Prompt:         "pick it up",
					WorkflowID:     "wf-1",
					Harness:        "claude",
					IdempotencyKey: "adopt-1",
				}, services.adopted)
			},
		},
		{
			name: "retrying",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter, ids []string) (string, error) {
				retried, err := svc.RetryAgent(ctx, connect.NewRequest(&agentv1.RetryAgentRequest{
					Id:      ids[0],
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
			check: func(t *testing.T, services *recordingAgentServices, ids []string) {
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
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter, ids []string) (string, error) {
				resumed, err := svc.ResumeAgent(ctx, connect.NewRequest(&agentv1.ResumeAgentRequest{Id: ids[0]}))
				if err != nil {
					return "", err
				}

				return resumed.Msg.GetAgent().GetName(), nil
			},
			wantAnswer: "spawned",
			check: func(t *testing.T, services *recordingAgentServices, ids []string) {
				assert.Equal(t, ids[:1], services.resumed)
			},
		},
		{
			name: "holding from the panel",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter, ids []string) (string, error) {
				held, err := svc.HoldAgent(ctx, connect.NewRequest(&agentv1.HoldAgentRequest{Id: ids[0]}))
				if err != nil {
					return "", err
				}

				return held.Msg.GetAgent().GetName(), nil
			},
			wantAnswer: "spawned",
			check: func(t *testing.T, services *recordingAgentServices, ids []string) {
				assert.Equal(t, []agents.HoldParams{{AgentID: ids[0]}}, services.held)
			},
		},
		{
			name: "holding from an agent",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter, ids []string) (string, error) {
				ctx = caller.With(ctx, caller.Caller{AgentID: betaID})
				held, err := svc.HoldAgent(ctx, connect.NewRequest(&agentv1.HoldAgentRequest{Id: ids[0]}))
				if err != nil {
					return "", err
				}

				return held.Msg.GetAgent().GetName(), nil
			},
			wantAnswer: "spawned",
			check: func(t *testing.T, services *recordingAgentServices, ids []string) {
				assert.Equal(t, []agents.HoldParams{{AgentID: ids[0], ByAgentID: betaID}}, services.held)
			},
		},
		{
			name: "releasing",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter, ids []string) (string, error) {
				released, err := svc.ReleaseAgent(ctx, connect.NewRequest(&agentv1.ReleaseAgentRequest{
					Id:   ids[0],
					Said: "I rebased your branch",
				}))
				if err != nil {
					return "", err
				}

				return released.Msg.GetAgent().GetName(), nil
			},
			wantAnswer: "spawned",
			check: func(t *testing.T, services *recordingAgentServices, ids []string) {
				assert.Equal(t, agents.ReleaseParams{
					AgentID: ids[0],
					Said:    "I rebased your branch",
				}, services.released)
			},
		},
		{
			name: "starting a workflow's orchestrator",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter, _ []string) (string, error) {
				started, err := svc.StartOrchestrator(ctx, connect.NewRequest(&agentv1.StartOrchestratorRequest{WorkflowId: "wf-1"}))
				if err != nil {
					return "", err
				}

				return started.Msg.GetAgent().GetName(), nil
			},
			wantAnswer: "spawned",
			check: func(t *testing.T, services *recordingAgentServices, _ []string) {
				assert.Equal(t, []string{"wf-1"}, services.orchestrated)
			},
		},
		{
			name: "planning a rebase",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter, ids []string) (string, error) {
				planned, err := svc.PlanRebase(ctx, connect.NewRequest(&agentv1.PlanRebaseRequest{AgentId: ids[0]}))
				if err != nil {
					return "", err
				}

				return fmt.Sprintf("%s onto %s, %d of %d to replay", planned.Msg.GetBranch(), planned.Msg.GetBaseRef(), planned.Msg.GetReplayCommits(), planned.Msg.GetCommits()), nil
			},
			wantAnswer: "agent/alpha onto origin/main, 2 of 3 to replay",
			check: func(t *testing.T, services *recordingAgentServices, ids []string) {
				assert.Equal(t, ids[:1], services.planned)
			},
		},
		{
			name: "rebasing",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter, ids []string) (string, error) {
				rebased, err := svc.RebaseWorktree(ctx, connect.NewRequest(&agentv1.RebaseWorktreeRequest{AgentId: ids[0]}))
				if err != nil {
					return "", err
				}

				return fmt.Sprintf("%s onto %s, conflicted %t", rebased.Msg.GetBranch(), rebased.Msg.GetBaseRef(), rebased.Msg.GetConflicted()), nil
			},
			wantAnswer: "agent/alpha onto origin/main, conflicted true",
			check: func(t *testing.T, services *recordingAgentServices, ids []string) {
				assert.Equal(t, ids[:1], services.rebased)
			},
		},
		{
			name: "deleting takes the whole batch",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter, ids []string) (string, error) {
				_, err := svc.DeleteAgents(ctx, connect.NewRequest(&agentv1.DeleteAgentsRequest{Ids: ids}))

				return "", err
			},
			check: func(t *testing.T, services *recordingAgentServices, ids []string) {
				assert.Equal(t, ids, services.deleted)
			},
		},
		{
			name: "archiving from the panel takes the whole batch",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter, ids []string) (string, error) {
				_, err := svc.ArchiveAgents(ctx, connect.NewRequest(&agentv1.ArchiveAgentsRequest{Ids: ids}))

				return "", err
			},
			check: func(t *testing.T, services *recordingAgentServices, ids []string) {
				assert.Equal(t, ids, services.archived)
			},
		},
		{
			name: "restoring answers with every agent restored",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter, ids []string) (string, error) {
				restored, err := svc.RestoreAgents(ctx, connect.NewRequest(&agentv1.RestoreAgentsRequest{Ids: ids}))
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
			check: func(t *testing.T, services *recordingAgentServices, ids []string) {
				assert.Equal(t, ids, services.restored)
			},
		},
		{
			name: "renaming",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter, ids []string) (string, error) {
				_, err := svc.UpdateAgent(ctx, connect.NewRequest(&agentv1.UpdateAgentRequest{
					Id:   ids[0],
					Name: "renamed",
				}))

				return "", err
			},
			check: func(t *testing.T, services *recordingAgentServices, ids []string) {
				assert.Equal(t, map[string]string{ids[0]: "renamed"}, services.renamed)
			},
		},
		{
			name: "hiding",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter, ids []string) (string, error) {
				_, err := svc.SetHidden(ctx, connect.NewRequest(&agentv1.SetHiddenRequest{
					Id:     ids[0],
					Hidden: true,
				}))

				return "", err
			},
			check: func(t *testing.T, services *recordingAgentServices, ids []string) {
				assert.Equal(t, map[string]bool{ids[0]: true}, services.hidden)
			},
		},
		{
			name: "claiming the failed runs",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter, ids []string) (string, error) {
				_, err := svc.MarkRunsRead(ctx, connect.NewRequest(&agentv1.MarkRunsReadRequest{Id: ids[0]}))

				return "", err
			},
			check: func(t *testing.T, services *recordingAgentServices, ids []string) {
				assert.Equal(t, ids[:1], services.runsRead)
			},
		},
		{
			name: "a report from the calling agent's own CLI",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter, ids []string) (string, error) {
				ctx = caller.With(ctx, caller.Caller{AgentID: ids[0]})
				_, err := svc.RecordReport(ctx, connect.NewRequest(&agentv1.RecordReportRequest{
					Reason:    "Bash",
					TurnEnded: true,
					TurnError: "Overloaded",
				}))

				return "", err
			},
			check: func(t *testing.T, services *recordingAgentServices, ids []string) {
				assert.Equal(t, agents.RecordReportParams{
					AgentID:   ids[0],
					Reason:    "Bash",
					TurnEnded: true,
					TurnError: "Overloaded",
				}, services.reported)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, services := newRecordedService(t)
			services.err = tt.err
			ids := []string{
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

// TestAdoptAnswersWithTheAgentAlreadyOnTheBranch covers the service's existing flag reaching the response.
func TestAdoptAnswersWithTheAgentAlreadyOnTheBranch(t *testing.T) {
	svc, services := newRecordedService(t)
	services.adoptExisting = true

	adopted, err := svc.Adopt(t.Context(), connect.NewRequest(&agentv1.AdoptRequest{
		Ref:        "someones/work",
		WorkflowId: "wf-1",
	}))
	require.NoError(t, err)

	assert.True(t, adopted.Msg.GetExisting())
	assert.Equal(t, "spawned", adopted.Msg.GetAgent().GetName())
}

// TestArchiveAgentsHoldsAnAgentToItselfAndItsOwnChildren leaves an orchestrator and the panel unrestricted.
func TestArchiveAgentsHoldsAnAgentToItselfAndItsOwnChildren(t *testing.T) {
	tests := []struct {
		name     string
		from     string
		archive  []string
		wantCode connect.Code
	}{
		{
			name:     "an agent archiving a stranger",
			from:     "parent",
			archive:  []string{"stranger"},
			wantCode: connect.CodePermissionDenied,
		},
		{
			name:     "one stranger refuses the whole batch",
			from:     "parent",
			archive:  []string{"child", "stranger"},
			wantCode: connect.CodePermissionDenied,
		},
		{
			name:    "an agent archiving its own child",
			from:    "parent",
			archive: []string{"child"},
		},
		{
			name:    "an agent archiving itself",
			from:    "parent",
			archive: []string{"parent"},
		},
		{
			name:    "an agent archiving itself and its own child",
			from:    "parent",
			archive: []string{"parent", "child"},
		},
		{
			name:     "a child archiving its parent",
			from:     "child",
			archive:  []string{"parent"},
			wantCode: connect.CodePermissionDenied,
		},
		{
			name:    "an orchestrator archiving anybody",
			from:    "orchestrator",
			archive: []string{"stranger"},
		},
		{
			name:    "the panel archiving anybody",
			archive: []string{"parent"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, services := newRecordedService(t)
			parent := services.rows.seed(agentstore.Agent{
				Name: "parent",
				Role: agentstore.RoleImplementer,
			})
			seeded := map[string]string{
				"parent": parent.ID,
				"child": services.rows.seed(agentstore.Agent{
					Name:          "child",
					ParentAgentID: &parent.ID,
					Role:          agentstore.RoleReviewer,
				}).ID,
				"stranger": services.rows.seed(agentstore.Agent{
					Name: "stranger",
					Role: agentstore.RoleImplementer,
				}).ID,
				"orchestrator": services.rows.seed(agentstore.Agent{
					Name: "orchestrator",
					Role: agentstore.RoleOrchestrator,
				}).ID,
			}
			ids := xslice.Map(tt.archive, func(name string) string {
				return seeded[name]
			})

			ctx := caller.With(t.Context(), caller.Caller{AgentID: seeded[tt.from]})
			_, err := svc.ArchiveAgents(ctx, connect.NewRequest(&agentv1.ArchiveAgentsRequest{Ids: ids}))

			if tt.wantCode != 0 {
				assert.Equal(t, tt.wantCode, connect.CodeOf(err))
				assert.Empty(t, services.archived)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, ids, services.archived)
		})
	}
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
				assert.Equal(t, "2026-09-20T12:00:00Z", rows["archived"].GetAgent().GetArchivedAt(), "in RFC 3339")
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
							ID:         uuid.NewString(),
							Name:       "archived",
							ArchivedAt: &archivedAt,
						},
					},
				},
				surroundings: agents.Surroundings{Mail: []mailstore.Mail{
					{
						ID:        "mail-1",
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
				_, err := svc.GetAgent(ctx, connect.NewRequest(&agentv1.GetAgentRequest{Id: alphaID}))

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
					ID:            alphaID,
					Name:          "alpha",
					Cwd:           "/repo",
					Prompt:        prompt,
					State:         agentstore.StateThinking,
					CreatedAt:     created,
					WorkflowID:    "wf-1",
					RepositoryURL: xptr.Ptr(komradRepository),
					Branch:        xptr.Ptr("agent/x"),
				},
				FailedRun:    true,
				FailedRunLog: xptr.Ptr(int32(4)),
			},
		},
		surroundings: agents.Surroundings{
			Mail: []mailstore.Mail{
				{
					ID:        "mail-1",
					ToAgentID: alphaID,
					ExpiresAt: time.Now().Add(time.Hour),
				},
			},
			PullRequests: []pullrequeststore.PullRequest{
				{
					ID:                "pr-740",
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
					ID:               "review-1",
					AgentID:          alphaID,
					RoundID:          "round-1",
					RoundOrdinal:     1,
					RoundSnapshotSHA: "round-1-sha",
					ReleasedAt:       xptr.Ptr(created),
				},
			},
			Workflows: []workflowstore.Workflow{{ID: "wf-1"}},
		},
	}

	got, err := newRowHandler(rows).GetAgent(t.Context(), connect.NewRequest(&agentv1.GetAgentRequest{Id: alphaID}))
	require.NoError(t, err)

	assert.True(t, got.Msg.GetUnreadMail(), "the agent is drawn beside its surroundings")
	require.Len(t, got.Msg.GetPullRequests(), 1)
	assert.Equal(t, int32(740), got.Msg.GetPullRequests()[0].GetNumber(), "with every pull request on its branch")
	assert.Equal(t, int32(740), got.Msg.GetPullRequest().GetNumber(), "and the current one's standing")
	assert.Equal(t, int32(1), got.Msg.GetLocalReviewRounds())

	agent := got.Msg.GetAgent()
	assert.Equal(t, "/repo", agent.GetCwd())
	assert.Equal(t, agentv1.AgentState_AGENT_STATE_THINKING, agent.GetState())
	assert.Equal(t, prompt, agent.GetPrompt(), "no view narrows a single agent")
	assert.Equal(t, int32(4), agent.GetFailedRunLog())
	require.NotNil(t, agent.Alive, "a dormant agent says so rather than leaving it out")
	assert.False(t, agent.GetAlive())
	assert.Equal(t, "2026-09-20T12:00:00Z", agent.GetCreatedAt(), "the creation time is RFC 3339")
}

// TestGetAgentResponseMirrorsAgentRow pins that every field AgentRow draws beside an agent is
// declared on GetAgentResponse the same way.
func TestGetAgentResponseMirrorsAgentRow(t *testing.T) {
	t.Parallel()

	rowFields := (&agentv1.AgentRow{}).ProtoReflect().Descriptor().Fields()
	responseFields := (&agentv1.GetAgentResponse{}).ProtoReflect().Descriptor().Fields()

	assert.Equal(t, rowFields.Len(), responseFields.Len())

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

			id := uuid.NewString()
			if tt.seed {
				id = rows.seed(agentstore.Agent{
					Name:  "alpha",
					Cwd:   "/repo",
					State: agentstore.StateThinking,
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
				req.Id = &id
			}

			_, err := svc.Log(ctx, connect.NewRequest(req))
			if tt.wantCode != 0 {
				assert.Equal(t, tt.wantCode, connect.CodeOf(err))
				return
			}

			require.NoError(t, err)

			record := logtest.Record(t, written.String())
			assert.Equal(t, "fetched origin/main", record["body"])
			assert.Equal(t, id, record[logging.TargetAgentID])
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
	released      agents.ReleaseParams
	reported      agents.RecordReportParams
	resumed       []string
	held          []agents.HoldParams
	orchestrated  []string
	planned       []string
	rebased       []string
	deleted       []string
	archived      []string
	restored      []string
	runsRead      []string
	renamed       map[string]string
	hidden        map[string]bool
}

// spawn seeds the agent the fake answers with among the rows.
func (f *recordingAgentServices) spawn() *agentstore.Agent {
	if f.spawnedAgent == nil {
		spawned := f.rows.seed(agentstore.Agent{
			Name:  "spawned",
			Cwd:   "/repo",
			State: agentstore.StateIdle,
		})
		f.spawnedAgent = &spawned
	}

	return f.spawnedAgent
}

// answer returns the spawned agent, or err in its place unless err is a spent idempotency key.
func (f *recordingAgentServices) answer() (*agentstore.Agent, error) {
	if f.err != nil && !errors.Is(f.err, agents.ErrAlreadyLaunched) {
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

func (f *recordingAgentServices) IsAgentRunning(agentstore.Agent) bool {
	return false
}

func (f *recordingAgentServices) ResumeAgent(_ context.Context, id string) (*agentstore.Agent, error) {
	f.resumed = append(f.resumed, id)

	return f.answer()
}

func (f *recordingAgentServices) Hold(_ context.Context, params agents.HoldParams) (*agentstore.Agent, error) {
	f.held = append(f.held, params)

	return f.answer()
}

func (f *recordingAgentServices) Release(_ context.Context, params agents.ReleaseParams) (*agentstore.Agent, error) {
	f.released = params

	return f.answer()
}

func (f *recordingAgentServices) Start(_ context.Context, workflowID string) (*agentstore.Agent, error) {
	f.orchestrated = append(f.orchestrated, workflowID)

	return f.answer()
}

func (f *recordingAgentServices) PlanRebase(_ context.Context, agentID string) (*checkouts.RebasePlan, error) {
	f.planned = append(f.planned, agentID)
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

func (f *recordingAgentServices) Rebase(_ context.Context, agentID string) (*checkouts.RebaseResult, error) {
	f.rebased = append(f.rebased, agentID)
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

func (f *recordingAgentServices) DeleteAgents(_ context.Context, ids []string) error {
	f.deleted = append(f.deleted, ids...)

	return f.err
}

func (f *recordingAgentServices) ArchiveAgents(_ context.Context, ids []string) error {
	f.archived = append(f.archived, ids...)

	return f.err
}

// RestoreAgents answers with an agent for every id.
func (f *recordingAgentServices) RestoreAgents(_ context.Context, ids []string) ([]agentstore.Agent, error) {
	f.restored = append(f.restored, ids...)
	if f.err != nil {
		return nil, f.err
	}

	return xslice.Map(ids, func(id string) agentstore.Agent {
		return agentstore.Agent{ID: id}
	}), nil
}

func (f *recordingAgentServices) SetHidden(_ context.Context, id string, hidden bool) error {
	if f.hidden == nil {
		f.hidden = map[string]bool{}
	}
	f.hidden[id] = hidden

	return f.err
}

func (f *recordingAgentServices) Rename(_ context.Context, id, name string) error {
	if f.renamed == nil {
		f.renamed = map[string]string{}
	}
	f.renamed[id] = name

	return f.err
}

func (f *recordingAgentServices) MarkRunsRead(_ context.Context, id string) error {
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
		WorkflowId: "wf-1",
	}))

	return err
}

func restoreAlpha(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error {
	_, err := svc.RestoreAgents(ctx, connect.NewRequest(&agentv1.RestoreAgentsRequest{Ids: []string{alphaID}}))

	return err
}

// newRowHandler is the handler over a fake of the rows, reading every agent as dormant.
func newRowHandler(rows *fakeRowReader) agentv1komrad.AgentServiceAdapter {
	return agentv1komrad.AgentServiceAdapter{Impl: NewHandler(NewHandlerParams{
		Agents:   rows,
		Rows:     rows,
		Liveness: &recordingAgentServices{},
	})}
}

// newRecordedService is the handler over fakes of every agent service, recording what each was asked.
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
		Liveness:     services,
		Lifecycle:    services,
		Resumer:      services,
		Holder:       services,
		Deleter:      services,
		Orchestrator: services,
		Rebaser:      services,
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
			name: "a prompt, a workflow, and an absolute directory are enough",
			req: &agentv1.CreateAgentRequest{
				Prompt:     "audit the migrations",
				Dir:        "/repo",
				WorkflowId: "wf-1",
			},
		},
		{
			name: "a home-relative directory is allowed, since the manager expands it",
			req: &agentv1.CreateAgentRequest{
				Prompt:     "go",
				Dir:        "~/repo",
				WorkflowId: "wf-1",
			},
		},
		{
			name: "no directory is allowed, since the workflow has one",
			req:  &agentv1.CreateAgentRequest{Prompt: "go", WorkflowId: "wf-1"},
		},
		{
			name:    "no prompt",
			req:     &agentv1.CreateAgentRequest{Dir: "/repo", WorkflowId: "wf-1"},
			wantErr: true,
		},
		{
			name:    "no workflow, which every agent needs",
			req:     &agentv1.CreateAgentRequest{Prompt: "go", Dir: "/repo"},
			wantErr: true,
		},
		{
			name: "a relative directory would be read against whichever pane is current",
			req: &agentv1.CreateAgentRequest{
				Prompt:     "go",
				Dir:        "repo",
				WorkflowId: "wf-1",
			},
			wantErr: true,
		},
		{
			name: "flags carrying a shell metacharacter are refused rather than quoted and run",
			req: &agentv1.CreateAgentRequest{
				Prompt:     "go",
				Dir:        "/repo",
				WorkflowId: "wf-1",
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
			name: "a delete naming no agent is refused",
			call: func(t *testing.T, c agentv1connect.AgentServiceClient) error {
				_, err := c.DeleteAgents(t.Context(), connect.NewRequest(&agentv1.DeleteAgentsRequest{Ids: nil}))

				return err
			},
			wantErr: true,
		},
		{
			name: "so is one carrying an empty id among real ones",
			call: func(t *testing.T, c agentv1connect.AgentServiceClient) error {
				_, err := c.DeleteAgents(t.Context(), connect.NewRequest(&agentv1.DeleteAgentsRequest{
					Ids: []string{uuid.NewString(), ""},
				}))

				return err
			},
			wantErr: true,
		},
		{
			name: "a delete of however many agents is taken",
			call: func(t *testing.T, c agentv1connect.AgentServiceClient) error {
				ids := make([]string, 0, 500)
				for range 500 {
					ids = append(ids, uuid.NewString())
				}

				_, err := c.DeleteAgents(t.Context(), connect.NewRequest(&agentv1.DeleteAgentsRequest{Ids: ids}))

				return err
			},
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
