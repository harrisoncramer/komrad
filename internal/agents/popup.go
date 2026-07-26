package agents

// popupTmux is what the popup needs from tmux: the global options it uses as
// flags, and the ability to detach a client.
type popupTmux interface {
	SetGlobal(name, value string)
	ShowGlobal(name string) string
	SetSessionOption(target, name, value string)
	DetachSession(name string)
}

// Popup is the chrome around the manager popup: whether it is showing, which
// tmux client opened it, and how it hides. It holds no application state.
type Popup struct {
	tmux popupTmux
}

// NewPopup builds the popup chrome over a tmux client.
func NewPopup(tmux popupTmux) *Popup {
	return &Popup{tmux: tmux}
}

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
func (p *Popup) MarkOpen() { p.tmux.SetGlobal(managerOpenFlag, "1") }

// MarkClosed records that the popup is hidden.
func (p *Popup) MarkClosed() { p.tmux.SetGlobal(managerOpenFlag, "0") }

// SetInvokingClient records the tmux client that opened the current popup.
func (p *Popup) SetInvokingClient(client string) {
	p.tmux.SetGlobal(managerClientFlag, client)
}

// Hide closes the popup by detaching the client viewing the persistent manager
// session. The session stays alive, so the next open restores it.
func (p *Popup) Hide() { p.tmux.DetachSession(ManagerSession) }

// SilenceStatus turns off a session's tmux status bar, so the manager panel and
// the agents it opens fill the popup without a tmux footer under them.
func (p *Popup) SilenceStatus(session string) {
	p.tmux.SetSessionOption(session, "status", "off")
}

// InvokingClient returns the tmux client that opened the current popup.
func (p *Popup) InvokingClient() string { return p.tmux.ShowGlobal(managerClientFlag) }

// PaneAlive reports whether an agent's tmux pane still exists, for pruning
// entries the panel lists from the service.
