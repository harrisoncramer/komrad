package ui

import (
	"fmt"
	"strings"
)

// FocusKey moves the cursor between the panel's sidebar and the agent's pane on the stage.
const FocusKey = "C-a"

const managerKeyOption = "@komrade_key"

const defaultPanelKey = "g"

const hideKeyOption = "@komrade_hide_key"

type popupTmux interface {
	keyNamer
	SetGlobal(name, value string)
	GetSessionClient(session string) string
	SetSessionOption(target, name, value string)
	BindKey(key string, command ...string)
	UnbindKey(key string)
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

// BindHide binds the panel key on the manager's own tmux server, which sources the user's config
// too and would otherwise open a popup inside the popup.
func (p *PopupWindow) BindHide(key string) {
	if key == "" {
		key = defaultPanelKey
	}

	if last := p.tmux.ShowGlobal(hideKeyOption); last != "" && last != key {
		p.tmux.UnbindKey(last)
	}

	p.tmux.BindKey(key, "detach-client", "-s", p.session)
	p.tmux.SetGlobal(hideKeyOption, key)
}

// HoldFocusKey takes the focus key while an agent's pane is on the stage beside sidebar, or gives
// it back to the panes when that pane is empty.
func (p *PopupWindow) HoldFocusKey(sidebar, agent string) {
	switch {
	case sidebar == "":
	case p.focus != nil && *p.focus == agent:
	case agent != "":
		p.tmux.BindRootKey(FocusKey, "if-shell", "-F", "#{==:#{pane_id},"+sidebar+"}",
			"select-pane -t "+agent, "select-pane -t "+sidebar)
		p.focus = &agent
	default:
		p.tmux.UnbindRootKey(FocusKey)
		p.focus = &agent
	}
}

// ReleaseFocusKey gives the focus key back to the panes on the panel's way out.
func (p *PopupWindow) ReleaseFocusKey() {
	if p.focus == nil || *p.focus == "" {
		return
	}

	p.tmux.UnbindRootKey(FocusKey)
	p.focus = nil
}

type keyNamer interface {
	ShowGlobal(name string) string
}

// PanelKey settles the panel key from the config this komrade was given, falling back to the user
// tmux server's own option and then to the built-in key.
func PanelKey(namer keyNamer, configured string) string {
	if configured != "" {
		return configured
	}

	if key := namer.ShowGlobal(managerKeyOption); key != "" {
		return key
	}

	return defaultPanelKey
}

type panelKeyTmux interface {
	keyNamer
	BindKeyChecked(key string, command ...string) error
	UnbindKey(key string)
	GetPrefixKeys() (map[string]string, error)
}

type BindPanelKeyParams struct {
	Self       string
	ConfigArgs []string
	// Key is the panel key this komrade asks for, empty meaning whatever the tmux option says.
	Key string
}

// BindPanelKey binds the panel key on the user's own tmux server, where it is pressed, and
// takes back any other key it bound there before.
func BindPanelKey(t panelKeyTmux, params BindPanelKeyParams) error {
	// A komrade that names its own key can be bound beside another one.
	key := PanelKey(t, params.Key)

	shown := panelCommand(params, "shown")
	hide := fmt.Sprintf("run-shell -b '%s'", panelCommand(params, "hide"))
	open := fmt.Sprintf("display-popup -w 100%% -h 100%% -x C -y C -T ' komrade ' -EE '%s'",
		panelCommand(params, "open"))

	bound, err := t.GetPrefixKeys()
	if err != nil {
		return err
	}

	for other, command := range bound {
		if strings.HasPrefix(command, `if-shell "`+shown+`" `) {
			t.UnbindKey(other)
		}
	}

	return t.BindKeyChecked(key, "if-shell", shown, hide, open)
}

func panelCommand(params BindPanelKeyParams, subcommand string) string {
	words := append([]string{params.Self, subcommand}, params.ConfigArgs...)

	return strings.Join(words, " ")
}

// IsShown reports whether anything is attached to the panel's session, which the panel cannot
// see for itself since hiding only detaches the client and leaves this program running.
func (p *PopupWindow) IsShown() bool {
	return p.tmux.GetSessionClient(p.session) != ""
}

// Hide closes the popup by detaching its client, leaving the session alive for the next open.
func (p *PopupWindow) Hide() {
	p.tmux.DetachSession(p.session)
}

// SilenceStatus turns off a session's tmux status bar, so the manager panel and the agents it
// opens fill the popup without a tmux footer under them.
func (p *PopupWindow) SilenceStatus(session string) {
	p.tmux.SetSessionOption(session, "status", "off")
}
