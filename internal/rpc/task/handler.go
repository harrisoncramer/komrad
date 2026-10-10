// Package task serves the task RPCs, which create, read and archive the outcomes agents work
// toward.
package task

import (
	"context"

	"komrad/internal/fail"
	"komrad/internal/rpc/render"
	"komrad/internal/rpcerr"
	"komrad/lib/xslice"
	taskv1 "komrad/proto/gen/task/v1"

	taskstore "komrad/internal/store/task"

	"google.golang.org/protobuf/types/known/emptypb"
)

type taskReadWriter interface {
	CreateTask(ctx context.Context, params taskstore.CreateParams) (*taskstore.Task, error)
	GetTask(ctx context.Context, ref string) (*taskstore.Task, error)
	ListTasks(ctx context.Context, workflowID string, archived bool) ([]taskstore.Task, error)
}

type taskArchiver interface {
	ArchiveTask(ctx context.Context, ref string) error
}

type NewHandlerParams struct {
	Tasks    taskReadWriter
	Archiver taskArchiver
}

type Handler struct {
	tasks    taskReadWriter
	archiver taskArchiver
}

// NewHandler constructs the handler over the service that owns the rules.
func NewHandler(params NewHandlerParams) *Handler {
	return &Handler{tasks: params.Tasks, archiver: params.Archiver}
}

// CreateTask records a task and returns it as stored.
func (s *Handler) CreateTask(ctx context.Context, req *taskv1.CreateTaskRequest) (*taskv1.CreateTaskResponse, *fail.Error) {
	created, err := s.tasks.CreateTask(ctx, taskstore.CreateParams{
		WorkflowID: req.GetWorkflowId(),
		Slug:       req.GetSlug(),
		Title:      req.GetTitle(),
		Goal:       req.GetGoal(),
	})
	if err != nil {
		return nil, rpcerr.From(err, "failed to create task")
	}

	return &taskv1.CreateTaskResponse{Task: render.Task(*created)}, nil
}

// GetTask returns one task by id or slug.
func (s *Handler) GetTask(ctx context.Context, req *taskv1.GetTaskRequest) (*taskv1.GetTaskResponse, *fail.Error) {
	found, err := s.tasks.GetTask(ctx, req.GetTask())
	if err != nil {
		return nil, rpcerr.From(err, "failed to get task")
	}

	return &taskv1.GetTaskResponse{Task: render.Task(*found)}, nil
}

// ListTasks returns a workflow's live or archived tasks, newest first.
func (s *Handler) ListTasks(ctx context.Context, req *taskv1.ListTasksRequest) (*taskv1.ListTasksResponse, *fail.Error) {
	listed, err := s.tasks.ListTasks(ctx, req.GetWorkflowId(), req.GetArchived())
	if err != nil {
		return nil, rpcerr.From(err, "failed to list tasks")
	}

	return &taskv1.ListTasksResponse{Tasks: xslice.Map(listed, render.Task)}, nil
}

// ArchiveTask takes a task and the live agents on it out of the live listings.
func (s *Handler) ArchiveTask(ctx context.Context, req *taskv1.ArchiveTaskRequest) (*emptypb.Empty, *fail.Error) {
	if err := s.archiver.ArchiveTask(ctx, req.GetTask()); err != nil {
		return nil, rpcerr.From(err, "failed to archive task")
	}

	return &emptypb.Empty{}, nil
}
