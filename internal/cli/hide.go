package cli

import (
	"weave/internal/tmux"

	"github.com/spf13/cobra"
)

func hideCmd(state *app) *cobra.Command {
	return &cobra.Command{
		Use:   "hide",
		Short: "Hide the panel popup without stopping anything",
		Long: `Hide the panel popup.

It detaches the client the popup is holding, which closes the popup. The manager
session stays up, so the next ` + "`weave open`" + ` comes back to the panel where it was, and
the service and the agents are untouched.

This is what the panel key runs while the popup is open, so it is rarely typed.`,
		Args:          cobra.NoArgs,
		Hidden:        true,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(_ *cobra.Command, _ []string) error {
			if socket := tmux.SocketFromEnv(state.env.Tmux); socket != "" {
				if popup, ok := findShownPanel(state, socket); ok {
					popup.Hide()

					return nil
				}
			}

			state.popup.Hide()

			return nil
		},
	}
}
