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

// BindHide binds the panel key on the manager's own tmux server to hiding the popup, since
// that server sources the user's config too and would otherwise open a popup inside the popup.
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

// BindPalette binds the palette key on the manager's own tmux server, since an agent's pane
// belongs to that server and the palette acts on the pane the key was pressed in.
func (p *PopupWindow) BindPalette(params BindPaletteParams) {
	key := p.tmux.ShowGlobal(paletteKeyOption)
	if key == "" {
		key = defaultPaletteKey
	}

	// tmux expands no format in a popup's command, so the palette reads the pane off the server
	// rather than being handed it here.
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

// Explainer: the panel runs in a tmux session of its own that stays alive between presses, so
// hiding it detaches the popup's client and opening it attaches again, which is why reopening
// lands on the exact view it left. The open command sets and clears the flag this binding
// branches on. That session lives on a tmux server of its own, invisible to this one, so hiding
// has to go through the weave binary, which knows the socket. Open finds this server from the
// TMUX variable rather than being told, because tmux expands no #{format} inside a popup command
// and the shell reads the unexpanded text as a comment. The popup opens with -EE rather than -E
// so it closes on success but stays up with the error on screen when startup fails, and Escape
// or C-c dismisses one held open that way.

// BindPanelKey binds the panel key on the user's own tmux server, where it is pressed.
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

// Hide closes the popup by detaching its client, leaving the session alive for the next open.
func (p *PopupWindow) Hide() { p.tmux.DetachSession(p.session) }

// SilenceStatus turns off a session's tmux status bar, so the manager panel and
// the agents it opens fill the popup without a tmux footer under them.
func (p *PopupWindow) SilenceStatus(session string) {
	p.tmux.SetSessionOption(session, "status", "off")
}

// InvokingClient returns the tmux client that opened the current popup.
func (p *PopupWindow) InvokingClient() string { return p.tmux.ShowGlobal(managerClientFlag) }
