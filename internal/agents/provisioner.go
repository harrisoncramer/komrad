package agents

import (
	"context"
	"log/slog"

	"komrad/internal/logging"
	agentstore "komrad/internal/store/agent"
)

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

// doesProvisionWorktree reports whether an agent's directory is komrad's to make.
func doesProvisionWorktree(ctx context.Context, workflows workflowReader, agent agentstore.Agent) bool {
	if agent.IsProvisionerDisabled() {
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
