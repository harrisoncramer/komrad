package cli

import (
	"github.com/spf13/cobra"
)

// hideCmd detaches the client viewing the manager session rather than detaching the session
// itself, since the key binding runs on the user's server and the session it would name lives
// on the manager's.
func hideCmd(state *app) *cobra.Command {
	return &cobra.Command{
		Use:   "hide",
		Short: "Hide the panel popup without stopping anything",
		Long: "Hide the panel popup.\n\n" +
			"It detaches the client the popup is holding, which closes the popup. The manager\n" +
			"session stays up, so the next `weave open` comes back to the panel where it was, and\n" +
			"the service and the agents are untouched.\n\n" +
			"This is what the panel key runs while the popup is open, so it is rarely typed.",
		Args:          cobra.NoArgs,
		Hidden:        true,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(_ *cobra.Command, _ []string) error {
			state.popup.Hide()
			state.popup.MarkClosed()

			return nil
		},
	}
}
