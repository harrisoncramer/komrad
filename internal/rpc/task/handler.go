package task

import (
	"context"
	"slices"

	"komrad/internal/agents"
	"komrad/internal/caller"
	"komrad/internal/fail"
	"komrad/internal/rpc/render"
	"komrad/internal/rpcerr"
	"komrad/internal/tasks"
	"komrad/lib/xptr"
	"komrad/lib/xslice"
	"komrad/proto/gen/oneofs"
	taskv1 "komrad/proto/gen/task/v1"
	"komrad/proto/gen/task/v1/taskv1komrad"

	taskstore "komrad/internal/store/task"

	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/emptypb"
)

type taskReadWriter interface {
	CreateTask(ctx context.Context, params taskstore.CreateParams) (*taskstore.Task, error)
	FindTask(ctx context.Context, task oneofs.Task) (*taskstore.Task, error)
	ListTasks(ctx context.Context, workflowID uuid.UUID, archived bool) ([]taskstore.Task, error)
	UpdateTask(ctx context.Context, params tasks.UpdateTaskParams) (*taskstore.Task, error)
	MarkTaskRunsRead(ctx context.Context, taskID uuid.UUID) error
	SetTaskHidden(ctx context.Context, task oneofs.Task, hidden bool) error
}

type taskArchiver interface {
	ArchiveTask(ctx context.Context, task oneofs.Task) error
}

type taskDeleter interface {
	DeleteTasks(ctx context.Context, params agents.DeleteTasksParams) error
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
func (s *Handler) CreateTask(ctx context.Context, req *taskv1komrad.CreateTaskRequest) (*taskv1.CreateTaskResponse, *fail.Error) {
	created, err := s.tasks.CreateTask(ctx, taskstore.CreateParams{
		WorkflowID: req.WorkflowId,
		Slug:       req.Slug,
		Title:      req.Title,
		Goal:       req.Goal,
	})
	if err != nil {
		return nil, rpcerr.From(err, "failed to create task")
	}

	return &taskv1.CreateTaskResponse{Task: render.Task(*created)}, nil
}

// GetTask returns one task by id or by slug.
func (s *Handler) GetTask(ctx context.Context, req *taskv1komrad.GetTaskRequest) (*taskv1.GetTaskResponse, *fail.Error) {
	found, err := s.tasks.FindTask(ctx, req.Task)
	if err != nil {
		return nil, rpcerr.From(err, "failed to get task")
	}

	return &taskv1.GetTaskResponse{Task: render.Task(*found)}, nil
}

// ListTasks returns a workflow's live or archived tasks, newest first.
func (s *Handler) ListTasks(ctx context.Context, req *taskv1komrad.ListTasksRequest) (*taskv1.ListTasksResponse, *fail.Error) {
	listed, err := s.tasks.ListTasks(ctx, req.WorkflowId, req.Archived)
	if err != nil {
		return nil, rpcerr.From(err, "failed to list tasks")
	}

	return &taskv1.ListTasksResponse{Tasks: xslice.Map(listed, render.Task)}, nil
}

// UpdateTask rewrites the slug, title, goal or notes of a task and returns it as stored.
func (s *Handler) UpdateTask(ctx context.Context, req *taskv1komrad.UpdateTaskRequest) (*taskv1.UpdateTaskResponse, *fail.Error) {
	updated, err := s.tasks.UpdateTask(ctx, tasks.UpdateTaskParams{
		Task: req.Task,
		Fields: taskstore.UpdateParams{
			Slug:               req.Slug,
			Title:              req.Title,
			Goal:               req.Goal,
			Notes:              req.Notes,
			ReadNotesUpdatedAt: req.NotesUpdatedAt,
		},
		CallerAgentID: caller.From(ctx).AgentID,
	})
	if err != nil {
		return nil, rpcerr.From(err, "failed to update task")
	}

	return &taskv1.UpdateTaskResponse{Task: render.Task(*updated)}, nil
}

// SetTaskNotes replaces a task's notes for the person or an agent that may write them.
func (s *Handler) SetTaskNotes(ctx context.Context, req *taskv1komrad.SetTaskNotesRequest) (*taskv1.SetTaskNotesResponse, *fail.Error) {
	written, err := s.tasks.UpdateTask(ctx, tasks.UpdateTaskParams{
		Task: req.Task,
		Fields: taskstore.UpdateParams{
			Notes:              xptr.Ptr(req.Notes),
			ReadNotesUpdatedAt: req.NotesUpdatedAt,
		},
		CallerAgentID: caller.From(ctx).AgentID,
	})
	if err != nil {
		return nil, rpcerr.From(err, "failed to set task notes")
	}

	return &taskv1.SetTaskNotesResponse{Task: render.Task(*written)}, nil
}

// ArchiveTask takes a task and the live agents on it out of the live listings.
func (s *Handler) ArchiveTask(ctx context.Context, req *taskv1komrad.ArchiveTaskRequest) (*emptypb.Empty, *fail.Error) {
	if err := s.archiver.ArchiveTask(ctx, req.Task); err != nil {
		return nil, rpcerr.From(err, "failed to archive task")
	}

	return &emptypb.Empty{}, nil
}

// DeleteTasks removes tasks' worktrees, and with records set the tasks for good along with every agent on them.
func (s *Handler) DeleteTasks(ctx context.Context, req *taskv1komrad.DeleteTasksRequest) (*emptypb.Empty, *fail.Error) {
	named := slices.Concat(
		xslice.Map(req.TaskIds, func(id uuid.UUID) oneofs.Task { return oneofs.Task{TaskId: &id} }),
		xslice.Map(req.TaskSlugs, func(slug string) oneofs.Task { return oneofs.Task{TaskSlug: &slug} }),
	)

	if err := s.deleter.DeleteTasks(ctx, agents.DeleteTasksParams{
		Tasks:     named,
		Records:   req.Records,
		ByAgentID: caller.From(ctx).AgentID,
	}); err != nil {
		return nil, rpcerr.From(err, "failed to delete tasks")
	}

	return &emptypb.Empty{}, nil
}

// SetTaskHidden hides a task from the panel's sidebar or shows it again.
func (s *Handler) SetTaskHidden(ctx context.Context, req *taskv1komrad.SetTaskHiddenRequest) (*emptypb.Empty, *fail.Error) {
	if err := s.tasks.SetTaskHidden(ctx, req.Task, req.Hidden); err != nil {
		return nil, rpcerr.From(err, "failed to hide the task")
	}

	return &emptypb.Empty{}, nil
}

// MarkTaskRunsRead claims the failed runs on a task.
func (s *Handler) MarkTaskRunsRead(ctx context.Context, req *taskv1komrad.MarkTaskRunsReadRequest) (*emptypb.Empty, *fail.Error) {
	if err := s.tasks.MarkTaskRunsRead(ctx, req.Id); err != nil {
		return nil, rpcerr.From(err, "failed to mark the task's runs read")
	}

	return &emptypb.Empty{}, nil
}
