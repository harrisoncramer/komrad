package cli

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"weave/internal/agents"
	"weave/internal/tmux"
	"weave/lib/xslice"

	"github.com/spf13/cobra"
)

// openCmd is the popup entrypoint, creating the persistent manager tmux session if it is
// not there and attaching either way. The tmux plugin runs this as the popup's -E command,
// so it blocks until the client detaches, which closes the popup.
//
// The session lives on the manager's own tmux server, which the user's server knows nothing
// about. Which server the popup came from is read out of this process's environment and
// recorded, so the panel can reach back for the things only that server can answer.
//
// Arguments are accepted and ignored, so a key binding that passes something is not
// refused. Nothing can be passed in usefully anyway, since tmux expands no format in a
// popup's command and an unexpanded one is a comment to the shell that runs it.
func openCmd(state *app) *cobra.Command {
	return &cobra.Command{
		Use:   "open",
		Short: "Open the panel popup, creating or attaching its persistent session",
		Long: "Open the panel popup over the current tmux session.\n\n" +
			"It creates the manager session if it is not there and attaches either way, so the\n" +
			"panel keeps its place between opens rather than starting over. The manager session\n" +
			"lives on weave's own tmux server, so it does not show up in `tmux ls`.\n\n" +
			"The tmux plugin runs this as the popup's command, so it blocks until the client\n" +
			"detaches, which is what closes the popup. Pressing the panel key again runs\n" +
			"`weave hide`. Run by hand outside a popup it attaches the manager session in the\n" +
			"terminal instead, which is the panel without the popup around it.",
		Args:          cobra.ArbitraryArgs,
		Hidden:        true,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(_ *cobra.Command, _ []string) error {
			// Creating the session detached and attaching separately, rather than with
			// one new-session -A, leaves room to record the user's socket between the
			// two. A single attaching command would hold the client for as long as the
			// popup is up.
			if err := ensureManagerSession(state); err != nil {
				return err
			}

			// Recorded first, since everything below reaches the user's server through
			// it. Rewritten on every open, so a popup from a different server or a
			// restarted one points the panel at the server actually showing it.
			socket := tmux.SocketFromEnv(state.env.Tmux)
			state.host.Record(socket)

			// Record the client that opened this popup so the panel picks the
			// right directory and client. Mark the popup shown so the binding hides
			// it on the next press. The mark clears when the attach below returns.
			state.popup.SetInvokingClient(invokingClient(socket, tmux.SessionFromEnv(state.env.Tmux)))
			state.popup.MarkOpen()
			defer state.popup.MarkClosed()

			// The manager server has its own copy of the user's key bindings, so the
			// panel key has to be given its inside-the-popup meaning there. Done per
			// open rather than once at creation, so changing the key takes effect
			// without restarting the panel.
			state.popup.BindHide()

			attach := state.tmuxCommand("attach-session", "-t", agents.ManagerSession)
			attach.Stdin = os.Stdin
			attach.Stdout = os.Stdout
			attach.Stderr = os.Stderr

			if err := attach.Run(); err != nil {
				return fmt.Errorf("failed to open manager popup: %w", err)
			}

			return nil
		},
	}
}

// ensureManagerSession brings the manager session up detached, doing nothing when it
// is already running. Existence is checked rather than left to new-session -A, which
// attaches to a session it finds and would then hold the popup open before anything
// after it ran.
//
// The panel runs as this session's command, so it inherits the tmux server's
// environment rather than the popup's, which is why the session is created the way
// buildManagerSessionArgs builds it.
func ensureManagerSession(state *app) error {
	if err := state.tmuxCommand("has-session", "-t", agents.ManagerSession).Run(); err == nil {
		return nil
	}

	cmd := state.tmuxCommand(buildManagerSessionArgs(state, getSelfPath())...)
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to create the manager session: %w", err)
	}

	return nil
}

// buildManagerSessionArgs is the tmux command line that creates the manager session,
// with self as the binary the session runs.
//
// A tmux server hands every session the environment it was itself started with, so a
// server that has been up for days is still handing out some shell nobody remembers.
// Every variable the panel and the server it spawns read is therefore forwarded with -e
// rather than left to that. Only the open that creates the session sets them, since every
// later one finds it already up.
//
// The config paths go on the command line for the same reason: the panel loads the files
// itself, and without being told which ones it would read the default while this process
// read whatever --config named. The resolved list is forwarded rather than the flags as
// written, so the child is told the paths instead of working them out and the two cannot
// disagree about what they are running on.
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

// tmuxCommand builds a tmux command against the manager's server with TMUX removed
// from the environment, so tmux does not refuse it as nested. The popup runs inside
// the user's client, which is where that variable comes from.
func (a *app) tmuxCommand(args ...string) *exec.Cmd {
	cmd := exec.Command("tmux", append([]string{"-L", a.env.TmuxSocket}, args...)...)
	cmd.Env = envWithoutTmux()

	return cmd
}

// envWithoutTmux returns the current environment with TMUX removed, so a tmux
// command run from inside a tmux client is not refused as a nested session.
func envWithoutTmux() []string {
	return xslice.Filter(os.Environ(), func(entry string) bool { return !strings.HasPrefix(entry, "TMUX=") })
}

// invokingClient is the client viewing this popup, found by asking the user's server
// which one is attached to the session the popup was opened from. It is asked for rather
// than passed in because the popup's own environment names its server and session but
// not the client, and nothing in the key binding can carry one.
func invokingClient(socket, session string) string {
	if socket == "" || session == "" {
		return ""
	}

	return tmux.NewClientAtPath(socket).GetSessionClient(session)
}
