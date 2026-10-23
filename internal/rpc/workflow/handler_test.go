package workflow

import (
	"context"
	"strings"
	"testing"

	"komrad/internal/agents"
	"komrad/internal/caller"
	"komrad/internal/eventprompts"
	"komrad/internal/rpc/render"
	workflowstore "komrad/internal/store/workflow"
	"komrad/lib/protoenum"
	"komrad/lib/xptr"
	eventv1 "komrad/proto/gen/event/v1"
	paginationv1 "komrad/proto/gen/pagination/v1"
	viewv1 "komrad/proto/gen/view/v1"
	workflowv1 "komrad/proto/gen/workflow/v1"
	workflowv1komrad "komrad/proto/gen/workflow/v1/workflowv1komrad"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var workflowID = uuid.MustParse("01a0d228-0000-7000-8000-0000000000aa")

type updateCall struct {
	id  uuid.UUID
	cfg workflowstore.Config
}

// fakeWorkflows answers from its fields and records what each call asked of it.
type fakeWorkflows struct {
	stored  []workflowstore.Workflow
	created []workflowstore.Config
	updated []updateCall
	deleted []agents.DeleteWorkflowParams
	listed  []bool
}

// NewWorkflow records the config and answers it as stored.
func (f *fakeWorkflows) NewWorkflow(_ context.Context, cfg workflowstore.Config) (*workflowstore.Workflow, error) {
	f.created = append(f.created, cfg)

	return &workflowstore.Workflow{
		ID:       workflowID,
		Name:     cfg.Name,
		Worktree: cfg.Worktree,
	}, nil
}

// GetWorkflow answers the first stored workflow.
func (f *fakeWorkflows) GetWorkflow(context.Context, uuid.UUID) (*workflowstore.Workflow, error) {
	return &f.stored[0], nil
}

// UpdateWorkflow records the update.
func (f *fakeWorkflows) UpdateWorkflow(_ context.Context, id uuid.UUID, cfg workflowstore.Config) error {
	f.updated = append(f.updated, updateCall{
		id:  id,
		cfg: cfg,
	})

	return nil
}

// ArchiveWorkflow answers nil.
func (f *fakeWorkflows) ArchiveWorkflow(context.Context, uuid.UUID) error {
	return nil
}

// RestoreWorkflow answers the first stored workflow.
func (f *fakeWorkflows) RestoreWorkflow(context.Context, uuid.UUID) (*workflowstore.Workflow, error) {
	return &f.stored[0], nil
}

// DeleteWorkflow records the params.
func (f *fakeWorkflows) DeleteWorkflow(_ context.Context, params agents.DeleteWorkflowParams) error {
	f.deleted = append(f.deleted, params)

	return nil
}

// ListWorkflows records which set the caller asked for and answers as many stored workflows as the limit allows.
func (f *fakeWorkflows) ListWorkflows(_ context.Context, params agents.ListWorkflowsParams) ([]workflowstore.Workflow, error) {
	f.listed = append(f.listed, params.Archived)

	return f.stored[:min(int(params.Limit), len(f.stored))], nil
}

// TestWorkflowRefusalsMapToCodes pins the code each refusal of the workflow service reaches the caller as.
func TestWorkflowRefusalsMapToCodes(t *testing.T) {
	tests := []struct {
		name     string
		call     func(ctx context.Context, svc workflowv1komrad.WorkflowServiceAdapter) error
		wantCode connect.Code
	}{
		{
			name: "listing with a page token that holds no id",
			call: func(ctx context.Context, svc workflowv1komrad.WorkflowServiceAdapter) error {
				_, err := svc.ListWorkflows(ctx, connect.NewRequest(&workflowv1.ListWorkflowsRequest{
					Pagination: &paginationv1.Pagination{Cursor: xptr.Ptr("eyJuIjoiYWxwaGEiLCJpIjoibm90LWFuLWlkIn0=")},
				}))

				return err
			},
			wantCode: connect.CodeInvalidArgument,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := newWorkflowHandler(&fakeWorkflows{})

			err := tt.call(t.Context(), svc)
			assert.Equal(t, tt.wantCode, connect.CodeOf(err))
		})
	}
}

// TestWorkflowWritesPassDown pins what each write hands the workflow service.
func TestWorkflowWritesPassDown(t *testing.T) {
	cfg := workflowstore.Config{
		Name:            "backend",
		Context:         "shared notes",
		Harness:         "codex",
		Cwd:             "/repo",
		Flags:           "--model opus",
		ReviewerHarness: "claude",
		ReviewerFlags:   "--model sonnet",
		Worktree: workflowstore.WorktreeConfig{
			Enabled:      true,
			BaseBranch:   "develop",
			Dir:          "/repo/.worktrees",
			SetupCommand: "mise install",
			CopyPaths:    []string{".env"},
			SymlinkPaths: []string{"node_modules"},
			DiffExclude:  []string{"proto/gen"},
		},
		Hooks: []workflowstore.Hook{
			{
				Event:  eventv1.Moment_MOMENT_AGENT_PRE_LAUNCH,
				Script: "warm-cache",
			},
		},
		EventPrompts: []workflowstore.EventPrompt{{Event: eventv1.Event_EVENT_REVIEW_APPROVED, Prompt: "open the pull request"}, {Event: eventv1.Event_EVENT_PULL_REQUEST_BEHIND, Muted: true}},
	}
	worktree := &workflowv1.WorktreeConfig{
		Enabled:      true,
		BaseBranch:   "develop",
		Dir:          "/repo/.worktrees",
		SetupCommand: "mise install",
		CopyPaths:    []string{".env"},
		SymlinkPaths: []string{"node_modules"},
		DiffExclude:  []string{"proto/gen"},
	}
	hooks := []*workflowv1.WorkflowHook{
		{
			Event:  eventv1.Moment_MOMENT_AGENT_PRE_LAUNCH,
			Script: "warm-cache",
		},
	}
	prompts := []*workflowv1.WorkflowEventPrompt{{Event: eventv1.Event_EVENT_REVIEW_APPROVED, Prompt: "open the pull request"}, {Event: eventv1.Event_EVENT_PULL_REQUEST_BEHIND, Muted: true}}

	tests := []struct {
		name string
		call func(ctx context.Context, svc workflowv1komrad.WorkflowServiceAdapter) error
		want fakeWorkflows
	}{
		{
			name: "creating hands down every field",
			call: func(ctx context.Context, svc workflowv1komrad.WorkflowServiceAdapter) error {
				_, err := svc.CreateWorkflow(ctx, connect.NewRequest(&workflowv1.CreateWorkflowRequest{
					Name:            cfg.Name,
					Context:         cfg.Context,
					Harness:         cfg.Harness,
					Cwd:             cfg.Cwd,
					Flags:           cfg.Flags,
					ReviewerHarness: cfg.ReviewerHarness,
					ReviewerFlags:   cfg.ReviewerFlags,
					Worktree:        worktree,
					Hooks:           hooks,
					EventPrompts:    &workflowv1.WorkflowEventPrompts{Prompts: prompts},
				}))

				return err
			},
			want: fakeWorkflows{created: []workflowstore.Config{cfg}},
		},
		{
			name: "updating hands down the id and every field",
			call: func(ctx context.Context, svc workflowv1komrad.WorkflowServiceAdapter) error {
				_, err := svc.UpdateWorkflow(ctx, connect.NewRequest(&workflowv1.UpdateWorkflowRequest{
					Id:              workflowID.String(),
					Name:            cfg.Name,
					Context:         cfg.Context,
					Harness:         cfg.Harness,
					Cwd:             cfg.Cwd,
					Flags:           cfg.Flags,
					ReviewerHarness: cfg.ReviewerHarness,
					ReviewerFlags:   cfg.ReviewerFlags,
					Worktree:        worktree,
					Hooks:           hooks,
					EventPrompts:    prompts,
				}))

				return err
			},
			want: fakeWorkflows{updated: []updateCall{
				{
					id:  workflowID,
					cfg: cfg,
				},
			}},
		},
		{
			name: "deleting hands down the id, the records choice and the agent asking",
			call: func(ctx context.Context, svc workflowv1komrad.WorkflowServiceAdapter) error {
				_, err := svc.DeleteWorkflow(caller.With(ctx, caller.Caller{AgentID: firstAgentID}), connect.NewRequest(&workflowv1.DeleteWorkflowRequest{
					Id:      workflowID.String(),
					Records: true,
				}))

				return err
			},
			want: fakeWorkflows{deleted: []agents.DeleteWorkflowParams{
				{
					ID:        workflowID,
					Records:   true,
					ByAgentID: firstAgentID,
				},
			}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorded := &fakeWorkflows{}

			require.NoError(t, tt.call(t.Context(), newWorkflowHandler(recorded)))
			assert.Equal(t, tt.want, *recorded)
		})
	}
}

// TestCreatingAWorkflowWithoutPromptsGivesTheDefaults pins that a create leaving its prompts unset
// hands down every event's default, and one sending an empty list hands down none.
func TestCreatingAWorkflowWithoutPromptsGivesTheDefaults(t *testing.T) {
	tests := []struct {
		name         string
		prompts      *workflowv1.WorkflowEventPrompts
		wantCount    int
		wantApproved string
	}{
		{
			name:         "prompts left unset take each event's default",
			wantCount:    len(protoenum.Values[eventv1.Event]()),
			wantApproved: "The reviewer is finished. Carry on with your next step.",
		},
		{
			name:    "an empty list takes none",
			prompts: &workflowv1.WorkflowEventPrompts{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorded := &fakeWorkflows{}

			_, err := newWorkflowHandler(recorded).CreateWorkflow(t.Context(), connect.NewRequest(&workflowv1.CreateWorkflowRequest{
				Name:         "backend",
				EventPrompts: tt.prompts,
			}))
			require.NoError(t, err)

			require.Len(t, recorded.created, 1)
			assert.Len(t, recorded.created[0].EventPrompts, tt.wantCount)
			assert.Equal(t, tt.wantApproved, eventprompts.Prompts(recorded.created[0].EventPrompts).Get(eventv1.Event_EVENT_REVIEW_APPROVED))
		})
	}
}

// TestListWorkflows pins which set a listing reads, which view draws the whole context, and when a page hands back a token.
func TestListWorkflows(t *testing.T) {
	long := strings.Repeat("c", render.SummaryHead+50)

	tests := []struct {
		name           string
		req            *workflowv1.ListWorkflowsRequest
		wantArchived   bool
		wantContexts   []string
		wantTruncated  []bool
		wantNextCursor bool
	}{
		{
			name:          "the default view draws the head of a long context",
			req:           &workflowv1.ListWorkflowsRequest{},
			wantContexts:  []string{long[:render.SummaryHead], "ship it"},
			wantTruncated: []bool{true, false},
		},
		{
			name: "the full view draws the whole of it",
			req: &workflowv1.ListWorkflowsRequest{
				View: viewv1.View_VIEW_FULL,
			},
			wantContexts:  []string{long, "ship it"},
			wantTruncated: []bool{false, false},
		},
		{
			name: "the archived flag reads the archive",
			req: &workflowv1.ListWorkflowsRequest{
				Archived: true,
				View:     viewv1.View_VIEW_FULL,
			},
			wantArchived:  true,
			wantContexts:  []string{long, "ship it"},
			wantTruncated: []bool{false, false},
		},
		{
			name: "a full page hands back a token for the next",
			req: &workflowv1.ListWorkflowsRequest{
				Pagination: &paginationv1.Pagination{Limit: xptr.Ptr[int32](1)},
				View:       viewv1.View_VIEW_FULL,
			},
			wantContexts:   []string{long},
			wantTruncated:  []bool{false},
			wantNextCursor: true,
		},
		{
			name: "a token a listing handed back is accepted",
			req: &workflowv1.ListWorkflowsRequest{
				Pagination: &paginationv1.Pagination{
					Limit:  xptr.Ptr[int32](1),
					Cursor: xptr.Ptr("eyJuIjoiYWxwaGEiLCJpIjoiMDFhMGQyMjgtMDAwMC03MDAwLTgwMDAtMDAwMDAwMDAwMGFhIn0="),
				},
				View: viewv1.View_VIEW_FULL,
			},
			wantContexts:   []string{long},
			wantTruncated:  []bool{false},
			wantNextCursor: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorded := &fakeWorkflows{stored: []workflowstore.Workflow{
				{
					ID:      workflowID,
					Name:    "alpha",
					Context: long,
				},
				{
					ID:      uuid.MustParse("01a0d228-0000-7000-8000-0000000000bb"),
					Name:    "beta",
					Context: "ship it",
				},
			}}
			svc := newWorkflowHandler(recorded)

			listed, err := svc.ListWorkflows(t.Context(), connect.NewRequest(tt.req))
			require.NoError(t, err)
			assert.Equal(t, []bool{tt.wantArchived}, recorded.listed)

			contexts := make([]string, 0, len(listed.Msg.GetWorkflows()))
			truncated := make([]bool, 0, len(listed.Msg.GetWorkflows()))
			for _, workflow := range listed.Msg.GetWorkflows() {
				contexts = append(contexts, workflow.GetContext())
				truncated = append(truncated, workflow.GetContextTruncated())
			}
			assert.Equal(t, tt.wantContexts, contexts)
			assert.Equal(t, tt.wantTruncated, truncated)
			assert.Equal(t, tt.wantNextCursor, listed.Msg.GetNextPageToken() != "")
		})
	}
}

func newWorkflowHandler(fake *fakeWorkflows) workflowv1komrad.WorkflowServiceAdapter {
	return workflowv1komrad.WorkflowServiceAdapter{Impl: NewHandler(NewHandlerParams{Workflows: fake})}
}

var (
	firstAgentID = uuid.New()
)
