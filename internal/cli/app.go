package cli

import (
	"komrad/config"
	"komrad/internal/engine"
	"komrad/internal/git"
	"komrad/internal/glyph"
	"komrad/internal/harness"
	"komrad/internal/loader"
	"komrad/internal/tmux"
	"komrad/internal/ui"
)

type app struct {
	session        *ui.Session
	env            *config.Environment
	keys           config.Keybindings
	vimMode        bool
	diffWrap       bool
	glyphs         glyph.Set
	loader         loader.Style
	harnesses      *harness.Registry
	defaultHarness string
	autoSetup      bool
	defaultFlags   map[string]string
	trustDirs      map[string]bool
	usage          config.Usage
	signIn         config.SignIn
	logLevel       string
	layout         config.Layout
	githubApp      config.GitHubApp
	gitConfig      config.Git
	configErr      error
	contextPath    string
	configStack    config.Stack
	configSources  map[string]config.Source
	tmux           *tmux.Client
	self           string
	debug          config.Debug
	refresher      *engine.TokenRefresher
}

// selfPath is the binary a panel window runs, this one unless a test names another.
func (a *app) selfPath() string {
	if a.self != "" {
		return a.self
	}

	return getSelfPath()
}

// gitClient reads checkouts with the SSH hosts the config names as standing in for a web host.
func (a *app) gitClient() *git.Client {
	return git.NewClient(git.NewClientParams{SSHHosts: a.gitConfig.GetSSHHosts})
}

// engine says where the agents are running, which the panel needs so it can show a terminal it
// cannot reach with a pane move.
func (a *app) engine() engine.Remote {
	return remoteOf(a.env)
}

func remoteOf(env *config.Environment) engine.Remote {
	return engine.Remote{
		URL: env.URL,
	}
}
