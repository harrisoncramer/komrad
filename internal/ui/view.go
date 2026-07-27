package ui

// View renders the active screen, or the unreachable screen over all of them while
// the service is not answering. Every screen below reads or writes through the
// service, so none can show anything true until it is back.
func (m model) View() string {
	if m.unreachable != nil {
		return m.unreachableView()
	}

	switch m.screen {
	case screenForm, screenRename, screenWorkflowForm, screenWorkflowDelete:
		return m.form.View()
	case screenSearch:
		return m.searchView()
	default:
		return m.agentsView()
	}
}
