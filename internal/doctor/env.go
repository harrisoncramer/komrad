package doctor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"weave/internal/appconfig"
)

// commandTimeout bounds any tool doctor shells out to. Everything it runs answers at once
// or is wedged, and a doctor that hangs is worse than one reporting a timeout.
const commandTimeout = 5 * time.Second

// Env is how a check reaches the machine. Every way out is a function here rather than a
// direct call, so a test can answer for a dependency the machine running it does not have
// and the whole list stays runnable without tmux, Postgres, or a server.
type Env struct {
	Settings

	// LookPath resolves a command on PATH and returns where it landed.
	LookPath func(name string) (string, error)
	// Output runs a command and returns what it wrote to stdout.
	Output func(ctx context.Context, name string, args ...string) (string, error)
	// LoadConfig parses the user's config file, the same call the root command makes,
	// so doctor and the panel's error screen cannot disagree about it.
	LoadConfig func() (*appconfig.Config, error)
	// ConfigExists reports whether the config file is actually there, which Load does
	// not say: a missing one is not an error and comes back as the defaults.
	ConfigExists func() bool
	// Connect opens a handle to the writable database that cannot write. Opening the
	// store the ordinary way migrates, which a diagnostic must not do.
	Connect func(ctx context.Context, url string) (Probe, error)
	// ConnectReadOnly opens the least-privilege login the same way the service does, so
	// what passes here is what gets the service past the same call.
	ConnectReadOnly func(ctx context.Context, url string) error
	// ProbeServer asks the health route who holds the listen address. It never reports
	// an error, since every way of not answering is itself the answer.
	ProbeServer func(ctx context.Context, addr string) ServerStatus
}

// Probe is the little a check needs from a Postgres that is already open.
type Probe interface {
	// Pending lists the migrations this build ships that the database has not applied.
	Pending(ctx context.Context) ([]string, error)
	Close()
}

// HarnessInfo is one agent CLI this build can run: the id an agent is recorded under, and
// the command a pane's shell has to resolve to start it.
type HarnessInfo struct {
	ID     string
	Binary string
}

// Settings is what this build would run with, gathered before doctor is reached so no
// check reads a file or an environment for itself.
type Settings struct {
	// TmuxSocket names weave's own tmux server, which is where agent panes are opened
	// and so the server whose options decide whether a launch parses.
	TmuxSocket string
	// InsideTmux is the user's tmux server as tmux names it in TMUX, empty when this is
	// not running under tmux. It is a second server to ask when weave's is not up.
	InsideTmux string
	// PanelSession is the tmux session the panel runs in, which is also the session
	// every agent gets a window in. Whether it is up decides whether stopping the
	// server is a fix or a way to strand somebody.
	PanelSession string
	// ConfigPath is the config file that would be read, named whether or not it exists.
	ConfigPath string
	// Harnesses is every agent CLI this build can run.
	Harnesses []HarnessInfo
	// DatabaseURL is the writable login everything weave stores lives behind, and
	// ReadOnlyDatabaseURL is the least-privilege one the MCP query tools use.
	DatabaseURL         string
	ReadOnlyDatabaseURL string
	// Addr is where the service listens, and Version is the build doing the checking,
	// which is what a server on that address has to match to be reused.
	Addr    string
	Version string
}

// NewEnv is the env pointed at the machine this is running on.
func NewEnv(settings Settings) Env {
	return Env{
		Settings:   settings,
		LookPath:   exec.LookPath,
		Output:     runOutput,
		LoadConfig: func() (*appconfig.Config, error) { return appconfig.Load(settings.ConfigPath) },
		ConfigExists: func() bool {
			_, err := os.Stat(settings.ConfigPath)

			return err == nil
		},
		Connect:         connectProbe,
		ConnectReadOnly: connectReadOnly,
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
