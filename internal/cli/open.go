package cli

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"claude-agents/internal/agents"

	"github.com/spf13/cobra"
)

// openCmd is the popup entrypoint. It creates the persistent manager tmux session
// if it does not exist and attaches, otherwise attaches to the running one. The
// tmux plugin runs this as the popup's -E command, so it blocks until the client
// detaches and then returns, which closes the popup. TMUX is stripped so tmux does
// not refuse the attach as nested, since the popup itself lives inside the user's
// tmux client.
//
// The session lives on the manager's own tmux server, which the user's server knows
// nothing about. tmux interpolates the socket path of the server that opened the
// popup into the command, and that path is recorded so the panel can reach back to
// it for the two things only it can answer.
func openCmd(state *app) *cobra.Command {
	return &cobra.Command{
		Use:           "open [client] [host-socket]",
		Short:         "Open the manager popup (create or attach its persistent session)",
		Args:          cobra.MaximumNArgs(2),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(_ *cobra.Command, args []string) error {
			// Creating the session detached and attaching to it separately, rather than
			// with one new-session -A, leaves room to record the user's socket on the
			// manager server between the two. A single attaching command holds the
			// client for as long as the popup is up, so nothing could run in between.
			if err := ensureManagerSession(state); err != nil {
				return err
			}

			// Recorded first, since everything below reaches the user's server through
			// it. Rewritten on every open, so a popup from a different server or a
			// restarted one points the panel at the server actually showing it.
			state.host.Record(argAt(args, 1))

			// Record the client that opened this popup so panel actions resolve the
			// right directory and client. Mark the popup shown so the binding hides
			// it on the next press. The mark clears when the attach below returns.
			state.popup.SetInvokingClient(argAt(args, 0))
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
		"-e", "CLAUDE_AGENTS_TMUX_SOCKET="+state.env.TmuxSocket,
		"-e", "DATABASE_URL="+state.env.DatabaseURL,
		"-e", "CLAUDE_AGENTS_ADDR="+state.env.Addr,
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

// argAt returns the positional argument at i, empty when it was not given.
func argAt(args []string, i int) string {
	if i >= len(args) {
		return ""
	}

	return args[i]
}
