package ui

import (
	"os"
	"path/filepath"
	"testing"

	"weave/config"
	"weave/internal/ui/layout"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// reloadPanel is a panel started on a config file, the way one is started on the user's own.
// It answers the panel and the path, so a case can edit the file the panel is reading and
// press the key.
func reloadPanel(t *testing.T, body string) (model, string) {
	t.Helper()

	path := filepath.Join(t.TempDir(), "config.toml")
	writeConfigFile(t, path, body)

	cfg, err := config.Load([]string{path})
	require.NoError(t, err)

	tmux := &recordingTmux{}
	panel := model{
		tmux:        tmux,
		frame:       layout.New(tmux, cfg.Layout),
		keys:        cfg.Keybindings,
		marks:       cfg.Marks,
		configPaths: []string{path},
		connection:  cfg.Connection,
		screen:      screenAgents,
	}

	return panel, path
}

func writeConfigFile(t *testing.T, path, body string) {
	t.Helper()

	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
}

// reload presses the reload key and settles what it produced, which is the read of the file
// and then the apply.
func reload(t *testing.T, m model) model {
	t.Helper()

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	next, ok := updated.(model)
	require.True(t, ok)

	for _, msg := range produced(cmd) {
		updated, _ = next.Update(msg)
		next, ok = updated.(model)
		require.True(t, ok)
	}

	return next
}

// TestReloadAppliesTheFile covers the reload key picking up what the file says now. The
// binding it changes is one the panel reads on every keypress, so the case presses that key
// afterwards rather than reading the field back.
func TestReloadAppliesTheFile(t *testing.T) {
	panel, path := reloadPanel(t, "")

	writeConfigFile(t, path, `
[keybindings.help]
toggle = ["x"]

[[marks]]
glyph = "!"
color = "red"

[layout]
split = 0.5
`)

	panel.filter.query = "login"
	panel.tree.selected = selection{ids: []string{"agent-1"}}

	panel = reload(t, panel)

	assert.Equal(t, []string{"x"}, panel.keys.Help.Toggle)
	assert.Equal(t, []config.Mark{{Glyph: "!", Color: "red"}}, panel.marks)
	assert.Nil(t, panel.banner)

	// The panel lands back on its own list with nothing standing over it, since a rebound
	// key under a filter or a form would be read with bindings the user was not pressing.
	assert.Equal(t, screenAgents, panel.screen)
	assert.Empty(t, panel.filter.query)
	assert.False(t, panel.tree.selected.active())

	// The key the file names now draws the legend, and the one it replaced does nothing.
	updated, _ := panel.Update(keyMsg("?"))
	assert.False(t, updated.(model).helpExpanded)

	updated, _ = panel.Update(keyMsg("x"))
	assert.True(t, updated.(model).helpExpanded)
}

// TestReloadKeepsTheOldConfigOnABadFile covers a file edited into something that will not
// parse. The panel is left on what it was already running, since dropping to the built-in
// defaults would rebind every key the user has set over a typo.
func TestReloadKeepsTheOldConfigOnABadFile(t *testing.T) {
	panel, path := reloadPanel(t, `
[keybindings.help]
toggle = ["x"]
`)

	writeConfigFile(t, path, `nonsense = true`)

	panel = reload(t, panel)

	assert.Equal(t, []string{"x"}, panel.keys.Help.Toggle)
	require.NotNil(t, panel.banner)
	assert.Contains(t, panel.banner.Summary, "Could not re-read")
	assert.Contains(t, panel.banner.Detail, "nonsense")
}

// TestReloadReportsAChangedConnection covers the settings a running process cannot pick up.
// The rest of the file is applied, since a database named somewhere else is no reason to
// hold back the keybindings underneath it.
func TestReloadReportsAChangedConnection(t *testing.T) {
	panel, path := reloadPanel(t, "")

	writeConfigFile(t, path, `
[keybindings.help]
toggle = ["x"]
`)

	// Set on the panel rather than written into the file, since every connection setting is
	// also bound to an environment variable and a checkout that exports one resolves the
	// file's value away.
	panel.connection.Addr = "127.0.0.1:9999"

	panel = reload(t, panel)

	assert.Equal(t, []string{"x"}, panel.keys.Help.Toggle)
	require.NotNil(t, panel.banner)
	assert.Contains(t, panel.banner.Summary, "connection settings changed")
}

// TestReloadResizesTheStage covers a layout edited while the panel runs. The split reaches
// tmux on the reload rather than on the next thing that happens to lay a pane out.
func TestReloadResizesTheStage(t *testing.T) {
	panel, path := reloadPanel(t, "")

	tmux, ok := panel.tmux.(*recordingTmux)
	require.True(t, ok)
	panel.frame.Attach("%0")
	tmux.resizes = nil

	writeConfigFile(t, path, "[layout]\nsplit = 0.5\n")

	reload(t, panel)

	assert.Equal(t, []int{50}, tmux.resizes)
}
