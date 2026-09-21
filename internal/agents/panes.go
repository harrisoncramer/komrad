package agents

import (
	"context"
	"log/slog"

	"weave/internal/logging"
	"weave/lib/xslice"

	agentstore "weave/internal/store/agent"
)

func isPaneOwnedBy(owner, agentID string) bool {
	return agentID != "" && owner == agentID
}

func getRunningAgents(ctx context.Context, tmux paneLister, agents []agentstore.Agent) map[string]bool {
	panes, err := tmux.GetPanes()
	if err != nil {
		logging.From(ctx).LogAttrs(ctx, slog.LevelDebug, "failed to read panes, reporting every agent as not running",
			logging.Err(err)...)
	}

	running := make(map[string]bool, len(agents))
	for _, agent := range agents {
		running[agent.ID] = isPaneOwnedBy(panes[agent.Pane].Owner, agent.ID)
	}

	return running
}

func killOwnedPanes(ctx context.Context, tmux paneKiller, agents []agentstore.Agent) {
	killPanes(ctx, tmux, xslice.Map(agents, func(agent agentstore.Agent) agentstore.AgentPane {
		return agentstore.AgentPane{AgentID: agent.ID, Pane: agent.Pane}
	}))
}

// killPanes kills each pane tmux still says belongs to the agent named beside it.
func killPanes(ctx context.Context, tmux paneKiller, owned []agentstore.AgentPane) {
	panes, err := tmux.GetPanes()
	if err != nil {
		logging.From(ctx).LogAttrs(ctx, slog.LevelDebug, "failed to read panes, killing none of them",
			logging.Err(err)...)

		return
	}

	for _, owner := range owned {
		if isPaneOwnedBy(panes[owner.Pane].Owner, owner.AgentID) {
			tmux.KillPane(owner.Pane)
		}
	}
}
