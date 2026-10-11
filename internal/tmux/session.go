package tmux

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"syscall"
)

// HasSession reports whether the server holds the named session.
func (c *Client) HasSession(ctx context.Context, session string) bool {
	return c.commandOutsideTmux(ctx, "has-session", "-t", session).Run() == nil
}

// RunToStderr runs a tmux command whose complaints go straight to this process's stderr.
func (c *Client) RunToStderr(ctx context.Context, args ...string) error {
	cmd := c.commandOutsideTmux(ctx, args...)
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

// ExecAttachSession replaces this process with a tmux client attached to the named session.
func (c *Client) ExecAttachSession(session string) error {
	path, err := exec.LookPath("tmux")
	if err != nil {
		return fmt.Errorf("failed to find tmux: %w", err)
	}

	args := slices.Concat([]string{"tmux"}, c.server, []string{"attach-session", "-t", session})
	if err := syscall.Exec(path, args, buildClientEnv()); err != nil {
		return fmt.Errorf("failed to attach session %q: %w", session, err)
	}

	return nil
}

func (c *Client) commandOutsideTmux(ctx context.Context, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "tmux", slices.Concat(c.server, args)...)
	cmd.Env = buildClientEnv()

	return cmd
}
