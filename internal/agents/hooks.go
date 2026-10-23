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
	"komrad/lib/protoenum"
	agentv1 "komrad/proto/gen/agent/v1"
	eventv1 "komrad/proto/gen/event/v1"

	"github.com/google/uuid"
)

type batchHooksParams struct {
	scripts     agentScriptRunner
	workflows   *workflowstore.WorkflowStore
	agents      []agentstore.Agent
	moment      eventv1.Moment
	skipGoneDir bool
}

func runPreHooksFor(ctx context.Context, params batchHooksParams) map[uuid.UUID]error {
	refused := make(map[uuid.UUID]error)
	if params.scripts == nil {
		return refused
	}

	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, agent := range params.agents {
		hooks := agentHooks(ctx, params.workflows, agent)
		if !scriptrun.HasHookOn(hooks, params.moment) {
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
				Moment:  params.moment,
			}.RunInTurn(ctx)
			if err == nil {
				return
			}

			mu.Lock()
			defer mu.Unlock()

			refused[agent.ID] = fmt.Errorf("failed to run the %s hook of %q: %w", protoenum.Name(params.moment), agent.Name, err)
		})
	}

	wg.Wait()

	return refused
}

func joinRefusals(agents []agentstore.Agent, refused map[uuid.UUID]error) error {
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
			Moment:  params.moment,
		}.Start(ctx)
	}
}

// agentHooks are the hooks a workflow lists for one of its agents, which only a writer that owns its
// directory runs.
func agentHooks(ctx context.Context, workflows *workflowstore.WorkflowStore, agent agentstore.Agent) []workflowstore.Hook {
	if agent.Role != agentv1.AgentRole_AGENT_ROLE_WRITER || agent.IsProvisionerDisabled() {
		return nil
	}

	return hooksFor(workflowFor(ctx, workflows, agent), agent.Role, agent.ProvisionEnabled)
}

func hooksFor(wf *workflowstore.Workflow, role agentv1.AgentRole, provisionEnabled *bool) []workflowstore.Hook {
	// The hooks a workflow lists are read only for a writer that owns its own directory, and a
	// writer told to keep the directory it was given was handed somebody else's.
	if wf == nil || role != agentv1.AgentRole_AGENT_ROLE_WRITER || agentstore.IsProvisionerDisabled(provisionEnabled) {
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
				slog.String(logging.WorkflowID, agent.WorkflowID.String()),
				slog.String(logging.ScriptAgentID, agent.ID.String()),
				slog.String(logging.ScriptAgentCwd, agent.Cwd),
			}, logging.Err(err)...)...)

		return nil
	}

	return wf
}
