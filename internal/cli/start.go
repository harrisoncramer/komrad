package cli

import (
	"github.com/spf13/cobra"
)

func startCmd(state *app) *cobra.Command {
	return &cobra.Command{
		Use:   "start",
		Short: "Start the service in the background if it is not already running",
		Long: `Start the service in the background if it is not already running.

It asks the configured address who is there first. A server of this build is left
alone; one of another build is asked to stop and replaced, so a rebuilt binary is
the one answering. Otherwise ` + "`weave serve`" + ` is spawned detached, with its output
going to the log file, and this waits for the address to answer before returning.

Opening the panel does the same thing on its own, so this is for scripts and for
bringing the service up without a panel.`,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			addr := state.env.Addr

			if probeServer(cmd.Context(), addr).current {
				cmd.Printf("the server at %s is already running\n", addr)

				return nil
			}

			if err := ensureServer(cmd.Context(), ensureServerParams{
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
