// Package cli builds the weave command tree and wires its dependencies.
//
// This file holds the root command and nothing else. Each subcommand lives in its
// own file named for it, holding that command and only the helpers it uses. What
// more than one command needs sits apart, in app.go for the shared state they are
// wired from and process.go for finding this binary and starting the server.
package cli

import (
	"fmt"
	"net/http"

	"weave/config"
	"weave/internal/agents"
	"weave/internal/appconfig"
	"weave/internal/harness"
	"weave/internal/harness/claude"
	"weave/internal/harness/codex"
	"weave/internal/tmux"
	"weave/internal/ui"
	"weave/proto/gen/action/v1/actionv1connect"
	"weave/proto/gen/agent/v1/agentv1connect"
	"weave/proto/gen/schedule/v1/schedulev1connect"
	"weave/proto/gen/workflow/v1/workflowv1connect"

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
		RunE: func(_ *cobra.Command, _ []string) error {
			// A server that will not start is carried into the panel and shown on its
			// error screen for the same reason a bad config is. This process is the
			// command of the tmux session behind the popup, so returning here ends that
			// session, and tmux tears its pane down with the message still on it. The
			// popup then closes on a success it never had, leaving nothing on screen to
			// say a database was unreachable.
			logPath := appconfig.LogPath()
			startupErr := ensureServer(state.env.Addr, selfPath(), logPath)

			base := "http://" + state.env.Addr
			agentClient := agentv1connect.NewAgentServiceClient(http.DefaultClient, base)
			workflowClient := workflowv1connect.NewWorkflowServiceClient(http.DefaultClient, base)
			scheduleClient := schedulev1connect.NewScheduleServiceClient(http.DefaultClient, base)
			actionClient := actionv1connect.NewActionServiceClient(http.DefaultClient, base)

			// Drop this tmux session's status bar so the panel and its stage pane
			// fill the popup without a footer beneath them.
			state.popup.SilenceStatus(agents.ManagerSession)

			// This process runs inside the persistent manager tmux session and never
			// exits on its own. The close and quit keys detach the popup rather than
			// quit, so the panel stays one long-lived program with its state
			// preserved between opens. Run returns only on a real error.
			return ui.Run(ui.Deps{
				Popup:      state.popup,
				Tmux:       state.tmux,
				Agents:     agentClient,
				Workflows:  workflowClient,
				Schedules:  scheduleClient,
				Keys:       state.keys,
				Actions:    actionClient,
				Marks:      state.marks,
				Layout:     state.layout,
				Addr:       state.env.Addr,
				Editor:     state.env.Editor,
				LogPath:    logPath,
				LogViewer:  state.logViewer,
				Binary:     selfPath(),
				ConfigErr:  state.configErr,
				StartupErr: startupErr,
			})
		},
	}

	root.PersistentFlags().StringVar(&configPath, "config", "", "path to config TOML")
	root.AddCommand(
		logsCmd(),
		openCmd(state),
		hideCmd(state),
		serveCmd(state),
		migrateCmd(state),
	)

	return root
}
