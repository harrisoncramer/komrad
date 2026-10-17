package cli

import (
	"komrad/lib/xhttp"

	"github.com/spf13/cobra"
)

func startCmd(state *app) *cobra.Command {
	return &cobra.Command{
		Use:   "start",
		Short: "Start the server in the background if it is not already running",
		Long: `Start the server in the background if it is not already running.

It asks the configured address who is there first. A server already answering is
left alone, whatever build it runs, so a rebuilt binary takes over only after
` + "`komrad stop`" + `. Otherwise ` + "`komrad serve`" + ` is spawned detached, with its output
going to the log file, and this waits for the address to answer before returning.

When connection.embedded_postgres is set, this starts the embedded Postgres
first if it is down, even when the server is already running. Only this and
` + "`komrad open`" + ` start it, and ` + "`komrad stop`" + ` leaves it running.

Opening the panel does the same thing on its own, so this is for scripts and for
bringing the server up without a panel.`,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			addr := state.env.Addr

			if err := startEmbeddedPostgres(cmd.Context(), getEmbeddedPostgresDir(state.env)); err != nil {
				return err
			}

			if probeServer(cmd.Context(), xhttp.NewTransport(), formatBase(addr)).answering {
				cmd.Printf("the server at %s is already running\n", addr)

				return nil
			}

			if err := ensureServer(cmd.Context(), ensureServerParams{
				Addr:       addr,
				Self:       getSelfPath(),
				LogPath:    state.env.LogPath(),
				ConfigArgs: configArgs(state.configStack),
			}); err != nil {
				return err
			}

			cmd.Printf("the server is listening at %s\n", addr)

			return nil
		},
	}
}
