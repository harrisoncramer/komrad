package cli

import (
	"errors"
	"fmt"

	"weave/internal/tmux"
	"weave/internal/ui"

	"github.com/spf13/cobra"
)

func tmuxCmd(state *app) *cobra.Command {
	return &cobra.Command{
		Use:   "tmux",
		Short: "Bind the panel key on your tmux server",
		Long: `Bind the panel key on your tmux server.

This is what a tmux config runs, as ` + "`run-shell 'weave tmux'`" + `, and it is where the
key that opens and hides the panel comes from. The path of this binary is resolved
once and written into the binding, so weave only has to be findable when tmux parses
its config rather than for the life of the server.

A tmux server has to be running, since binding a key does not start one. The binding
belongs to the server rather than to a session, so it outlives the session it was
made from and dies with the server, and running this again only replaces it. Run it
by hand after changing @weave_key to bind the new one without restarting tmux.`,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(_ *cobra.Command, _ []string) error {
			server, err := panelKeyServer(state)
			if err != nil {
				return err
			}

			if _, err := server.SocketPath(); err != nil {
				return fmt.Errorf("no tmux server is running to bind the panel key on, so start tmux and "+
					"run this again, or let tmux bind it on startup with `run-shell 'weave tmux'` in your "+
					"tmux config: %w", err)
			}

			return ui.BindPanelKey(server, ui.BindPanelKeyParams{
				Self:        getSelfPath(),
				ConfigPaths: state.configPaths,
			})
		},
	}
}

func panelKeyServer(state *app) (*tmux.Client, error) {
	socket, err := panelKeySocket(state.env.Tmux, func() string {
		path, err := state.tmux.SocketPath()
		if err != nil {
			return ""
		}

		return path
	}, state.tmux.GetHostSocket)
	if err != nil {
		return nil, err
	}

	return socket.client(), nil
}

func panelKeySocket(env string, manager, host func() string) (tmuxSocket, error) {
	socket := tmux.SocketFromEnv(env)
	if socket == "" {
		return tmuxSocket{name: tmux.DefaultSocket}, nil
	}

	if socket != manager() {
		return tmuxSocket{path: socket}, nil
	}

	recorded := host()
	if recorded == "" {
		return tmuxSocket{}, errors.New("this is weave's own tmux server, and no panel has opened yet to " +
			"record which server is yours, so run `weave tmux` from a shell on your own tmux server instead")
	}

	return tmuxSocket{path: recorded}, nil
}

type tmuxSocket struct {
	name string
	path string
}

func (s tmuxSocket) client() *tmux.Client {
	if s.path != "" {
		return tmux.NewClientAtPath(s.path)
	}

	return tmux.NewClient(s.name)
}
