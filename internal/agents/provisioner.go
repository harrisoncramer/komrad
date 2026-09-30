package agents

import (
	"context"
	"log/slog"

	"komrad/internal/logging"
	agentstore "komrad/internal/store/agent"
	workflowstore "komrad/internal/store/workflow"
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

func deprovisionerFor(ctx context.Context, workflows workflowReader, agent agentstore.Agent) string {
	if agent.IsProvisionerDisabled() {
		return ""
	}

	_, deprovisioner := worktreeScripts(workflowFor(ctx, workflows, agent), agent.ProvisionEnabled)

	return deprovisioner
}

func provisionerFor(ctx context.Context, workflows workflowReader, agent agentstore.Agent) string {
	if agent.IsProvisionerDisabled() {
		return ""
	}

	provisioner, _ := worktreeScripts(workflowFor(ctx, workflows, agent), agent.ProvisionEnabled)

	return provisioner
}

func worktreeScripts(wf *workflowstore.Workflow, provisionEnabled *bool) (provisioner, deprovisioner string) {
	// The worktree is komrad's own, so an agent gets the built-in pair or nothing at all.
	if wf == nil || !wf.Worktree.Enabled || agentstore.IsProvisionerDisabled(provisionEnabled) {
		return "", ""
	}

	return worktreeProvisioner, worktreeDeprovisioner
}
