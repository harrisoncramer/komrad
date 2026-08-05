package cli

import (
	"github.com/spf13/cobra"
)

// stopCmd asks a running server to stop and waits for it to be gone.
//
// It exists so a reload can ask rather than signal. A signal says nothing back, which
// leaves whoever sent it to guess at how long to wait and then kill on a timer, and
// guessing short is what abandons a launch partway through.
func stopCmd(state *app) *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Ask the running service to stop, and wait until it is gone",
		Long: "Ask the running service to stop, and wait until it is gone.\n\n" +
			"This asks rather than signals, so the service can finish the launch it is in the\n" +
			"middle of and say when it is done. It returns once the process has actually gone,\n" +
			"not when the address stops answering, which happens well before the shutdown is\n" +
			"finished. Nothing serving at the address is not an error.\n\n" +
			"Agents keep running: they are tmux panes, not children of this process. They are\n" +
			"picked back up when the service returns.",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			addr := state.env.Addr

			pid, err := stopServer(addr)
			if err != nil {
				return err
			}

			if pid == 0 {
				cmd.Printf("nothing is serving at %s\n", addr)

				return nil
			}

			cmd.Printf("the server at %s has stopped\n", addr)

			return nil
		},
	}
}
