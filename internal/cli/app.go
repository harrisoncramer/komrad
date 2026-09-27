package cli

import (
	"komrad/config"
	"komrad/internal/engine"
	"komrad/internal/harness"
	"komrad/internal/tmux"
	"komrad/internal/ui"
)

type app struct {
	session        *ui.Session
	env            *config.Environment
	keys           config.Keybindings
	harnesses      *harness.Registry
	defaultHarness string
	defaultFlags   map[string]string
	usage          config.Usage
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
	return engine.Remote{
		Destination: env.SSH,
		Forward:     env.Forward,
	}
}
