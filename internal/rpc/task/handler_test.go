package task

import (
	"context"
	"errors"
	"testing"
	"time"

	"komrad/internal/caller"
	"komrad/internal/store"
	"komrad/internal/tasks"
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
	updated  []tasks.UpdateTaskParams
	archived []string
	deleted  [][]string
	marked   []string
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

// UpdateTask records the params and answers the stored task unless err refuses it.
func (f *fakeTasks) UpdateTask(_ context.Context, params tasks.UpdateTaskParams) (*taskstore.Task, error) {
	f.updated = append(f.updated, params)
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

// MarkRunsRead records the task and answers err.
func (f *fakeTasks) MarkRunsRead(_ context.Context, taskID string) error {
	f.marked = append(f.marked, taskID)

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
	setNotes := func(ctx context.Context, svc taskv1komrad.TaskServiceAdapter) error {
		_, err := svc.SetTaskNotes(ctx, connect.NewRequest(&taskv1.SetTaskNotesRequest{Task: "fix-login", Notes: "Found it."}))

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
			name:     "notes written since they were read",
			call:     setNotes,
			err:      taskstore.ErrNotesChanged,
			wantCode: connect.CodeFailedPrecondition,
		},
		{
			name:     "notes of a task the agent is not on",
			call:     setNotes,
			err:      tasks.ErrNotesNotYours,
			wantCode: connect.CodePermissionDenied,
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
	assert.Equal(t, []tasks.UpdateTaskParams{{
		Ref:    "fix-login",
		Fields: taskstore.UpdateParams{Title: xptr.Ptr("Fix the login")},
	}}, fake.updated, "a field left unset reaches the service as nil")
	assert.Equal(t, taskID, updated.Msg.GetTask().GetId())

	_, err = svc.ArchiveTask(ctx, connect.NewRequest(&taskv1.ArchiveTaskRequest{Task: taskID}))
	require.NoError(t, err)
	assert.Equal(t, []string{taskID}, fake.archived)

	_, err = svc.DeleteTasks(ctx, connect.NewRequest(&taskv1.DeleteTasksRequest{Tasks: []string{taskID, "fix-logout"}}))
	require.NoError(t, err)
	assert.Equal(t, [][]string{{taskID, "fix-logout"}}, fake.deleted)

	_, err = svc.MarkRunsRead(ctx, connect.NewRequest(&taskv1.MarkTaskRunsReadRequest{Id: taskID}))
	require.NoError(t, err)
	assert.Equal(t, []string{taskID}, fake.marked)
}

// TestSetTaskNotesPassesDown pins that a notes write hands down the caller and the version it read,
// and renders the version written back.
func TestSetTaskNotesPassesDown(t *testing.T) {
	readAt := time.Date(2026, 10, 9, 12, 0, 0, 123456000, time.UTC)
	writtenAt := time.Date(2026, 10, 9, 13, 0, 0, 654321000, time.UTC)
	fake := &fakeTasks{stored: taskstore.Task{
		ID:             taskID,
		Slug:           "fix-login",
		Notes:          "Found it.",
		NotesUpdatedAt: &writtenAt,
	}}
	ctx := caller.With(t.Context(), caller.Caller{AgentID: "agent-1"})

	written, err := newTaskHandler(fake).SetTaskNotes(ctx, connect.NewRequest(&taskv1.SetTaskNotesRequest{
		Task:           "fix-login",
		Notes:          "Found it.",
		NotesUpdatedAt: xptr.Ptr(readAt.Format(time.RFC3339Nano)),
	}))
	require.NoError(t, err)

	require.Len(t, fake.updated, 1)
	assert.Equal(t, "fix-login", fake.updated[0].Ref)
	assert.Equal(t, xptr.Ptr("Found it."), fake.updated[0].Fields.Notes)
	assert.Equal(t, "agent-1", fake.updated[0].CallerAgentID)
	require.NotNil(t, fake.updated[0].Fields.ReadNotesUpdatedAt)
	assert.True(t, readAt.Equal(*fake.updated[0].Fields.ReadNotesUpdatedAt))
	assert.Equal(t, "Found it.", written.Msg.GetTask().GetNotes())
	assert.Equal(t, writtenAt.Format(time.RFC3339Nano), written.Msg.GetTask().GetNotesUpdatedAt())
}
