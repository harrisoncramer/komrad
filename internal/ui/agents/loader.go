package agents

import (
	"slices"

	"komrad/internal/ui/panel"
	"komrad/internal/ui/screen"
	"komrad/internal/view"
	agentv1 "komrad/proto/gen/agent/v1"
)

// IsLoading reports whether any row has an agent at work or setting up, or checks running.
func (a *Screen) IsLoading(in screen.Inputs) bool {
	return slices.ContainsFunc(a.rows(in), func(r agentRow) bool { return isRowLoading(in.Listing, r) })
}

func isRowLoading(l panel.Listing, r agentRow) bool {
	if r.isTask() {
		writer, ok := r.writer()

		return r.task.PullRequest.IsChecksRunning() || ok && isAgentLoading(writer)
	}
	if r.header {
		return r.orchestrator != nil && isThinking(*r.orchestrator)
	}

	return l.Tasks.FindForAgent(r.item.Agent).PullRequest.IsChecksRunning() || isAgentLoading(r.item)
}

// isAgentLoading reports whether an agent is setting up or at work, either of which turns the loader.
func isAgentLoading(item panel.AgentRow) bool {
	return item.Agent.State == agentv1.AgentState_AGENT_STATE_SETTING_UP || isThinking(item)
}

// isThinking reports whether an agent is at work behind a pane still running, which turns the
// loader after its name.
func isThinking(item panel.AgentRow) bool {
	return item.Process() == view.ProcessAlive && item.Status() == view.StatusThinking
}
