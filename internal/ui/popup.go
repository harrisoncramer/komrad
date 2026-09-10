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
	BindRootKey(key string, command ...string)
	UnbindRootKey(key string)
	DetachSession(name string)
}

type PopupWindow struct {
	tmux    popupTmux
	session string
	// focus is the pane the key is bound to jump to, empty where it is unbound, and nil until the
	// panel has settled it once.
	focus *string
}

// NewPopupWindow builds the popup's window over a tmux client, for the panel running in the
// named tmux session.
func NewPopupWindow(tmux popupTmux, session string) *PopupWindow {
	return &PopupWindow{tmux: tmux, session: session}
}

// focusKey moves the cursor between the panel's sidebar and the agent's pane on the stage.
const focusKey = "C-a"

const managerKeyOption = "@weave_key"

const defaultPanelKey = "g"

// BindHide binds the panel key on the manager's own tmux server, which sources the user's config
// too and would otherwise open a popup inside the popup.
func (p *PopupWindow) BindHide(key string) {
	if key == "" {
		key = defaultPanelKey
	}

	p.tmux.BindKey(key, "detach-client", "-s", p.session)
}

// Explainer: the focus key is bound outside the prefix table on weave's own tmux server, which
// is the only place that sees every press: a pane running an agent hands its keys to that agent,
// so the panel's own bubbletea bindings can never reach past the sidebar. Which way the press
// goes is settled by tmux comparing the active pane against the sidebar's, with no shell and no
// call back into weave, and both arms name the pane they move to, so a split the user opened by
// hand is never what the key lands on. A press with no pane to go back to is refused by tmux and
// does nothing, which is the same outcome as returning to a pane that has since died. A root
// binding is unconditional for as long as it stands, so the panel binds it only while an agent's
// pane is on the stage, and gives it back when the stage holds none and on the screens that
// stage no agent.

// HoldFocusKey takes the focus key while an agent's pane is on the stage beside sidebar, or gives
// it back to the panes when that pane is empty.
func (p *PopupWindow) HoldFocusKey(sidebar, agent string) {
	switch {
	case sidebar == "":
	case p.focus != nil && *p.focus == agent:
	case agent != "":
		p.tmux.BindRootKey(focusKey, "if-shell", "-F", "#{==:#{pane_id},"+sidebar+"}",
			"select-pane -t "+agent, "select-pane -t "+sidebar)
		p.focus = &agent
	default:
		p.tmux.UnbindRootKey(focusKey)
		p.focus = &agent
	}
}

// ReleaseFocusKey gives the focus key back to the panes, which the panel owes them on its way out
// because the tmux server it bound the key on outlives it.
func (p *PopupWindow) ReleaseFocusKey() {
	if p.focus == nil || *p.focus == "" {
		return
	}

	p.tmux.UnbindRootKey(focusKey)
	p.focus = nil
}

type keyNamer interface {
	ShowGlobal(name string) string
}

// UserPanelKey returns the panel key as the user's own tmux server names it, empty where it
// names none.
func UserPanelKey(namer keyNamer) string {
	return namer.ShowGlobal(managerKeyOption)
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

// IsShown reports whether anything is attached to the panel's session, which the panel cannot
// see for itself since hiding only detaches the client and leaves this program running.
func (p *PopupWindow) IsShown() bool {
	return p.tmux.GetSessionClient(p.session) != ""
}

// Hide closes the popup by detaching its client, leaving the session alive for the next open.
func (p *PopupWindow) Hide() { p.tmux.DetachSession(p.session) }

// SilenceStatus turns off a session's tmux status bar, so the manager panel and the agents it
// opens fill the popup without a tmux footer under them.
func (p *PopupWindow) SilenceStatus(session string) {
	p.tmux.SetSessionOption(session, "status", "off")
}
