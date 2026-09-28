package cli

import (
	"errors"

	"komrad/internal/tmux"

	"github.com/spf13/cobra"
)

func attachCmd(state *app) *cobra.Command {
	return &cobra.Command{
		Use:   "attach",
		Short: "Attach the running panel in this terminal",
		Long: `Attach the running panel in this terminal.

It attaches the session ` + "`komrad open`" + ` puts up, so any number of terminals show the
same panel. Detaching gives this terminal back and leaves everything running.

It attaches a panel that is already up and refuses when there is none, since
starting one is what ` + "`komrad open`" + ` is for.`,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !isPanelRunning(cmd.Context(), state) {
				return errors.New("the panel is not running, so there is nothing to attach: " +
					"run `komrad open` to start it")
			}

			socket := tmux.SocketFromEnv(state.env.Tmux)
			state.tmux.RecordHostSocket(socket)
			state.tmux.RecordHostPath(hostPath(socket))

			return attachManagerSession(state)
		},
	}
}
