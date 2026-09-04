package ui

import (
	"fmt"
	"strings"
)

type popupTmux interface {
	SetGlobal(name, value string)
	GetSessionClient(session string) string
	SetSessionOption(target, name, value string)
	BindKey(key string, command ...string)
	DetachSession(name string)
}

type PopupWindow struct {
	tmux    popupTmux
	session string
}

// NewPopupWindow builds the popup's window over a tmux client, for the panel running
// in the named tmux session.
func NewPopupWindow(tmux popupTmux, session string) *PopupWindow {
	return &PopupWindow{tmux: tmux, session: session}
}

const managerClientFlag = "@weave_client"

const managerKeyOption = "@weave_key"

const defaultPanelKey = "g"

const paletteKeyOption = "@weave_palette_key"

const defaultPaletteKey = "k"

const (
	palettePopupWidth  = "60"
	palettePopupHeight = "60%"
)

// BindHide binds the panel key, handed in because only the user's own server names it, on the
// manager's own tmux server, which sources the user's config too and would otherwise open a
// popup inside the popup.
func (p *PopupWindow) BindHide(key string) {
	if key == "" {
		key = defaultPanelKey
	}

	p.tmux.BindKey(key, "detach-client", "-s", p.session)
}

type BindPaletteParams struct {
	Key         string
	Self        string
	ConfigPaths []string
}

// BindPalette binds the palette key on the manager's own tmux server, since an agent's pane
// belongs to that server and the palette acts on the pane the key was pressed in.
func (p *PopupWindow) BindPalette(params BindPaletteParams) {
	key := params.Key
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
		"-T", " commands ",
		"-EE", params.Self, "palette",
	}
	for _, path := range params.ConfigPaths {
		command = append(command, "--config", path)
	}

	p.tmux.BindKey(key, command...)
}

type keyNamer interface {
	ShowGlobal(name string) string
}

// UserKeys returns the panel and palette keys as the user's own tmux server names them, empty
// where it names neither, since that server is the only place either can be set.
func UserKeys(namer keyNamer) (panel, palette string) {
	return namer.ShowGlobal(managerKeyOption), namer.ShowGlobal(paletteKeyOption)
}

type panelKeyTmux interface {
	keyNamer
	BindKeyChecked(key string, command ...string) error
}

type BindPanelKeyParams struct {
	Self        string
	ConfigPaths []string
}

// Explainer: the panel runs in a tmux session of its own that stays alive between presses, so
// hiding it detaches the popup's client and opening it attaches again, which is why reopening
// lands on the exact view it left. That session lives on a tmux server of its own, invisible to
// this one, so both branches go through the weave binary, which knows the socket. Open finds
// this server from the TMUX variable rather than being told, because tmux expands no #{format}
// inside a popup command and the shell reads the unexpanded text as a comment. The popup opens
// with -EE rather than -E so it closes on success but stays up with the error on screen when
// startup fails, and Escape or C-c dismisses one held open that way. Which branch runs is
// settled by asking weave on every press whether the panel is on screen here, rather than by a
// flag this server keeps, so a press can never read back a stale answer and refuse to open: an
// answer weave cannot give reads as hidden, and hidden opens the popup.

// BindPanelKey binds the panel key on the user's own tmux server, where it is pressed.
func BindPanelKey(t panelKeyTmux, params BindPanelKeyParams) error {
	key := t.ShowGlobal(managerKeyOption)
	if key == "" {
		key = defaultPanelKey
	}

	hide := fmt.Sprintf("run-shell -b '%s'", panelCommand(params, "hide"))
	open := fmt.Sprintf("display-popup -w 100%% -h 100%% -x C -y C -T ' weave ' -EE '%s'",
		panelCommand(params, "open"))

	return t.BindKeyChecked(key, "if-shell", panelCommand(params, "shown"), hide, open)
}

func panelCommand(params BindPanelKeyParams, subcommand string) string {
	words := []string{params.Self, subcommand}
	for _, path := range params.ConfigPaths {
		words = append(words, "--config", path)
	}

	return strings.Join(words, " ")
}

// IsShown reports whether anything is attached to the panel's session, which is what the panel
// cannot see for itself: hiding detaches the client and leaves this program running.
func (p *PopupWindow) IsShown() bool {
	return p.tmux.GetSessionClient(p.session) != ""
}

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
