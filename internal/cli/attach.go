package cli

import (
	"errors"

	"weave/internal/tmux"
	"weave/internal/ui"

	"github.com/spf13/cobra"
)

// attachCmd shows the running panel in the terminal it was typed in rather than in the popup
// the panel key opens. It is the same session either way, so the panel keeps its place and the
// agents in it carry on.
//
// It refuses rather than creating the session, unlike open, because this is a second way to
// look at a panel that is running and not a second way to start one. A command that quietly
// brought up a service and its database connections because somebody typed it in the wrong
// terminal is worse than a sentence saying nothing is up.
//
// The popup's open flag is left alone. It records whether the popup is showing, the key binding
// reads it to choose between opening and hiding, and an attach in a terminal is neither of
// those. Writing it here would leave the key hiding a popup that was never up.
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

			// Only when this terminal is a pane on the user's own server, which is the one case
			// where an attach knows a server to reach back to. Outside tmux there is nothing to
			// record, and whatever a previous open recorded is left as it was rather than
			// cleared, since a server that can still answer beats none at all.
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
