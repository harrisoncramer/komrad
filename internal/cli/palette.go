package cli

import (
	"weave/internal/palette"

	"github.com/spf13/cobra"
)

// paletteCmd is the command palette: one key, a query, and whatever that query turns out to
// mean; it is hidden because the key binding is how it is reached rather than the name. What
// it draws is a skeleton for now: the popup opens and closes, but nothing a query could
// resolve to is written yet.
func paletteCmd(*app) *cobra.Command {
	return &cobra.Command{
		Use:   "palette",
		Short: "Open the command palette over the panel",
		Long: "Open the command palette.\n\n" +
			"The panel binds this to a key on its own tmux server, so it opens as a popup over\n" +
			"whatever you are looking at. Escape closes it.\n\n" +
			"Set `@weave_palette_key` in your tmux config to change the key.",
		Args:          cobra.NoArgs,
		Hidden:        true,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(*cobra.Command, []string) error {
			return palette.Run()
		},
	}
}
