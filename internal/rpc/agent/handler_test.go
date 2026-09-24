package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"testing"
	"time"

	agentv1weave "weave/proto/gen/agent/v1/agentv1weave"

	"weave/config"
	"weave/internal/agents"
	"weave/internal/caller"
	"weave/internal/logging"
	"weave/internal/logtest"
	"weave/internal/mail"
	"weave/internal/marks"
	"weave/internal/reviews"
	"weave/internal/rpc/render"
	"weave/internal/store"
	"weave/internal/storetest"
	"weave/lib/xptr"
	"weave/lib/xslice"
	agentv1 "weave/proto/gen/agent/v1"
	paginationv1 "weave/proto/gen/pagination/v1"
	viewv1 "weave/proto/gen/view/v1"

	agentstore "weave/internal/store/agent"
	mailstore "weave/internal/store/mail"
	reviewstore "weave/internal/store/review"
	runstore "weave/internal/store/run"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
)

const missingID = "00000000-0000-0000-0000-000000000000"

// TestLifecycleRefusalsMapToCodes pins the code each refusal of the agent services reaches the
// caller as.
func TestLifecycleRefusalsMapToCodes(t *testing.T) {
	tests := []struct {
		name     string
		call     func(ctx context.Context, svc agentv1weave.AgentServiceAdapter) error
		err      error
		wantCode connect.Code
	}{
		{
			name: "a launch that cannot start",
			call: func(ctx context.Context, svc agentv1weave.AgentServiceAdapter) error {
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
			call: func(ctx context.Context, svc agentv1weave.AgentServiceAdapter) error {
				_, err := svc.RequestReview(ctx, connect.NewRequest(&agentv1.RequestReviewRequest{Prompt: "read it"}))

				return err
			},
			wantCode: connect.CodeInvalidArgument,
		},
		{
			name: "a second reviewer",
			call: func(ctx context.Context, svc agentv1weave.AgentServiceAdapter) error {
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
			call: func(ctx context.Context, svc agentv1weave.AgentServiceAdapter) error {
				_, err := svc.RetryAgent(ctx, connect.NewRequest(&agentv1.RetryAgentRequest{Id: alphaID}))

				return err
			},
			err:      agents.ErrAgentNotFailed,
			wantCode: connect.CodeFailedPrecondition,
		},
		{
			name: "a resume of an archived agent",
			call: func(ctx context.Context, svc agentv1weave.AgentServiceAdapter) error {
				_, err := svc.ResumeAgent(ctx, connect.NewRequest(&agentv1.ResumeAgentRequest{Id: alphaID}))

				return err
			},
			err:      agents.ErrAgentArchived,
			wantCode: connect.CodeFailedPrecondition,
		},
		{
			name: "an agent holding itself",
			call: func(ctx context.Context, svc agentv1weave.AgentServiceAdapter) error {
				ctx = caller.With(ctx, caller.Caller{AgentID: alphaID})
				_, err := svc.HoldAgent(ctx, connect.NewRequest(&agentv1.HoldAgentRequest{Id: alphaID}))

				return err
			},
			wantCode: connect.CodeInvalidArgument,
		},
		{
			name: "a hold on an agent that will not stop",
			call: func(ctx context.Context, svc agentv1weave.AgentServiceAdapter) error {
				_, err := svc.HoldAgent(ctx, connect.NewRequest(&agentv1.HoldAgentRequest{Id: alphaID}))

				return err
			},
			err:      agents.ErrUnsettled,
			wantCode: connect.CodeFailedPrecondition,
		},
		{
			name: "a release of an agent nobody holds",
			call: func(ctx context.Context, svc agentv1weave.AgentServiceAdapter) error {
				_, err := svc.ReleaseAgent(ctx, connect.NewRequest(&agentv1.ReleaseAgentRequest{Id: alphaID}))

				return err
			},
			err:      agents.ErrNotHeld,
			wantCode: connect.CodeFailedPrecondition,
		},
		{
			name: "a delete naming an agent nobody holds",
			call: func(ctx context.Context, svc agentv1weave.AgentServiceAdapter) error {
				_, err := svc.DeleteAgents(ctx, connect.NewRequest(&agentv1.DeleteAgentsRequest{Ids: []string{alphaID}}))

				return err
			},
			err:      fmt.Errorf("%w: %s", agents.ErrAgentNotFound, alphaID),
			wantCode: connect.CodeNotFound,
		},
		{
			name: "an archive naming an agent nobody holds",
			call: func(ctx context.Context, svc agentv1weave.AgentServiceAdapter) error {
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
			name: "a queue that refuses the archive sweep",
			call: func(ctx context.Context, svc agentv1weave.AgentServiceAdapter) error {
				_, err := svc.EmptyArchive(ctx, connect.NewRequest(&emptypb.Empty{}))

				return err
			},
			err:      errors.New("the database is gone"),
			wantCode: connect.CodeInternal,
		},
		{
			name: "an orchestrator for a workflow that is not there",
			call: func(ctx context.Context, svc agentv1weave.AgentServiceAdapter) error {
				_, err := svc.StartOrchestrator(ctx, connect.NewRequest(&agentv1.StartOrchestratorRequest{WorkflowId: missingID}))

				return err
			},
			err:      agents.ErrWorkflowNotFound,
			wantCode: connect.CodeNotFound,
		},
		{
			name: "a rename onto a name another agent holds",
			call: func(ctx context.Context, svc agentv1weave.AgentServiceAdapter) error {
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
			call: func(ctx context.Context, svc agentv1weave.AgentServiceAdapter) error {
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
			name: "recording the pull request key",
			call: func(ctx context.Context, svc agentv1weave.AgentServiceAdapter) error {
				_, err := svc.SetMetadata(ctx, connect.NewRequest(&agentv1.SetMetadataRequest{
					Id:    alphaID,
					Key:   "pr",
					Value: xptr.Ptr("https://github.com/o/r/pull/1"),
				}))

				return err
			},
			err:      agents.ErrPullRequestKey,
			wantCode: connect.CodeInvalidArgument,
		},
		{
			name: "a report from nothing that is an agent",
			call: func(ctx context.Context, svc agentv1weave.AgentServiceAdapter) error {
				_, err := svc.RecordReport(ctx, connect.NewRequest(&agentv1.RecordReportRequest{TurnEnded: true}))

				return err
			},
			wantCode: connect.CodeInvalidArgument,
		},
		{
			name: "reading an agent nobody holds",
			call: func(ctx context.Context, svc agentv1weave.AgentServiceAdapter) error {
				_, err := svc.GetAgent(ctx, connect.NewRequest(&agentv1.GetAgentRequest{Id: missingID}))

				return err
			},
			wantCode: connect.CodeNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, _, launcher := serviceWithLauncher(t)
			launcher.err = tt.err

			err := tt.call(t.Context(), svc)

			assert.Equal(t, tt.wantCode, connect.CodeOf(err))
		})
	}
}

// TestLifecycleRequestsReachTheServices pins what each request hands the agent services and what
// it answers with.
func TestLifecycleRequestsReachTheServices(t *testing.T) {
	tests := []struct {
		name     string
		call     func(ctx context.Context, svc agentv1weave.AgentServiceAdapter, ids []string) (string, error)
		err      error
		wantName string
		check    func(t *testing.T, launcher *fakeLauncher, ids []string)
	}{
		{
			name: "a spent idempotency key answers with the agent it launched",
			call: func(ctx context.Context, svc agentv1weave.AgentServiceAdapter, _ []string) (string, error) {
				created, err := svc.CreateAgent(ctx, connect.NewRequest(&agentv1.CreateAgentRequest{
					Prompt:         "go",
					IdempotencyKey: xptr.Ptr("triage:ENG-5260"),
				}))

				return created.Msg.GetAgent().GetName(), err
			},
			err:      agents.ErrAlreadyLaunched,
			wantName: "spawned",
			check: func(t *testing.T, launcher *fakeLauncher, _ []string) {
				assert.Equal(t, "triage:ENG-5260", launcher.created.IdempotencyKey)
			},
		},
		{
			name: "creating",
			call: func(ctx context.Context, svc agentv1weave.AgentServiceAdapter, ids []string) (string, error) {
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

				return created.Msg.GetAgent().GetName(), err
			},
			wantName: "spawned",
			check: func(t *testing.T, launcher *fakeLauncher, ids []string) {
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
				}, launcher.created)
			},
		},
		{
			name: "requesting a review for the calling agent",
			call: func(ctx context.Context, svc agentv1weave.AgentServiceAdapter, ids []string) (string, error) {
				ctx = caller.With(ctx, caller.Caller{AgentID: ids[0]})
				requested, err := svc.RequestReview(ctx, connect.NewRequest(&agentv1.RequestReviewRequest{
					Prompt:         "read the parser change",
					Title:          "second-reader",
					Harness:        "codex",
					Flags:          "--sandbox read-only",
					IdempotencyKey: xptr.Ptr("review-1"),
				}))

				return requested.Msg.GetReviewer().GetName(), err
			},
			wantName: "spawned",
			check: func(t *testing.T, launcher *fakeLauncher, ids []string) {
				assert.Equal(t, agents.RequestReviewParams{
					AgentID:        ids[0],
					Prompt:         "read the parser change",
					Title:          "second-reader",
					Harness:        "codex",
					Flags:          "--sandbox read-only",
					IdempotencyKey: "review-1",
				}, launcher.reviewed)
			},
		},
		{
			name: "adopting",
			call: func(ctx context.Context, svc agentv1weave.AgentServiceAdapter, _ []string) (string, error) {
				adopted, err := svc.Adopt(ctx, connect.NewRequest(&agentv1.AdoptRequest{
					Ref:            "https://github.com/o/r/pull/1069",
					Prompt:         "pick it up",
					Title:          "logo",
					Flags:          "--model opus",
					Harness:        "claude",
					WorkflowId:     "wf-1",
					IdempotencyKey: xptr.Ptr("adopt-1"),
				}))

				return adopted.Msg.GetAgent().GetName(), err
			},
			wantName: "spawned",
			check: func(t *testing.T, launcher *fakeLauncher, _ []string) {
				assert.Equal(t, agents.AdoptParams{
					Ref:            "https://github.com/o/r/pull/1069",
					Title:          "logo",
					Flags:          "--model opus",
					Prompt:         "pick it up",
					WorkflowID:     "wf-1",
					Harness:        "claude",
					IdempotencyKey: "adopt-1",
				}, launcher.adopted)
			},
		},
		{
			name: "retrying",
			call: func(ctx context.Context, svc agentv1weave.AgentServiceAdapter, ids []string) (string, error) {
				retried, err := svc.RetryAgent(ctx, connect.NewRequest(&agentv1.RetryAgentRequest{
					Id:      ids[0],
					Flags:   "--model opus",
					Prompt:  "try again",
					Harness: "codex",
				}))

				return retried.Msg.GetAgent().GetName(), err
			},
			wantName: "spawned",
			check: func(t *testing.T, launcher *fakeLauncher, ids []string) {
				assert.Equal(t, agents.RetryCreateParams{
					ID:      ids[0],
					Flags:   "--model opus",
					Prompt:  "try again",
					Harness: "codex",
				}, launcher.retried)
			},
		},
		{
			name: "resuming",
			call: func(ctx context.Context, svc agentv1weave.AgentServiceAdapter, ids []string) (string, error) {
				resumed, err := svc.ResumeAgent(ctx, connect.NewRequest(&agentv1.ResumeAgentRequest{Id: ids[0]}))

				return resumed.Msg.GetAgent().GetName(), err
			},
			wantName: "spawned",
			check: func(t *testing.T, launcher *fakeLauncher, ids []string) {
				assert.Equal(t, ids[:1], launcher.resumed)
			},
		},
		{
			name: "holding from the panel",
			call: func(ctx context.Context, svc agentv1weave.AgentServiceAdapter, ids []string) (string, error) {
				held, err := svc.HoldAgent(ctx, connect.NewRequest(&agentv1.HoldAgentRequest{Id: ids[0]}))

				return held.Msg.GetAgent().GetName(), err
			},
			wantName: "spawned",
			check: func(t *testing.T, launcher *fakeLauncher, ids []string) {
				assert.Equal(t, ids[:1], launcher.held)
			},
		},
		{
			name: "releasing",
			call: func(ctx context.Context, svc agentv1weave.AgentServiceAdapter, ids []string) (string, error) {
				released, err := svc.ReleaseAgent(ctx, connect.NewRequest(&agentv1.ReleaseAgentRequest{
					Id:   ids[0],
					Said: "I rebased your branch",
				}))

				return released.Msg.GetAgent().GetName(), err
			},
			wantName: "spawned",
			check: func(t *testing.T, launcher *fakeLauncher, ids []string) {
				assert.Equal(t, agents.ReleaseParams{
					AgentID: ids[0],
					Said:    "I rebased your branch",
				}, launcher.released)
			},
		},
		{
			name: "starting a workflow's orchestrator",
			call: func(ctx context.Context, svc agentv1weave.AgentServiceAdapter, _ []string) (string, error) {
				started, err := svc.StartOrchestrator(ctx, connect.NewRequest(&agentv1.StartOrchestratorRequest{WorkflowId: "wf-1"}))

				return started.Msg.GetAgent().GetName(), err
			},
			wantName: "spawned",
			check: func(t *testing.T, launcher *fakeLauncher, _ []string) {
				assert.Equal(t, []string{"wf-1"}, launcher.orchestrated)
			},
		},
		{
			name: "deleting takes the whole batch",
			call: func(ctx context.Context, svc agentv1weave.AgentServiceAdapter, ids []string) (string, error) {
				_, err := svc.DeleteAgents(ctx, connect.NewRequest(&agentv1.DeleteAgentsRequest{Ids: ids}))

				return "", err
			},
			check: func(t *testing.T, launcher *fakeLauncher, ids []string) {
				assert.Equal(t, ids, launcher.deleted)
			},
		},
		{
			name: "archiving from the panel takes the whole batch",
			call: func(ctx context.Context, svc agentv1weave.AgentServiceAdapter, ids []string) (string, error) {
				_, err := svc.ArchiveAgents(ctx, connect.NewRequest(&agentv1.ArchiveAgentsRequest{Ids: ids}))

				return "", err
			},
			check: func(t *testing.T, launcher *fakeLauncher, ids []string) {
				assert.Equal(t, ids, launcher.archived)
			},
		},
		{
			name: "restoring answers with every agent restored",
			call: func(ctx context.Context, svc agentv1weave.AgentServiceAdapter, ids []string) (string, error) {
				restored, err := svc.RestoreAgents(ctx, connect.NewRequest(&agentv1.RestoreAgentsRequest{Ids: ids}))
				names := xslice.Map(restored.Msg.GetAgents(), func(agent *agentv1.Agent) string {
					return agent.GetName()
				})
				slices.Sort(names)

				return strings.Join(names, " "), err
			},
			wantName: "alpha beta",
			check: func(t *testing.T, launcher *fakeLauncher, ids []string) {
				assert.Equal(t, ids, launcher.restored)
			},
		},
		{
			name: "emptying the archive",
			call: func(ctx context.Context, svc agentv1weave.AgentServiceAdapter, _ []string) (string, error) {
				_, err := svc.EmptyArchive(ctx, connect.NewRequest(&emptypb.Empty{}))

				return "", err
			},
			check: func(t *testing.T, launcher *fakeLauncher, _ []string) {
				assert.Equal(t, 1, launcher.emptied)
			},
		},
		{
			name: "renaming",
			call: func(ctx context.Context, svc agentv1weave.AgentServiceAdapter, ids []string) (string, error) {
				_, err := svc.UpdateAgent(ctx, connect.NewRequest(&agentv1.UpdateAgentRequest{
					Id:   ids[0],
					Name: "renamed",
				}))

				return "", err
			},
			check: func(t *testing.T, launcher *fakeLauncher, ids []string) {
				assert.Equal(t, map[string]string{ids[0]: "renamed"}, launcher.renamed)
			},
		},
		{
			name: "hiding",
			call: func(ctx context.Context, svc agentv1weave.AgentServiceAdapter, ids []string) (string, error) {
				_, err := svc.SetHidden(ctx, connect.NewRequest(&agentv1.SetHiddenRequest{
					Id:     ids[0],
					Hidden: true,
				}))

				return "", err
			},
			check: func(t *testing.T, launcher *fakeLauncher, ids []string) {
				assert.Equal(t, map[string]bool{ids[0]: true}, launcher.hidden)
			},
		},
		{
			name: "recording a fact",
			call: func(ctx context.Context, svc agentv1weave.AgentServiceAdapter, ids []string) (string, error) {
				_, err := svc.SetMetadata(ctx, connect.NewRequest(&agentv1.SetMetadataRequest{
					Id:    ids[0],
					Key:   "linear_issue",
					Value: xptr.Ptr("https://linear.app/x/issue/ENG-1"),
				}))

				return "", err
			},
			check: func(t *testing.T, launcher *fakeLauncher, ids []string) {
				assert.Equal(t, agents.SetMetadataParams{
					AgentID: ids[0],
					Key:     "linear_issue",
					Value:   xptr.Ptr("https://linear.app/x/issue/ENG-1"),
				}, launcher.metadata)
			},
		},
		{
			name: "claiming the failed runs",
			call: func(ctx context.Context, svc agentv1weave.AgentServiceAdapter, ids []string) (string, error) {
				_, err := svc.MarkRunsRead(ctx, connect.NewRequest(&agentv1.MarkRunsReadRequest{Id: ids[0]}))

				return "", err
			},
			check: func(t *testing.T, launcher *fakeLauncher, ids []string) {
				assert.Equal(t, ids[:1], launcher.runsRead)
			},
		},
		{
			name: "a report from the calling agent's own CLI",
			call: func(ctx context.Context, svc agentv1weave.AgentServiceAdapter, ids []string) (string, error) {
				ctx = caller.With(ctx, caller.Caller{AgentID: ids[0]})
				_, err := svc.RecordReport(ctx, connect.NewRequest(&agentv1.RecordReportRequest{
					Reason:    "Bash",
					TurnEnded: true,
				}))

				return "", err
			},
			check: func(t *testing.T, launcher *fakeLauncher, ids []string) {
				assert.Equal(t, agents.RecordReportParams{
					AgentID:   ids[0],
					Reason:    "Bash",
					TurnEnded: true,
				}, launcher.reported)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, st, launcher := serviceWithLauncher(t)
			launcher.err = tt.err
			ids := []string{
				st.SeedAgent(t, agentstore.Agent{Name: "alpha"}).ID,
				st.SeedAgent(t, agentstore.Agent{Name: "beta"}).ID,
			}

			name, err := tt.call(t.Context(), svc, ids)
			require.NoError(t, err)

			assert.Equal(t, tt.wantName, name)
			tt.check(t, launcher, ids)
		})
	}
}

// TestArchiveAgentsHoldsAnAgentToItsOwnChildren pins that an agent other than an orchestrator
// archives only its own children.
func TestArchiveAgentsHoldsAnAgentToItsOwnChildren(t *testing.T) {
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
			svc, st, launcher := serviceWithLauncher(t)
			parent := st.SeedAgent(t, agentstore.Agent{
				Name: "parent",
				Role: agentstore.RoleImplementer,
			})
			seeded := map[string]string{
				"parent": parent.ID,
				"child": st.SeedAgent(t, agentstore.Agent{
					Name:          "child",
					WorkflowID:    parent.WorkflowID,
					ParentAgentID: &parent.ID,
					Role:          agentstore.RoleReviewer,
				}).ID,
				"stranger": st.SeedAgent(t, agentstore.Agent{
					Name: "stranger",
					Role: agentstore.RoleImplementer,
				}).ID,
				"orchestrator": st.SeedAgent(t, agentstore.Agent{
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
				assert.Empty(t, launcher.archived)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, ids, launcher.archived)
		})
	}
}

// TestListAgents pins what a listed row carries from the watched sets beside it, and what the
// request's view and archive flag change.
func TestListAgents(t *testing.T) {
	prompt := strings.Repeat("a", render.SummaryHead+50)

	tests := []struct {
		name  string
		req   *agentv1.ListAgentsRequest
		check func(t *testing.T, rows map[string]*agentv1.AgentRow)
	}{
		{
			name: "the default listing is the live agents, their prompts cut to the head",
			req:  &agentv1.ListAgentsRequest{},
			check: func(t *testing.T, rows map[string]*agentv1.AgentRow) {
				require.Len(t, rows, 1)
				alpha := rows["alpha"].GetAgent()
				assert.Equal(t, prompt[:render.SummaryHead], alpha.GetPrompt())
				assert.True(t, alpha.GetPromptTruncated())
				assert.True(t, rows["alpha"].GetUnreadMail(), "the mail sent to it is read from the watched mail")
				assert.True(t, alpha.GetFailedRun())
				assert.Nil(t, alpha.ArchivedAt)
			},
		},
		{
			name: "the full view keeps the whole prompt",
			req:  &agentv1.ListAgentsRequest{View: viewv1.View_VIEW_FULL},
			check: func(t *testing.T, rows map[string]*agentv1.AgentRow) {
				assert.Equal(t, prompt, rows["alpha"].GetAgent().GetPrompt())
				assert.False(t, rows["alpha"].GetAgent().GetPromptTruncated())
			},
		},
		{
			name: "asking for the archived agents lists only those",
			req:  &agentv1.ListAgentsRequest{Archived: true},
			check: func(t *testing.T, rows map[string]*agentv1.AgentRow) {
				require.Len(t, rows, 1)
				archivedAt, err := time.Parse(time.RFC3339, rows["archived"].GetAgent().GetArchivedAt())
				require.NoError(t, err, "an archived agent says when it was archived, in RFC 3339")
				assert.WithinDuration(t, time.Now(), archivedAt, time.Minute)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, st := service(t)
			ctx := t.Context()
			alpha := st.SeedAgent(t, agentstore.Agent{
				Name:   "alpha",
				Prompt: prompt,
			})
			seedFailedRun(t, st, alpha.ID)
			_, err := st.Mail.Send(ctx, mailstore.SendParams{
				FromName:  "beta",
				ToAgentID: alpha.ID,
				Body:      "refunded_cents",
				Deadline:  time.Hour,
			})
			require.NoError(t, err)
			archived := st.SeedAgent(t, agentstore.Agent{Name: "archived"})
			_, err = st.Agents.Archive(ctx, []string{archived.ID})
			require.NoError(t, err)

			listed, err := svc.ListAgents(ctx, connect.NewRequest(tt.req))
			require.NoError(t, err)

			tt.check(t, xslice.KeyBy(listed.Msg.GetAgents(), func(row *agentv1.AgentRow) string {
				return row.GetAgent().GetName()
			}))
		})
	}
}

// TestGetAgent pins that one agent is read whole, its liveness written even when false.
func TestGetAgent(t *testing.T) {
	svc, st := service(t)
	prompt := strings.Repeat("a", render.SummaryHead+50)
	alpha := st.SeedAgent(t, agentstore.Agent{
		Name:   "alpha",
		Cwd:    "/repo",
		Prompt: prompt,
		State:  agentstore.StateThinking,
	})
	seedFailedRun(t, st, alpha.ID)

	got, err := svc.GetAgent(t.Context(), connect.NewRequest(&agentv1.GetAgentRequest{Id: alpha.ID}))
	require.NoError(t, err)

	agent := got.Msg.GetAgent().GetAgent()
	assert.Equal(t, "/repo", agent.GetCwd())
	assert.Equal(t, agentv1.AgentState_AGENT_STATE_THINKING, agent.GetState())
	assert.Equal(t, prompt, agent.GetPrompt(), "no view narrows a single agent")
	assert.Equal(t, int32(4), agent.GetFailedRunLog())
	require.NotNil(t, agent.Alive, "a dormant agent says so rather than leaving it out")
	assert.False(t, agent.GetAlive())

	created, err := time.Parse(time.RFC3339, agent.GetCreatedAt())
	require.NoError(t, err, "the creation time is RFC 3339")
	assert.WithinDuration(t, time.Now(), created, time.Minute)
}

// TestSetMark covers the mark over the wire, across both kinds of row, where a call that
// reached nothing live is refused and one that took the letter off another row is not.
func TestSetMark(t *testing.T) {
	t.Run("an id nobody holds is not a quiet success", func(t *testing.T) {
		svc, _ := service(t)

		_, err := svc.SetMark(t.Context(), connect.NewRequest(&agentv1.SetMarkRequest{
			Target: &agentv1.SetMarkRequest_AgentId{AgentId: uuid.NewString()},
			Mark:   xptr.Ptr("a"),
		}))
		assert.Equal(t, connect.CodeNotFound, connect.CodeOf(err))
	})

	t.Run("an agent takes a letter and reads it back off its row", func(t *testing.T) {
		svc, st := service(t)
		ctx := t.Context()
		id := st.SeedAgent(t, agentstore.Agent{Name: "alpha", State: agentstore.StateThinking}).ID

		_, err := svc.SetMark(ctx, connect.NewRequest(&agentv1.SetMarkRequest{
			Target: &agentv1.SetMarkRequest_AgentId{AgentId: id},
			Mark:   xptr.Ptr("a"),
		}))
		require.NoError(t, err)

		got, err := svc.GetAgent(ctx, connect.NewRequest(&agentv1.GetAgentRequest{Id: id}))
		require.NoError(t, err)
		assert.Equal(t, xptr.Ptr("a"), got.Msg.GetAgent().Mark)
	})

	t.Run("a letter an agent holds moves to the workflow named", func(t *testing.T) {
		svc, st := service(t)
		ctx := t.Context()
		held := st.SeedAgent(t, agentstore.Agent{Name: "alpha", State: agentstore.StateThinking})

		_, err := svc.SetMark(ctx, connect.NewRequest(&agentv1.SetMarkRequest{
			Target: &agentv1.SetMarkRequest_AgentId{AgentId: held.ID},
			Mark:   xptr.Ptr("a"),
		}))
		require.NoError(t, err)

		_, err = svc.SetMark(ctx, connect.NewRequest(&agentv1.SetMarkRequest{
			Target: &agentv1.SetMarkRequest_WorkflowId{WorkflowId: held.WorkflowID},
			Mark:   xptr.Ptr("a"),
		}))
		require.NoError(t, err)

		gone, err := svc.GetAgent(ctx, connect.NewRequest(&agentv1.GetAgentRequest{Id: held.ID}))
		require.NoError(t, err)
		assert.Nil(t, gone.Msg.GetAgent().Mark, "the agent that held it is left with nothing")
	})

	t.Run("an unset mark clears the one the row carries", func(t *testing.T) {
		svc, st := service(t)
		ctx := t.Context()
		id := st.SeedAgent(t, agentstore.Agent{Name: "alpha", State: agentstore.StateThinking}).ID

		_, err := svc.SetMark(ctx, connect.NewRequest(&agentv1.SetMarkRequest{
			Target: &agentv1.SetMarkRequest_AgentId{AgentId: id},
			Mark:   xptr.Ptr("a"),
		}))
		require.NoError(t, err)

		_, err = svc.SetMark(ctx, connect.NewRequest(&agentv1.SetMarkRequest{
			Target: &agentv1.SetMarkRequest_AgentId{AgentId: id},
		}))
		require.NoError(t, err)

		got, err := svc.GetAgent(ctx, connect.NewRequest(&agentv1.GetAgentRequest{Id: id}))
		require.NoError(t, err)
		assert.Nil(t, got.Msg.GetAgent().Mark)
	})
}

// TestListMarks covers the listing: every letter, by letter, each naming the one row it jumps
// to.
func TestListMarks(t *testing.T) {
	svc, st := service(t)
	ctx := t.Context()
	held := st.SeedAgent(t, agentstore.Agent{Name: "alpha", State: agentstore.StateThinking})

	_, err := svc.SetMark(ctx, connect.NewRequest(&agentv1.SetMarkRequest{
		Target: &agentv1.SetMarkRequest_WorkflowId{WorkflowId: held.WorkflowID},
		Mark:   xptr.Ptr("b"),
	}))
	require.NoError(t, err)
	_, err = svc.SetMark(ctx, connect.NewRequest(&agentv1.SetMarkRequest{
		Target: &agentv1.SetMarkRequest_AgentId{AgentId: held.ID},
		Mark:   xptr.Ptr("a"),
	}))
	require.NoError(t, err)

	listed, err := svc.ListMarks(ctx, connect.NewRequest(&emptypb.Empty{}))
	require.NoError(t, err)
	require.Len(t, listed.Msg.GetMarks(), 2)
	assert.Equal(t, "a", listed.Msg.GetMarks()[0].GetMark())
	assert.Equal(t, held.ID, listed.Msg.GetMarks()[0].GetAgentId())
	assert.Nil(t, listed.Msg.GetMarks()[0].WorkflowId)
	assert.Equal(t, "b", listed.Msg.GetMarks()[1].GetMark())
	assert.Equal(t, held.WorkflowID, listed.Msg.GetMarks()[1].GetWorkflowId())
	assert.Nil(t, listed.Msg.GetMarks()[1].AgentId)
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
			svc, st := service(t)

			id := uuid.NewString()
			if tt.seed {
				id = st.SeedAgent(t, agentstore.Agent{
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
				req.Script = &tt.script
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

// allSearchHits flattens both sections of a response, for the tests that care which turns
// came back rather than which way each one matched.
func allSearchHits(msg *agentv1.SearchTranscriptsResponse) []*agentv1.SearchHit {
	return append(append([]*agentv1.SearchHit{}, msg.GetMatches()...), msg.GetNear()...)
}

// TestSearchTranscripts covers every field on a hit.
func TestSearchTranscripts(t *testing.T) {
	tests := []struct {
		name     string
		turns    []string
		archive  bool
		query    string
		wantSeqs []int32
	}{
		{
			name:  "a query nothing said matches nothing",
			turns: []string{"talking about postgres"},
			query: "sailboats",
		},
		{
			name:     "a hit names the turn that matched rather than the first one",
			turns:    []string{"opening question", "the reconciler derives its markers"},
			query:    "reconciler",
			wantSeqs: []int32{1},
		},
		{
			name:     "an archived agent is still searchable, and says so",
			turns:    []string{"the reconciler derives its markers"},
			archive:  true,
			query:    "reconciler",
			wantSeqs: []int32{0},
		},
		{
			name:     "a subject raised twice is a hit per turn",
			turns:    []string{"first on postgres", "later on postgres again"},
			query:    "postgres",
			wantSeqs: []int32{0, 1},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, st := service(t)
			ctx := t.Context()

			agent := st.SeedAgent(t, agentstore.Agent{Name: "alpha", State: agentstore.StateThinking})
			content := seedTranscript(t, st, agent.ID, tt.turns)
			if tt.archive {
				_, archiveErr := st.Agents.Archive(ctx, []string{agent.ID})
				require.NoError(t, archiveErr)
			}

			found, err := svc.SearchTranscripts(ctx, connect.NewRequest(&agentv1.SearchTranscriptsRequest{
				Query: tt.query,
			}))
			require.NoError(t, err)
			require.Len(t, allSearchHits(found.Msg), len(tt.wantSeqs))

			seqs := make([]int32, 0, len(tt.wantSeqs))
			for _, hit := range allSearchHits(found.Msg) {
				seqs = append(seqs, hit.GetSeq())

				assert.Equal(t, agent.ID, hit.GetAgentId())
				assert.Equal(t, "alpha", hit.GetName(), "the hit carries the agent's name, not its id alone")
				assert.Equal(t, tt.archive, hit.GetArchived())
				assert.Equal(t, "user", hit.GetRole())
				assert.Contains(t, hit.GetSnippet(), tt.query, "the snippet holds the text that matched")

				turn := tt.turns[hit.GetSeq()]
				assert.Equal(t, turn, content[hit.GetContentOffset():hit.GetContentOffset()+int64(len(turn))])
			}

			assert.ElementsMatch(t, tt.wantSeqs, seqs)
		})
	}
}

// TestSearchTranscriptsScope covers who a search reaches, which the handler works out from
// the caller rather than from the request alone.
func TestSearchTranscriptsScope(t *testing.T) {
	tests := []struct {
		name      string
		callerIs  string
		asksFor   string
		wantNames []string
	}{
		{
			name:      "an agent that names nobody reads its own conversation",
			callerIs:  "alpha",
			wantNames: []string{"alpha"},
		},
		{
			name:      "an agent that names another reads that one",
			callerIs:  "alpha",
			asksFor:   "beta",
			wantNames: []string{"beta"},
		},
		{
			name:      "an agent that names itself is where it started",
			callerIs:  "alpha",
			asksFor:   "alpha",
			wantNames: []string{"alpha"},
		},
		{
			name:      "the panel is not an agent and reads every conversation",
			wantNames: []string{"alpha", "beta"},
		},
		{
			name:      "and can still narrow to one",
			asksFor:   "beta",
			wantNames: []string{"beta"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, st := service(t)

			ids := map[string]string{}
			for _, name := range []string{"alpha", "beta"} {
				agent := st.SeedAgent(t, agentstore.Agent{Name: name, State: agentstore.StateThinking})
				ids[name] = agent.ID
				seedTranscript(t, st, agent.ID, []string{"the reconciler derives its markers"})
			}

			ctx := t.Context()
			if tt.callerIs != "" {
				ctx = caller.With(ctx, caller.Caller{AgentID: ids[tt.callerIs]})
			}

			req := &agentv1.SearchTranscriptsRequest{Query: "reconciler"}
			if tt.asksFor != "" {
				req.AgentId = xptr.Ptr(ids[tt.asksFor])
			}

			found, err := svc.SearchTranscripts(ctx, connect.NewRequest(req))
			require.NoError(t, err)

			hits := allSearchHits(found.Msg)

			names := xslice.Map(hits, func(hit *agentv1.SearchHit) string {
				return hit.GetName()
			})
			assert.ElementsMatch(t, tt.wantNames, names)
		})
	}
}

// TestPagedReadsRefuseAMangledToken covers a token that decodes but holds an id no row can have.
func TestPagedReadsRefuseAMangledToken(t *testing.T) {
	const (
		listToken   = "eyJpIjoibm90LWFuLWlkIn0="
		searchToken = "eyJtIjp7InIiOjAuNSwiaSI6Im5vdC1hbi1pZCIsInMiOjF9fQ=="
	)

	tests := []struct {
		name  string
		token string
		call  func(t *testing.T, svc agentv1weave.AgentServiceAdapter, page *paginationv1.Pagination) error
	}{
		{
			name:  "listing agents",
			token: listToken,
			call: func(t *testing.T, svc agentv1weave.AgentServiceAdapter, page *paginationv1.Pagination) error {
				_, err := svc.ListAgents(t.Context(), connect.NewRequest(&agentv1.ListAgentsRequest{Pagination: page}))
				return err
			},
		},
		{
			name:  "searching transcripts",
			token: searchToken,
			call: func(t *testing.T, svc agentv1weave.AgentServiceAdapter, page *paginationv1.Pagination) error {
				_, err := svc.SearchTranscripts(t.Context(), connect.NewRequest(&agentv1.SearchTranscriptsRequest{
					Query:      "reconciler",
					Pagination: page,
				}))

				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, _ := service(t)

			err := tt.call(t, svc, &paginationv1.Pagination{Cursor: proto.String(tt.token)})
			require.Error(t, err)
			assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
		})
	}
}

// TestSearchTranscriptsScopeSurvivesACursor covers the second page of a scoped search,
// which a replayed token must not widen.
func TestSearchTranscriptsScopeSurvivesACursor(t *testing.T) {
	svc, st := service(t)

	ids := map[string]string{}
	for _, name := range []string{"alpha", "beta"} {
		agent := st.SeedAgent(t, agentstore.Agent{Name: name, State: agentstore.StateThinking})
		ids[name] = agent.ID
		seedTranscript(t, st, agent.ID, []string{"the reconciler derives its markers", "more on the reconciler"})
	}

	ctx := caller.With(t.Context(), caller.Caller{AgentID: ids["alpha"]})

	var (
		names []string
		token *string
	)
	for range 4 {
		req := &agentv1.SearchTranscriptsRequest{
			Query:      "reconciler",
			Pagination: &paginationv1.Pagination{Limit: proto.Int32(1), Cursor: token},
		}

		found, err := svc.SearchTranscripts(ctx, connect.NewRequest(req))
		require.NoError(t, err)
		if len(allSearchHits(found.Msg)) == 0 {
			break
		}

		names = append(names, xslice.Map(allSearchHits(found.Msg), func(hit *agentv1.SearchHit) string {
			return hit.GetName()
		})...)

		token = found.Msg.NextPageToken
		if token == nil {
			break
		}
	}

	assert.Equal(t, []string{"alpha", "alpha"}, names, "both of its own turns, and neither of the other agent's")
}

// TestCrossAgentReadIsRecorded covers the line a read of somebody else's conversation
// leaves, and the silence a read of your own does.
func TestCrossAgentReadIsRecorded(t *testing.T) {
	tests := []struct {
		name       string
		callerIs   string
		read       func(t *testing.T, svc agentv1weave.AgentServiceAdapter, ctx context.Context, target string)
		wantRecord bool
	}{
		{
			name:     "an agent searching another agent",
			callerIs: "alpha",
			read: func(t *testing.T, svc agentv1weave.AgentServiceAdapter, ctx context.Context, target string) {
				t.Helper()
				_, err := svc.SearchTranscripts(ctx, connect.NewRequest(&agentv1.SearchTranscriptsRequest{
					Query:   "reconciler",
					AgentId: xptr.Ptr(target),
				}))
				require.NoError(t, err)
			},
			wantRecord: true,
		},
		{
			name:     "an agent fetching another agent's whole conversation",
			callerIs: "alpha",
			read: func(t *testing.T, svc agentv1weave.AgentServiceAdapter, ctx context.Context, target string) {
				t.Helper()
				_, err := svc.GetTranscript(ctx, connect.NewRequest(&agentv1.GetTranscriptRequest{AgentId: target}))
				require.NoError(t, err)
			},
			wantRecord: true,
		},
		{
			name:     "an agent fetching another agent's last turns",
			callerIs: "alpha",
			read: func(t *testing.T, svc agentv1weave.AgentServiceAdapter, ctx context.Context, target string) {
				t.Helper()
				_, err := svc.GetLastTurns(ctx, connect.NewRequest(&agentv1.GetLastTurnsRequest{AgentId: target}))
				require.NoError(t, err)
			},
			wantRecord: true,
		},
		{
			name:     "an agent searching itself",
			callerIs: "beta",
			read: func(t *testing.T, svc agentv1weave.AgentServiceAdapter, ctx context.Context, _ string) {
				t.Helper()
				_, err := svc.SearchTranscripts(ctx, connect.NewRequest(&agentv1.SearchTranscriptsRequest{Query: "reconciler"}))
				require.NoError(t, err)
			},
		},
		{
			name: "the panel reading anyone, which is what it is for",
			read: func(t *testing.T, svc agentv1weave.AgentServiceAdapter, ctx context.Context, target string) {
				t.Helper()
				_, err := svc.GetTranscript(ctx, connect.NewRequest(&agentv1.GetTranscriptRequest{AgentId: target}))
				require.NoError(t, err)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, st := service(t)

			ids := map[string]string{}
			for _, name := range []string{"alpha", "beta"} {
				agent := st.SeedAgent(t, agentstore.Agent{Name: name, State: agentstore.StateThinking})
				ids[name] = agent.ID
				seedTranscript(t, st, agent.ID, []string{"the reconciler derives its markers"})
			}

			var written strings.Builder
			ctx := logging.WithLogger(t.Context(), logging.New(&written, slog.LevelInfo))
			if tt.callerIs != "" {
				ctx = caller.With(ctx, caller.Caller{AgentID: ids[tt.callerIs]})
			}

			tt.read(t, svc, ctx, ids["beta"])

			if !tt.wantRecord {
				assert.Empty(t, written.String(), "reading your own conversation is not worth a line")
				return
			}

			record := logtest.Record(t, written.String())
			assert.Equal(t, "agent read another agent's transcript", record["body"])
			assert.Equal(t, ids["beta"], record[logging.TargetAgentID])
		})
	}
}

// TestGetTranscript covers the agent being looked up first, an unknown agent and one with
// nothing synced yet both reading as empty content.
func TestGetTranscript(t *testing.T) {
	tests := []struct {
		name     string
		agentID  string
		turns    []string
		wantCode connect.Code
		want     string
	}{
		{
			name:     "an id no agent holds is refused",
			agentID:  uuid.NewString(),
			wantCode: connect.CodeNotFound,
		},
		{
			name:     "and so is an id that is not an id",
			agentID:  "not-a-uuid",
			wantCode: connect.CodeNotFound,
		},
		{
			name: "an agent with nothing synced yet is empty rather than missing",
			want: "",
		},
		{
			name:  "a synced agent hands back the whole conversation",
			turns: []string{"opening question", "the answer"},
			want:  "opening question\n\nthe answer",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, st := service(t)

			agent := st.SeedAgent(t, agentstore.Agent{Name: "alpha", State: agentstore.StateThinking})
			seedTranscript(t, st, agent.ID, tt.turns)

			id := tt.agentID
			if id == "" {
				id = agent.ID
			}

			got, err := svc.GetTranscript(t.Context(), connect.NewRequest(&agentv1.GetTranscriptRequest{AgentId: id}))
			if tt.wantCode != 0 {
				assert.Equal(t, tt.wantCode, connect.CodeOf(err))
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got.Msg.GetContent())
		})
	}
}

// fakeLauncher stands in for every agent service the handler calls, recording each request.
type fakeLauncher struct {
	t            *testing.T
	st           *storetest.Stores
	err          error
	alive        bool
	spawnedAgent *agentstore.Agent
	created      agents.NewAgentParams
	reviewed     agents.RequestReviewParams
	adopted      agents.AdoptParams
	retried      agents.RetryCreateParams
	released     agents.ReleaseParams
	metadata     agents.SetMetadataParams
	reported     agents.RecordReportParams
	resumed      []string
	held         []string
	orchestrated []string
	deleted      []string
	archived     []string
	restored     []string
	runsRead     []string
	renamed      map[string]string
	hidden       map[string]bool
	emptied      int
}

// spawn writes the agent the fake answers with to the store.
func (f *fakeLauncher) spawn() *agentstore.Agent {
	if f.spawnedAgent == nil {
		f.spawnedAgent = f.st.SeedAgent(f.t, agentstore.Agent{
			Name:  "spawned",
			Cwd:   "/repo",
			State: agentstore.StateIdle,
		})
	}

	return f.spawnedAgent
}

// answer returns the spawned agent, or err in its place unless err is a spent idempotency key.
func (f *fakeLauncher) answer() (*agentstore.Agent, error) {
	if f.err != nil && !errors.Is(f.err, agents.ErrAlreadyLaunched) {
		return nil, f.err
	}

	return f.spawn(), f.err
}

func (f *fakeLauncher) NewAgent(_ context.Context, params agents.NewAgentParams) (*agentstore.Agent, error) {
	f.created = params

	return f.answer()
}

func (f *fakeLauncher) RequestReview(_ context.Context, params agents.RequestReviewParams) (*agentstore.Agent, string, error) {
	f.reviewed = params
	reviewer, err := f.answer()

	return reviewer, "", err
}

func (f *fakeLauncher) RetryCreate(_ context.Context, params agents.RetryCreateParams) (*agentstore.Agent, error) {
	f.retried = params

	return f.answer()
}

func (f *fakeLauncher) Adopt(_ context.Context, params agents.AdoptParams) (*agentstore.Agent, error) {
	f.adopted = params

	return f.answer()
}

// IsAgentRunning answers for the fake's agents, which are live exactly when the test says so.
func (f *fakeLauncher) IsAgentRunning(agentstore.Agent) bool {
	return f.alive
}

func (f *fakeLauncher) ResumeAgent(_ context.Context, id string) (*agentstore.Agent, error) {
	f.resumed = append(f.resumed, id)

	return f.answer()
}

func (f *fakeLauncher) Hold(_ context.Context, agentID string) (*agentstore.Agent, error) {
	f.held = append(f.held, agentID)

	return f.answer()
}

func (f *fakeLauncher) Release(_ context.Context, params agents.ReleaseParams) (*agentstore.Agent, error) {
	f.released = params

	return f.answer()
}

func (f *fakeLauncher) Start(_ context.Context, workflowID string) (*agentstore.Agent, error) {
	f.orchestrated = append(f.orchestrated, workflowID)

	return f.answer()
}

func (f *fakeLauncher) DeleteAgents(_ context.Context, ids []string) error {
	f.deleted = append(f.deleted, ids...)

	return f.err
}

func (f *fakeLauncher) ArchiveAgents(_ context.Context, ids []string) error {
	f.archived = append(f.archived, ids...)

	return f.err
}

// RestoreAgents answers with an agent for every id.
func (f *fakeLauncher) RestoreAgents(_ context.Context, ids []string) ([]agentstore.Agent, error) {
	f.restored = append(f.restored, ids...)
	if f.err != nil {
		return nil, f.err
	}

	return xslice.Map(ids, func(id string) agentstore.Agent {
		return agentstore.Agent{ID: id}
	}), nil
}

func (f *fakeLauncher) EmptyArchive(context.Context) error {
	f.emptied++

	return f.err
}

func (f *fakeLauncher) SetHidden(_ context.Context, id string, hidden bool) error {
	if f.hidden == nil {
		f.hidden = map[string]bool{}
	}
	f.hidden[id] = hidden

	return f.err
}

func (f *fakeLauncher) SetMetadata(_ context.Context, params agents.SetMetadataParams) error {
	f.metadata = params

	return f.err
}

func (f *fakeLauncher) Rename(_ context.Context, id, name string) error {
	if f.renamed == nil {
		f.renamed = map[string]string{}
	}
	f.renamed[id] = name

	return f.err
}

func (f *fakeLauncher) MarkRunsRead(_ context.Context, id string) error {
	f.runsRead = append(f.runsRead, id)

	return f.err
}

func (f *fakeLauncher) RecordReport(_ context.Context, params agents.RecordReportParams) error {
	f.reported = params

	return f.err
}

func adoptMain(ctx context.Context, svc agentv1weave.AgentServiceAdapter) error {
	_, err := svc.Adopt(ctx, connect.NewRequest(&agentv1.AdoptRequest{
		Ref:        "main",
		WorkflowId: "wf-1",
	}))

	return err
}

func restoreAlpha(ctx context.Context, svc agentv1weave.AgentServiceAdapter) error {
	_, err := svc.RestoreAgents(ctx, connect.NewRequest(&agentv1.RestoreAgentsRequest{Ids: []string{alphaID}}))

	return err
}

// seedFailedRun records a script that failed for the agent, in the fourth run log.
func seedFailedRun(t *testing.T, st *storetest.Stores, agentID string) {
	t.Helper()

	id, err := st.Runs.Begin(t.Context(), runstore.BeginParams{
		AgentID:   agentID,
		Trigger:   "post-launch",
		Script:    "install-deps",
		LogNumber: xptr.Ptr(int32(4)),
	})
	require.NoError(t, err)
	require.NoError(t, st.Runs.Finish(t.Context(), runstore.FinishParams{
		ID:      id,
		Failure: "a hook failed",
	}))
}

// markService is the real thing over the test stores, a mark spanning two tables a fake would
// only agree with itself about.
func markService(st *storetest.Stores) *marks.MarkService {
	return marks.NewMarkService(marks.NewMarkServiceParams{Marks: st.Marks, Tx: st.Pool})
}

func service(t *testing.T) (agentv1weave.AgentServiceAdapter, *storetest.Stores) {
	t.Helper()
	svc, st, _ := serviceWithLauncher(t)

	return svc, st
}

func seedTranscript(t *testing.T, st *storetest.Stores, agentID string, turns []string) string {
	t.Helper()

	if len(turns) == 0 {
		return ""
	}

	st.SeedTranscript(t, agentID, turns...)

	content, err := st.Transcripts.Get(t.Context(), agentID)
	require.NoError(t, err)

	return content
}

func serviceWithLauncher(t *testing.T) (agentv1weave.AgentServiceAdapter, *storetest.Stores, *fakeLauncher) {
	t.Helper()

	st := storetest.Open(t)
	launcher := &fakeLauncher{t: t, st: st}

	mailer := mail.NewMailService(mail.NewMailServiceParams{
		MailReadWriter: st.Mail,
		Agents:         st.Agents,
		Workflows:      st.Workflows,
		Config:         config.Mail{},
	})

	rounds := reviewstore.NewReviewStore(st.Pool.GetDB())
	review := reviews.NewReviewService(reviews.NewReviewServiceParams{
		Threads:   rounds,
		Messages:  rounds,
		Viewed:    rounds,
		Rounds:    rounds,
		Reviews:   rounds,
		Snapshots: fakeSnapshotter{},
		Workflows: st.Workflows,
		StateDir:  t.TempDir(),
		Agents:    st.Agents,
		Mailer:    mailer,
	})

	return agentv1weave.AgentServiceAdapter{Impl: NewHandler(NewHandlerParams{
		Agents:      st.Agents,
		Rows:        st.Rows,
		Writer:      launcher,
		Transcripts: st.Transcripts,
		Mail:        mailer,
		Review:      review,
		Marks:       markService(st),
		Watched: WatchedListers{
			Marks:        st.Marks,
			Mail:         st.Mail,
			PullRequests: st.PullRequests,
			Repositories: st.PullRequestSyncs,
			Reviews:      rounds,
			Workflows:    st.Workflows,
		},
		Creator:      launcher,
		Adopter:      launcher,
		Liveness:     launcher,
		Lifecycle:    launcher,
		Resumer:      launcher,
		Holder:       launcher,
		Archive:      launcher,
		Orchestrator: launcher,
	})}, st, launcher
}
