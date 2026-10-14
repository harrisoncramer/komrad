package task

import (
	"context"
	"testing"
	"time"

	"komrad/internal/caller"
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

// fakeTasks answers from its fields and records what each call asked of it.
type fakeTasks struct {
	stored  taskstore.Task
	updated []tasks.UpdateTaskParams
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

// GetTask answers the stored task.
func (f *fakeTasks) GetTask(context.Context, string) (*taskstore.Task, error) {
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
func (f *fakeTasks) ArchiveTask(context.Context, string) error {
	return nil
}

// DeleteTasks answers nil.
func (f *fakeTasks) DeleteTasks(context.Context, []string) error {
	return nil
}

// MarkTaskRunsRead answers nil.
func (f *fakeTasks) MarkTaskRunsRead(context.Context, string) error {
	return nil
}

// SetTaskHidden answers nil.
func (f *fakeTasks) SetTaskHidden(context.Context, string, bool) error {
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
