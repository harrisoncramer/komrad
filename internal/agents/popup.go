package agents

// managerOpenFlag marks that the popup is currently shown, so the binding hides
// it rather than opening a second one. The open command sets it while the popup
// is up and clears it on close. The tmux plugin reads it.
const managerOpenFlag = "@claude_manager_open"

// managerClientFlag holds the tmux client that opened the current popup. The open
// command writes it on each open, and the manager reads it live, so actions that
// need the invoking client use the one actually viewing the popup rather than one
// fixed when the persistent panel first started.
const managerClientFlag = "@claude_manager_client"

// MarkOpen records that the popup is shown.
func (m *AgentManager) MarkOpen() { m.tmux.SetGlobal(managerOpenFlag, "1") }

// MarkClosed records that the popup is hidden.
func (m *AgentManager) MarkClosed() { m.tmux.SetGlobal(managerOpenFlag, "0") }

// SetInvokingClient records the tmux client that opened the current popup.
func (m *AgentManager) SetInvokingClient(client string) {
	m.tmux.SetGlobal(managerClientFlag, client)
}

// HideManager hides the popup by detaching the client viewing the persistent
// manager session. The session stays alive, so the next open restores it.
func (m *AgentManager) HideManager() { m.tmux.DetachSession(ManagerSession) }

// SilenceStatus turns off a session's tmux status bar, so the manager panel and
// the agents it opens fill the popup without a tmux footer under them.
func (m *AgentManager) SilenceStatus(session string) {
	m.tmux.SetSessionOption(session, "status", "off")
}

// InvokingClient returns the tmux client that opened the current popup.
func (m *AgentManager) InvokingClient() string { return m.tmux.ShowGlobal(managerClientFlag) }

// PaneAlive reports whether an agent's tmux pane still exists, for pruning
// entries the panel lists from the service.
