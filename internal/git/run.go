// Package git runs git and holds the operations weave needs from it, so that one convention for
// timeouts, context and error wrapping covers every caller rather than each hand-rolling its own.
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

// Run is git's output with the trailing newline off.
func Run(ctx context.Context, dir string, args ...string) (string, error) {
	out, err := RunBytes(ctx, dir, args...)

	return strings.TrimSpace(string(out)), err
}

// RunBytes is git's output whole, for the callers reading a patch or a file rather than a line.
func RunBytes(ctx context.Context, dir string, args ...string) ([]byte, error) {
	return runWithEnv(ctx, dir, nil, args...)
}

// runWithEnv is git run with an environment of its own, which writing an index or a commit
// outside the agent's own repository state needs.
func runWithEnv(ctx context.Context, dir string, env []string, args ...string) ([]byte, error) {
	return runInput(ctx, dir, env, nil, args...)
}

// runInput is git run with something written to its standard input, for the commands that take
// their paths that way rather than as arguments.
func runInput(ctx context.Context, dir string, env []string, input []byte, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	cmd.Env = env
	cmd.Stdin = bytes.NewReader(input)

	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}

	return out, nil
}

// runEnv is runWithEnv trimmed, for the snapshot commands that read back a single object name.
func runEnv(ctx context.Context, dir string, env []string, args ...string) (string, error) {
	out, err := runWithEnv(ctx, dir, env, args...)

	return strings.TrimSpace(string(out)), err
}

// runAllowing is git run where one exit code carries an answer rather than a failure, so the
// output of a command that reports its finding by exiting non-zero is not thrown away.
func runAllowing(ctx context.Context, dir string, allowed int, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)

	out, err := cmd.Output()
	trimmed := strings.TrimSpace(string(out))
	if err == nil {
		return trimmed, nil
	}

	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == allowed {
		return trimmed, nil
	}

	return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
}
