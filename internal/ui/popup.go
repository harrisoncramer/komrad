package ui

// popupTmux is what the popup needs from tmux, the global options it uses as flags
// plus the ability to bind a key and detach a client. The client behind it spans two
// tmux servers. Options are the user's, since the plugin's key binding reads them
// there, while the session and the key bound inside the popup are the manager's.
type popupTmux interface {
	SetGlobal(name, value string)
	ShowGlobal(name string) string
	SetSessionOption(target, name, value string)
	BindKey(key string, command ...string)
	DetachSession(name string)
}

// PopupWindow is the popup as a window: whether it is showing, which tmux client
// opened it, and how it hides. It is the panel's own window rather than anything
// about agents, and it holds no application state.
type PopupWindow struct {
	tmux popupTmux
	// session is the tmux session the panel runs in, the one Hide detaches from.
	session string
}

// NewPopupWindow builds the popup's window over a tmux client, for the panel running
// in the named tmux session.
func NewPopupWindow(tmux popupTmux, session string) *PopupWindow {
	return &PopupWindow{tmux: tmux, session: session}
}

// managerOpenFlag marks that the popup is currently shown, so the binding hides
// it rather than opening a second one. The open command sets it while the popup
// is up. Both open's own exit and hide clear it, so a stuck flag from a killed
// or crashed open self-heals on the next hide attempt. The tmux plugin reads it.
const managerOpenFlag = "@weave_open"

// managerClientFlag holds the tmux client that opened the current popup. The open
// command writes it on each open, and the manager reads it live, so anything that
// needs the invoking client uses the one actually viewing the popup rather than one
// fixed when the persistent panel first started.
const managerClientFlag = "@weave_client"

// managerKeyOption holds the user's chosen panel key. The tmux plugin reads it on
// the user's server to bind the key there, and the popup reads the same option to
// bind its own meaning for it inside the popup.
const managerKeyOption = "@weave_key"

// defaultPanelKey is the panel key when the user has chosen none. The plugin script
// carries the same default, since it binds the key on the user's server.
const defaultPanelKey = "g"

// paletteKeyOption holds the user's chosen palette key, read off their own server beside
// the panel key.
const paletteKeyOption = "@weave_palette_key"

// defaultPaletteKey is the palette key when the user has chosen none.
const defaultPaletteKey = "k"

// The popup the palette is drawn in, wide enough for an action's description and tall enough
// for a list worth narrowing.
const (
	palettePopupWidth  = "60"
	palettePopupHeight = "60%"
)

// BindHide binds the panel key on the manager's own tmux server to hiding the
// popup. The popup's client is attached to that server, so a keypress inside the
// popup is read by its bindings rather than the user's. That server sources the
// user's tmux config as any new server does, which binds the key to opening the
// popup, and inside the popup that would stack a second one on top of itself.
func (p *PopupWindow) BindHide() {
	key := p.tmux.ShowGlobal(managerKeyOption)
	if key == "" {
		key = defaultPanelKey
	}

	p.tmux.BindKey(key, "detach-client", "-s", p.session)
}

// BindPaletteParams is what binding the palette key takes: the binary the popup runs and the
// config layers this process resolved, so the palette reads the files the panel was started
// on rather than working them out again.
type BindPaletteParams struct {
	Self        string
	ConfigPaths []string
}

// BindPalette binds the palette key on the manager's own tmux server to a popup running the
// palette. It is bound there rather than on the user's server because an agent's pane is the
// manager's, and the palette acts on the agent whose pane the key was pressed in.
//
// The pane is not passed on the command line, since tmux expands no format in a popup's
// command. The palette reads it back off the server instead.
func (p *PopupWindow) BindPalette(params BindPaletteParams) {
	key := p.tmux.ShowGlobal(paletteKeyOption)
	if key == "" {
		key = defaultPaletteKey
	}

	// -EE keeps the popup up when the command exits non-zero, so a pane holding no agent and
	// an action that would not start say so rather than closing over the message.
	command := []string{
		"display-popup",
		"-w", palettePopupWidth,
		"-h", palettePopupHeight,
		"-x", "C", "-y", "C",
		"-T", " actions ",
		"-EE", params.Self, "palette",
	}
	for _, path := range params.ConfigPaths {
		command = append(command, "--config", path)
	}

	p.tmux.BindKey(key, command...)
}

// MarkOpen records that the popup is shown.
func (p *PopupWindow) MarkOpen() { p.tmux.SetGlobal(managerOpenFlag, "1") }

// MarkClosed records that the popup is hidden.
func (p *PopupWindow) MarkClosed() { p.tmux.SetGlobal(managerOpenFlag, "0") }

// SetInvokingClient records the tmux client that opened the current popup.
func (p *PopupWindow) SetInvokingClient(client string) {
	p.tmux.SetGlobal(managerClientFlag, client)
}

// Hide closes the popup by detaching the client viewing the persistent manager
// session. The session stays alive, so the next open restores it.
func (p *PopupWindow) Hide() { p.tmux.DetachSession(p.session) }

// SilenceStatus turns off a session's tmux status bar, so the manager panel and
// the agents it opens fill the popup without a tmux footer under them.
func (p *PopupWindow) SilenceStatus(session string) {
	p.tmux.SetSessionOption(session, "status", "off")
}

// InvokingClient returns the tmux client that opened the current popup.
func (p *PopupWindow) InvokingClient() string { return p.tmux.ShowGlobal(managerClientFlag) }
