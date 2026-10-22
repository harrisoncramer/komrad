package agents

import (
	"context"
	"log/slog"

	"komrad/internal/logging"
	"komrad/internal/scriptrun"
	agentstore "komrad/internal/store/agent"
	workflowstore "komrad/internal/store/workflow"
)

type runDeprovisionParams struct {
	deprovision agentScriptRunner
	agent       agentstore.Agent
	name        string
	done        func(error)
}

// runDeprovision starts a deprovisioner, calling done exactly once with the outcome,
// including a refusal to start.
func runDeprovision(ctx context.Context, params runDeprovisionParams) {
	if params.deprovision == nil || params.name == "" {
		if params.done != nil {
			params.done(nil)
		}

		return
	}

	if err := params.deprovision.RunDeprovision(ctx, params.agent, params.name, params.done); err != nil {
		attrs := []slog.Attr{
			slog.String(logging.ScriptName, params.name),
			slog.String(logging.ScriptAgentID, params.agent.ID.String()),
			slog.String(logging.ScriptAgentCwd, params.agent.Cwd),
		}
		logging.From(ctx).LogAttrs(ctx, slog.LevelError, "the deprovisioner would not start", append(attrs, logging.Err(err)...)...)

		if params.done != nil {
			params.done(err)
		}
	}
}

func provisionerFor(ctx context.Context, workflows *workflowstore.WorkflowStore, agent agentstore.Agent) string {
	if agent.IsProvisionerDisabled() {
		return ""
	}

	provisioner, _ := scriptrun.WorktreeScripts(workflowFor(ctx, workflows, agent), agent.ProvisionEnabled)

	return provisioner
}
