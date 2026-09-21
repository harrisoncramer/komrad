package cli

import (
	"errors"

	"weave/config"
	"weave/internal/agents"
	"weave/internal/tmux"
	"weave/internal/ui"

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

	_, here := findShownPanel(state, socket)

	// This runs on the user's server on every press, the last moment before the panel is on
	// screen.
	state.tmux.RecordHostPath(hostPath(socket))

	return here
}

// findShownPanel is the popup on screen from the named server, this machine's or an engine's.
func findShownPanel(state *app, socket string) (*ui.PopupWindow, bool) {
	if state.tmux.GetHostSocket() == socket && state.popup.IsShown() {
		return state.popup, true
	}

	names, err := config.EngineNames()
	if err != nil {
		return nil, false
	}

	for _, name := range names {
		setup, err := loadEngine(state.configStack, name)
		if err != nil {
			continue
		}

		manager := tmux.NewClient(setup.env.TmuxSocket)
		popup := ui.NewPopupWindow(manager, agents.ManagerSession)

		if manager.GetHostSocket() == socket && popup.IsShown() {
			return popup, true
		}
	}

	return nil, false
}
