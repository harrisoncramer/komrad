package cli

import (
	"komrad/internal/version"
	"komrad/lib/xhttp"

	"github.com/spf13/cobra"
)

func versionCmd(state *app) *cobra.Command {
	return &cobra.Command{
		Use:         "version",
		Annotations: map[string]string{survivesBadConfig: ""},
		Short:       "Print which build this binary is, and which one is serving",
		Long: `Print which build this binary is, and which one is answering on the address.

The two can differ. The server is long-lived and keeps the binary it started with,
so rebuilding replaces the file on disk without changing what is running. When the
second line is not the first, restart the server.`,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cmd.Printf("this binary: %s\n", version.Describe(version.Version(), version.Commit()))

			base := state.getAPIBase()

			probe := probeServer(cmd.Context(), state.signTransport(xhttp.NewTransport()), base)
			if probe.refusal != nil {
				return probe.refusal
			}

			if !probe.answering {
				cmd.Printf("nothing is serving at %s\n", state.nameServer())

				return nil
			}

			cmd.Printf("the server at %s: %s\n", state.nameServer(), version.Describe(probe.version, probe.commit))

			return nil
		},
	}
}
