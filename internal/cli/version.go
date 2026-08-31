package cli

import (
	"weave/internal/version"

	"github.com/spf13/cobra"
)

func versionCmd(state *app) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print which build this binary is, and which one is serving",
		Long: "Print which build this binary is, and which one is answering on the address.\n\n" +
			"The two can differ. The service is long-lived and keeps the binary it started with,\n" +
			"so rebuilding replaces the file on disk without changing what is running. When the\n" +
			"second line is not the first, restart the service.",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			addr := state.env.Addr
			cmd.Printf("this binary: %s\n", version.Version())

			probe := probeServer(cmd.Context(), addr)
			if !probe.answering {
				cmd.Printf("nothing is serving at %s\n", addr)

				return nil
			}

			cmd.Printf("the server at %s: %q\n", addr, probe.version)

			return nil
		},
	}
}
