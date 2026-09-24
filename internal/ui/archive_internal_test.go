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

func (m model) archiveHelp() keyMap {
	return m.listHelpOf(m.archive.listParams(m.shared()))
}
