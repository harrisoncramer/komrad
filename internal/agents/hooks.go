package agents

import (
	"context"
	"errors"
	"log/slog"
	"slices"

	"komrad/internal/logging"
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

	go func() {
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

// startRestoreHooks tells every agent a restore took out of the archive that its row is back
// but dormant.
func startRestoreHooks(ctx context.Context, scripts agentScriptRunner, workflows workflowReader, restored []agentstore.Agent) {
	for _, agent := range restored {
		startHooks(ctx, hookRunParams{
			scripts: scripts,
			agent:   agent,
			hooks:   agentHooks(ctx, workflows, agent),
			event:   workflowstore.HookPostRestore,
		})
	}
}

// agentHooks are the hooks a workflow lists for one of its agents, none for an agent working in
// a directory somebody else prepared.
func agentHooks(ctx context.Context, workflows workflowReader, agent agentstore.Agent) []workflowstore.Hook {
	if agent.ParentAgentID != nil || IsProvisionerDisabled(agent.ProvisionEnabled) {
		return nil
	}

	wf := workflowFor(ctx, workflows, agent)
	if wf == nil {
		return nil
	}

	return wf.Hooks
}

// runDeprovision starts a deprovisioner, calling done exactly once with the outcome,
// including a refusal to start.
func runDeprovision(ctx context.Context, deprovision agentScriptRunner, agent agentstore.Agent, name string, done func(error)) { //nolint:revive // a params struct is the follow-up
	if deprovision == nil || name == "" {
		if done != nil {
			done(nil)
		}

		return
	}

	if err := deprovision.RunDeprovision(ctx, agent, name, done); err != nil {
		attrs := []slog.Attr{
			slog.String(logging.ScriptName, name),
			slog.String(logging.ScriptAgentID, agent.ID),
			slog.String(logging.ScriptAgentCwd, agent.Cwd),
		}
		logging.From(ctx).LogAttrs(ctx, slog.LevelError, "the deprovisioner would not start", append(attrs, logging.Err(err)...)...)

		if done != nil {
			done(err)
		}
	}
}

// IsProvisionerDisabled reports whether an agent has been told to keep the directory it was
// given rather than be provisioned one.
func IsProvisionerDisabled(override *bool) bool {
	return override != nil && !*override
}

// doesProvisionWorktree reports whether an agent's directory is komrad's to make.
func doesProvisionWorktree(ctx context.Context, workflows workflowReader, agent agentstore.Agent) bool {
	if IsProvisionerDisabled(agent.ProvisionEnabled) {
		return false
	}

	wf := workflowFor(ctx, workflows, agent)

	return wf != nil && wf.Worktree.Enabled
}

func deprovisionerFor(ctx context.Context, workflows workflowReader, agent agentstore.Agent) string {
	if !doesProvisionWorktree(ctx, workflows, agent) {
		return ""
	}

	return worktreeDeprovisioner
}

func provisionerFor(ctx context.Context, workflows workflowReader, agent agentstore.Agent) string {
	if !doesProvisionWorktree(ctx, workflows, agent) {
		return ""
	}

	return worktreeProvisioner
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
