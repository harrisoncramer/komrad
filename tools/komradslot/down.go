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
	probe(ctx context.Context, name string, args ...string) error
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

// probe runs a command for its exit status alone.
func (e execRunner) probe(ctx context.Context, name string, args ...string) error {
	return exec.CommandContext(ctx, name, args...).Run()
}

// down stops everything slot n runs, drops its database and logins, and gives the slot back.
func (r registry) down(ctx context.Context, n int) error {
	if n < 1 || n > slotMax {
		return fmt.Errorf("%d is not a slot", n)
	}

	r.log.Printf("taking down slot %d", n)

	if err := r.cmds.probe(ctx, "tmux", "-L", getSocket(n), "kill-server"); err != nil {
		r.log.Printf("no tmux server of slot %d was stopped: %v", n, err)
	}

	if err := r.cmds.probe(ctx, "pkill", "-f", fmt.Sprintf("komrad serve --addr %s( |$)", serverAddr(n))); err != nil {
		r.log.Printf("no server of slot %d was stopped: %v", n, err)
	}

	dropDatabase := fmt.Sprintf("DROP DATABASE IF EXISTS %s WITH (FORCE)", getDatabase(n))
	dropLogins := fmt.Sprintf("DROP ROLE IF EXISTS %s, %s_reader", getLogin(n), getLogin(n))
	if err := r.cmds.run(ctx, "docker", "compose", "-p", containerProject, "exec", "-T", "db",
		"psql", "-U", "komrad", "-d", "postgres", "-v", "ON_ERROR_STOP=1", "-c", dropDatabase, "-c", dropLogins); err != nil {
		return r.keepClaim(n, fmt.Errorf("failed to drop the database of slot %d, start the test container with mise run test:container:up and try again: %w", n, err))
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
