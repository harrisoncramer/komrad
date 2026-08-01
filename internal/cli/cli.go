// Package cli builds the weave command tree and wires its dependencies.
//
// This file holds the root command and nothing else. Each subcommand lives in its
// own file named for it, holding that command and only the helpers it uses. What
// more than one command needs sits apart, in app.go for the shared state they are
// wired from and process.go for finding this binary and starting the server.
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
	state := &app{}
	var configPath string

	// One instance per CLI the binary implements, so every command shares it.
	claudeHarness := claude.New("")
	var codexHarness *codex.Harness

	root := &cobra.Command{
		Use:           "weave",
		Short:         "tmux panel and CLI for background coding agents",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(_ *cobra.Command, _ []string) error {
			env, err := config.Load()
			if err != nil {
				return err
			}

			// A config that will not parse is carried into the panel and shown on its
			// error screen rather than ending the process here. Exiting wrote the
			// message to a popup tmux tore down in the same instant, so the only way
			// to read it was to run the binary by hand. The panel runs on the built-in
			// keybindings meanwhile, which is what it would have used anyway.
			cfg, err := appconfig.Load(configPath)
			if err != nil {
				state.configErr = err
				cfg = appconfig.Defaults()
			}

			state.env = env
			state.keys = cfg.Keybindings
			state.defaultHarness = cfg.DefaultHarness
			state.maxAgents = cfg.MaxAgents

			// Codex is built here rather than beside claude because one of its settings is
			// the user's to make, and the config is only loaded once the command runs.
			codexHarness = codex.New(codex.Options{TrustLaunchDirs: cfg.Harness.Codex.TrustLaunchDirs})
			state.defaultFlags = map[string]string{
				claudeHarness.GetID(): cfg.Harness.Claude.Flags,
				codexHarness.GetID():  cfg.Harness.Codex.Flags,
			}

			// The registry decides what an agent may name, so a configured default it does
			// not hold is a config error like any other and reaches the panel's error screen
			// rather than ending the process here.
			harnesses, err := harness.NewRegistry(claudeHarness, codexHarness)
			if err != nil {
				return err
			}
			if _, err := harnesses.Resolve(cfg.DefaultHarness); err != nil && state.configErr == nil {
				state.configErr = fmt.Errorf("default_harness: %w", err)
			}
			state.harnesses = harnesses
			state.marks = cfg.Marks
			state.layout = cfg.Layout
			state.logLevel = cfg.Log.Level
			state.logViewer = cfg.Log.Viewer
			state.configPath = configPath
			// Two tmux servers, held as one client. The manager's own server owns the
			// session, its windows, and its panes. The user's server answers where the
			// client viewing the popup is sitting and holds the options the plugin's
			// key binding reads, and is found through a socket path the manager server
			// records on every open.
			manager := tmux.NewClient(env.TmuxSocket)
			state.host = tmux.NewHost(manager)
			state.tmux = tmux.NewServers(manager, state.host)
			state.popup = ui.NewPopup(state.tmux, agents.ManagerSession)

			return nil
		},
	}

	root.PersistentFlags().StringVar(&configPath, "config", "", "path to config TOML")
	root.AddCommand(
		logsCmd(state),
		panelCmd(state),
		openCmd(state),
		hideCmd(state),
		serveCmd(state),
		startCmd(state),
		stopCmd(state),
		migrateCmd(state),
		readonlyRoleCmd(state),
	)

	return root
}
