package cli

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// The commands that talk to the service over HTTP share these, locating this binary
// so it can respawn itself and making sure something is listening before a request
// is sent.

// The spawned server is polled for this long before it is given up on, in steps
// of this size.
const (
	serveWaitAttempts = 50
	serveWaitInterval = 40 * time.Millisecond
)

// ensureServer makes sure a server is reachable at addr, spawning a detached serve
// process if not. It returns an error if the server never becomes reachable.
//
// The spawned process writes to a log rather than to this process's output, since
// it outlives whoever started it. So when it dies during startup its reason is on
// disk and nowhere the caller can see, and reporting only that the address never
// answered names the symptom while the cause sits unread in a file. The lines it
// wrote before exiting are read back and carried in the error instead.
func ensureServer(addr, self, logPath string) error {
	if serverAnswering(addr) {
		return nil
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
		if serverReady(addr) {
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

// serverAnswering reports whether anything is serving at addr, whatever it answers. A
// server already up is not to be spawned over, and one running an older build that has no
// health route still holds the port: treating its 404 as nobody there would start a second
// process whose only outcome is failing to bind.
func serverAnswering(addr string) bool {
	res, err := healthCheck(addr)
	if err != nil {
		return false
	}
	defer func() { _ = res.Body.Close() }()

	return true
}

// serverReady reports whether the service can actually serve, which is what the wait after
// a spawn is waiting for. A listening socket is not the same thing: the server binds before
// it can reach its database, so a panel that started on the first successful dial came up
// against a service that failed every call it made.
func serverReady(addr string) bool {
	res, err := healthCheck(addr)
	if err != nil {
		return false
	}
	defer func() { _ = res.Body.Close() }()

	return res.StatusCode == http.StatusOK
}

// healthCheck asks the service whether it is ready. The timeout is the dial timeout this
// replaced, since the request is one round trip to loopback and a database ping.
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
