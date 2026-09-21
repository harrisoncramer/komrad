package cli

import (
	"errors"
	"os"

	"github.com/spf13/cobra"
)

const engineUnitPath = "/etc/systemd/system/weave.service"

var errUnitManaged = errors.New("the service here is run by systemd, and a stopped unit stays down until someone with root starts it again; use systemctl stop weave or systemctl restart weave instead")

func stopCmd(state *app) *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Ask the running service to stop, and wait until it is gone",
		Long: `Ask the running service to stop, and wait until it is gone.

This asks rather than signals, so the service can finish the launch it is in the
middle of and say when it is done. It returns once the process has actually gone,
not when the address stops answering, which happens well before the shutdown is
finished. Nothing serving at the address is not an error.

Agents keep running: they are tmux panes, not children of this process. They are
picked back up when the service returns.

On a machine where the service is a systemd unit this refuses, since a service
stopped this way stays down until systemctl starts it again.`,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := requireNotUnitManaged(engineUnitPath); err != nil {
				return err
			}

			addr := state.env.Addr

			pid, err := stopServer(cmd.Context(), addr)
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

func requireNotUnitManaged(unitPath string) error {
	if _, err := os.Stat(unitPath); err != nil {
		return nil
	}

	return errUnitManaged
}
