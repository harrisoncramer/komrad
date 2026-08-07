package cli

import (
	"weave/config"
	"weave/internal/harness"
	"weave/internal/tmux"
	"weave/internal/ui"
)

// app is what every subcommand is wired from; nothing here reaches the database, since the
// panel is a client of the service and the service builds its own stores from what is
// gathered here.
type app struct {
	popup *ui.PopupWindow
	env   *config.Environment
	keys  config.Keybindings
	// harnesses is every agent CLI this binary can run, which the services get an
	// agent's own harness through.
	harnesses *harness.Registry
	// defaultHarness is the CLI an agent runs when neither it nor its workflow names
	// one, and defaultFlags are the configured flags each harness's agents inherit,
	// keyed by harness id.
	defaultHarness string
	defaultFlags   map[string]string
	maxAgents      int
	// marks are the marks the panel's mark key steps an agent through.
	marks []config.Mark
	// logViewer is what the panel's log key opens the log in, the user's own command.
	logViewer string
	// logLevel is how much the served process writes to its log, read here with the rest of
	// the config so serve is handed what this process parsed rather than reading the file again.
	logLevel string
	// layout is how the panel divides the popup between its list and the stage.
	layout config.Layout
	// configErr is set when the user's config file would not parse; the panel shows it on its
	// error screen and serve logs it, since serve otherwise carries on with defaults
	// indistinguishable from a run on the user's file.
	configErr error
	// contextPath is the file holding the standing text every agent is started with,
	// read from the config at startup.
	contextPath string
	// configPaths is the layer list this process resolved, weakest first and empty on the
	// default path; serve keeps the paths rather than the parsed config since it reads the
	// files again per request.
	configPaths []string
	// configSources is loader metadata used by doctor to explain precedence.
	configSources map[string]config.Source
	// tmux is shared with the services, but the UI also needs it directly for the stage,
	// which is its own layout outside the agent API; it covers both tmux servers, routing
	// each call to the one that can answer it.
	tmux *tmux.Servers
	// host is the user's tmux server, kept alongside so open can record which one opened
	// the popup; everything else goes through tmux.
	host *tmux.Host
}
