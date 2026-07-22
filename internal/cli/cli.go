// Package cli builds the claude-agents command tree and wires its dependencies.
package cli

import (
	"fmt"
	"os"
	"strings"

	"claude-agents/config"
	"claude-agents/internal/agents"
	"claude-agents/internal/appconfig"
	"claude-agents/internal/panel"
	"claude-agents/internal/store"
	"claude-agents/internal/tmux"

	"github.com/spf13/cobra"
)

type app struct {
	manager *agents.AgentManager
	env     *config.Config
}

// NewRootCommand builds the claude-agents command tree. Running it with no
// subcommand opens the panel.
func NewRootCommand() *cobra.Command {
	state := &app{}
	var configPath string

	root := &cobra.Command{
		Use:           "claude-agents [client]",
		Short:         "tmux panel and CLI for background Claude agents",
		Args:          cobra.MaximumNArgs(1),
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

			sessions, err := store.Open(env.DBPath)
			if err != nil {
				return err
			}

			state.env = env
			state.manager = agents.NewAgentManager(agents.Params{
				Tmux:        tmux.NewClient(env.TmuxSocket),
				Store:       sessions,
				SelfPath:    selfPath(),
				ClaudeFlags: cfg.ClaudeFlags,
				Hooks: agents.Hooks{
					PreCreate:  cfg.Hooks.PreCreate,
					PostCreate: cfg.Hooks.PostCreate,
					OnDelete:   cfg.Hooks.OnDelete,
				},
			})
			return nil
		},
		RunE: func(_ *cobra.Command, args []string) error {
			return panel.Run(state.manager, firstOrEmpty(args))
		},
	}

	root.PersistentFlags().StringVar(&configPath, "config", "", "path to config TOML")
	root.AddCommand(
		newCmd(state),
		notifyCmd(state),
		statusCmd(state),
		hookCmd(state),
		chatCmd(state),
		chatOpenCmd(state),
	)

	return root
}

func newCmd(state *app) *cobra.Command {
	var params agents.NewSessionParams

	cmd := &cobra.Command{
		Use:   "new",
		Short: "Create a background agent session non-interactively",
		RunE: func(_ *cobra.Command, _ []string) error {
			state.manager.NewSession(params)
			return nil
		},
	}

	flags := cmd.Flags()
	flags.StringVar(&params.Title, "title", "", "session title (blank auto-names)")
	flags.StringVar(&params.Dir, "dir", "", "working directory (blank = current)")
	flags.StringVar(&params.Flags, "flags", "", "extra claude flags")
	flags.StringVar(&params.Prompt, "prompt", "", "initial prompt")
	flags.StringVar(&params.Client, "client", "", "invoking tmux client")

	return cmd
}

func notifyCmd(state *app) *cobra.Command {
	return &cobra.Command{
		Use:   "notify [message...]",
		Short: "Record this agent as done with a message",
		RunE: func(_ *cobra.Command, args []string) error {
			state.manager.Notify(state.env.MainSession, state.env.TmuxPane, strings.Join(args, " "))
			return nil
		},
	}
}

func statusCmd(state *app) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Print the status-bar aggregate",
		RunE: func(_ *cobra.Command, _ []string) error {
			fmt.Print(state.manager.Status())
			return nil
		},
	}
}

func hookCmd(state *app) *cobra.Command {
	return &cobra.Command{
		Use:   "hook <prompt|stop>",
		Short: "Advance agent state from a Claude Code lifecycle event",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			state.manager.Hook(args[0], state.env.MainSession, state.env.TmuxPane)
			return nil
		},
	}
}

func chatCmd(state *app) *cobra.Command {
	return &cobra.Command{
		Use:   "chat <session> <pane>",
		Short: "Attach to an agent's session in the current client",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			return state.manager.AttachChat(args[0], args[1])
		},
	}
}

func chatOpenCmd(state *app) *cobra.Command {
	return &cobra.Command{
		Use:   "chat-open <session> <pane>",
		Short: "Open the large chat popup for an agent",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			state.manager.OpenChatPopup(args[0], args[1])
			return nil
		},
	}
}

func firstOrEmpty(args []string) string {
	if len(args) == 0 {
		return ""
	}

	return args[0]
}

func selfPath() string {
	path, err := os.Executable()
	if err != nil {
		return "claude-agents"
	}

	return path
}
