// Package task serves the task RPCs, which create, read, rewrite, archive and delete the
// outcomes agents work toward.
package task

import (
	"context"

	"komrad/internal/caller"
	"komrad/internal/fail"
	"komrad/internal/rpc/render"
	"komrad/internal/rpcerr"
	"komrad/internal/tasks"
	"komrad/lib/dates"
	"komrad/lib/xptr"
	"komrad/lib/xslice"
	taskv1 "komrad/proto/gen/task/v1"

	taskstore "komrad/internal/store/task"

	"google.golang.org/protobuf/types/known/emptypb"
)

type taskReadWriter interface {
	CreateTask(ctx context.Context, params taskstore.CreateParams) (*taskstore.Task, error)
	GetTask(ctx context.Context, ref string) (*taskstore.Task, error)
	ListTasks(ctx context.Context, workflowID string, archived bool) ([]taskstore.Task, error)
	UpdateTask(ctx context.Context, params tasks.UpdateTaskParams) (*taskstore.Task, error)
	MarkRunsRead(ctx context.Context, taskID string) error
}

type taskArchiver interface {
	ArchiveTask(ctx context.Context, ref string) error
}

type taskDeleter interface {
	DeleteTasks(ctx context.Context, refs []string) error
}

type NewHandlerParams struct {
	Tasks    taskReadWriter
	Archiver taskArchiver
	Deleter  taskDeleter
}

type Handler struct {
	tasks    taskReadWriter
	archiver taskArchiver
	deleter  taskDeleter
}

// NewHandler constructs the handler over the service that owns the rules.
func NewHandler(params NewHandlerParams) *Handler {
	return &Handler{
		tasks:    params.Tasks,
		archiver: params.Archiver,
		deleter:  params.Deleter,
	}
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

// UpdateTask rewrites the slug, title, goal or notes of a task and returns it as stored.
func (s *Handler) UpdateTask(ctx context.Context, req *taskv1.UpdateTaskRequest) (*taskv1.UpdateTaskResponse, *fail.Error) {
	updated, err := s.tasks.UpdateTask(ctx, tasks.UpdateTaskParams{
		Ref: req.GetTask(),
		Fields: taskstore.UpdateParams{
			Slug:               req.Slug,
			Title:              req.Title,
			Goal:               req.Goal,
			Notes:              req.Notes,
			ReadNotesUpdatedAt: dates.ParsePtr(req.GetNotesUpdatedAt()),
		},
		CallerAgentID: caller.From(ctx).AgentID,
	})
	if err != nil {
		return nil, rpcerr.From(err, "failed to update task")
	}

	return &taskv1.UpdateTaskResponse{Task: render.Task(*updated)}, nil
}

// SetTaskNotes replaces a task's notes for the person or an agent that may write them.
func (s *Handler) SetTaskNotes(ctx context.Context, req *taskv1.SetTaskNotesRequest) (*taskv1.SetTaskNotesResponse, *fail.Error) {
	written, err := s.tasks.UpdateTask(ctx, tasks.UpdateTaskParams{
		Ref: req.GetTask(),
		Fields: taskstore.UpdateParams{
			Notes:              xptr.Ptr(req.GetNotes()),
			ReadNotesUpdatedAt: dates.ParsePtr(req.GetNotesUpdatedAt()),
		},
		CallerAgentID: caller.From(ctx).AgentID,
	})
	if err != nil {
		return nil, rpcerr.From(err, "failed to set task notes")
	}

	return &taskv1.SetTaskNotesResponse{Task: render.Task(*written)}, nil
}

// ArchiveTask takes a task and the live agents on it out of the live listings.
func (s *Handler) ArchiveTask(ctx context.Context, req *taskv1.ArchiveTaskRequest) (*emptypb.Empty, *fail.Error) {
	if err := s.archiver.ArchiveTask(ctx, req.GetTask()); err != nil {
		return nil, rpcerr.From(err, "failed to archive task")
	}

	return &emptypb.Empty{}, nil
}

// DeleteTasks removes tasks for good along with every agent on them.
func (s *Handler) DeleteTasks(ctx context.Context, req *taskv1.DeleteTasksRequest) (*emptypb.Empty, *fail.Error) {
	if err := s.deleter.DeleteTasks(ctx, req.GetTasks()); err != nil {
		return nil, rpcerr.From(err, "failed to delete tasks")
	}

	return &emptypb.Empty{}, nil
}

// MarkRunsRead claims the failed runs on a task.
func (s *Handler) MarkRunsRead(ctx context.Context, req *taskv1.MarkTaskRunsReadRequest) (*emptypb.Empty, *fail.Error) {
	if err := s.tasks.MarkRunsRead(ctx, req.GetId()); err != nil {
		return nil, rpcerr.From(err, "failed to mark the task's runs read")
	}

	return &emptypb.Empty{}, nil
}
