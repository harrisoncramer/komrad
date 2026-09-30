package cli

import (
	"komrad/internal/tmux"

	"github.com/spf13/cobra"
)

func visualCmd(_ *app) *cobra.Command {
	return &cobra.Command{
		Use:   "visual <file>",
		Short: "Run the stand-in visual mode selects in",
		Long: `Run the stand-in visual mode selects in.

Visual mode draws the sidebar as tiles on a private tmux server, one for each of
its blocks, with only the block that has the keys in copy-mode, so a selection
stops at that block's edge. This command builds that server from file, swaps a
client of it into the sidebar's place, and gives the sidebar back once copy-mode
ends. Nothing here is meant to be run by hand.`,
		Args:          cobra.ExactArgs(1),
		Hidden:        true,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return tmux.RunCopyStandIn(cmd.Context(), args[0])
		},
	}
}
