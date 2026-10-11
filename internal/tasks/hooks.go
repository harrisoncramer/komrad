package tasks

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"

	"komrad/internal/logging"
	"komrad/internal/scriptrun"
	"komrad/internal/scripts"
	"komrad/internal/store"
	"komrad/lib/xptr"

	agentstore "komrad/internal/store/agent"
	taskstore "komrad/internal/store/task"
	workflowstore "komrad/internal/store/workflow"
)

// moment is one moment in a task's life and the agent whose action brought it about, nil when none did.
type moment struct {
	scripts  scriptrun.HookRunner
	workflow *workflowstore.Workflow
	task     taskstore.Task
	actor    *agentstore.Agent
	event    string
}

// QueueOpening takes a place in its task's hook queue for an agent opening the task, nil when it does not open it.
func (s *TaskService) QueueOpening(agent agentstore.Agent, live []agentstore.Agent) *scriptrun.HookTurn {
	if s.scripts == nil || !isOpening(agent, live) {
		return nil
	}

	return s.scripts.QueueHooks(*agent.TaskID)
}

type RunPreOpenParams struct {
	Agent    agentstore.Agent
	Task     taskstore.Task
	Workflow *workflowstore.Workflow
	Turn     *scriptrun.HookTurn
}

// RunPreOpen runs the pre-open hooks of the task an agent opens, and otherwise waits for an agent opening it
// already, opening it in that agent's place when it failed.
func (s *TaskService) RunPreOpen(ctx context.Context, params RunPreOpenParams) error {
	agent := params.Agent
	if s.scripts == nil || agent.TaskID == nil {
		return nil
	}

	turn := params.Turn
	if turn == nil {
		if err := s.scripts.AwaitHooks(ctx, *agent.TaskID); err != nil {
			return err
		}

		live, err := s.agents.ListLiveOnTask(ctx, *agent.TaskID)
		if err != nil {
			return err
		}

		turn = s.QueueOpening(agent, live)
		if turn == nil {
			return nil
		}
		defer turn.Done()
	}

	if err := turn.Wait(ctx); err != nil {
		return err
	}

	return moment{
		scripts:  s.scripts,
		workflow: params.Workflow,
		task:     params.Task,
		actor:    &agent,
		event:    workflowstore.HookTaskPreOpen,
	}.toHookRun().Run(ctx)
}

// RunPreOpenRestored runs the pre-open hooks of each task a restore takes from no live agent to one, stopping at
// the first that fails, and waits for those of each task it joins.
func (s *TaskService) RunPreOpenRestored(ctx context.Context, archived []agentstore.Agent) error {
	if s.scripts == nil {
		return nil
	}

	found, err := s.tasks.ListByID(ctx, agentstore.TaskIDs(archived))
	if err != nil {
		return err
	}

	for _, task := range found {
		actor := findTaskActor(archived, task.ID)
		if actor == nil {
			continue
		}

		live, err := s.agents.ListLiveOnTask(ctx, task.ID)
		if err != nil {
			return err
		}
		if !isOpening(*actor, live) {
			if err := s.scripts.AwaitHooks(ctx, task.ID); err != nil {
				return err
			}

			continue
		}

		if err := (moment{
			scripts:  s.scripts,
			workflow: s.readWorkflow(ctx, task.WorkflowID),
			task:     task,
			actor:    actor,
			event:    workflowstore.HookTaskPreOpen,
		}.toHookRun().RunInTurn(ctx)); err != nil {
			return fmt.Errorf("failed to run the %s hook of task %q: %w", workflowstore.HookTaskPreOpen, task.Slug, err)
		}
	}

	return nil
}

// StartPostArchive starts the post-archive hooks of each task an archive archived, as the agent among actors that archived it.
func (s *TaskService) StartPostArchive(ctx context.Context, archived []taskstore.Task, actors []agentstore.Agent) {
	for _, task := range archived {
		moment{
			scripts:  s.scripts,
			workflow: s.readWorkflow(ctx, task.WorkflowID),
			task:     task,
			actor:    findTaskActor(actors, task.ID),
			event:    workflowstore.HookTaskPostArchive,
		}.toHookRun().Start(ctx)
	}
}

// isOpening reports whether an agent starting on a task opens it, which it does when no other agent on the task is live.
func isOpening(agent agentstore.Agent, live []agentstore.Agent) bool {
	if agent.TaskID == nil || agent.ParentAgentID != nil {
		return false
	}

	return !slices.ContainsFunc(live, func(other agentstore.Agent) bool {
		return other.ID != agent.ID && other.State != agentstore.StateFailedToCreate
	})
}

// toHookRun is the hook run of a task's moment, as the acting agent, in the task's worktree while it
// has one and its workflow's directory otherwise.
func (p moment) toHookRun() scriptrun.HookRun {
	var runAs agentstore.Agent
	if p.actor != nil {
		runAs = *p.actor
	}
	runAs.WorkflowID = p.task.WorkflowID
	runAs.TaskID = &p.task.ID

	// An agent that provisions nothing is handed no worktree, which keeps the worktree's
	// variables from a script running in the workflow's own directory.
	if hasLiveCheckout(p.task) {
		runAs.Cwd = p.task.Checkout.Cwd
		runAs.ProvisionEnabled = nil
	} else {
		runAs.Cwd = ""
		if p.workflow != nil {
			runAs.Cwd = p.workflow.Cwd
		}
		runAs.ProvisionEnabled = xptr.Ptr(false)
	}

	var hooks []workflowstore.Hook
	if p.workflow != nil {
		hooks = p.workflow.Hooks
	}

	return scriptrun.HookRun{
		Scripts: p.scripts,
		Agent:   runAs,
		Hooks:   hooks,
		Event:   p.event,
	}
}

// hasLiveCheckout reports whether a task has a worktree that is still there.
func hasLiveCheckout(task taskstore.Task) bool {
	return task.Checkout.Cwd != "" && !task.Checkout.WorktreeRemoved && scripts.RequireUsableDir(task.Checkout.Cwd) == nil
}

// findTaskActor picks the agent among agents whose action fired a task's moment, preferring the
// task's writer and then any agent on it that is not a child.
func findTaskActor(agents []agentstore.Agent, taskID string) *agentstore.Agent {
	var actor *agentstore.Agent
	for _, agent := range agents {
		if agent.TaskID == nil || *agent.TaskID != taskID {
			continue
		}
		if actor == nil || rankActor(agent) > rankActor(*actor) {
			actor = &agent
		}
	}

	return actor
}

func rankActor(agent agentstore.Agent) int {
	switch {
	case agent.ParentAgentID != nil:
		return 0
	case agent.IsProvisionerDisabled():
		return 1
	default:
		return 2
	}
}

func (s *TaskService) readWorkflow(ctx context.Context, workflowID string) *workflowstore.Workflow {
	wf, err := s.workflows.GetIncludingArchived(ctx, workflowID)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		logging.From(ctx).LogAttrs(ctx, slog.LevelError, "the workflow could not be read",
			append([]slog.Attr{slog.String(logging.WorkflowID, workflowID)}, logging.Err(err)...)...)

		return nil
	}

	return wf
}
