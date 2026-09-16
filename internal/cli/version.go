package cli

import (
	"weave/internal/version"

	"github.com/spf13/cobra"
)

func versionCmd(state *app) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print which build this binary is, and which one is serving",
		Long: `Print which build this binary is, and which one is answering on the address.

The two can differ. The service is long-lived and keeps the binary it started with,
so rebuilding replaces the file on disk without changing what is running. When the
second line is not the first, restart the service.`,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cmd.Printf("this binary: %s\n", version.Describe(version.Version(), version.Commit()))

			addr, release, err := state.dialAPI(cmd.Context())
			if err != nil {
				return err
			}

			defer release()

			probe := probeServer(cmd.Context(), addr)
			if !probe.answering {
				cmd.Printf("nothing is serving at %s\n", state.env.Addr)

				return nil
			}

			cmd.Printf("the server at %s: %s\n", state.env.Addr, version.Describe(probe.version, probe.commit))

			return nil
		},
	}
}
