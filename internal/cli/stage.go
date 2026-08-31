package cli

import (
	"weave/internal/stage"

	"github.com/spf13/cobra"
)

func stageCmd(*app) *cobra.Command {
	return &cobra.Command{
		Use:   "stage <file>",
		Short: "Draw a file into the panel's stage pane",
		Long: `Draw a file into the panel's stage pane, redrawing it whenever a key arrives.

The panel runs this in the pane beside it and taps a key at it once it has rewritten
the file. It is not meant to be run by hand.`,
		Hidden:        true,
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(_ *cobra.Command, args []string) error {
			return stage.Run(args[0])
		},
	}
}
