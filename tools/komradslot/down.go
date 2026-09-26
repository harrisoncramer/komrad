package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
)

type runner interface {
	run(ctx context.Context, name string, args ...string) error
}

// execRunner runs a command with its output sent to out.
type execRunner struct {
	out io.Writer
}

func (e execRunner) run(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = e.out
	cmd.Stderr = e.out

	return cmd.Run()
}

// down stops everything slot n runs and gives the slot back, keeping the claim when the
// database cannot be taken down.
func (r registry) down(ctx context.Context, n int) error {
	if n < 1 || n > slotMax {
		return fmt.Errorf("%d is not a slot", n)
	}

	r.log.Printf("taking down slot %d", n)

	if err := r.cmds.run(ctx, "tmux", "-L", socket(n), "kill-server"); err != nil {
		r.log.Printf("no tmux server of slot %d was stopped: %v", n, err)
	}

	if err := r.cmds.run(ctx, "pkill", "-f", fmt.Sprintf("komrad serve --addr %s( |$)", serverAddr(n))); err != nil {
		r.log.Printf("no server of slot %d was stopped: %v", n, err)
	}

	if err := r.cmds.run(ctx, "docker", "info"); err != nil {
		return r.keepClaim(n, fmt.Errorf("failed to reach docker to take down the database of slot %d: %w", n, err))
	}

	if err := r.cmds.run(ctx, "docker", "compose", "-p", socket(n), "down", "--volumes", "--remove-orphans"); err != nil {
		return r.keepClaim(n, fmt.Errorf("failed to take down the database of slot %d: %w", n, err))
	}

	if err := os.RemoveAll(r.slotDir(n)); err != nil {
		return fmt.Errorf("failed to give slot %d back: %w", n, err)
	}

	return nil
}

func (r registry) keepClaim(n int, cause error) error {
	if err := os.Remove(filepath.Join(r.slotDir(n), "down")); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("%w, and failed to clear its down marker: %w", cause, err)
	}

	return cause
}
