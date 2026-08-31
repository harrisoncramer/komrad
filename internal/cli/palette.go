package cli

import (
	"weave/internal/palette"

	"github.com/spf13/cobra"
)

func paletteCmd(*app) *cobra.Command {
	return &cobra.Command{
		Use:   "palette",
		Short: "Open the command palette over the panel",
		Long: `Open the command palette.

The panel binds this to a key on its own tmux server, so it opens as a popup over
whatever you are looking at. Escape closes it.

Set ` + "`@weave_palette_key`" + ` in your tmux config to change the key.`,
		Args:          cobra.NoArgs,
		Hidden:        true,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(*cobra.Command, []string) error {
			return palette.Run()
		},
	}
}
