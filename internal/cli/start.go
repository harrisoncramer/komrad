package cli

import (
	"github.com/spf13/cobra"
)

// startCmd starts the server if this build is not serving already, and waits until it can
// answer.
//
// The panel does this for itself on the way up, so this exists for anyone who wants the
// service running without opening the popup. A server from this build is left alone rather
// than started twice.
func startCmd(state *app) *cobra.Command {
	return &cobra.Command{
		Use:           "start",
		Short:         "Start the server if it is not already running",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			addr := state.env.Addr

			if probeServer(addr).current {
				cmd.Printf("the server at %s is already running\n", addr)

				return nil
			}

			if err := ensureServer(addr, selfPath(), state.env.LogPath()); err != nil {
				return err
			}

			cmd.Printf("the server is listening at %s\n", addr)

			return nil
		},
	}
}
