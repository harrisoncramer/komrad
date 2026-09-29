package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"

	"komrad/internal/logging"
	"komrad/internal/server"
	"komrad/internal/version"
	"komrad/lib/clock"
	"komrad/lib/xhttp"
)

const (
	serveWaitFor      = 15 * time.Second
	serveWaitInterval = 40 * time.Millisecond
)

const healthTimeout = 3 * time.Second

const stopRequestTimeout = 5 * time.Second

const stopWaitFor = 30 * time.Second

const stopPollInterval = 100 * time.Millisecond

type ensureServerParams struct {
	Addr       string
	Self       string
	LogPath    string
	ConfigArgs []string
}

func ensureServer(ctx context.Context, params ensureServerParams) error {
	addr, logPath := params.Addr, params.LogPath

	probe := probeServer(ctx, addr)
	if probe.answering {
		if probe.current {
			return nil
		}

		if err := replaceServer(ctx, addr, version.Describe(probe.version, probe.commit)); err != nil {
			return err
		}
	}

	args := append([]string{"serve", "--addr", addr}, params.ConfigArgs...)

	// The server outlives the command that spawned it, so it is not bound to this context.
	cmd := exec.Command(params.Self, args...) //nolint:noctx // a cancelled start must not kill the server it started
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	logFile, err := openLogFile(logPath)
	if err != nil {
		return err
	}
	defer func() { _ = logFile.Close() }()

	logEnd, err := logFile.Seek(0, io.SeekEnd)
	if err != nil {
		return fmt.Errorf("failed to read the end of the server log %s: %w", logPath, err)
	}

	cmd.Stdout = logFile
	cmd.Stderr = logFile

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start the agent server: %w", err)
	}

	exited := make(chan struct{})
	go func() {
		defer logging.LogAndRepanic(context.Background())

		_ = cmd.Wait() //nolint:errcheck // a non-zero exit is the signal itself, and the reason comes from the log tail
		close(exited)
	}()

	deadline := time.Now().Add(serveWaitFor)
	for time.Now().Before(deadline) {
		if probeServer(ctx, addr).ready {
			return nil
		}

		select {
		case <-exited:
			if reason := serveLogTail(logPath, logEnd); reason != "" {
				return fmt.Errorf("the agent server stopped during startup: %s", reason)
			}

			return fmt.Errorf("the agent server stopped during startup without saying why, see %s", logPath)
		case <-time.After(serveWaitInterval):
		}
	}

	return fmt.Errorf("the agent server did not become ready at %s within %s, see %s", addr, serveWaitFor, logPath)
}

func replaceServer(ctx context.Context, addr, reported string) error {
	if _, err := stopServer(ctx, addr); err != nil {
		return fmt.Errorf("failed to replace the server at %s, which is build %s where this is %s: %w",
			addr, reported, version.Describe(version.Version(), version.Commit()), err)
	}

	return nil
}

type serverProbe struct {
	answering bool
	ready     bool
	version   string
	commit    string
	revision  string
	current   bool
}

func probeServer(ctx context.Context, addr string) serverProbe {
	res, err := healthCheck(ctx, addr)
	if err != nil {
		return serverProbe{}
	}
	defer func() { _ = res.Body.Close() }()

	probe := serverProbe{answering: true, ready: res.StatusCode == http.StatusOK}

	var body server.Health
	if err := json.NewDecoder(res.Body).Decode(&body); err == nil {
		probe.version = body.Version
		probe.commit = body.Commit
		probe.revision = body.Revision
		probe.current = isSameBuild(body, version.Version(), version.Commit())
	}

	return probe
}

func healthCheck(ctx context.Context, addr string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+addr+server.HealthPath, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to build a health request for %s: %w", addr, err)
	}

	client := xhttp.NewClient(healthTimeout)

	return client.Do(req)
}

func getSelfPath() string {
	path, err := os.Executable()
	if err != nil {
		return "komrad"
	}

	return path
}

func stopServer(ctx context.Context, addr string) (int, error) {
	pid, err := requestStop(ctx, addr)
	if err != nil {
		return 0, err
	}

	if pid == 0 {
		return 0, nil
	}

	if err := awaitStopped(ctx, pid, stopWaitFor); err != nil {
		return 0, err
	}

	return pid, nil
}

func requestStop(ctx context.Context, addr string) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+addr+server.StopPath, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to build a stop request for %s: %w", addr, err)
	}
	req.Header.Set("Content-Type", "text/plain")

	client := xhttp.NewClient(stopRequestTimeout)

	res, err := client.Do(req)
	if err != nil {
		var opErr *net.OpError
		if errors.As(err, &opErr) {
			return 0, nil
		}

		return 0, fmt.Errorf("failed to ask the server at %s to stop: %w", addr, err)
	}
	defer func() { _ = res.Body.Close() }()

	if res.StatusCode != http.StatusAccepted {
		return 0, fmt.Errorf("the server at %s would not stop: %s", addr, res.Status)
	}

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return 0, fmt.Errorf("failed to read what the server at %s answered: %w", addr, err)
	}

	pid, err := strconv.Atoi(strings.TrimSpace(string(body)))
	if err != nil {
		return 0, fmt.Errorf("failed to read which process the server at %s is: %w", addr, err)
	}

	return pid, nil
}

func awaitStopped(ctx context.Context, pid int, within time.Duration) error {
	deadline := time.Now().Add(within)

	for time.Now().Before(deadline) {
		if !isProcessAlive(pid) {
			return nil
		}

		clock.Sleep(ctx, stopPollInterval)
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("failed to wait for process %d to exit: %w", pid, err)
		}
	}

	return fmt.Errorf("the server was asked to stop and process %d is still running after %s", pid, within)
}

func isProcessAlive(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}

	return process.Signal(syscall.Signal(0)) == nil
}
