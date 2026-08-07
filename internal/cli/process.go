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

	"weave/internal/logging"
	"weave/internal/server"
	"weave/internal/version"
)

// The spawned server is polled for this long before it is given up on, in steps
// of this size.
const (
	serveWaitAttempts = 50
	serveWaitInterval = 40 * time.Millisecond
)

// stopWaitFor is how long the stop command waits for the process to go, longer than every
// stage of serve's own teardown put together, since the point of asking rather than
// signalling is to find out what happened.
const stopWaitFor = 30 * time.Second

// stopPollInterval is how often the address is retried while waiting.
const stopPollInterval = 100 * time.Millisecond

// ensureServerParams is what it takes to reach a server or spawn one; ConfigPaths is carried
// because the spawned process reads the config files for itself, so leaving them out would
// serve a panel started with --config out of different files than the ones it was pointed at.
type ensureServerParams struct {
	Addr        string
	Self        string
	LogPath     string
	ConfigPaths []string
}

// ensureServer makes sure a server from this build is reachable at the address, replacing one
// from another build and spawning one when nothing is there.
//
// The spawned process writes to a log rather than to this process's output, since it outlives
// whoever started it, so the lines it wrote before dying are read back and carried in the
// error rather than left unread in a file.
func ensureServer(params ensureServerParams) error {
	addr, logPath := params.Addr, params.LogPath

	probe := probeServer(addr)
	if probe.answering {
		if probe.current {
			return nil
		}

		if err := replaceServer(addr, probe.version); err != nil {
			return err
		}
	}

	// One --config per layer, in order, since the order is what decides which file wins.
	args := []string{"serve", "--addr", addr}
	for _, path := range params.ConfigPaths {
		args = append(args, "--config", path)
	}

	cmd := exec.Command(params.Self, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	// The log is the only place the spawned server can say anything, so failing to open it
	// is reported rather than shrugged at: carrying on without it would spawn a server whose
	// output went nowhere, and the reason it died would be gone for good.
	logFile, err := openServeLog(logPath)
	if err != nil {
		return err
	}
	defer func() { _ = logFile.Close() }()

	// Where the log already ends, so a failure reports what this server wrote and
	// not what a previous one did.
	logEnd, err := logFile.Seek(0, io.SeekEnd)
	if err != nil {
		return fmt.Errorf("failed to read the end of the server log %s: %w", logPath, err)
	}

	// The child gets its own copy of the descriptor, so this one is closed on the way
	// out whether the server started or not.
	cmd.Stdout = logFile
	cmd.Stderr = logFile

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start the agent server: %w", err)
	}

	// Reaped in the background, both so a server that fails immediately is noticed
	// within a poll rather than at the end of the whole wait, and so one that starts
	// cleanly does not sit as a zombie for the life of this process.
	exited := make(chan struct{})
	go func() {
		defer logging.LogAndRepanic(context.Background())

		_ = cmd.Wait() //nolint:errcheck // a non-zero exit is the signal itself, and the reason comes from the log tail
		close(exited)
	}()

	for range serveWaitAttempts {
		// Readiness rather than the version, since a rebuild replaces the binary in place and
		// the build this process spawned can be a newer one than the build it is itself.
		if probeServer(addr).ready {
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

	return fmt.Errorf("the agent server did not become ready at %s, see %s", addr, logPath)
}

// replaceServer asks the build holding addr to stop, so the panel is not left talking to a
// server whose fields, routes and enums are not the ones it was built against; the error names
// both builds, since a server that will not stop leaves the panel with nothing but this
// sentence on screen.
func replaceServer(addr, reported string) error {
	if _, err := stopServer(addr); err != nil {
		return fmt.Errorf("the server at %s is build %q and this is %s, so it has to be replaced: %w",
			addr, reported, version.Version(), err)
	}

	return nil
}

// serverProbe is what the health route said about whatever holds the address.
type serverProbe struct {
	answering bool
	// ready is a database it can reach rather than only a socket that accepts, since the
	// server binds before it can reach its store.
	ready bool
	// version is the build it reported and current whether that is this build, both empty when
	// the answer was not this service's health.
	version string
	current bool
}

// probeServer asks who is on addr, where a body that is not this build's health leaves the
// version empty and marks the server for replacing rather than reuse.
func probeServer(addr string) serverProbe {
	res, err := healthCheck(addr)
	if err != nil {
		return serverProbe{}
	}
	defer func() { _ = res.Body.Close() }()

	probe := serverProbe{answering: true, ready: res.StatusCode == http.StatusOK}

	var body server.Health
	if err := json.NewDecoder(res.Body).Decode(&body); err == nil {
		probe.version = body.Version
		probe.current = body.Version == version.Version()
	}

	return probe
}

// healthCheck asks the health route, on a timeout sized for one loopback round trip and a
// database ping.
func healthCheck(addr string) (*http.Response, error) {
	client := &http.Client{Timeout: 3 * time.Second}

	return client.Get("http://" + addr + server.HealthPath)
}

func getSelfPath() string {
	path, err := os.Executable()
	if err != nil {
		return "weave"
	}

	return path
}

// stopServer asks the server at addr to stop and waits for its process to be gone, returning
// the pid it stopped and zero when nothing was serving.
func stopServer(addr string) (int, error) {
	pid, err := requestStop(addr)
	if err != nil {
		return 0, err
	}

	if pid == 0 {
		return 0, nil
	}

	if err := awaitStopped(pid, stopWaitFor); err != nil {
		return 0, err
	}

	return pid, nil
}

// requestStop asks the server at addr to stop and returns the pid it answered with, zero
// when nothing was there to ask; a connection that is refused means the work is already done,
// a success rather than a failure to report to whoever is reloading.
func requestStop(addr string) (int, error) {
	client := &http.Client{Timeout: 5 * time.Second}

	res, err := client.Post("http://"+addr+server.StopPath, "text/plain", nil)
	if err != nil {
		var opErr *net.OpError
		if errors.As(err, &opErr) {
			return 0, nil
		}

		return 0, fmt.Errorf("failed to ask the server at %s to stop: %w", addr, err)
	}
	defer func() { _ = res.Body.Close() }()

	// A server too old to hold this route answers 404, which has to be said plainly: it is
	// still running afterwards, and reporting success would have a reload carry on and
	// respawn a panel against the process it meant to replace.
	if res.StatusCode != http.StatusAccepted {
		return 0, fmt.Errorf("the server at %s would not stop: %s", addr, res.Status)
	}

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return 0, fmt.Errorf("failed to read what the server at %s answered: %w", addr, err)
	}

	pid, err := strconv.Atoi(strings.TrimSpace(string(body)))
	if err != nil {
		return 0, fmt.Errorf("the server at %s did not say which process it is: %w", addr, err)
	}

	return pid, nil
}

// awaitStopped waits for the process to be gone; it watches the process rather than the
// address, since the listener closes at the top of the teardown, so an address that has
// stopped answering says only that the shutdown has begun.
func awaitStopped(pid int, within time.Duration) error {
	deadline := time.Now().Add(within)

	for time.Now().Before(deadline) {
		if !isProcessAlive(pid) {
			return nil
		}

		time.Sleep(stopPollInterval)
	}

	return fmt.Errorf("the server was asked to stop and process %d is still running after %s", pid, within)
}

// isProcessAlive reports whether a process still exists: signal zero is delivered to nothing
// and only reports whether it could have been.
func isProcessAlive(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}

	return process.Signal(syscall.Signal(0)) == nil
}
