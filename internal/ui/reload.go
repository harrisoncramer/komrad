package ui

import (
	"weave/config"
	"weave/internal/errui"

	tea "github.com/charmbracelet/bubbletea"
)

// reloadResult is the config read back off the disk, and the reason it could not be read.
// The read is a command like every other, so a file on a slow disk cannot hold the render
// loop.
type reloadResult struct {
	cfg *config.Config
	err error
}

// reloadCmd re-reads the layers this process was started with. The paths are re-read rather
// than the parsed config being kept, since the point is to pick up what the file says now.
func (m model) reloadCmd() tea.Cmd {
	paths := m.configPaths

	return func() tea.Msg {
		cfg, err := config.Load(paths)

		return reloadResult{cfg: cfg, err: err}
	}
}

// applyReload puts a re-read config in force. Everything the panel decides for itself is
// replaced, and the panel lands back on the agents list with nothing open over it: a
// keybinding that changed under an open form or a standing filter would leave the user
// holding keys that no longer do what they did when they were pressed.
//
// Nothing on screen is lost by this. Every row the panel draws came from the service on the
// last listing and the next one is a second away, so the reload costs the cursor and
// whatever was typed into a filter, and no more than that.
func (m model) applyReload(result reloadResult) (tea.Model, tea.Cmd) {
	if result.err != nil {
		m.report("Could not re-read the configuration, so the old one is still in force", result.err)

		return m, nil
	}

	m.keys = result.cfg.Keybindings
	m.marks = result.cfg.Marks
	m.logViewer = result.cfg.Log.Viewer
	m.frame.Relayout(result.cfg.Layout)

	m.pendingForm = false
	m.form = nil
	m.values = formValues{}
	m.target = formTarget{}
	m.filter = filterState{}
	m.tree.selected = selection{}

	m.banner = nil

	// The instance the panel is a client of, which cannot be changed under a running
	// process: the clients point at the old address and the service holds the old database
	// open. Said plainly rather than applied halfway, since the alternative is a panel
	// claiming to run on a config it is not.
	if result.cfg.Connection != m.connection {
		m.banner = &errui.Banner{
			Summary: "The connection settings changed, which this panel cannot pick up",
			Detail:  "the database, the address, the tmux socket and the cache directory are what this process is, so run `weave stop` and `weave start`",
		}
	}

	return m.returnToAgents()
}
