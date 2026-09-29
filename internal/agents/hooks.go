package agents

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"

	"komrad/internal/logging"
	"komrad/internal/scripts"
	"komrad/internal/store"

	agentstore "komrad/internal/store/agent"
	workflowstore "komrad/internal/store/workflow"
)

// hookRunParams is one moment's worth of work: the hooks a workflow lists, the agent they run
// against, and the moment itself.
type hookRunParams struct {
	scripts agentScriptRunner
	agent   agentstore.Agent
	hooks   []workflowstore.Hook
	event   string
}

// runHooks runs the hooks on one moment in order, waiting for each and stopping at the first
// that fails.
func runHooks(ctx context.Context, params hookRunParams) error {
	if params.scripts == nil {
		return nil
	}

	for _, hook := range params.hooks {
		if hook.Event != params.event {
			continue
		}

		if err := params.scripts.RunHook(ctx, params.agent, hook.Script, params.event); err != nil {
			return err
		}
	}

	return nil
}

func runPreHooks(ctx context.Context, params hookRunParams) error {
	if params.scripts == nil || !hasHookOn(params.hooks, params.event) {
		return nil
	}

	turn := params.scripts.QueueHooks(params.agent.ID)
	defer turn.Done()

	if err := turn.Wait(ctx); err != nil {
		return err
	}

	return runHooks(ctx, params)
}

type batchHooksParams struct {
	scripts     agentScriptRunner
	workflows   workflowReader
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
		if !hasHookOn(hooks, params.event) {
			continue
		}
		if params.skipGoneDir && scripts.RequireUsableDir(agent.Cwd) != nil {
			continue
		}

		wg.Go(func() {
			err := runPreHooks(ctx, hookRunParams{
				scripts: params.scripts,
				agent:   agent,
				hooks:   hooks,
				event:   params.event,
			})
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

// hasHookOn reports whether a workflow hangs any script on one moment.
func hasHookOn(hooks []workflowstore.Hook, event string) bool {
	return slices.ContainsFunc(hooks, func(hook workflowstore.Hook) bool { return hook.Event == event })
}

// startHooks runs a moment's hooks in the background.
func startHooks(ctx context.Context, params hookRunParams) {
	if params.scripts == nil || !hasHookOn(params.hooks, params.event) {
		return
	}

	bg := context.WithoutCancel(ctx)
	turn := params.scripts.QueueHooks(params.agent.ID)

	go func() {
		defer turn.Done()

		if err := turn.Wait(bg); err != nil {
			return
		}

		if err := runHooks(bg, hookRunParams{
			scripts: params.scripts,
			agent:   params.agent,
			hooks:   params.hooks,
			event:   params.event,
		}); err != nil {
			logging.From(bg).LogAttrs(bg, slog.LevelError, "a hook failed",
				append([]slog.Attr{
					slog.String(logging.RunTrigger, params.event),
					slog.String(logging.ScriptAgentID, params.agent.ID),
				}, logging.Err(err)...)...)
		}
	}()
}

func startHooksFor(ctx context.Context, params batchHooksParams) {
	for _, agent := range params.agents {
		startHooks(ctx, hookRunParams{
			scripts: params.scripts,
			agent:   agent,
			hooks:   agentHooks(ctx, params.workflows, agent),
			event:   params.event,
		})
	}
}

// agentHooks are the hooks a workflow lists for one of its agents, none for an agent working in
// a directory somebody else prepared.
func agentHooks(ctx context.Context, workflows workflowReader, agent agentstore.Agent) []workflowstore.Hook {
	if agent.ParentAgentID != nil || agent.IsProvisionerDisabled() {
		return nil
	}

	wf := workflowFor(ctx, workflows, agent)
	if wf == nil {
		return nil
	}

	return wf.Hooks
}

func workflowFor(ctx context.Context, workflows workflowReader, agent agentstore.Agent) *workflowstore.Workflow {
	wf, err := workflows.GetIncludingArchived(ctx, agent.WorkflowID)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		attrs := []slog.Attr{
			slog.String(logging.WorkflowID, agent.WorkflowID),
			slog.String(logging.ScriptAgentID, agent.ID),
			slog.String(logging.ScriptAgentCwd, agent.Cwd),
		}
		logging.From(ctx).LogAttrs(ctx, slog.LevelError, "the workflow behind an agent could not be read", append(attrs, logging.Err(err)...)...)

		return nil
	}

	return wf
}
