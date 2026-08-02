package doctor

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// commandTimeout bounds any tool doctor shells out to. Everything it runs answers at once
// or is wedged, and a doctor that hangs is worse than one reporting a timeout.
const commandTimeout = 5 * time.Second

// Env is how a check reaches the machine. Every way out is a function here rather than a
// direct call, so a test can answer for a dependency the machine running it does not have
// and the whole list stays runnable without tmux, Postgres, or a server.
type Env struct {
	// LookPath resolves a command on PATH and returns where it landed.
	LookPath func(name string) (string, error)
	// Output runs a command and returns what it wrote to stdout.
	Output func(ctx context.Context, name string, args ...string) (string, error)
}

// NewEnv is the env pointed at the machine this is running on.
func NewEnv() Env {
	return Env{
		LookPath: exec.LookPath,
		Output:   runOutput,
	}
}

// runOutput runs a command under the timeout and returns its trimmed stdout. What the
// command wrote to stderr is folded into the error, since a tool that failed usually
// explains itself there and that sentence is most of what the check has to report.
func runOutput(ctx context.Context, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()

	out, err := exec.CommandContext(ctx, name, args...).Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && len(exitErr.Stderr) > 0 {
			return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(string(exitErr.Stderr)))
		}

		return "", err
	}

	return strings.TrimSpace(string(out)), nil
}
