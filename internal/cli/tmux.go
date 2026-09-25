package cli

import (
	"errors"
	"fmt"

	"komrad/internal/tmux"
	"komrad/internal/ui"

	"github.com/spf13/cobra"
)

func tmuxCmd(state *app) *cobra.Command {
	var ifRunning bool

	cmd := &cobra.Command{
		Use:   "tmux",
		Short: "Bind the panel key on your tmux server",
		Long: `Bind the panel key on your tmux server.

This is what a tmux config runs, as ` + "`run-shell 'komrad tmux'`" + `, and it is where the
key that opens and hides the panel comes from. The path of this binary is resolved
once and written into the binding, so komrad only has to be findable when tmux parses
its config rather than for the life of the server.

There is one key however many engines this machine knows: pressed, it opens this
machine's panel, or a picker when engine files are present. An engine has no key of
its own, so --engine is refused here.

A tmux server has to be running, since binding a key does not start one. The binding
belongs to the server rather than to a session, so it outlives the session it was
made from and dies with the server. Running this again binds the key as it now
stands and unbinds the one it bound before, so run it by hand after changing
@komrad_key rather than restarting tmux, and after building komrad somewhere else.

Run from a pane on komrad's own server, it binds on the server the last open of the
panel came from, so a rebuild from inside the panel reaches your key too.

With --if-running it says nothing and does nothing when no tmux server is up, which
is what a build script wants.`,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(_ *cobra.Command, _ []string) error {
			if state.configStack.Engine != "" {
				return errors.New("an engine has no panel key of its own: bind the one key with `komrad tmux` and " +
					"choose the engine when you press it")
			}

			server, err := panelKeyServer(state)
			if err != nil {
				return err
			}

			if _, err := server.GetSocketPath(); err != nil {
				if ifRunning {
					return nil
				}

				return fmt.Errorf("no tmux server is running to bind the panel key on, so start tmux and "+
					"run this again, or let tmux bind it on startup with `run-shell 'komrad tmux'` in your "+
					"tmux config: %w", err)
			}

			return ui.BindPanelKey(server, ui.BindPanelKeyParams{
				Self:       getSelfPath(),
				ConfigArgs: configArgs(state.configStack),
				Key:        state.env.PanelKey,
			})
		},
	}

	cmd.Flags().BoolVar(&ifRunning, "if-running", false, "bind only when a tmux server is running, and say nothing when none is")

	return cmd
}

func panelKeyServer(state *app) (*tmux.Client, error) {
	socket, err := panelKeySocket(state.env.Tmux, func() string {
		path, err := state.tmux.GetSocketPath()
		if err != nil {
			return ""
		}

		return path
	}, func(path string) string {
		return tmux.NewClientAtPath(path).GetHostSocket()
	})
	if err != nil {
		return nil, err
	}

	return socket.client(), nil
}

func panelKeySocket(env string, manager func() string, hostOf func(string) string) (tmuxSocket, error) {
	socket := tmux.SocketFromEnv(env)
	if socket == "" {
		return tmuxSocket{name: tmux.DefaultSocket}, nil
	}

	if recorded := hostOf(socket); recorded != "" {
		return tmuxSocket{path: recorded}, nil
	}

	if socket == manager() {
		return tmuxSocket{}, errors.New("this is komrad's own tmux server, and no panel has opened yet to " +
			"record which server is yours, so run `komrad tmux` from a shell on your own tmux server instead")
	}

	return tmuxSocket{path: socket}, nil
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
