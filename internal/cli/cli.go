// Package cli builds the weave command tree and wires its dependencies.
//
// This file holds the root command and nothing else. Each subcommand lives in its
// own file named for it, holding that command and only the helpers it uses. The
// shared state lives in app.go instead.
package cli

import (
	"fmt"

	"weave/config"
	"weave/internal/agents"
	"weave/internal/appconfig"
	"weave/internal/harness"
	"weave/internal/harness/claude"
	"weave/internal/harness/codex"
	"weave/internal/tmux"
	"weave/internal/ui"

	"github.com/spf13/cobra"
)

// NewRootCommand builds the weave command tree. Running it with no
// subcommand opens the panel.
func NewRootCommand() *cobra.Command {

	app := &app{} // The app holds basically all of our application state, which drives the CLI commands and the bubbletea models.

	var configPath string

	root := &cobra.Command{
		Use:           "weave",
		Short:         "tmux panel and CLI for background coding agents",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(_ *cobra.Command, _ []string) error {

			env, err := config.Load() // Loads the environment, tmux socket name, cache directory, etc.
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			cfg, err := appconfig.Load(configPath) // Loads the user's configuration -- keybindings, marks, etc.
			if err != nil {
				app.configErr = err
				cfg = appconfig.Defaults()
			}

			// Load in the harnesses and verify that we can reach them. Then attach to the app.

			codexHarness := codex.New(codex.Options{TrustLaunchDirs: cfg.Harness.Codex.TrustLaunchDirs})
			claudeHarness := claude.New("")
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

			manager := tmux.NewClient(env.TmuxSocket)
			app.host = tmux.NewHost(manager)
			app.tmux = tmux.NewServers(manager, app.host)

			app.popup = ui.NewPopup(app.tmux, agents.ManagerSession)
			app.contextPath, _ = cfg.ContextFile() // No context file is not an error
			app.defaultFlags = map[string]string{  // Set up default flags for the harnesses provided by the user in their config file.
				claudeHarness.GetID(): cfg.Harness.Claude.Flags,
				codexHarness.GetID():  cfg.Harness.Codex.Flags,
			}

			return nil
		},
	}

	root.PersistentFlags().StringVar(&configPath, "config", "", "path to config TOML")
	root.AddCommand(
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
	)

	return root
}
