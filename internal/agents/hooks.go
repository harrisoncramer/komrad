package agents

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"komrad/internal/logging"
	"komrad/internal/scriptrun"
	"komrad/internal/scripts"
	"komrad/internal/store"

	agentstore "komrad/internal/store/agent"
	workflowstore "komrad/internal/store/workflow"
)

type batchHooksParams struct {
	scripts     agentScriptRunner
	workflows   *workflowstore.WorkflowStore
	agents      []agentstore.Agent
	event       string
	skipGoneDir bool
}

func runPreHooksFor(ctx context.Context, params batchHooksParams) map[string]error {
	refused := make(map[string]error)
	if params.scripts == nil {
		return refused
	}

	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, agent := range params.agents {
		hooks := agentHooks(ctx, params.workflows, agent)
		if !scriptrun.HasHookOn(hooks, params.event) {
			continue
		}
		if params.skipGoneDir && scripts.RequireUsableDir(agent.Cwd) != nil {
			continue
		}

		wg.Go(func() {
			err := scriptrun.HookRun{
				Scripts: params.scripts,
				Agent:   agent,
				Hooks:   hooks,
				Event:   params.event,
			}.RunInTurn(ctx)
			if err == nil {
				return
			}

			mu.Lock()
			defer mu.Unlock()

			refused[agent.ID] = fmt.Errorf("failed to run the %s hook of %q: %w", params.event, agent.Name, err)
		})
	}

	wg.Wait()

	return refused
}

func joinRefusals(agents []agentstore.Agent, refused map[string]error) error {
	var errs []error
	for _, agent := range agents {
		if err, ok := refused[agent.ID]; ok {
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}

func startHooksFor(ctx context.Context, params batchHooksParams) {
	for _, agent := range params.agents {
		scriptrun.HookRun{
			Scripts: params.scripts,
			Agent:   agent,
			Hooks:   agentHooks(ctx, params.workflows, agent),
			Event:   params.event,
		}.Start(ctx)
	}
}

// agentHooks are the hooks a workflow lists for one of its agents, none for an agent working in
// a directory somebody else prepared.
func agentHooks(ctx context.Context, workflows *workflowstore.WorkflowStore, agent agentstore.Agent) []workflowstore.Hook {
	if agent.ParentAgentID != nil || agent.IsProvisionerDisabled() {
		return nil
	}

	return hooksFor(workflowFor(ctx, workflows, agent), agent.ParentAgentID != nil, agent.ProvisionEnabled)
}

func hooksFor(wf *workflowstore.Workflow, hasParent bool, provisionEnabled *bool) []workflowstore.Hook {
	// The hooks a workflow lists are read only for the agents that own their own directory, and a
	// child or an agent told to keep the directory it was given was handed somebody else's.
	if wf == nil || hasParent || agentstore.IsProvisionerDisabled(provisionEnabled) {
		return nil
	}

	return wf.Hooks
}

func workflowFor(ctx context.Context, workflows *workflowstore.WorkflowStore, agent agentstore.Agent) *workflowstore.Workflow {
	wf, err := workflows.GetIncludingArchived(ctx, agent.WorkflowID)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		logging.From(ctx).LogAttrs(ctx, slog.LevelError, "the workflow could not be read",
			append([]slog.Attr{
				slog.String(logging.WorkflowID, agent.WorkflowID),
				slog.String(logging.ScriptAgentID, agent.ID),
				slog.String(logging.ScriptAgentCwd, agent.Cwd),
			}, logging.Err(err)...)...)

		return nil
	}

	return wf
}
