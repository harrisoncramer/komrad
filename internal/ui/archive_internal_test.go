package ui

import (
	"weave/internal/ui/panel"
	"weave/lib/xslice"
)

func archivedItems(agents []panel.Agent) []panel.AgentRow {
	return xslice.Map(agents, func(agent panel.Agent) panel.AgentRow {
		return panel.AgentRow{Agent: agent}
	})
}
