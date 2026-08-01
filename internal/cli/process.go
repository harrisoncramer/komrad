package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"syscall"
	"time"

	"weave/internal/version"
)

// The spawned server is polled for this long before it is given up on, in steps
// of this size.
const (
	serveWaitAttempts = 50
	serveWaitInterval = 40 * time.Millisecond
)

// ensureServer makes sure a server from this build is reachable at addr, replacing one from
// another build and spawning one when nothing is there.
//
// The spawned process writes to a log rather than to this process's output, since it outlives
// whoever started it, so the lines it wrote before dying are read back and carried in the
// error rather than left unread in a file.
func ensureServer(addr, self, logPath string) error {
	probe := probeServer(addr)
	if probe.answering {
		if probe.current {
			return nil
		}

		if err := replaceServer(addr, probe.version); err != nil {
			return err
		}
	}

	cmd := exec.Command(self, "serve", "--addr", addr)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	// The log is the only place the spawned server can say anything, so failing to
	// open it is reported rather than shrugged at. Carrying on without it would spawn
	// a server whose output went nowhere, and the reason it died would be gone for
	// good.
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
		_ = cmd.Wait()
		close(exited)
	}()

	for range serveWaitAttempts {
		// Readiness rather than the version, since air rewrites the binary in place and the
		// build this process spawned can be a newer one than the build it is itself.
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
// server whose fields, routes and enums are not the ones it was built against. The error names
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

	var body health
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

	return client.Get("http://" + addr + HealthPath)
}

func selfPath() string {
	path, err := os.Executable()
	if err != nil {
		return "weave"
	}

	return path
}
