package cli

import (
	"weave/config"
	"weave/internal/agents"
	"weave/internal/engine"
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
	githubApp      config.GitHubApp
	configErr      error
	contextPath    string
	configStack    config.Stack
	configSources  map[string]config.Source
	tmux           *tmux.Client
	self           string
}

// selfPath is the binary a panel window runs, this one unless a test names another.
func (a *app) selfPath() string {
	if a.self != "" {
		return a.self
	}

	return getSelfPath()
}

// engine says where the agents are running, which the panel needs so it can show a terminal it
// cannot reach with a pane move.
func (a *app) engine() engine.Remote {
	socket := a.env.EngineTmuxSocket
	if socket == "" {
		socket = a.env.TmuxSocket
	}

	return engine.Remote{
		Destination: a.env.SSH,
		Exec:        a.env.Exec,
		Forward:     a.env.Forward,
		User:        a.env.EngineUser,
		Socket:      socket,
		Session:     agents.ManagerSession,
	}
}
