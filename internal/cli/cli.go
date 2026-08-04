// Package cli builds the weave command tree and wires its dependencies.
//
// This file holds the root command and nothing else. Each subcommand lives in its
// own file named for it, holding that command and only the helpers it uses. The
// shared state lives in app.go instead.
package cli

import (
	"fmt"
	"log/slog"

	"weave/config"
	"weave/internal/agents"
	"weave/internal/appconfig"
	"weave/internal/harness"
	"weave/internal/harness/claude"
	"weave/internal/harness/codex"
	"weave/internal/logging"
	"weave/internal/tmux"
	"weave/internal/ui"

	"github.com/spf13/cobra"
)

// NewRootCommand builds the weave command tree. Running it with no
// subcommand opens the panel.
func NewRootCommand() *cobra.Command {

	// The app holds basically all of our application state, which drives the CLI commands and the bubbletea models.
	// It's state is set by the root command which precedes every other subcommand in weave.

	app := &app{}

	var configPaths []string

	root := &cobra.Command{
		Use:   "weave",
		Short: "tmux panel and CLI for background coding agents",
		Long: "Weave runs coding agents in tmux panes and keeps track of them.\n\n" +
			"There are two processes. `weave serve` is the long-lived service that owns the\n" +
			"database, answers the API and the MCP endpoint, and watches every pane. The panel\n" +
			"is a client of it, which is why it can be closed and reopened without stopping\n" +
			"anything. Every other command here is a client of the same service.\n\n" +
			"The panel is normally reached through the tmux key binding rather than by name,\n" +
			"and the commands below are for the times something has to be done by hand: check\n" +
			"the machine, start or stop the service, or call an endpoint from a script.",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {

			configPaths = resolveConfigPaths(configPaths)

			// The files come first, since the environment resolves on top of them and a
			// config that will not parse has to stop most commands before anything opens a
			// database on the built-in defaults.
			cfg, report, err := appconfig.LoadWithReport(configPaths)
			if err != nil {
				if isConfigFatal(cmd) {
					return err
				}

				app.configErr = err
				cfg = appconfig.Defaults()
				report = &appconfig.Report{Sources: map[string]appconfig.Source{}}
			}

			env := config.Load(config.Environment{
				DatabaseURL:         cfg.Connection.DatabaseURL,
				ReadOnlyDatabaseURL: cfg.Connection.ReadOnlyDatabaseURL,
				Addr:                cfg.Connection.Addr,
				TmuxSocket:          cfg.Connection.TmuxSocket,
				CacheDir:            cfg.Connection.CacheDir,
			})

			codexHarness := codex.NewHarness(codex.NewHarnessParams{
				TrustLaunchDirs: cfg.Harness.Codex.TrustLaunchDirs,
			})
			claudeHarness := claude.NewHarness(claude.NewHarnessParams{
				ProjectsDir: claude.DefaultProjectsDir(), // TBD, let folks give different claude dir?
			})

			harnessRegistry, err := harness.NewRegistry(claudeHarness, codexHarness)
			if err != nil {
				return fmt.Errorf("failed to create registry: %w", err)
			}
			if _, err := harnessRegistry.GetByID(cfg.DefaultHarness); err != nil && app.configErr == nil {
				app.configErr = fmt.Errorf("default_harness: %w", err)
			}
			app.harnesses = harnessRegistry

			app.marks = cfg.Marks
			app.layout = cfg.Layout
			app.logLevel = cfg.Log.Level
			app.logViewer = cfg.Log.Viewer
			app.configPaths = configPaths
			app.env = env
			app.configSources = report.Sources
			app.keys = cfg.Keybindings
			app.defaultHarness = cfg.DefaultHarness
			app.maxAgents = cfg.MaxAgents

			//  A tmux server is one long-lived process that owns sessions, windows, and panes. You reach it through a unix socket.
			//  Every tmux command you run is a short-lived client process that connects to that socket.
			//  Weave starts a second server, and connects to it with a client (called manager). Any command issued through the manager will be routed to
			//  that other tmux server. This is why nothing in your regular tmux server can see weave sessions or state. To see them, run `tmux -L weave ls`

			manager := tmux.NewClient(env.TmuxSocket)
			app.host = tmux.NewHost(manager)
			app.tmux = tmux.NewServers(manager, app.host)

			app.popup = ui.NewPopupWindow(app.tmux, agents.ManagerSession)
			contextPath, err := cfg.GetContextFile()
			if err != nil {
				ctx := cmd.Context()
				// Only reached when the config directory cannot be worked out at all. Agents
				// then start with no standing context, which is worth a line rather than a
				// silent difference in what every agent is told.
				logging.From(ctx).LogAttrs(ctx, slog.LevelDebug, "failed to resolve the context file", logging.Err(err)...)
			}

			app.contextPath = contextPath
			app.defaultFlags = map[string]string{ // Set up default flags for the harnesses provided by the user in their config file.
				claudeHarness.GetID(): cfg.Harness.Claude.Flags,
				codexHarness.GetID():  cfg.Harness.Codex.Flags,
			}

			return nil
		},
	}

	// Repeatable, and each occurrence is a layer merged over the ones before it. StringArray
	// rather than StringSlice because StringSlice splits on commas, which would mangle a path
	// holding one and invite a second syntax for the same thing.
	root.PersistentFlags().StringArrayVar(&configPaths, "config", nil,
		"path to a config TOML, repeatable, later files win over earlier ones")
	root.AddCommand(
		doctorCmd(app),
		panelCmd(app),
		openCmd(app),
		hideCmd(app),
		serveCmd(app),
		startCmd(app),
		stopCmd(app),
		versionCmd(app),
		migrateCmd(app),
		readonlyRoleCmd(app),
		rpcCmd(app),
	)

	return root
}

// commandsSurvivingBadConfig are the commands that still run on a config that will not parse.
// Everything else refuses, since the file names the database now and falling back to the
// built-in default would quietly connect somewhere else and write there.
var commandsSurvivingBadConfig = map[string]bool{
	"doctor":  true,
	"version": true,
	"panel":   true,
}

func resolveConfigPaths(flags []string) []string {
	if len(flags) > 0 {
		return flags
	}

	return config.LayerPaths()
}

// isConfigFatal reports whether a config that will not parse should stop this command.
func isConfigFatal(cmd *cobra.Command) bool {
	return !commandsSurvivingBadConfig[cmd.Name()]
}
