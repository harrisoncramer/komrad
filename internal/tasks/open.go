package tasks

import (
	"context"
	"fmt"
	"slices"

	"komrad/internal/scriptrun"
	"komrad/internal/store"

	agentstore "komrad/internal/store/agent"
	taskstore "komrad/internal/store/task"
)

// Opened is the task an agent was put on and the place in its hook queue the agent holds when it
// opens it.
type Opened struct {
	Task *taskstore.Task
	Turn *scriptrun.HookTurn
}

type OpenParams struct {
	Agent  agentstore.Agent
	Joined *taskstore.Task
	Title  string
	Slug   string
	Goal   string
	// Hidden is whether the task is hidden as the agent joins it.
	Hidden bool
	// Stand is what the task made for the agent stands on, nil for none.
	Stand *taskstore.Stand
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
			Stand:      params.Stand,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to make a task for agent %q: %w", agent.Name, err)
		}
		opened.Task = created
	}
	if opened.Task == nil {
		return opened, nil
	}

	agent.TaskID = &opened.Task.ID
	locked, err := taskStore.GetUndeletedForUpdate(ctx, *agent.TaskID)
	if err != nil {
		return nil, err
	}
	agents := agentstore.NewAgentStore(db)
	if params.Joined != nil {
		if err := requireJoinable(ctx, agents, agent, *locked); err != nil {
			return nil, err
		}
	}

	if params.Hidden {
		if _, err := taskStore.SetHidden(ctx, opened.Task.ID, true); err != nil {
			return nil, err
		}
		opened.Task.Hidden = true
	}

	live, err := agents.ListLiveOnTask(ctx, *agent.TaskID)
	if err != nil {
		return nil, err
	}
	opened.Turn = s.QueueOpening(agent, live)

	return opened, nil
}

func requireJoinable(ctx context.Context, agents *agentstore.AgentStore, agent agentstore.Agent, task taskstore.Task) error {
	if task.ArchivedAt != nil {
		return ErrArchived.Saying("task %q is archived", task.Slug)
	}
	if agent.Role != agentstore.RoleWriter {
		return nil
	}

	onTask, err := agents.ListOnTasks(ctx, []string{task.ID})
	if err != nil {
		return err
	}
	if i := slices.IndexFunc(onTask, agentstore.Agent.IsWriter); i >= 0 {
		return ErrHasWriter.Saying("%q writes task %q", onTask[i].Name, task.Slug)
	}

	return nil
}

// isOwnTaskNeeded reports whether the agent is given a task of its own as it is written, which one
// joining a task and an orchestrator are not.
func isOwnTaskNeeded(params OpenParams) bool {
	return params.Joined == nil && params.Agent.Role != agentstore.RoleOrchestrator
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
		return errHasCheckout
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
		return errHasCheckout
	}

	return nil
}
