package tasks

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"komrad/internal/logging"
	"komrad/internal/scriptrun"
	"komrad/internal/store"

	agentstore "komrad/internal/store/agent"
	taskstore "komrad/internal/store/task"
)

// Opened is the task an agent was put on, whether putting it there brought the task back from the
// archive, and the place in its hook queue the agent holds when it opens it.
type Opened struct {
	Task     *taskstore.Task
	Restored bool
	Turn     *scriptrun.HookTurn
}

type OpenParams struct {
	Agent  agentstore.Agent
	Joined *taskstore.Task
	Parent *agentstore.Agent
	Title  string
	Slug   string
	Goal   string
	// JoinsCheckout is whether the agent becomes the writer of its task's existing checkout.
	JoinsCheckout bool
}

// Open puts an agent about to be written in db on its task, making it one of its own when it needs one.
func (s *TaskService) Open(ctx context.Context, db store.DBTX, params OpenParams) (*Opened, error) {
	taskStore := taskstore.NewTaskStore(db)
	agent := params.Agent

	opened := &Opened{Task: params.Joined}
	if isOwnTaskNeeded(params) {
		created, err := createForAgent(ctx, taskStore, createForAgentParams{
			WorkflowID: agent.WorkflowID,
			Name:       agent.Name,
			Title:      params.Title,
			Prompt:     agent.Prompt,
			Slug:       params.Slug,
			Goal:       params.Goal,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to make a task for agent %q: %w", agent.Name, err)
		}
		opened.Task = created
	}
	if opened.Task == nil && params.Parent != nil && params.Parent.TaskID != nil {
		found, err := taskStore.Get(ctx, *params.Parent.TaskID)
		if err != nil {
			return nil, fmt.Errorf("failed to read the task of agent %q's parent: %w", agent.Name, err)
		}
		opened.Task = found
	}
	if opened.Task == nil {
		return opened, nil
	}

	agent.TaskID = &opened.Task.ID
	if params.JoinsCheckout {
		claimed, restored, err := ClaimWriter(ctx, db, ClaimWriterParams{TaskID: opened.Task.ID, Cwd: agent.Cwd})
		if err != nil {
			return nil, err
		}
		opened.Task, opened.Restored = claimed, restored
	}

	if err := taskStore.LockUndeleted(ctx, *agent.TaskID); err != nil {
		return nil, err
	}

	live, err := agentstore.NewAgentStore(db).ListLiveOnTask(ctx, *agent.TaskID)
	if err != nil {
		return nil, err
	}
	opened.Turn = s.QueueOpening(agent, live)

	return opened, nil
}

// isOwnTaskNeeded reports whether the agent is given a task of its own as it is written, which a child
// sharing its parent's and an orchestrator are not.
func isOwnTaskNeeded(params OpenParams) bool {
	return params.Joined == nil && params.Parent == nil && params.Agent.Role != agentstore.RoleOrchestrator
}

type ClaimWriterParams struct {
	TaskID string
	Cwd    string
}

// ClaimWriter makes a launch joining its task's checkout at Cwd the task's writer, refusing it while
// another writer is live or once the worktree is gone, and reports whether it brought an archived
// task back.
func ClaimWriter(ctx context.Context, db store.DBTX, params ClaimWriterParams) (*taskstore.Task, bool, error) {
	taskStore := taskstore.NewTaskStore(db)

	task, err := taskStore.GetUndeletedForUpdate(ctx, params.TaskID)
	if err != nil {
		return nil, false, err
	}
	if !task.Checkout.IsUsable() || task.Checkout.Cwd != params.Cwd {
		return nil, false, ErrWorktreeRemoved.Saying("task %q no longer has its worktree at %q", task.Slug, params.Cwd)
	}

	writer, err := agentstore.NewAgentStore(db).GetTaskWriter(ctx, task.ID)
	switch {
	case err == nil:
		return nil, false, ErrHasWriter.Saying("%q is still writing task %q", writer.Name, task.Slug)
	case !errors.Is(err, store.ErrNotFound):
		return nil, false, err
	}

	if task.ArchivedAt == nil {
		return task, false, nil
	}

	if err := taskStore.Restore(ctx, []string{task.ID}); err != nil {
		return nil, false, fmt.Errorf("failed to restore task %q for its new writer: %w", task.Slug, err)
	}
	task.ArchivedAt = nil

	return task, true, nil
}

type ClaimCheckoutParams struct {
	TaskID        string
	AgentName     string
	AdoptedBranch string
	RepositoryURL string
}

// ClaimCheckout records on a task how its checkout is to be made, ahead of the directory the
// launch then provisions.
func ClaimCheckout(ctx context.Context, db store.DBTX, params ClaimCheckoutParams) error {
	claimed, err := taskstore.NewTaskStore(db).ClaimCheckout(ctx, taskstore.ClaimCheckoutParams{
		ID:            params.TaskID,
		AdoptedBranch: params.AdoptedBranch,
		RepositoryURL: params.RepositoryURL,
	})
	if err != nil {
		return fmt.Errorf("failed to claim the checkout of task %q for agent %q: %w", params.TaskID, params.AgentName, err)
	}
	if !claimed {
		return ErrHasCheckout
	}

	return nil
}

// ClaimCheckoutAgain claims the checkout of a retried agent's task again, refusing when another launch
// has claimed it since.
func (s *TaskService) ClaimCheckoutAgain(ctx context.Context, agent agentstore.Agent) error {
	claimed, err := s.tasks.ClaimCheckoutAgain(ctx, *agent.TaskID)
	if err != nil {
		return fmt.Errorf("failed to claim the checkout of task %q again for agent %q: %w", *agent.TaskID, agent.Name, err)
	}
	if !claimed {
		return ErrHasCheckout
	}

	return nil
}

// Rearchive archives again the task a failed join brought back, unless an agent besides agentID has
// since started on it, logging any failure.
func (s *TaskService) Rearchive(ctx context.Context, taskID, agentID string) {
	err := s.tx.InTx(ctx, func(db store.DBTX) error {
		return taskstore.NewTaskStore(db).ArchiveIdleBesides(ctx, taskID, agentID)
	})
	if err != nil {
		logging.From(ctx).LogAttrs(ctx, slog.LevelError, "failed to archive the task a failed join restored",
			append([]slog.Attr{
				slog.String(logging.AgentID, agentID),
				slog.String(logging.TaskID, taskID),
			}, logging.Err(err)...)...)
	}
}
