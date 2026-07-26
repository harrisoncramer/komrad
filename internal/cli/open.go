package cli

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"claude-agents/internal/agents"

	"github.com/spf13/cobra"
)

// openCmd is the popup entrypoint. It creates the persistent manager session if
// it does not exist and attaches, otherwise attaches to the running one. The
// tmux plugin runs this as the popup's -E command, so it blocks until the client
// detaches and then returns, which closes the popup. TMUX is stripped so tmux
// does not refuse the attach as nested, since the popup itself lives inside the
// user's tmux client.
func openCmd(state *app) *cobra.Command {
	return &cobra.Command{
		Use:           "open [client]",
		Short:         "Open the manager popup (create or attach its persistent session)",
		Args:          cobra.MaximumNArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(_ *cobra.Command, args []string) error {
			// Record the client that opened this popup so panel actions resolve the
			// right directory and client. Mark the popup shown so the binding hides
			// it on the next press. The mark clears when the attach below returns.
			state.popup.SetInvokingClient(firstOrEmpty(args))
			state.popup.MarkOpen()
			defer state.popup.MarkClosed()

			// The panel runs as this session's command, so it inherits the tmux
			// server's environment rather than the popup's. Forward the resolved
			// config with -e so the panel reads the same values this process did,
			// including any shell overrides. -e applies only when the session is
			// first created. On later opens new-session -A just attaches.
			tmuxArgs := []string{
				"-L", state.env.TmuxSocket, "new-session", "-A", "-s", agents.ManagerSession,
				"-e", "CLAUDE_AGENTS_TMUX_SOCKET=" + state.env.TmuxSocket,
				"-e", "DATABASE_URL=" + state.env.DatabaseURL,
				"-e", "CLAUDE_AGENTS_ADDR=" + state.env.Addr,
				"--", selfPath(),
			}

			cmd := exec.Command("tmux", tmuxArgs...)
			cmd.Env = envWithoutTmux()
			cmd.Stdin = os.Stdin
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr

			if err := cmd.Run(); err != nil {
				return fmt.Errorf("failed to open manager popup: %w", err)
			}

			return nil
		},
	}
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

func firstOrEmpty(args []string) string {
	if len(args) == 0 {
		return ""
	}

	return args[0]
}
