package cli

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"sync"
	"time"

	"komrad/config"
	"komrad/internal/controlplane"
	"komrad/internal/logging"
	"komrad/internal/server"
	"komrad/lib/xhttp"
)

// engineAnswerTimeout bounds asking an engine's address whether anything answers there.
const engineAnswerTimeout = 2 * time.Second

// localRunner starts and stops the engines behind a control plane on this machine, running
// komrad start and komrad stop against each engine's file.
type localRunner struct {
	logger  *slog.Logger
	self    string
	engines map[string]config.ControlPlaneEngine
	client  *http.Client
	mu      sync.Mutex
	// moving is each engine a komrad start or komrad stop is still running for.
	moving map[string]controlplane.EngineState
}

type newLocalRunnerParams struct {
	Logger  *slog.Logger
	Self    string
	Engines map[string]config.ControlPlaneEngine
}

func newLocalRunner(params newLocalRunnerParams) *localRunner {
	transport := xhttp.NewTransport()
	transport.Proxy = nil

	return &localRunner{
		logger:  params.Logger,
		self:    params.Self,
		engines: params.Engines,
		client:  xhttp.NewClientOver(transport, engineAnswerTimeout),
		moving:  make(map[string]controlplane.EngineState),
	}
}

func (l *localRunner) Start(_ context.Context, engine string) error {
	return l.run(engine, "start", controlplane.EngineStarting)
}

func (l *localRunner) Stop(_ context.Context, engine string) error {
	return l.run(engine, "stop", controlplane.EngineStopping)
}

// State is starting or stopping while komrad start or komrad stop runs for the engine, and
// otherwise running when anything answers at the engine's address.
func (l *localRunner) State(ctx context.Context, engine string) (controlplane.EngineState, error) {
	l.mu.Lock()
	state, moving := l.moving[engine]
	l.mu.Unlock()
	if moving {
		return state, nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+l.engines[engine].Addr+server.HealthPath, nil)
	if err != nil {
		return "", fmt.Errorf("failed to build the request for engine %s: %w", engine, err)
	}

	resp, err := l.client.Do(req)
	if err != nil {
		return controlplane.EngineStopped, nil //nolint:nilerr // an engine nothing answers for is stopped
	}
	_ = resp.Body.Close()

	return controlplane.EngineRunning, nil
}

// run spawns komrad command for the engine, unless one is already running for it the same
// way, and refuses one going the other way.
func (l *localRunner) run(engine, command string, state controlplane.EngineState) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if moving, isMoving := l.moving[engine]; isMoving {
		if moving != state {
			return fmt.Errorf("the engine is %s, so %s it once that finishes", moving, command)
		}

		return nil
	}

	// The command outlives the request that asked for it.
	cmd := exec.Command(l.self, command, "--engine", engine) //nolint:noctx,gosec // komrad's own binary, on an engine the config names
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	cmd.Env = config.WithoutConnectionEnvironment(os.Environ())
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to run komrad %s for engine %s: %w", command, engine, err)
	}
	l.moving[engine] = state

	go func() {
		err := cmd.Wait()

		l.mu.Lock()
		delete(l.moving, engine)
		l.mu.Unlock()

		if err != nil {
			l.logger.LogAttrs(context.Background(), slog.LevelWarn, "komrad failed to start or stop an engine",
				append(logging.Err(err), slog.String(logging.EngineName, engine), slog.String(logging.LifecycleAction, command), slog.String(logging.ProcessOutput, output.String()))...)
		}
	}()

	return nil
}
