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
	"strings"
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

// down stops everything slot n runs, drops its database, and gives the slot back.
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

	if err := r.drop(ctx, n); err != nil {
		return r.keepClaim(n, err)
	}

	if err := os.RemoveAll(r.slotDir(n)); err != nil {
		return fmt.Errorf("failed to give slot %d back: %w", n, err)
	}

	return nil
}

// drop stops the embedded Postgres slot n keeps its database in and deletes its cluster.
func (r registry) drop(ctx context.Context, n int) error {
	dir := getPostgresDir(r.slotDir(n))
	clusters, err := filepath.Glob(filepath.Join(dir, "data-*"))
	if err != nil {
		return fmt.Errorf("failed to find the database of slot %d: %w", n, err)
	}

	for _, data := range clusters {
		if err := r.stopPostgres(ctx, dir, data); err != nil {
			return fmt.Errorf("failed to drop the database of slot %d: %w", n, err)
		}

		if err := os.RemoveAll(data); err != nil {
			return fmt.Errorf("failed to drop the database of slot %d: %w", n, err)
		}
	}

	return nil
}

// stopPostgres stops the Postgres serving the cluster in data, when it is running.
func (r registry) stopPostgres(ctx context.Context, dir, data string) error {
	major := strings.TrimPrefix(filepath.Base(data), "data-")
	programs, err := filepath.Glob(filepath.Join(dir, major+".*", "bin", "pg_ctl"))
	if err != nil {
		return fmt.Errorf("failed to find the pg_ctl of %s: %w", data, err)
	}
	if len(programs) == 0 {
		return nil
	}

	pgCtl := programs[len(programs)-1]
	if running := r.cmds.probe(ctx, pgCtl, "status", "-D", data) == nil; !running {
		return nil
	}

	return r.cmds.run(ctx, pgCtl, "stop", "-D", data, "-m", "immediate", "-w")
}

func (r registry) keepClaim(n int, cause error) error {
	if err := os.Remove(filepath.Join(r.slotDir(n), "down")); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("%w, and failed to clear its down marker: %w", cause, err)
	}

	return cause
}
