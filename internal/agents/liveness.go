package agents

import (
	"context"

	agentstore "weave/internal/store/agent"
)

// PaneLiveness answers whether an agent is still running, which is true exactly while the pane
// its row names is still stamped as its own.
type PaneLiveness struct {
	tmux paneOwnerReader
}

// NewPaneLiveness builds the reader over the tmux server.
func NewPaneLiveness(panes paneOwnerReader) *PaneLiveness {
	return &PaneLiveness{tmux: panes}
}

// IsAgentRunning reports whether the agent still owns its recorded pane.
func (l *PaneLiveness) IsAgentRunning(agent agentstore.Agent) bool {
	return isPaneOwnedBy(l.tmux.GetPaneOwner(agent.Pane), agent.ID)
}

// GetRunningAgents returns agent liveness keyed by agent ID.
func (l *PaneLiveness) GetRunningAgents(ctx context.Context, agents []agentstore.Agent) map[string]bool {
	return getRunningAgents(ctx, l.tmux, agents)
}
