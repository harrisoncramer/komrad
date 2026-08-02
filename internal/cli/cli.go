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

	var configPath string

	root := &cobra.Command{
		Use:           "weave",
		Short:         "tmux panel and CLI for background coding agents",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {

			env, err := config.Load() // Loads the environment, tmux socket name, cache directory, etc.
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			cfg, err := appconfig.Load(configPath) // Loads the user's configuration -- keybindings, marks, etc.
			if err != nil {
				app.configErr = err
				cfg = appconfig.Defaults()
			}

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
			app.configPath = configPath
			app.env = env
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

	root.PersistentFlags().StringVar(&configPath, "config", "", "path to config TOML")
	root.AddCommand(
		doctorCmd(app),
		logsCmd(app),
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
