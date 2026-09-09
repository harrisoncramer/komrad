package cli

import (
	"errors"

	"weave/internal/tmux"

	"github.com/spf13/cobra"
)

// ErrPanelHidden is an answer rather than a failure, reported in the exit status and never
// printed.
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

// isPanelShownHere answers for this server alone.
func isPanelShownHere(state *app) bool {
	socket := tmux.SocketFromEnv(state.env.Tmux)
	if socket == "" {
		return false
	}

	here := state.tmux.GetHostSocket() == socket && state.popup.IsShown()

	// This runs on the user's server on every press, which is the last moment before the panel
	// is on screen, so it is where the directory a new agent inherits is freshest.
	state.tmux.RecordHostPath(hostPath(socket))

	return here
}
