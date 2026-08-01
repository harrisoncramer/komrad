package cli

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"weave/internal/agents"
	"weave/internal/tmux"

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
		Use:           "open",
		Short:         "Open the manager popup (create or attach its persistent session)",
		Args:          cobra.ArbitraryArgs,
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

			// Record the client that opened this popup so the panel resolves the
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
// environment rather than the popup's. The resolved config is forwarded with -e so
// the panel reads the same values this process did, including any shell overrides.
// Only the open that creates the session sets them, since every later one finds it
// already up.
func ensureManagerSession(state *app) error {
	if err := state.tmuxCommand("has-session", "-t", agents.ManagerSession).Run(); err == nil {
		return nil
	}

	cmd := state.tmuxCommand(
		"new-session", "-d", "-s", agents.ManagerSession,
		"-e", "WEAVE_TMUX_SOCKET="+state.env.TmuxSocket,
		"-e", "DATABASE_URL="+state.env.DatabaseURL,
		"-e", "WEAVE_ADDR="+state.env.Addr,
		"-e", "EDITOR="+state.env.Editor,
		"--", selfPath(),
	)
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to create the manager session: %w", err)
	}

	return nil
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
	full := os.Environ()
	filtered := make([]string, 0, len(full))
	for _, entry := range full {
		if !strings.HasPrefix(entry, "TMUX=") {
			filtered = append(filtered, entry)
		}
	}

	return filtered
}

// invokingClient is the client viewing this popup, found by asking the user's server
// which one is attached to the session the popup was opened from. It is asked for rather
// than passed in because the popup's own environment names its server and session but
// not the client, and nothing in the key binding can carry one.
func invokingClient(socket, session string) string {
	if socket == "" || session == "" {
		return ""
	}

	return tmux.NewClientAtPath(socket).SessionClient(session)
}
