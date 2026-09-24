package ui

// agentHelp is the agents legend as the frame draws it.
func (m model) agentHelp() keyMap {
	return m.listHelp(m.agents.help(m.shared()))
}

// agentsHeader is the list's header as the frame draws it.
func (m model) agentsHeader() string {
	params := m.agents.listParams(m.shared())

	return m.tabbedHeader(params.title, params.tally)
}

func (m model) agentsListView() string {
	return m.screenListView(m.agents.listParams(m.shared()))
}
