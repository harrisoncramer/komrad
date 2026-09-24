package cli

import (
	"komrade/config"
	"komrade/internal/agents"
	"komrade/internal/engine"
	"komrade/internal/harness"
	"komrade/internal/tmux"
	"komrade/internal/ui"
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
	return remoteOf(a.env)
}

func remoteOf(env *config.Environment) engine.Remote {
	socket := env.EngineTmuxSocket
	if socket == "" {
		socket = env.TmuxSocket
	}

	return engine.Remote{
		Destination: env.SSH,
		Exec:        env.Exec,
		Forward:     env.Forward,
		User:        env.EngineUser,
		Socket:      socket,
		Session:     agents.ManagerSession,
	}
}
