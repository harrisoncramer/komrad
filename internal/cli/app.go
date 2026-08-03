package cli

import (
	"time"

	"weave/config"
	"weave/internal/appconfig"
	"weave/internal/harness"
	"weave/internal/tmux"
	"weave/internal/ui"
)

// app is what every subcommand is wired from. Each command holds only the part
// it drives. Nothing here reaches the database: the panel is a client of the service,
// and the service builds its own stores from what is gathered here.
type app struct {
	popup *ui.PopupWindow
	env   *config.Config
	keys  appconfig.Keybindings
	// harnesses is every agent CLI this binary can run, which the services get an
	// agent's own harness through.
	harnesses *harness.Registry
	// defaultHarness is the CLI an agent runs when neither it nor its workflow names
	// one, and defaultFlags are the configured flags each harness's agents inherit,
	// keyed by harness id.
	defaultHarness string
	defaultFlags   map[string]string
	maxAgents      int
	// purgeArchiveAfter is how long an archived agent is kept, zero to keep it forever.
	purgeArchiveAfter time.Duration
	// marks are the marks the panel's mark key steps an agent through.
	marks []appconfig.Mark
	// logViewer is what the panel's log key opens the log in, the user's own command.
	logViewer string
	// logLevel is how much the served process writes to its log. It is read here with the
	// rest of the config, so serve is handed what this process parsed like every other
	// setting rather than reading the file again.
	logLevel string
	// layout is how the panel divides the popup between its list and the stage.
	layout appconfig.Layout
	// configErr is set when the user's config file would not parse. The panel shows it on
	// its error screen and serve writes it to the log, since serve carries on with the
	// defaults and is otherwise indistinguishable from one running on the user's file.
	configErr error
	// contextPath is the file holding the standing text every agent is started with,
	// read from the config at startup.
	contextPath string
	// configPath is the file the user pointed at, empty for the default location.
	// Serve keeps the path rather than the config parsed from it, since it reads the
	// file again per request.
	configPath string
	// tmux is shared with the services. The UI needs it directly for the stage,
	// which is its own layout rather than anything the agent API owns. It covers
	// both tmux servers, sending each call to the one that can answer it.
	tmux *tmux.Servers
	// host is the user's tmux server, kept alongside so the open command can record
	// which one opened the popup. Every other use of it goes through tmux.
	host *tmux.Host
}
