package cli

import (
	"errors"

	"weave/internal/tmux"
	"weave/internal/ui"

	"github.com/spf13/cobra"
)

func attachCmd(state *app) *cobra.Command {
	return &cobra.Command{
		Use:   "attach",
		Short: "Attach the running panel in this terminal",
		Long: "Attach the running panel in this terminal.\n\n" +
			"This is the panel without the popup around it, on the same session the panel key\n" +
			"opens, so both show the same thing and neither disturbs the other. The panel key\n" +
			"detaches it, which gives this terminal back and leaves everything running.\n\n" +
			"It attaches a panel that is already up and refuses when there is none, since\n" +
			"starting one is what the panel key is for.",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(_ *cobra.Command, _ []string) error {
			if !managerSessionRunning(state) {
				return errors.New("the panel is not running, so there is nothing to attach: " +
					"press the panel key, or run `weave open`, to start it")
			}

			if socket := tmux.SocketFromEnv(state.env.Tmux); socket != "" {
				state.host.Record(socket)
				state.popup.SetInvokingClient(invokingClient(socket, tmux.SessionFromEnv(state.env.Tmux)))
			}

			state.popup.BindHide()
			state.popup.BindPalette(ui.BindPaletteParams{
				Self:        getSelfPath(),
				ConfigPaths: state.configPaths,
			})

			return attachManagerSession(state)
		},
	}
}
