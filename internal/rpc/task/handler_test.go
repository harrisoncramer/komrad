package task

import (
	"context"
	"errors"
	"testing"
	"time"

	"komrad/internal/store"
	"komrad/lib/xptr"
	taskv1 "komrad/proto/gen/task/v1"
	"komrad/proto/gen/task/v1/taskv1komrad"

	taskstore "komrad/internal/store/task"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const taskID = "01a11e6f-0000-7000-8000-00000000000a"

type listCall struct {
	workflowID string
	archived   bool
}

// fakeTasks answers from its fields and records what each call asked of it.
type fakeTasks struct {
	stored   taskstore.Task
	err      error
	created  []taskstore.CreateParams
	got      []string
	listed   []listCall
	updated  []updateCall
	archived []string
	deleted  [][]string
}

type updateCall struct {
	ref    string
	params taskstore.UpdateParams
}

// CreateTask records the params and answers them as stored unless err refuses it.
func (f *fakeTasks) CreateTask(_ context.Context, params taskstore.CreateParams) (*taskstore.Task, error) {
	f.created = append(f.created, params)
	if f.err != nil {
		return nil, f.err
	}

	return &taskstore.Task{
		ID:         taskID,
		WorkflowID: params.WorkflowID,
		Slug:       params.Slug,
		Title:      params.Title,
		Goal:       params.Goal,
	}, nil
}

// GetTask records the ref and answers the stored task unless err refuses it.
func (f *fakeTasks) GetTask(_ context.Context, ref string) (*taskstore.Task, error) {
	f.got = append(f.got, ref)
	if f.err != nil {
		return nil, f.err
	}

	return &f.stored, nil
}

// ListTasks records the listing and answers the stored task unless err refuses it.
func (f *fakeTasks) ListTasks(_ context.Context, workflowID string, archived bool) ([]taskstore.Task, error) {
	f.listed = append(f.listed, listCall{workflowID: workflowID, archived: archived})
	if f.err != nil {
		return nil, f.err
	}

	return []taskstore.Task{f.stored}, nil
}

// UpdateTask records the ref and params and answers the stored task unless err refuses it.
func (f *fakeTasks) UpdateTask(_ context.Context, ref string, params taskstore.UpdateParams) (*taskstore.Task, error) {
	f.updated = append(f.updated, updateCall{ref: ref, params: params})
	if f.err != nil {
		return nil, f.err
	}

	return &f.stored, nil
}

// ArchiveTask records the ref and answers err.
func (f *fakeTasks) ArchiveTask(_ context.Context, ref string) error {
	f.archived = append(f.archived, ref)

	return f.err
}

// DeleteTasks records the refs and answers err.
func (f *fakeTasks) DeleteTasks(_ context.Context, refs []string) error {
	f.deleted = append(f.deleted, refs)

	return f.err
}

func newTaskHandler(fake *fakeTasks) taskv1komrad.TaskServiceAdapter {
	return taskv1komrad.TaskServiceAdapter{Impl: NewHandler(NewHandlerParams{
		Tasks:    fake,
		Archiver: fake,
		Deleter:  fake,
	})}
}

// TestTaskRefusalsMapToCodes pins the code each refusal of the task service reaches the caller as.
func TestTaskRefusalsMapToCodes(t *testing.T) {
	create := func(ctx context.Context, svc taskv1komrad.TaskServiceAdapter) error {
		_, err := svc.CreateTask(ctx, connect.NewRequest(&taskv1.CreateTaskRequest{
			Slug:       "fix-login",
			Title:      "Fix login",
			WorkflowId: "workflow",
		}))

		return err
	}
	get := func(ctx context.Context, svc taskv1komrad.TaskServiceAdapter) error {
		_, err := svc.GetTask(ctx, connect.NewRequest(&taskv1.GetTaskRequest{Task: "fix-login"}))

		return err
	}

	tests := []struct {
		name     string
		call     func(ctx context.Context, svc taskv1komrad.TaskServiceAdapter) error
		err      error
		wantCode connect.Code
	}{
		{
			name:     "a slug that is not kebab-case",
			call:     create,
			err:      taskstore.ErrBadSlug,
			wantCode: connect.CodeInvalidArgument,
		},
		{
			name:     "a slug another task has held",
			call:     create,
			err:      store.ErrNameTaken,
			wantCode: connect.CodeAlreadyExists,
		},
		{
			name:     "a workflow nothing holds",
			call:     create,
			err:      store.ErrNotFound,
			wantCode: connect.CodeNotFound,
		},
		{
			name:     "reading a task nothing holds",
			call:     get,
			err:      store.ErrNotFound,
			wantCode: connect.CodeNotFound,
		},
		{
			name: "renaming a task to a slug another task holds",
			call: func(ctx context.Context, svc taskv1komrad.TaskServiceAdapter) error {
				_, err := svc.UpdateTask(ctx, connect.NewRequest(&taskv1.UpdateTaskRequest{Task: "fix-login", Slug: xptr.Ptr("fix-logout")}))

				return err
			},
			err:      store.ErrNameTaken,
			wantCode: connect.CodeAlreadyExists,
		},
		{
			name: "deleting a task nothing holds",
			call: func(ctx context.Context, svc taskv1komrad.TaskServiceAdapter) error {
				_, err := svc.DeleteTasks(ctx, connect.NewRequest(&taskv1.DeleteTasksRequest{Tasks: []string{"fix-login"}}))

				return err
			},
			err:      store.ErrNotFound,
			wantCode: connect.CodeNotFound,
		},
		{
			name: "archiving a task nothing holds",
			call: func(ctx context.Context, svc taskv1komrad.TaskServiceAdapter) error {
				_, err := svc.ArchiveTask(ctx, connect.NewRequest(&taskv1.ArchiveTaskRequest{Task: "fix-login"}))

				return err
			},
			err:      store.ErrNotFound,
			wantCode: connect.CodeNotFound,
		},
		{
			name: "a listing the store could not read",
			call: func(ctx context.Context, svc taskv1komrad.TaskServiceAdapter) error {
				_, err := svc.ListTasks(ctx, connect.NewRequest(&taskv1.ListTasksRequest{WorkflowId: "workflow"}))

				return err
			},
			err:      errors.New("connection refused"),
			wantCode: connect.CodeInternal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := newTaskHandler(&fakeTasks{err: tt.err})

			err := tt.call(t.Context(), svc)
			assert.Equal(t, tt.wantCode, connect.CodeOf(err))
			require.ErrorIs(t, err, tt.err)
		})
	}
}

// TestTaskCallsPassDown pins what each call hands the task service and what it renders back.
func TestTaskCallsPassDown(t *testing.T) {
	archivedAt := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	fake := &fakeTasks{stored: taskstore.Task{
		ID:         taskID,
		WorkflowID: "workflow",
		Slug:       "fix-login",
		Title:      "Fix login",
		Goal:       "The login page loads.",
		ArchivedAt: &archivedAt,
	}}
	svc := newTaskHandler(fake)
	ctx := t.Context()

	created, err := svc.CreateTask(ctx, connect.NewRequest(&taskv1.CreateTaskRequest{
		Slug:       "fix-login",
		Title:      "Fix login",
		Goal:       "The login page loads.",
		WorkflowId: "workflow",
	}))
	require.NoError(t, err)
	assert.Equal(t, []taskstore.CreateParams{{
		WorkflowID: "workflow",
		Slug:       "fix-login",
		Title:      "Fix login",
		Goal:       "The login page loads.",
	}}, fake.created)
	assert.Equal(t, taskID, created.Msg.GetTask().GetId())
	assert.Nil(t, created.Msg.GetTask().ArchivedAt, "a live task carries no archive stamp")

	got, err := svc.GetTask(ctx, connect.NewRequest(&taskv1.GetTaskRequest{Task: "fix-login"}))
	require.NoError(t, err)
	assert.Equal(t, []string{"fix-login"}, fake.got)
	assert.Equal(t, "The login page loads.", got.Msg.GetTask().GetGoal())
	assert.NotNil(t, got.Msg.GetTask().ArchivedAt, "an archived task carries its stamp")

	listed, err := svc.ListTasks(ctx, connect.NewRequest(&taskv1.ListTasksRequest{WorkflowId: "workflow", Archived: true}))
	require.NoError(t, err)
	assert.Equal(t, []listCall{{workflowID: "workflow", archived: true}}, fake.listed)
	require.Len(t, listed.Msg.GetTasks(), 1)
	assert.Equal(t, "fix-login", listed.Msg.GetTasks()[0].GetSlug())

	updated, err := svc.UpdateTask(ctx, connect.NewRequest(&taskv1.UpdateTaskRequest{Task: "fix-login", Title: xptr.Ptr("Fix the login")}))
	require.NoError(t, err)
	assert.Equal(t, []updateCall{{
		ref:    "fix-login",
		params: taskstore.UpdateParams{Title: xptr.Ptr("Fix the login")},
	}}, fake.updated, "a field left unset reaches the service as nil")
	assert.Equal(t, taskID, updated.Msg.GetTask().GetId())

	_, err = svc.ArchiveTask(ctx, connect.NewRequest(&taskv1.ArchiveTaskRequest{Task: taskID}))
	require.NoError(t, err)
	assert.Equal(t, []string{taskID}, fake.archived)

	_, err = svc.DeleteTasks(ctx, connect.NewRequest(&taskv1.DeleteTasksRequest{Tasks: []string{taskID, "fix-logout"}}))
	require.NoError(t, err)
	assert.Equal(t, [][]string{{taskID, "fix-logout"}}, fake.deleted)
}
