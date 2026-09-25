package ui_test

import (
	"strings"
	"testing"

	"komrad/internal/ui"
	agentv1 "komrad/proto/gen/agent/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

const (
	panelKeyOption = "@komrad_key"
	builtInKey     = "g"
)

type bindRecorder struct {
	client    string
	globals   map[string]string
	bound     map[string][]string
	unbound   []string
	spellings map[string]string
	err       error
}

// TestPopupPanelKey covers the order the panel key is settled in.
func TestPopupPanelKey(t *testing.T) {
	tests := []struct {
		name       string
		configured string
		option     string
		want       string
	}{
		{
			name: "nothing names a key, so the built-in one stands",
			want: builtInKey,
		},
		{
			name:   "the tmux option names one",
			option: "w",
			want:   "w",
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
			tmux := &bindRecorder{}
			if tt.option != "" {
				tmux.SetGlobal(panelKeyOption, tt.option)
			}

			assert.Equal(t, tt.want, ui.PanelKey(tmux, tt.configured))
		})
	}
}

// TestPopupBindPanelKey covers the binding on the user's own tmux server that opens the panel.
func TestPopupBindPanelKey(t *testing.T) {
	local := ui.BindPanelKeyParams{
		Self:       "/usr/local/bin/komrad",
		ConfigArgs: []string{"--config", "/etc/komrad.toml", "--config", "/home/dev/komrad.toml"},
	}

	t.Run("a refusal reaches the person who asked for it", func(t *testing.T) {
		tmux := &bindRecorder{err: assert.AnError}

		require.Error(t, ui.BindPanelKey(tmux, local))
	})

	t.Run("the key runs this binary's own path and writes no flag a later press could read back", func(t *testing.T) {
		tmux := &bindRecorder{}

		require.NoError(t, ui.BindPanelKey(tmux, local))

		assert.NotContains(t, tmux.globals, "@komrad_open")
		assert.Equal(t, []string{
			"if-shell",
			"/usr/local/bin/komrad shown --config /etc/komrad.toml --config /home/dev/komrad.toml",
			"run-shell -b '/usr/local/bin/komrad hide --config /etc/komrad.toml --config /home/dev/komrad.toml'",
			"display-popup -w 100% -h 100% -x C -y C -T ' komrad ' " +
				"-EE '/usr/local/bin/komrad open --config /etc/komrad.toml --config /home/dev/komrad.toml'",
		}, tmux.bound[builtInKey])
	})

	t.Run("a changed key takes back only the one this komrad bound before", func(t *testing.T) {
		local := ui.BindPanelKeyParams{Self: "/usr/local/bin/komrad", ConfigArgs: []string{"--config", "/etc/komrad.toml"}}
		remote := ui.BindPanelKeyParams{
			Self:       "/usr/local/bin/komrad",
			ConfigArgs: []string{"--config", "/etc/komrad.toml", "--engine", "remote"},
			Key:        "G",
		}

		tmux := &bindRecorder{spellings: map[string]string{"c-g": "C-g"}}
		tmux.SetGlobal(panelKeyOption, "c-g")
		require.NoError(t, tmux.BindKeyChecked("s", "split-window"))
		require.NoError(t, ui.BindPanelKey(tmux, remote))
		require.NoError(t, ui.BindPanelKey(tmux, local))
		require.NoError(t, ui.BindPanelKey(tmux, local))

		assert.Contains(t, tmux.bound, "C-g", "a key bound again is bound as tmux spells it, not taken for a stale one")
		assert.Equal(t, []string{"C-g"}, tmux.unbound, "and the run before it is what was taken back")

		tmux.SetGlobal(panelKeyOption, "j")
		require.NoError(t, ui.BindPanelKey(tmux, local))

		assert.Equal(t, []string{"C-g", "C-g"}, tmux.unbound)
		assert.Contains(t, tmux.bound, "j")
		assert.Contains(t, tmux.bound, "G", "the other engine's key, whose command this one's is a prefix of, stands")
		assert.Contains(t, tmux.bound, "s", "and so does a binding that is not komrad's")
	})
}

// TestPopupWindow covers the popup's own tmux session and the key that hides it.
func TestPopupWindow(t *testing.T) {
	t.Run("a client attached to the panel's session is somebody looking", func(t *testing.T) {
		assert.True(t, ui.NewPopupWindow(&bindRecorder{client: "/dev/ttys004"}, "komrad").IsShown())
		assert.False(t, ui.NewPopupWindow(&bindRecorder{}, "komrad").IsShown(), "the popup was hidden, which detached it")
	})

	t.Run("the panel key hides the popup", func(t *testing.T) {
		tmux := &bindRecorder{}

		ui.NewPopupWindow(tmux, "manager").BindHide("")

		assert.Equal(t, []string{"detach-client", "-s", "manager"}, tmux.bound[builtInKey])
	})

	t.Run("the hide binding forgets the key it bound last and leaves one bound again", func(t *testing.T) {
		tmux := &bindRecorder{}

		ui.NewPopupWindow(tmux, "manager").BindHide("w")
		ui.NewPopupWindow(tmux, "manager").BindHide("j")
		ui.NewPopupWindow(tmux, "manager").BindHide("j")

		assert.NotContains(t, tmux.bound, "w")
		assert.Equal(t, []string{"detach-client", "-s", "manager"}, tmux.bound["j"])
		assert.Equal(t, []string{"w"}, tmux.unbound)
	})

	t.Run("the focus key toggles between the sidebar and the agent's pane", func(t *testing.T) {
		tmux := &bindRecorder{}

		ui.NewPopupWindow(tmux, "komrad").HoldFocusKey("%3", "%9")

		assert.Equal(t, []string{
			"if-shell", "-F", "#{==:#{pane_id},%3}",
			"select-pane -t %9", "select-pane -t %3",
		}, tmux.bound["root:"+ui.FocusKey], "a tmux format rather than a shell command")
	})

	t.Run("no sidebar binds nothing", func(t *testing.T) {
		tmux := &bindRecorder{}

		ui.NewPopupWindow(tmux, "komrad").HoldFocusKey("", "%9")

		assert.Empty(t, tmux.bound)
	})

	t.Run("an empty stage gives the key back once and then says nothing", func(t *testing.T) {
		tmux := &bindRecorder{}
		popup := ui.NewPopupWindow(tmux, "komrad")

		popup.HoldFocusKey("%3", "%9")
		popup.HoldFocusKey("%3", "")
		popup.HoldFocusKey("%3", "")

		assert.NotContains(t, tmux.bound, "root:"+ui.FocusKey)
		assert.Equal(t, []string{ui.FocusKey}, tmux.unbound)
	})
}

// TestPopupFocusKey covers the panel holding the focus key only while an agent's pane is on show.
func TestPopupFocusKey(t *testing.T) {
	held := []string{
		"if-shell", "-F", "#{==:#{pane_id},%0}",
		"select-pane -t %9", "select-pane -t %0",
	}

	t.Run("the panel going gives the key back", func(t *testing.T) {
		tmux := &bindRecorder{}
		popup := ui.NewPopupWindow(tmux, "komrad")

		popup.HoldFocusKey("%3", "%9")
		popup.ReleaseFocusKey()

		assert.Equal(t, []string{ui.FocusKey}, tmux.unbound, "tmux keeps a root binding until the server dies")
	})

	t.Run("releasing a key nobody held says nothing to tmux", func(t *testing.T) {
		tmux := &bindRecorder{}

		ui.NewPopupWindow(tmux, "komrad").ReleaseFocusKey()

		assert.Empty(t, tmux.unbound)
	})

	t.Run("a staged agent holds the key once and a screen staging none gives it back", func(t *testing.T) {
		harness := openFocus(t)
		harness.watch(buildSnapshot(buildFocusRows()...))

		command, bound := harness.panel.ReadRootBinding(ui.FocusKey)
		require.True(t, bound)
		assert.Equal(t, held, command)
		assert.Equal(t, 1, harness.panel.CountRootBinds(), "a key that is already right is left alone")

		given := len(harness.panel.ReadRootUnbinds())

		harness.press("t")

		assertFocusKeyGivenBack(t, harness, given)
	})

	t.Run("the key follows the pane that takes the stage", func(t *testing.T) {
		harness := openFocus(t)
		second := newFocusAgent("a2", "%12")
		second.CreatedAt = "2026-09-20T11:00:00Z"
		harness.watch(buildSnapshot(append(buildFocusRows(), second)...))

		harness.press("j")
		harness.fireTimers()

		command, bound := harness.panel.ReadRootBinding(ui.FocusKey)
		require.True(t, bound)
		assert.Equal(t, "select-pane -t %12", command[3])
		assert.Equal(t, 2, harness.panel.CountRootBinds())
	})

	t.Run("an agent whose pane goes gives the key back", func(t *testing.T) {
		harness := openFocus(t)
		rows := buildFocusRows()
		rows[1].(*agentv1.Agent).Alive = proto.Bool(false)
		rows[1].(*agentv1.Agent).PaneId = ""
		given := len(harness.panel.ReadRootUnbinds())

		harness.watch(buildSnapshot(rows...))

		assertFocusKeyGivenBack(t, harness, given)
	})

	t.Run("a form over the list gives the key back and closing it takes it again", func(t *testing.T) {
		harness := openFocus(t)
		given := len(harness.panel.ReadRootUnbinds())

		harness.press("r")
		assertFocusKeyGivenBack(t, harness, given)

		harness.press("esc")
		_, bound := harness.panel.ReadRootBinding(ui.FocusKey)
		assert.True(t, bound)
	})

	t.Run("a header row with the pane parked behind it gives the key back", func(t *testing.T) {
		harness := openFocus(t)
		given := len(harness.panel.ReadRootUnbinds())

		harness.press("k")
		harness.fireTimers()
		require.True(t, harness.panel.IsZoomed(), "a header row takes the window")
		require.Equal(t, "%9", harness.panel.ReadGuestPane(), "with the agent's pane left parked behind it")

		assertFocusKeyGivenBack(t, harness, given)
	})
}

func (t *bindRecorder) SetGlobal(name, value string) {
	if t.globals == nil {
		t.globals = map[string]string{}
	}

	t.globals[name] = value
}

func (t *bindRecorder) ShowGlobal(name string) string {
	return t.globals[name]
}

func (t *bindRecorder) GetSessionClient(string) string {
	return t.client
}

func (t *bindRecorder) SetSessionOption(string, string, string) {}

func (t *bindRecorder) DetachSession(string) {}

func (t *bindRecorder) BindKey(key string, command ...string) {
	if t.bound == nil {
		t.bound = map[string][]string{}
	}

	t.bound[key] = command
}

func (t *bindRecorder) BindKeyChecked(key string, command ...string) error {
	if listed, isRespelled := t.spellings[key]; isRespelled {
		key = listed
	}

	t.BindKey(key, command...)

	return t.err
}

func (t *bindRecorder) UnbindKey(key string) {
	delete(t.bound, key)
	t.unbound = append(t.unbound, key)
}

func (t *bindRecorder) BindRootKey(key string, command ...string) {
	t.BindKey("root:"+key, command...)
}

func (t *bindRecorder) UnbindRootKey(key string) {
	delete(t.bound, "root:"+key)
	t.unbound = append(t.unbound, key)
}

func (t *bindRecorder) GetPrefixKeys() (map[string]string, error) {
	table := map[string]string{}
	for key, command := range t.bound {
		table[key] = printBinding(command)
	}

	return table, nil
}

func openFocus(t *testing.T) *panelHarness {
	t.Helper()

	harness := newPanelHarness(t, panelHarnessParams{})
	answerWorktreeReads(harness)
	harness.watch(buildSnapshot(buildFocusRows()...))

	return harness
}

func buildFocusRows() []proto.Message {
	return []proto.Message{newLiveWorkflow("w1", "backend"), newFocusAgent("a1", "%9")}
}

func newFocusAgent(id, pane string) *agentv1.Agent {
	agent := newAgentAt(id, "w1", 30)
	agent.PaneId = pane
	agent.Alive = proto.Bool(true)

	return agent
}

func assertFocusKeyGivenBack(t *testing.T, harness *panelHarness, given int) {
	t.Helper()

	_, bound := harness.panel.ReadRootBinding(ui.FocusKey)
	assert.False(t, bound)
	assert.Equal(t, []string{ui.FocusKey}, harness.panel.ReadRootUnbinds()[given:], "given back once")
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
