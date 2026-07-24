package panel

// View renders the active screen.
func (m model) View() string {
	switch m.screen {
	case screenForm, screenRename, screenWorkflowForm, screenWorkflowDelete:
		return m.form.View()
	case screenSearch:
		return m.searchView()
	default:
		return m.chatsView()
	}
}
