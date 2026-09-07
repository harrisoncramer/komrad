package cli

import (
	"weave/config"
	"weave/internal/harness"
	"weave/internal/tmux"
	"weave/internal/ui"
)

type app struct {
	popup          *ui.PopupWindow
	env            *config.Environment
	keys           config.Keybindings
	harnesses      *harness.Registry
	defaultHarness string
	defaultFlags   map[string]string
	maxAgents      int
	maxChildren    int
	logViewer      string
	processViewer  string
	logLevel       string
	layout         config.Layout
	focusKey       string
	configErr      error
	contextPath    string
	configPaths    []string
	configSources  map[string]config.Source
	tmux           *tmux.Client
}
