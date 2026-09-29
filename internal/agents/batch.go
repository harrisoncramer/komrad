package agents

import (
	"context"

	"komrad/config"
	"komrad/internal/store"

	agentstore "komrad/internal/store/agent"
	workflowstore "komrad/internal/store/workflow"
)

type archiveBatchParams struct {
	tmux      paneKiller
	scripts   agentScriptRunner
	workflows workflowReader
	live      []agentstore.Agent
	owned     []agentstore.Agent
	archive   func(ctx context.Context) ([]agentstore.AgentPane, error)
}

func archiveBatch(ctx context.Context, params archiveBatchParams) error {
	if err := joinRefusals(params.live, runPreHooksFor(ctx, batchHooksParams{
		scripts:     params.scripts,
		workflows:   params.workflows,
		agents:      params.live,
		event:       workflowstore.HookPreArchive,
		skipGoneDir: true,
	})); err != nil {
		return err
	}

	// Killing what was read first keeps a failed write from hiding a live CLI, leaving the row
	// for the reconciler to tidy.
	killOwnedPanes(ctx, params.tmux, params.owned)

	taken, err := params.archive(ctx)
	if err != nil {
		return err
	}
	// A pane recorded in between only reaches here through what the write took off the rows.
	killPanes(ctx, params.tmux, taken)

	startHooksFor(ctx, batchHooksParams{
		scripts:   params.scripts,
		workflows: params.workflows,
		agents:    params.live,
		event:     workflowstore.HookPostArchive,
	})

	return nil
}

type agentsByIDLister interface {
	ListByID(ctx context.Context, ids []string) ([]agentstore.Agent, error)
}

type restoreBatchParams struct {
	tx        txRunner
	usage     config.Usage
	scripts   agentScriptRunner
	workflows workflowReader
	agents    agentsByIDLister
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
		event:       workflowstore.HookPreRestore,
		skipGoneDir: true,
	})); err != nil {
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

		return params.restore(ctx, db)
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
		event:     workflowstore.HookPostRestore,
	})

	return nil
}
