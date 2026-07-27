// Package cli builds the claude-agents command tree and wires its dependencies.
//
// This file holds the root command and nothing else. Each subcommand lives in its
// own file named for it, holding that command and only the helpers it uses. What
// more than one command needs sits apart, in app.go for the shared state they are
// wired from and process.go for finding this binary and starting the server.
package cli

import (
	"net/http"

	"claude-agents/config"
	"claude-agents/internal/agents"
	"claude-agents/internal/appconfig"
	"claude-agents/internal/tmux"
	"claude-agents/internal/ui"
	"claude-agents/proto/gen/agent/v1/agentv1connect"
	"claude-agents/proto/gen/workflow/v1/workflowv1connect"

	"github.com/spf13/cobra"
)

// NewRootCommand builds the claude-agents command tree. Running it with no
// subcommand opens the panel.
func NewRootCommand() *cobra.Command {
	state := &app{}
	var configPath string

	root := &cobra.Command{
		Use:           "claude-agents",
		Short:         "tmux panel and CLI for background Claude agents",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(_ *cobra.Command, _ []string) error {
			env, err := config.Load()
			if err != nil {
				return err
			}

			cfg, err := appconfig.Load(configPath)
			if err != nil {
				return err
			}

			state.env = env
			state.keys = cfg.Keybindings
			state.claudeFlags = cfg.ClaudeFlags
			state.tmux = tmux.NewClient(env.TmuxSocket)
			state.popup = ui.NewPopup(state.tmux, agents.ManagerSession)

			return nil
		},
		RunE: func(_ *cobra.Command, _ []string) error {
			if err := ensureServer(state.env.Addr, selfPath()); err != nil {
				return err
			}

			base := "http://" + state.env.Addr
			agentClient := agentv1connect.NewAgentServiceClient(http.DefaultClient, base)
			workflowClient := workflowv1connect.NewWorkflowServiceClient(http.DefaultClient, base)

			// Drop this tmux session's status bar so the panel and its stage pane
			// fill the popup without a footer beneath them.
			state.popup.SilenceStatus(agents.ManagerSession)

			// This process runs inside the persistent manager tmux session and never
			// exits on its own. The close and quit keys detach the popup rather than
			// quit, so the panel stays one long-lived program with its state
			// preserved between opens. Run returns only on a real error.
			return ui.Run(ui.Deps{
				Popup:     state.popup,
				Tmux:      state.tmux,
				Agents:    agentClient,
				Workflows: workflowClient,
				Keys:      state.keys,
				Addr:      state.env.Addr,
			})
		},
	}

	root.PersistentFlags().StringVar(&configPath, "config", "", "path to config TOML")
	root.AddCommand(
		openCmd(state),
		statusCmd(state),
		serveCmd(state),
		migrateCmd(state),
	)

	return root
}
