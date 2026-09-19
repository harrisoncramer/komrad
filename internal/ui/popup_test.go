package ui

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type bindingTmux struct {
	globals   map[string]string
	bound     map[string][]string
	unbound   []string
	rootBinds int
}

func (t *bindingTmux) SetGlobal(name, value string) {
	if t.globals == nil {
		t.globals = map[string]string{}
	}

	t.globals[name] = value
}

func (t *bindingTmux) ShowGlobal(name string) string           { return t.globals[name] }
func (t *bindingTmux) GetSessionClient(string) string          { return "" }
func (t *bindingTmux) SetSessionOption(string, string, string) {}
func (t *bindingTmux) DetachSession(string)                    {}

func (t *bindingTmux) BindKey(key string, command ...string) {
	if t.bound == nil {
		t.bound = map[string][]string{}
	}

	t.bound[key] = command
}

func (t *bindingTmux) BindRootKey(key string, command ...string) {
	if t.bound == nil {
		t.bound = map[string][]string{}
	}

	t.bound["root:"+key] = command
	t.rootBinds++
}

func (t *bindingTmux) UnbindKey(key string) {
	delete(t.bound, key)

	t.unbound = append(t.unbound, key)
}

func (t *bindingTmux) UnbindRootKey(key string) {
	delete(t.bound, "root:"+key)

	t.unbound = append(t.unbound, key)
}

type panelKeyRecorder struct {
	bindingTmux
	spellings map[string]string
	table     map[string]string
	err       error
}

func (t *panelKeyRecorder) BindKeyChecked(key string, command ...string) error {
	if listed, respelled := t.spellings[key]; respelled {
		key = listed
	}

	t.BindKey(key, command...)

	if t.table == nil {
		t.table = map[string]string{}
	}

	t.table[key] = printBinding(command)

	return t.err
}

func (t *panelKeyRecorder) UnbindKey(key string) {
	t.bindingTmux.UnbindKey(key)

	delete(t.table, key)
}

func (t *panelKeyRecorder) GetPrefixKeys() (map[string]string, error) {
	return t.table, nil
}

func printBinding(command []string) string {
	words := make([]string, 0, len(command))
	for _, word := range command {
		if strings.Contains(word, " ") {
			word = `"` + word + `"`
		}

		words = append(words, word)
	}

	return strings.Join(words, " ")
}

// TestBindPanelKey pins the binding naming this binary's own path, and pins both branches going
// through it.
func TestBindPanelKey(t *testing.T) {
	tests := []struct {
		name    string
		chosen  string
		wantKey string
	}{
		{
			name:    "no chosen key falls back to the default",
			chosen:  "",
			wantKey: defaultPanelKey,
		},
		{
			name:    "the user's own key wins",
			chosen:  "w",
			wantKey: "w",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmux := &panelKeyRecorder{}
			if tt.chosen != "" {
				tmux.SetGlobal(managerKeyOption, tt.chosen)
			}

			require.NoError(t, BindPanelKey(tmux, BindPanelKeyParams{
				Self:       "/usr/local/bin/weave",
				ConfigArgs: []string{"--config", "/etc/weave.toml", "--config", "/home/dev/weave.toml"},
			}))

			command, bound := tmux.bound[tt.wantKey]
			require.True(t, bound, "the panel key is not bound")

			assert.NotContains(t, tmux.globals, "@weave_open",
				"the binding writes no flag a later press could read back as stale")
			assert.Equal(t, []string{
				"if-shell",
				"/usr/local/bin/weave shown --config /etc/weave.toml --config /home/dev/weave.toml",
				"run-shell -b '/usr/local/bin/weave hide --config /etc/weave.toml --config /home/dev/weave.toml'",
				"display-popup -w 100% -h 100% -x C -y C -T ' weave ' " +
					"-EE '/usr/local/bin/weave open --config /etc/weave.toml --config /home/dev/weave.toml'",
			}, command)
		})
	}
}

// TestBindPanelKeyTakesBackTheKeyItBoundLast pins the binding a run before it made going when
// the key changes, and only that one, whatever spelling the key was written in.
func TestBindPanelKeyTakesBackTheKeyItBoundLast(t *testing.T) {
	local := BindPanelKeyParams{Self: "/usr/local/bin/weave", ConfigArgs: []string{"--config", "/etc/weave.toml"}}
	remote := BindPanelKeyParams{
		Self:       "/usr/local/bin/weave",
		ConfigArgs: []string{"--config", "/etc/weave.toml", "--engine", "remote"},
		Key:        "G",
	}

	tmux := &panelKeyRecorder{spellings: map[string]string{"c-g": "C-g"}}
	tmux.SetGlobal(managerKeyOption, "c-g")
	require.NoError(t, tmux.BindKeyChecked("s", "split-window"))
	require.NoError(t, BindPanelKey(tmux, remote))
	require.NoError(t, BindPanelKey(tmux, local))
	require.NoError(t, BindPanelKey(tmux, local))

	assert.Contains(t, tmux.bound, "C-g", "a key bound again is bound as tmux spells it, not taken for a stale one")
	assert.Equal(t, []string{"C-g"}, tmux.unbound, "and the run before it is what was taken back")

	tmux.SetGlobal(managerKeyOption, "j")
	require.NoError(t, BindPanelKey(tmux, local))

	assert.Equal(t, []string{"C-g", "C-g"}, tmux.unbound, "the key this weave bound before is the only one taken back")
	assert.NotContains(t, tmux.bound, "C-g")
	assert.Contains(t, tmux.bound, "j")
	assert.Contains(t, tmux.bound, "G", "the other engine's key, whose command this one's is a prefix of, stands")
	assert.Contains(t, tmux.bound, "s", "and so does a binding that is not weave's")
}

// TestBindPanelKeyReportsRefusal pins the refusal reaching the person who asked for it.
func TestBindPanelKeyReportsRefusal(t *testing.T) {
	tmux := &panelKeyRecorder{err: assert.AnError}

	require.Error(t, BindPanelKey(tmux, BindPanelKeyParams{Self: "/usr/local/bin/weave"}))
}

// TestIsShown covers the panel asking whether anybody is looking at it, which is a client on
// its own session rather than anything a popup wrote down as it opened.
func TestIsShown(t *testing.T) {
	tests := []struct {
		name   string
		client string
		want   bool
	}{
		{
			name:   "a client is attached to the panel's session",
			client: "/dev/ttys004",
			want:   true,
		},
		{name: "the popup was hidden, which detached it"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmux := &recordingTmux{sessionClient: tt.client}

			assert.Equal(t, tt.want, NewPopupWindow(tmux, "test-session").IsShown())
		})
	}
}

// TestBindHide pins the key the popup closes on being the one handed in.
func TestBindHide(t *testing.T) {
	tests := []struct {
		name    string
		chosen  string
		wantKey string
	}{
		{
			name:    "no chosen key falls back to the default",
			wantKey: defaultPanelKey,
		},
		{
			name:    "the user's own key wins",
			chosen:  "w",
			wantKey: "w",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmux := &bindingTmux{}

			NewPopupWindow(tmux, "manager").BindHide(tt.chosen)

			assert.Equal(t, []string{"detach-client", "-s", "manager"}, tmux.bound[tt.wantKey],
				"the panel key is not bound to hiding the popup")
		})
	}
}

// TestBindHideForgetsTheKeyItBoundLast pins the old hide binding going when the key changes.
func TestBindHideForgetsTheKeyItBoundLast(t *testing.T) {
	tmux := &bindingTmux{}

	NewPopupWindow(tmux, "manager").BindHide("w")
	NewPopupWindow(tmux, "manager").BindHide("j")

	_, bound := tmux.bound["w"]
	assert.False(t, bound, "the key the last open bound no longer hides the panel")
	assert.Equal(t, []string{"detach-client", "-s", "manager"}, tmux.bound["j"])

	NewPopupWindow(tmux, "manager").BindHide("j")

	assert.Equal(t, []string{"w"}, tmux.unbound, "a key bound again is left standing")
}

// TestPanelKey pins the order the key is settled in, which both the binding that opens the panel
// and the one that hides it read.
func TestPanelKey(t *testing.T) {
	tests := []struct {
		name       string
		configured string
		option     string
		want       string
	}{
		{
			name: "nothing names a key, so the built-in one stands",
			want: defaultPanelKey,
		},
		{
			name:   "the tmux option names one",
			option: "w",
			want:   "w",
		},
		{
			name:       "the config names one",
			configured: "w",
			want:       "w",
		},
		{
			name:       "the config beats the tmux option",
			configured: "w",
			option:     "j",
			want:       "w",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmux := &bindingTmux{}
			if tt.option != "" {
				tmux.SetGlobal(managerKeyOption, tt.option)
			}

			assert.Equal(t, tt.want, PanelKey(tmux, tt.configured))
		})
	}
}

// TestBindFocusKey pins the branch test being a tmux format and not a shell command, and pins
// the two ways of binding nothing.
func TestBindFocusKey(t *testing.T) {
	tests := []struct {
		name    string
		sidebar string
		agent   string
		want    []string
	}{
		{
			name:    "the key toggles between the sidebar and the agent's own pane",
			sidebar: "%3",
			agent:   "%9",
			want: []string{
				"if-shell", "-F", "#{==:#{pane_id},%3}",
				"select-pane -t %9", "select-pane -t %3",
			},
		},
		{
			name:    "no sidebar binds nothing",
			sidebar: "",
			agent:   "%9",
		},
		{
			name:    "no agent pane binds nothing",
			sidebar: "%3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmux := &bindingTmux{}

			NewPopupWindow(tmux, "weave").HoldFocusKey(tt.sidebar, tt.agent)

			command, bound := tmux.bound["root:"+FocusKey]
			if tt.want == nil {
				assert.False(t, bound)

				return
			}

			require.True(t, bound)
			assert.Equal(t, tt.want, command)
		})
	}
}

// TestHoldFocusKeyGivesItBack pins the key going back to the panes, and that no key means no tmux call at
// all rather than an unbind of the empty string.
func TestHoldFocusKeyGivesItBack(t *testing.T) {
	tmux := &bindingTmux{}
	popup := NewPopupWindow(tmux, "weave")

	popup.HoldFocusKey("%3", "%9")
	popup.HoldFocusKey("%3", "")

	_, bound := tmux.bound["root:C-a"]
	assert.False(t, bound)
	assert.Equal(t, []string{"C-a"}, tmux.unbound)

	popup.HoldFocusKey("%3", "")

	assert.Equal(t, []string{"C-a"}, tmux.unbound)
}

// TestFocusKeyHeldOnlyOverAStagedAgent checks the key is bound while an agent's pane is on the
// stage and given back on a screen that stages none.
func TestFocusKeyHeldOnlyOverAStagedAgent(t *testing.T) {
	binds := &bindingTmux{}
	m := FocusKeyModel(binds)

	m.holdFocusKey()

	command, bound := binds.bound["root:C-a"]
	require.True(t, bound)
	assert.Equal(t, []string{
		"if-shell", "-F", "#{==:#{pane_id},%3}",
		"select-pane -t %9", "select-pane -t %3",
	}, command)

	m.screen = screenMail
	m.holdFocusKey()

	_, bound = binds.bound["root:C-a"]
	assert.False(t, bound)
	assert.Equal(t, []string{"C-a"}, binds.unbound)
}

// TestFocusKeyGivenBackByAnEmptyStage checks the key is unbound once the guest leaves the stage,
// which would otherwise jump to a pane nobody can see.
func TestFocusKeyGivenBackByAnEmptyStage(t *testing.T) {
	binds := &bindingTmux{}
	m := FocusKeyModel(binds)

	m.holdFocusKey()
	m.frame.ReleaseGuest("%9")
	m.holdFocusKey()

	_, bound := binds.bound["root:C-a"]
	assert.False(t, bound)
	assert.Equal(t, []string{"C-a"}, binds.unbound)
}

// TestFocusKeyReachesTmuxOnce checks a key that is already right is left alone.
func TestFocusKeyReachesTmuxOnce(t *testing.T) {
	binds := &bindingTmux{}
	m := FocusKeyModel(binds)

	m.holdFocusKey()
	m.holdFocusKey()

	assert.Equal(t, 1, binds.rootBinds)
}

// TestFocusKeyFollowsTheStagedPane checks the key is rebound when another agent's pane takes the
// stage, rather than being left pointed at the one before it.
func TestFocusKeyFollowsTheStagedPane(t *testing.T) {
	binds := &bindingTmux{}
	m := FocusKeyModel(binds)

	m.holdFocusKey()
	m.frame.ShowGuest("%12")
	m.holdFocusKey()

	command, bound := binds.bound["root:C-a"]
	require.True(t, bound)
	assert.Equal(t, []string{
		"if-shell", "-F", "#{==:#{pane_id},%3}",
		"select-pane -t %12", "select-pane -t %3",
	}, command)
	assert.Equal(t, 2, binds.rootBinds)
}

func FocusKeyModel(binds *bindingTmux) model {
	m := filterModel()
	m.popup = NewPopupWindow(binds, "weave")
	m.pane = "%3"
	for i := range m.listing.agents {
		m.listing.agents[i].alive = true
		m.listing.agents[i].agent.Pane = "%9"
	}

	m.frame.Attach("%3")
	m.frame.ShowGuest("%9")

	return m
}

// TestFocusKeyGivenBackByAForm checks the key is unbound while a form is up over the agents
// list, the form taking the whole window and leaving the staged pane nowhere to be seen.
func TestFocusKeyGivenBackByAForm(t *testing.T) {
	binds := &bindingTmux{}
	m := FocusKeyModel(binds)

	m.holdFocusKey()

	opened, _ := m.openRename(m.listing.agents[0].agent)
	m = asModel(t, opened)
	m.holdFocusKey()

	_, bound := binds.bound["root:C-a"]
	assert.False(t, bound)
	assert.Equal(t, []string{"C-a"}, binds.unbound)

	closed, _ := m.closeForm()
	m = asModel(t, closed)
	m.holdFocusKey()

	_, bound = binds.bound["root:C-a"]
	assert.True(t, bound, "closing the form gives the pane back and the key with it")
}

// TestFocusKeyGivenBackByAHeaderRow checks the key is unbound on a row with no pane of its own,
// where the panel takes the window while the pane it covers stays parked in the stage.
func TestFocusKeyGivenBackByAHeaderRow(t *testing.T) {
	binds := &bindingTmux{}
	m := FocusKeyModel(binds)

	m.holdFocusKey()

	m.tree.cursor = 0
	m.stageSelection()
	require.True(t, m.frame.IsSolo(), "a header row takes the window")
	require.Equal(t, "%9", m.frame.GuestPane(), "with the agent's pane left parked behind it")

	m.holdFocusKey()

	_, bound := binds.bound["root:C-a"]
	assert.False(t, bound)
	assert.Equal(t, []string{"C-a"}, binds.unbound)
}
