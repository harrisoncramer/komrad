// Package git runs git and holds the operations komrade needs from it, with one convention for
// timeouts, context and error wrapping covering every caller.
package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// commandTimeout is how long any one git command is given.
const commandTimeout = 30 * time.Second

// runner starts one git command where the repository is, this machine or an engine.
type runner interface {
	command(ctx context.Context, dir string, env []string, args ...string) *exec.Cmd
}

type localRunner struct{}

func (localRunner) command(ctx context.Context, dir string, env []string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	cmd.Env = env

	return cmd
}

var local runner = localRunner{}

// Run is git's output with the trailing newline off.
func Run(ctx context.Context, dir string, args ...string) (string, error) {
	return runOn(ctx, local, dir, args...)
}

// RunBytes is git's output whole, for the callers reading a patch or a file rather than a line.
func RunBytes(ctx context.Context, dir string, args ...string) ([]byte, error) {
	return runBytesOn(ctx, local, dir, args...)
}

func runOn(ctx context.Context, on runner, dir string, args ...string) (string, error) {
	out, err := runBytesOn(ctx, on, dir, args...)

	return strings.TrimSpace(string(out)), err
}

func runBytesOn(ctx context.Context, on runner, dir string, args ...string) ([]byte, error) {
	return runInput(ctx, on, dir, nil, nil, args...)
}

// runWithEnv is git run with an environment of its own, which writing an index or a commit
// outside the agent's own repository state needs.
func runWithEnv(ctx context.Context, dir string, env []string, args ...string) ([]byte, error) {
	return runInput(ctx, local, dir, env, nil, args...)
}

// runInput is git run with something written to its standard input, for the commands that take
// their paths that way rather than as arguments.
func runInput(ctx context.Context, on runner, dir string, env []string, input []byte, args ...string) ([]byte, error) { //nolint:revive // a params struct is the follow-up
	ctx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()

	cmd := on.command(ctx, dir, env, args...)
	cmd.Stdin = bytes.NewReader(input)

	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to run git %s: %w", strings.Join(args, " "), err)
	}

	return out, nil
}

// runEnv runs a git command under env and trims what it printed.
func runEnv(ctx context.Context, dir string, env []string, args ...string) (string, error) {
	out, err := runWithEnv(ctx, dir, env, args...)

	return strings.TrimSpace(string(out)), err
}

// runAllowing is git run where one exit code carries an answer rather than a failure, so the
// output of a command that reports its finding by exiting non-zero is not thrown away.
func runAllowing(ctx context.Context, on runner, dir string, allowed int, args ...string) (string, error) { //nolint:revive // a params struct is the follow-up
	ctx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()

	cmd := on.command(ctx, dir, nil, args...)

	out, err := cmd.Output()
	trimmed := strings.TrimSpace(string(out))
	if err == nil {
		return trimmed, nil
	}

	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == allowed {
		return trimmed, nil
	}

	return "", fmt.Errorf("failed to run git %s: %w", strings.Join(args, " "), err)
}
