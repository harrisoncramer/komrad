// Package git runs git and holds the operations weave needs from it, so that one convention for
// timeouts, context and error wrapping covers every caller rather than each hand-rolling its own.
package git

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// commandTimeout is how long any one git command is given, which is generous for the operations
// weave runs and short enough that a hung git does not hold a pane open forever.
const commandTimeout = 30 * time.Second

// Run is git's output with the trailing newline off, which is what almost every caller wants.
func Run(ctx context.Context, dir string, args ...string) (string, error) {
	out, err := RunBytes(ctx, dir, args...)

	return strings.TrimSpace(string(out)), err
}

// RunBytes is git's output whole, for the callers that read a patch or a file rather than a line.
func RunBytes(ctx context.Context, dir string, args ...string) ([]byte, error) {
	return RunWithEnv(ctx, dir, nil, args...)
}

// RunWithEnv is git run with an environment of its own, which is what writing an index or a commit
// outside the agent's own repository state needs.
func RunWithEnv(ctx context.Context, dir string, env []string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	cmd.Env = env

	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}

	return out, nil
}

// runEnv is RunWithEnv trimmed, for the snapshot commands that read back a single object name.
func runEnv(ctx context.Context, dir string, env []string, args ...string) (string, error) {
	out, err := RunWithEnv(ctx, dir, env, args...)

	return strings.TrimSpace(string(out)), err
}
