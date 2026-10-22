package task

import (
	"context"
	"testing"
	"time"

	"komrad/internal/agents"
	"komrad/internal/caller"
	"komrad/internal/tasks"
	"komrad/lib/xptr"
	"komrad/proto/gen/oneofs"
	taskv1 "komrad/proto/gen/task/v1"
	"komrad/proto/gen/task/v1/taskv1komrad"

	taskstore "komrad/internal/store/task"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	taskID      = "01a11e6f-0000-7000-8000-00000000000a"
	otherTaskID = "01a11e6f-0000-7000-8000-00000000000b"
)

// fakeTasks answers from its fields and records what each call asked of it.
type fakeTasks struct {
	stored  taskstore.Task
	updated []tasks.UpdateTaskParams
	deleted []agents.DeleteTasksParams
}

// CreateTask answers the params as stored.
func (f *fakeTasks) CreateTask(_ context.Context, params taskstore.CreateParams) (*taskstore.Task, error) {
	return &taskstore.Task{
		ID:         taskID,
		WorkflowID: params.WorkflowID,
		Slug:       params.Slug,
		Title:      params.Title,
		Goal:       params.Goal,
	}, nil
}

// FindTask answers the stored task.
func (f *fakeTasks) FindTask(context.Context, oneofs.Task) (*taskstore.Task, error) {
	return &f.stored, nil
}

// ListTasks answers the stored task.
func (f *fakeTasks) ListTasks(context.Context, string, bool) ([]taskstore.Task, error) {
	return []taskstore.Task{f.stored}, nil
}

// UpdateTask records the params and answers the stored task.
func (f *fakeTasks) UpdateTask(_ context.Context, params tasks.UpdateTaskParams) (*taskstore.Task, error) {
	f.updated = append(f.updated, params)

	return &f.stored, nil
}

// ArchiveTask answers nil.
func (f *fakeTasks) ArchiveTask(context.Context, oneofs.Task) error {
	return nil
}

// DeleteTasks records the params.
func (f *fakeTasks) DeleteTasks(_ context.Context, params agents.DeleteTasksParams) error {
	f.deleted = append(f.deleted, params)

	return nil
}

// MarkTaskRunsRead answers nil.
func (f *fakeTasks) MarkTaskRunsRead(context.Context, string) error {
	return nil
}

// SetTaskHidden answers nil.
func (f *fakeTasks) SetTaskHidden(context.Context, oneofs.Task, bool) error {
	return nil
}

func newTaskHandler(fake *fakeTasks) taskv1komrad.TaskServiceAdapter {
	return taskv1komrad.TaskServiceAdapter{Impl: NewHandler(NewHandlerParams{
		Tasks:    fake,
		Archiver: fake,
		Deleter:  fake,
	})}
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
		Task:           &taskv1.SetTaskNotesRequest_TaskSlug{TaskSlug: "fix-login"},
		Notes:          "Found it.",
		NotesUpdatedAt: xptr.Ptr(readAt.Format(time.RFC3339Nano)),
	}))
	require.NoError(t, err)

	require.Len(t, fake.updated, 1)
	assert.Equal(t, oneofs.Task{TaskSlug: xptr.Ptr("fix-login")}, fake.updated[0].Task)
	assert.Equal(t, xptr.Ptr("Found it."), fake.updated[0].Fields.Notes)
	assert.Equal(t, "agent-1", fake.updated[0].CallerAgentID)
	require.NotNil(t, fake.updated[0].Fields.ReadNotesUpdatedAt)
	assert.True(t, readAt.Equal(*fake.updated[0].Fields.ReadNotesUpdatedAt))
	assert.Equal(t, "Found it.", written.Msg.GetTask().GetNotes())
	assert.Equal(t, writtenAt.Format(time.RFC3339Nano), written.Msg.GetTask().GetNotesUpdatedAt())
}

// TestDeleteTasksPassesDown pins that a delete hands down the tasks by id, those named by slug
// after those named by id, the records choice and the agent asking.
func TestDeleteTasksPassesDown(t *testing.T) {
	fake := &fakeTasks{stored: taskstore.Task{ID: taskID, Slug: "fix-login"}}
	ctx := caller.With(t.Context(), caller.Caller{AgentID: "agent-1"})

	_, err := newTaskHandler(fake).DeleteTasks(ctx, connect.NewRequest(&taskv1.DeleteTasksRequest{
		TaskIds:   []string{otherTaskID},
		TaskSlugs: []string{"fix-login"},
		Records:   true,
	}))
	require.NoError(t, err)

	assert.Equal(t, []agents.DeleteTasksParams{
		{
			Tasks: []oneofs.Task{
				{TaskId: xptr.Ptr(uuid.MustParse(otherTaskID))},
				{TaskSlug: xptr.Ptr("fix-login")},
			},
			Records:   true,
			ByAgentID: "agent-1",
		},
	}, fake.deleted)
}

// TestGetTaskRefusesAMalformedID pins that an id that is not a uuid is answered as an invalid
// argument before the handler runs.
func TestGetTaskRefusesAMalformedID(t *testing.T) {
	_, err := newTaskHandler(&fakeTasks{}).GetTask(t.Context(), connect.NewRequest(&taskv1.GetTaskRequest{
		Task: &taskv1.GetTaskRequest_TaskId{TaskId: "fix-login"},
	}))
	assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
}
