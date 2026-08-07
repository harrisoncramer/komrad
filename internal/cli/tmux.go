package cli

import (
	"errors"
	"fmt"

	"weave/internal/tmux"
	"weave/internal/ui"

	"github.com/spf13/cobra"
)

// tmuxCmd binds the panel key on the user's tmux server, which is the one thing a tmux
// config has to do to reach weave. The binding names the absolute path of this binary, so the
// key keeps working even when tmux's PATH is minimal, since a server hands every pane the
// environment it was started with and one started from a GUI launcher keeps that minimal
// PATH for life.
func tmuxCmd(state *app) *cobra.Command {
	return &cobra.Command{
		Use:   "tmux",
		Short: "Bind the panel key on your tmux server",
		Long: "Bind the panel key on your tmux server.\n\n" +
			"This is what a tmux config runs, as `run-shell 'weave tmux'`, and it is where the\n" +
			"key that opens and hides the panel comes from. The path of this binary is resolved\n" +
			"once and written into the binding, so weave only has to be findable when tmux parses\n" +
			"its config rather than for the life of the server.\n\n" +
			"A tmux server has to be running, since binding a key does not start one. The binding\n" +
			"belongs to the server rather than to a session, so it outlives the session it was\n" +
			"made from and dies with the server, and running this again only replaces it. Run it\n" +
			"by hand after changing @weave_key to bind the new one without restarting tmux.",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(_ *cobra.Command, _ []string) error {
			server, err := panelKeyServer(state)
			if err != nil {
				return err
			}

			// Asked before binding, so a server that is not there is one sentence about tmux
			// rather than whatever the first failed bind happened to say.
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

// panelKeyServer is a client for the tmux server the panel key belongs on, found by asking
// weave's own server which socket it is on and which one the panel last opened from. Neither
// question is asked before the answer can change anything, so the common case — a shell
// outside tmux settling on the first check — spawns no tmux process at all.
func panelKeyServer(state *app) (*tmux.Client, error) {
	socket, err := panelKeySocket(state.env.Tmux, func() string {
		// An error here is weave's own server not running, which means this cannot be its
		// popup, so it reads the same as a socket that is not the manager's.
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

// panelKeySocket picks which tmux server the panel key belongs on rather than inheriting it,
// since a bare tmux command reads its target out of TMUX and inside the panel's popup that
// names weave's own server, where a bound key is one nobody can press even though binding it
// reports success. There are three cases: a shell outside tmux means the server tmux itself
// would reach, a shell inside a tmux pane (a config's run-shell) means the server that pane
// belongs to, and the popup means the user's server, which the panel records on every open.
// The two servers are looked up through functions rather than passed, so the common first
// case asks nothing of weave's own server.
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

// tmuxSocket names one tmux server, as either a socket name or a socket path; the two are
// different flags to tmux rather than two spellings of one value, so which was chosen is
// carried rather than resolved into a string.
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
