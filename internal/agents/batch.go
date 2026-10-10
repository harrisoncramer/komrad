package agents

import (
	"context"
	"fmt"

	"komrad/config"
	"komrad/internal/store"
	"komrad/internal/tasks"

	agentstore "komrad/internal/store/agent"
	taskstore "komrad/internal/store/task"
	workflowstore "komrad/internal/store/workflow"
)

// archiveTaken is what an archive's write took off its rows, the panes and the tasks it archived.
type archiveTaken struct {
	panes []agentstore.AgentPane
	tasks []taskstore.Task
}

type archiveBatchParams struct {
	tmux      paneKiller
	scripts   agentScriptRunner
	workflows *workflowstore.WorkflowStore
	live      []agentstore.Agent
	owned     []agentstore.Agent
	screens   screenKeeper
	archive   func(ctx context.Context) (*archiveTaken, error)
}

func archiveBatch(ctx context.Context, params archiveBatchParams) error {
	if err := joinRefusals(params.live, runPreHooksFor(ctx, batchHooksParams{
		scripts:     params.scripts,
		workflows:   params.workflows,
		agents:      params.live,
		event:       workflowstore.HookAgentPreArchive,
		skipGoneDir: true,
	})); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), teardownTimeout)
	defer cancel()

	// Killing what was read first keeps a failed write from hiding a live CLI, leaving the row
	// for the reconciler to tidy.
	captureOwnedScreens(ctx, captureOwnedScreensParams{
		tmux:    params.tmux,
		screens: params.screens,
		agents:  params.owned,
	})
	killOwnedPanes(ctx, params.tmux, params.owned)

	taken, err := params.archive(ctx)
	if err != nil {
		return err
	}
	// A pane recorded in between only reaches here through what the write took off the rows.
	killPanes(ctx, params.tmux, taken.panes)

	startHooksFor(ctx, batchHooksParams{
		scripts:   params.scripts,
		workflows: params.workflows,
		agents:    params.live,
		event:     workflowstore.HookAgentPostArchive,
	})
	startTaskPostArchive(ctx, params, taken.tasks)

	return nil
}

// startTaskPostArchive starts the post-archive hooks of each task an archive archived.
func startTaskPostArchive(ctx context.Context, params archiveBatchParams, archivedTasks []taskstore.Task) {
	for _, task := range archivedTasks {
		startHooks(ctx, taskHookParams{
			scripts:  params.scripts,
			workflow: readWorkflow(ctx, params.workflows, task.WorkflowID),
			task:     task,
			actor:    findTaskActor(params.live, task.ID),
			event:    workflowstore.HookTaskPostArchive,
		}.toHookRun())
	}
}

type restoreBatchParams struct {
	tx        txRunner
	usage     config.Usage
	scripts   agentScriptRunner
	workflows *workflowstore.WorkflowStore
	agents    *agentstore.AgentStore
	tasks     *tasks.TaskService
	archived  []agentstore.Agent
	relist    func(ctx context.Context, agents *agentstore.AgentStore) ([]agentstore.Agent, error)
	restore   func(ctx context.Context, db store.DBTX) error
}

func restoreBatch(ctx context.Context, params restoreBatchParams) error {
	if err := params.tx.InTx(ctx, func(db store.DBTX) error {
		return claimRoomToRestore(ctx, claimRoomToRestoreParams{
			agents:   agentstore.NewAgentStore(db),
			usage:    params.usage,
			archived: params.archived,
		})
	}); err != nil {
		return err
	}

	if err := joinRefusals(params.archived, runPreHooksFor(ctx, batchHooksParams{
		scripts:     params.scripts,
		workflows:   params.workflows,
		agents:      params.archived,
		event:       workflowstore.HookAgentPreRestore,
		skipGoneDir: true,
	})); err != nil {
		return err
	}
	if err := runTaskPreOpen(ctx, params); err != nil {
		return err
	}

	restoring := params.archived
	err := params.tx.InTx(ctx, func(db store.DBTX) error {
		agents := agentstore.NewAgentStore(db)

		if params.relist != nil {
			relisted, err := params.relist(ctx, agents)
			if err != nil {
				return err
			}
			restoring = relisted
		}
		// A partial restore would read as a whole one, so the ceiling and the names live agents
		// hold are both checked before anything is written.
		if err := claimRoomToRestore(ctx, claimRoomToRestoreParams{
			agents:   agents,
			usage:    params.usage,
			archived: restoring,
		}); err != nil {
			return err
		}

		if err := params.restore(ctx, db); err != nil {
			return err
		}

		return taskstore.NewTaskStore(db).Restore(ctx, agentstore.TaskIDs(restoring))
	})
	if err != nil {
		return err
	}

	restored, err := params.agents.ListByID(ctx, agentstore.IDs(restoring))
	if err != nil {
		return err
	}
	startHooksFor(ctx, batchHooksParams{
		scripts:   params.scripts,
		workflows: params.workflows,
		agents:    restored,
		event:     workflowstore.HookAgentPostRestore,
	})

	return nil
}

// runTaskPreOpen runs the pre-open hooks of each task a restore takes from no live agent to one, stopping at
// the first that fails, and waits for those of each task it joins.
func runTaskPreOpen(ctx context.Context, params restoreBatchParams) error {
	if params.scripts == nil {
		return nil
	}

	found, err := params.tasks.ListTasksByID(ctx, agentstore.TaskIDs(params.archived))
	if err != nil {
		return err
	}

	for _, task := range found {
		actor := findTaskActor(params.archived, task.ID)
		if actor == nil {
			continue
		}

		live, err := params.agents.ListLiveOnTask(ctx, task.ID)
		if err != nil {
			return err
		}
		if !isOpening(*actor, live) {
			if err := params.scripts.AwaitHooks(ctx, task.ID); err != nil {
				return err
			}

			continue
		}

		if err := runPreHooks(ctx, taskHookParams{
			scripts:  params.scripts,
			workflow: readWorkflow(ctx, params.workflows, task.WorkflowID),
			task:     task,
			actor:    actor,
			event:    workflowstore.HookTaskPreOpen,
		}.toHookRun()); err != nil {
			return fmt.Errorf("failed to run the %s hook of task %q: %w", workflowstore.HookTaskPreOpen, task.Slug, err)
		}
	}

	return nil
}
