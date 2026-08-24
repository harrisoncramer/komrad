package ui

import (
	"fmt"
	"strings"
)

type popupTmux interface {
	SetGlobal(name, value string)
	ShowGlobal(name string) string
	SetSessionOption(target, name, value string)
	BindKey(key string, command ...string)
	DetachSession(name string)
}

type PopupWindow struct {
	tmux popupTmux

	session string
}

// NewPopupWindow builds the popup's window over a tmux client, for the panel running
// in the named tmux session.
func NewPopupWindow(tmux popupTmux, session string) *PopupWindow {
	return &PopupWindow{tmux: tmux, session: session}
}

const managerOpenFlag = "@weave_open"

const managerClientFlag = "@weave_client"

const managerKeyOption = "@weave_key"

const defaultPanelKey = "g"

const paletteKeyOption = "@weave_palette_key"

const defaultPaletteKey = "k"

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

type panelKeyTmux interface {
	ShowGlobal(name string) string
	SetGlobalChecked(name, value string) error
	BindKeyChecked(key string, command ...string) error
}

type BindPanelKeyParams struct {
	Self        string
	ConfigPaths []string
}

// BindPanelKey binds the panel key on the user's own tmux server, where it is pressed.
//
// The manager lives in a persistent tmux session shown through a popup. Pressing the key while
// the popup is up hides it by detaching that session's client, which leaves the panel running
// so reopening restores the exact view. Pressing it while hidden opens the popup, attaching to
// the persistent session. The open command owns the flag the binding branches on: it sets it
// while the popup is up and clears it when the popup closes.
//
// That session lives on a tmux server of its own, so it stays out of this server's session
// list and cannot be attached to outside the popup. Hiding therefore goes through the binary,
// which knows that server's socket. Open finds this server for itself, out of the TMUX
// variable tmux sets in the popup, because nothing can be handed over in the binding: tmux
// does not expand a #{format} in a popup command, and the shell running the command reads the
// unexpanded text as a comment.
//
// -EE, rather than -E, closes the popup only when the command succeeds. Detaching to hide the
// panel is a success, so that still closes it, but a startup failure leaves the popup up with
// its error on screen instead of destroying the popup in the same instant the message is
// written. Dismiss a held-open popup with Escape or C-c.
func BindPanelKey(t panelKeyTmux, params BindPanelKeyParams) error {
	key := t.ShowGlobal(managerKeyOption)
	if key == "" {
		key = defaultPanelKey
	}

	if t.ShowGlobal(managerOpenFlag) == "" {
		if err := t.SetGlobalChecked(managerOpenFlag, "0"); err != nil {
			return err
		}
	}

	hide := fmt.Sprintf("run-shell -b '%s'", panelCommand(params, "hide"))
	open := fmt.Sprintf("display-popup -w 100%% -h 100%% -x C -y C -T ' weave ' -EE '%s'",
		panelCommand(params, "open"))

	return t.BindKeyChecked(key, "if-shell", "-F", fmt.Sprintf("#{==:#{%s},1}", managerOpenFlag), hide, open)
}

func panelCommand(params BindPanelKeyParams, subcommand string) string {
	words := []string{params.Self, subcommand}
	for _, path := range params.ConfigPaths {
		words = append(words, "--config", path)
	}

	return strings.Join(words, " ")
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
