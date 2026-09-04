package cli

import (
	"errors"

	"weave/internal/tmux"

	"github.com/spf13/cobra"
)

func attachCmd(state *app) *cobra.Command {
	return &cobra.Command{
		Use:   "attach",
		Short: "Attach the running panel in this terminal",
		Long: `Attach the running panel in this terminal.

This is the panel without the popup around it, on the same session the panel key
opens, so both show the same thing and neither disturbs the other. The panel key
detaches it, which gives this terminal back and leaves everything running.

It attaches a panel that is already up and refuses when there is none, since
starting one is what the panel key is for.`,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !isManagerSessionRunning(cmd.Context(), state) {
				return errors.New("the panel is not running, so there is nothing to attach: " +
					"press the panel key, or run `weave open`, to start it")
			}

			socket := tmux.SocketFromEnv(state.env.Tmux)
			state.tmux.RecordHostSocket(socket)
			state.tmux.RecordHostPath(hostPath(socket))

			state.popup.BindHide(userPanelKey(state, socket))

			return attachManagerSession(cmd.Context(), state)
		},
	}
}
