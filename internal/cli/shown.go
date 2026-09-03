package cli

import (
	"errors"

	"weave/internal/tmux"

	"github.com/spf13/cobra"
)

// ErrPanelHidden is an answer rather than a failure, since `weave shown` says what it found in
// its exit status and prints nothing either way.
var ErrPanelHidden = errors.New("the panel is not on screen on this tmux server")

func shownCmd(state *app) *cobra.Command {
	return &cobra.Command{
		Use:   "shown",
		Short: "Say in the exit status whether the panel is on screen on this tmux server",
		Long: `Say whether the panel is on screen on this tmux server.

The panel key runs this to decide which of its two branches to take, so it is asked
on every press and answers from what is true at that moment rather than from
anything a previous press left behind. It succeeds when the panel has a client
attached and this is the server that opened it, and fails otherwise, which is what
sends the key to ` + "`weave open`" + `.

Failing is the safe direction: a press that cannot get an answer opens the panel
rather than refusing to.`,
		Args:          cobra.NoArgs,
		Hidden:        true,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(_ *cobra.Command, _ []string) error {
			if !isPanelShownHere(state) {
				return ErrPanelHidden
			}

			return nil
		},
	}
}

// isPanelShownHere answers for this server alone, since a panel on screen somewhere else is one
// this press is asking to have here.
func isPanelShownHere(state *app) bool {
	socket := tmux.SocketFromEnv(state.env.Tmux)
	if socket == "" {
		return false
	}

	return state.tmux.GetHostSocket() == socket && state.popup.IsShown()
}
