package cli

import (
	"errors"

	"weave/internal/tmux"
	"weave/internal/ui"

	"github.com/spf13/cobra"
)

// attachCmd shows the running panel in the terminal it was typed in, on the same session the
// panel key opens; it refuses rather than starting one, since this is a second way to look at
// a running panel, not a second way to start it. It leaves the popup's open flag alone, since
// that flag is what the panel key reads to choose between opening and hiding, and an attach in
// a terminal is neither.
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

			// Only recorded when this terminal is a pane on the user's own server; outside
			// tmux nothing is cleared, since a server that can still answer beats none at all.
			if socket := tmux.SocketFromEnv(state.env.Tmux); socket != "" {
				state.host.Record(socket)
				state.popup.SetInvokingClient(invokingClient(socket, tmux.SessionFromEnv(state.env.Tmux)))
			}

			// Bound per attach for the reason open binds them per open: a changed key or a
			// replaced binary takes effect without restarting the panel.
			state.popup.BindHide()
			state.popup.BindPalette(ui.BindPaletteParams{
				Self:        getSelfPath(),
				ConfigPaths: state.configPaths,
			})

			return attachManagerSession(state)
		},
	}
}
