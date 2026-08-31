package cli

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"weave/internal/agents"
	"weave/internal/tmux"
	"weave/internal/ui"
	"weave/lib/xslice"

	"github.com/spf13/cobra"
)

func openCmd(state *app) *cobra.Command {
	var detach bool

	cmd := &cobra.Command{
		Use:   "open",
		Short: "Open the panel popup, creating or attaching its persistent session",
		Long: `Open the panel popup over the current tmux session.

It creates the manager session if it is not there and attaches either way, so the
panel keeps its place between opens rather than starting over. The manager session
lives on weave's own tmux server, so it does not show up in ` + "`tmux ls`" + `.

The tmux plugin runs this as the popup's command, so it blocks until the client
detaches, which is what closes the popup. Pressing the panel key again runs
` + "`weave hide`" + `. Run by hand outside a popup it attaches the manager session in the
terminal instead, which is the panel without the popup around it.

With --detach it creates the session and returns, which is what a script wants when
it needs somewhere for agents to be put back into.`,
		Args:          cobra.ArbitraryArgs,
		Hidden:        true,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(_ *cobra.Command, _ []string) error {
			if err := ensureManagerSession(state); err != nil {
				return err
			}

			if detach {
				return nil
			}

			socket := tmux.SocketFromEnv(state.env.Tmux)
			state.host.Record(socket)

			state.popup.SetInvokingClient(invokingClient(socket, tmux.SessionFromEnv(state.env.Tmux)))
			state.popup.MarkOpen()
			defer state.popup.MarkClosed()

			state.popup.BindHide()

			state.popup.BindPalette(ui.BindPaletteParams{
				Self:        getSelfPath(),
				ConfigPaths: state.configPaths,
			})

			return attachManagerSession(state)
		},
	}

	cmd.Flags().BoolVar(&detach, "detach", false, "create the manager session if it is not there, then return without attaching")

	return cmd
}

func attachManagerSession(state *app) error {
	attach := state.tmuxCommand("attach-session", "-t", agents.ManagerSession)
	attach.Stdin = os.Stdin
	attach.Stdout = os.Stdout
	attach.Stderr = os.Stderr

	if err := attach.Run(); err != nil {
		return fmt.Errorf("failed to attach the panel: %w", err)
	}

	return nil
}

func managerSessionRunning(state *app) bool {
	return state.tmuxCommand("has-session", "-t", agents.ManagerSession).Run() == nil
}

func ensureManagerSession(state *app) error {
	if managerSessionRunning(state) {
		return nil
	}

	cmd := state.tmuxCommand(buildManagerSessionArgs(state, getSelfPath())...)
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to create the manager session: %w", err)
	}

	return nil
}

func buildManagerSessionArgs(state *app, self string) []string {
	args := []string{
		"new-session", "-d", "-s", agents.ManagerSession,
		"-e", "WEAVE_TMUX_SOCKET=" + state.env.TmuxSocket,
		"-e", "WEAVE_DATABASE_URL=" + state.env.DatabaseURL,
		"-e", "WEAVE_READONLY_DATABASE_URL=" + state.env.ReadOnlyDatabaseURL,
		"-e", "WEAVE_ADDR=" + state.env.Addr,
		"-e", "EDITOR=" + state.env.Editor,
		"-e", "WEAVE_CACHE_DIR=" + state.env.CacheDir,
		"--", self, "panel",
	}

	for _, path := range state.configPaths {
		args = append(args, "--config", path)
	}

	return args
}

func (a *app) tmuxCommand(args ...string) *exec.Cmd {
	cmd := exec.Command("tmux", append([]string{"-L", a.env.TmuxSocket}, args...)...)
	cmd.Env = envWithoutTmux()

	return cmd
}

func envWithoutTmux() []string {
	return xslice.Filter(os.Environ(), func(entry string) bool { return !strings.HasPrefix(entry, "TMUX=") })
}

func invokingClient(socket, session string) string {
	if socket == "" || session == "" {
		return ""
	}

	return tmux.NewClientAtPath(socket).GetSessionClient(session)
}
