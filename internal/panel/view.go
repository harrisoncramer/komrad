package panel

func (m model) View() string {
	switch m.screen {
	case screenForm, screenRename, screenWorkflowForm, screenWorkflowDelete:
		return m.form.View()
	default:
		return m.chatsView()
	}
}
