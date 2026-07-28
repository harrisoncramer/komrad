package cli

import (
	"github.com/spf13/cobra"
)

// hideCmd hides the panel by detaching the client viewing the manager session. The
// tmux plugin runs this rather than detaching the session itself, since the key
// binding is on the user's server and the session it would name lives on the
// manager's.
func hideCmd(state *app) *cobra.Command {
	return &cobra.Command{
		Use:           "hide",
		Short:         "Hide the manager popup",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(_ *cobra.Command, _ []string) error {
			state.popup.Hide()

			return nil
		},
	}
}
