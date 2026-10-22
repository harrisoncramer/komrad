package agents

import (
	"context"
	"fmt"
	"strings"

	"komrad/config"
	"komrad/internal/store"
	"komrad/internal/tasks"

	agentstore "komrad/internal/store/agent"
	taskstore "komrad/internal/store/task"
	workflowstore "komrad/internal/store/workflow"

	"github.com/google/uuid"
)

// archiveTaken is what an archive's write took off its rows, the panes and the tasks it archived.
type archiveTaken struct {
	panes []agentstore.AgentPane
	tasks []taskstore.Task
}

type agentName struct {
	workflowID uuid.UUID
	name       string
}

type archiveBatchParams struct {
	tmux      paneKiller
	scripts   agentScriptRunner
	workflows *workflowstore.WorkflowStore
	tasks     *tasks.TaskService
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
	params.tasks.StartPostArchive(ctx, taken.tasks, params.live)

	return nil
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
	if err := params.tasks.RunPreOpenRestored(ctx, params.archived); err != nil {
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
		// A partial restore would read as a whole one, so the restore checks the ceiling and the names
		// live agents hold before it writes anything.
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

type claimRoomToRestoreParams struct {
	agents   *agentstore.AgentStore
	usage    config.Usage
	archived []agentstore.Agent
}

func claimRoomToRestore(ctx context.Context, params claimRoomToRestoreParams) error {
	if err := claimRoomForAgents(ctx, params.agents, params.usage.MaxAgents, len(params.archived)); err != nil {
		return err
	}
	if err := claimRoomForRestoredReviewers(ctx, params.agents, params.usage.Task.MaxReviewers, params.archived); err != nil {
		return err
	}
	if err := claimRoomForRestoredTasks(ctx, params.agents, params.usage.Orchestrator.MaxTasks, params.archived); err != nil {
		return err
	}

	return requireNamesFree(ctx, params.agents, params.archived)
}

func requireNamesFree(ctx context.Context, agents *agentstore.AgentStore, archived []agentstore.Agent) error {
	returning := make(map[agentName]int, len(archived))
	for _, agent := range archived {
		returning[agentName{workflowID: agent.WorkflowID, name: agent.Name}]++
	}

	var clashing []string
	checked := make(map[agentName]bool, len(archived))

	for _, agent := range archived {
		claimed := agentName{workflowID: agent.WorkflowID, name: agent.Name}
		if checked[claimed] {
			continue
		}
		checked[claimed] = true

		if returning[claimed] > 1 {
			clashing = append(clashing, agent.Name)

			continue
		}

		taken, err := agents.ExistsActiveName(ctx, agent.WorkflowID, agent.Name)
		if err != nil {
			return fmt.Errorf("failed to check the name of agent %q: %w", agent.Name, err)
		}
		if taken {
			clashing = append(clashing, agent.Name)
		}
	}
	if len(clashing) > 0 {
		return fmt.Errorf("%w: rename whatever holds %s before restoring", store.ErrNameTaken, strings.Join(clashing, ", "))
	}

	return nil
}
