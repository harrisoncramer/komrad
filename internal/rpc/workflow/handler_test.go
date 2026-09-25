package workflow

import (
	"context"
	"errors"
	"strings"
	"testing"

	"komrad/internal/agents"
	"komrad/internal/rpc/render"
	"komrad/internal/store"
	paginationv1 "komrad/proto/gen/pagination/v1"
	viewv1 "komrad/proto/gen/view/v1"
	workflowv1 "komrad/proto/gen/workflow/v1"
	workflowv1komrad "komrad/proto/gen/workflow/v1/workflowv1komrad"

	workflowstore "komrad/internal/store/workflow"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

const workflowID = "01a0d228-0000-7000-8000-0000000000aa"

type updateCall struct {
	id  string
	cfg workflowstore.Config
}

// fakeWorkflows answers from its fields and records what each call asked of it.
type fakeWorkflows struct {
	stored   []workflowstore.Workflow
	err      error
	created  []workflowstore.Config
	updated  []updateCall
	archived []string
	restored []string
	deleted  []string
	listed   []bool
}

// NewWorkflow records the config and answers it as stored unless err refuses it.
func (f *fakeWorkflows) NewWorkflow(_ context.Context, cfg workflowstore.Config) (*workflowstore.Workflow, error) {
	f.created = append(f.created, cfg)
	if f.err != nil {
		return nil, f.err
	}

	return &workflowstore.Workflow{
		ID:       workflowID,
		Name:     cfg.Name,
		Worktree: cfg.Worktree,
	}, nil
}

// GetWorkflow answers the first stored workflow unless err refuses it.
func (f *fakeWorkflows) GetWorkflow(context.Context, string) (*workflowstore.Workflow, error) {
	if f.err != nil {
		return nil, f.err
	}

	return &f.stored[0], nil
}

// UpdateWorkflow records the update and answers err.
func (f *fakeWorkflows) UpdateWorkflow(_ context.Context, id string, cfg workflowstore.Config) error {
	f.updated = append(f.updated, updateCall{
		id:  id,
		cfg: cfg,
	})

	return f.err
}

// ArchiveWorkflow records the id and answers err.
func (f *fakeWorkflows) ArchiveWorkflow(_ context.Context, id string) error {
	f.archived = append(f.archived, id)

	return f.err
}

// RestoreWorkflow records the id and answers the first stored workflow unless err refuses it.
func (f *fakeWorkflows) RestoreWorkflow(_ context.Context, id string) (*workflowstore.Workflow, error) {
	f.restored = append(f.restored, id)
	if f.err != nil {
		return nil, f.err
	}

	return &f.stored[0], nil
}

// DeleteWorkflow records the id and answers err.
func (f *fakeWorkflows) DeleteWorkflow(_ context.Context, id string) error {
	f.deleted = append(f.deleted, id)

	return f.err
}

// ListWorkflows records which set was asked for and answers as many stored workflows as the limit allows.
func (f *fakeWorkflows) ListWorkflows(_ context.Context, params agents.ListWorkflowsParams) ([]workflowstore.Workflow, error) {
	f.listed = append(f.listed, params.Archived)
	if f.err != nil {
		return nil, f.err
	}

	return f.stored[:min(int(params.Limit), len(f.stored))], nil
}

// TestWorkflowRefusalsMapToCodes pins the code each refusal of the workflow service reaches the caller as.
func TestWorkflowRefusalsMapToCodes(t *testing.T) {
	create := func(ctx context.Context, svc workflowv1komrad.WorkflowServiceAdapter) error {
		_, err := svc.CreateWorkflow(ctx, connect.NewRequest(&workflowv1.CreateWorkflowRequest{Name: "backend"}))

		return err
	}
	update := func(ctx context.Context, svc workflowv1komrad.WorkflowServiceAdapter) error {
		_, err := svc.UpdateWorkflow(ctx, connect.NewRequest(&workflowv1.UpdateWorkflowRequest{Id: workflowID}))

		return err
	}
	archive := func(ctx context.Context, svc workflowv1komrad.WorkflowServiceAdapter) error {
		_, err := svc.ArchiveWorkflow(ctx, connect.NewRequest(&workflowv1.ArchiveWorkflowRequest{Id: workflowID}))

		return err
	}
	restore := func(ctx context.Context, svc workflowv1komrad.WorkflowServiceAdapter) error {
		_, err := svc.RestoreWorkflow(ctx, connect.NewRequest(&workflowv1.RestoreWorkflowRequest{Id: workflowID}))

		return err
	}
	deleteWorkflow := func(ctx context.Context, svc workflowv1komrad.WorkflowServiceAdapter) error {
		_, err := svc.DeleteWorkflow(ctx, connect.NewRequest(&workflowv1.DeleteWorkflowRequest{Id: workflowID}))

		return err
	}

	tests := []struct {
		name     string
		call     func(ctx context.Context, svc workflowv1komrad.WorkflowServiceAdapter) error
		err      error
		wantCode connect.Code
	}{
		{
			name:     "a worktree workflow with no cwd",
			call:     create,
			err:      agents.ErrWorktreeNeedsCwd,
			wantCode: connect.CodeInvalidArgument,
		},
		{
			name:     "a cwd that is not a primary checkout",
			call:     create,
			err:      agents.ErrWorktreeCwdNotPrimaryCheckout,
			wantCode: connect.CodeInvalidArgument,
		},
		{
			name:     "a copy path outside the repo",
			call:     update,
			err:      agents.ErrWorktreeUnsafePath,
			wantCode: connect.CodeInvalidArgument,
		},
		{
			name:     "a blank base branch",
			call:     update,
			err:      agents.ErrWorktreeNeedsBase,
			wantCode: connect.CodeInvalidArgument,
		},
		{
			name:     "a hook on a moment komrad never fires",
			call:     create,
			err:      agents.ErrUnknownHookEvent,
			wantCode: connect.CodeInvalidArgument,
		},
		{
			name:     "a name a live workflow holds",
			call:     create,
			err:      store.ErrNameTaken,
			wantCode: connect.CodeAlreadyExists,
		},
		{
			name: "reading a workflow nothing holds",
			call: func(ctx context.Context, svc workflowv1komrad.WorkflowServiceAdapter) error {
				_, err := svc.GetWorkflow(ctx, connect.NewRequest(&workflowv1.GetWorkflowRequest{Id: workflowID}))

				return err
			},
			err:      store.ErrNotFound,
			wantCode: connect.CodeNotFound,
		},
		{
			name:     "updating a workflow nothing holds",
			call:     update,
			err:      agents.ErrWorkflowNotFound,
			wantCode: connect.CodeNotFound,
		},
		{
			name:     "archiving a workflow nothing holds",
			call:     archive,
			err:      agents.ErrWorkflowNotFound,
			wantCode: connect.CodeNotFound,
		},
		{
			name:     "archiving a workflow still scheduled into",
			call:     archive,
			err:      agents.ErrWorkflowHasSchedules,
			wantCode: connect.CodeFailedPrecondition,
		},
		{
			name:     "restoring a workflow that is not archived",
			call:     restore,
			err:      agents.ErrWorkflowNotFound,
			wantCode: connect.CodeNotFound,
		},
		{
			name:     "restoring past the agent ceiling",
			call:     restore,
			err:      agents.ErrTooManyAgents,
			wantCode: connect.CodeResourceExhausted,
		},
		{
			name:     "deleting a workflow that is not archived",
			call:     deleteWorkflow,
			err:      agents.ErrWorkflowNotFound,
			wantCode: connect.CodeNotFound,
		},
		{
			name:     "deleting a workflow whose schedule another workflow's agents hold",
			call:     deleteWorkflow,
			err:      agents.ErrScheduleHasAgents,
			wantCode: connect.CodeFailedPrecondition,
		},
		{
			name: "listing with a page token that holds no id",
			call: func(ctx context.Context, svc workflowv1komrad.WorkflowServiceAdapter) error {
				_, err := svc.ListWorkflows(ctx, connect.NewRequest(&workflowv1.ListWorkflowsRequest{
					Pagination: &paginationv1.Pagination{Cursor: proto.String("eyJuIjoiYWxwaGEiLCJpIjoibm90LWFuLWlkIn0=")},
				}))

				return err
			},
			wantCode: connect.CodeInvalidArgument,
		},
		{
			name: "a listing the store could not read",
			call: func(ctx context.Context, svc workflowv1komrad.WorkflowServiceAdapter) error {
				_, err := svc.ListWorkflows(ctx, connect.NewRequest(&workflowv1.ListWorkflowsRequest{}))

				return err
			},
			err:      errors.New("connection refused"),
			wantCode: connect.CodeInternal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := newWorkflowHandler(&fakeWorkflows{err: tt.err})

			err := tt.call(t.Context(), svc)
			assert.Equal(t, tt.wantCode, connect.CodeOf(err))
			if tt.err != nil {
				require.ErrorIs(t, err, tt.err)
			}
		})
	}
}

// TestWorkflowWritesPassDown pins what each write hands the workflow service.
func TestWorkflowWritesPassDown(t *testing.T) {
	cfg := workflowstore.Config{
		Name:    "backend",
		Context: "shared notes",
		Harness: "codex",
		Cwd:     "/repo",
		Flags:   "--model opus",
		Worktree: workflowstore.WorktreeConfig{
			Enabled:      true,
			BaseBranch:   "develop",
			Dir:          "/repo/.worktrees",
			CopyPaths:    []string{".env"},
			SymlinkPaths: []string{"node_modules"},
			DiffExclude:  []string{"proto/gen"},
		},
		Hooks: []workflowstore.Hook{
			{
				Event:  workflowstore.HookPreLaunch,
				Script: "warm-cache",
			},
		},
	}
	worktree := &workflowv1.WorktreeConfig{
		Enabled:      true,
		BaseBranch:   "develop",
		Dir:          "/repo/.worktrees",
		CopyPaths:    []string{".env"},
		SymlinkPaths: []string{"node_modules"},
		DiffExclude:  []string{"proto/gen"},
	}
	hooks := []*workflowv1.WorkflowHook{
		{
			Event:  workflowstore.HookPreLaunch,
			Script: "warm-cache",
		},
	}

	tests := []struct {
		name string
		call func(ctx context.Context, svc workflowv1komrad.WorkflowServiceAdapter) error
		want fakeWorkflows
	}{
		{
			name: "creating hands down every field",
			call: func(ctx context.Context, svc workflowv1komrad.WorkflowServiceAdapter) error {
				_, err := svc.CreateWorkflow(ctx, connect.NewRequest(&workflowv1.CreateWorkflowRequest{
					Name:     cfg.Name,
					Context:  cfg.Context,
					Harness:  cfg.Harness,
					Cwd:      cfg.Cwd,
					Flags:    cfg.Flags,
					Worktree: worktree,
					Hooks:    hooks,
				}))

				return err
			},
			want: fakeWorkflows{created: []workflowstore.Config{cfg}},
		},
		{
			name: "updating hands down the id and every field",
			call: func(ctx context.Context, svc workflowv1komrad.WorkflowServiceAdapter) error {
				_, err := svc.UpdateWorkflow(ctx, connect.NewRequest(&workflowv1.UpdateWorkflowRequest{
					Id:       workflowID,
					Name:     cfg.Name,
					Context:  cfg.Context,
					Harness:  cfg.Harness,
					Cwd:      cfg.Cwd,
					Flags:    cfg.Flags,
					Worktree: worktree,
					Hooks:    hooks,
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
			name: "archiving hands down the id",
			call: func(ctx context.Context, svc workflowv1komrad.WorkflowServiceAdapter) error {
				_, err := svc.ArchiveWorkflow(ctx, connect.NewRequest(&workflowv1.ArchiveWorkflowRequest{Id: workflowID}))

				return err
			},
			want: fakeWorkflows{archived: []string{workflowID}},
		},
		{
			name: "deleting hands down the id",
			call: func(ctx context.Context, svc workflowv1komrad.WorkflowServiceAdapter) error {
				_, err := svc.DeleteWorkflow(ctx, connect.NewRequest(&workflowv1.DeleteWorkflowRequest{Id: workflowID}))

				return err
			},
			want: fakeWorkflows{deleted: []string{workflowID}},
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

// TestWorkflowReadsRender pins that every read answers with the workflow the service handed back, context whole.
func TestWorkflowReadsRender(t *testing.T) {
	stored := workflowstore.Workflow{
		ID:      workflowID,
		Name:    "backend",
		Context: strings.Repeat("c", render.SummaryHead+50),
	}

	tests := []struct {
		name string
		call func(ctx context.Context, svc workflowv1komrad.WorkflowServiceAdapter) (*workflowv1.Workflow, error)
	}{
		{
			name: "reading one",
			call: func(ctx context.Context, svc workflowv1komrad.WorkflowServiceAdapter) (*workflowv1.Workflow, error) {
				got, err := svc.GetWorkflow(ctx, connect.NewRequest(&workflowv1.GetWorkflowRequest{Id: workflowID}))
				if err != nil {
					return nil, err
				}

				return got.Msg.GetWorkflow(), nil
			},
		},
		{
			name: "restoring one",
			call: func(ctx context.Context, svc workflowv1komrad.WorkflowServiceAdapter) (*workflowv1.Workflow, error) {
				got, err := svc.RestoreWorkflow(ctx, connect.NewRequest(&workflowv1.RestoreWorkflowRequest{Id: workflowID}))
				if err != nil {
					return nil, err
				}

				return got.Msg.GetWorkflow(), nil
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := newWorkflowHandler(&fakeWorkflows{stored: []workflowstore.Workflow{stored}})

			got, err := tt.call(t.Context(), svc)
			require.NoError(t, err)
			assert.True(t, proto.Equal(render.Workflow(stored, render.WorkflowOptions{}), got))
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
				Pagination: &paginationv1.Pagination{Limit: proto.Int32(1)},
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
					Limit:  proto.Int32(1),
					Cursor: proto.String("eyJuIjoiYWxwaGEiLCJpIjoiMDFhMGQyMjgtMDAwMC03MDAwLTgwMDAtMDAwMDAwMDAwMGFhIn0="),
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
					ID:      "01a0d228-0000-7000-8000-0000000000bb",
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
