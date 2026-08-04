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
		Use:   "start",
		Short: "Start the service in the background if it is not already running",
		Long: "Start the service in the background if it is not already running.\n\n" +
			"It asks the configured address who is there first. A server of this build is left\n" +
			"alone; one of another build is asked to stop and replaced, so a rebuilt binary is\n" +
			"the one answering. Otherwise `weave serve` is spawned detached, with its output\n" +
			"going to the log file, and this waits for the address to answer before returning.\n\n" +
			"Opening the panel does the same thing on its own, so this is for scripts and for\n" +
			"bringing the service up without a panel.",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			addr := state.env.Addr

			if probeServer(addr).current {
				cmd.Printf("the server at %s is already running\n", addr)

				return nil
			}

			if err := ensureServer(ensureServerParams{
				Addr:        addr,
				Self:        getSelfPath(),
				LogPath:     state.env.LogPath(),
				ConfigPaths: state.configPaths,
			}); err != nil {
				return err
			}

			cmd.Printf("the server is listening at %s\n", addr)

			return nil
		},
	}
}
