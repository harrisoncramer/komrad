package cli

import (
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
	if serverReachable(addr) {
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
		if serverReachable(addr) {
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

	return fmt.Errorf("the agent server did not become reachable at %s, see %s", addr, logPath)
}

// openServeLog opens the log the spawned server writes to, creating the directory
// it lives in. The server outlives the process that started it, so it cannot write
// to that process's output and this file is where everything it has to say goes.
func openServeLog(path string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("failed to create the directory for the server log %s: %w", path, err)
	}

	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("failed to open the server log %s: %w", path, err)
	}

	return file, nil
}

// serveLogLines is how much of the tail is worth carrying. A failed dial spends
// several lines on the addresses it tried, and the line naming what failed sits
// above them.
const serveLogLines = 12

// serveLogTail reads what the server logged from offset on, as a single line. The
// error it builds is rendered wrapped, so the newlines the log uses would only put
// breaks in it that the screen has to fold again.
func serveLogTail(path string, offset int64) string {
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer func() { _ = file.Close() }()

	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		return ""
	}

	written, err := io.ReadAll(file)
	if err != nil {
		return ""
	}

	trimmed := strings.TrimSpace(string(written))
	if trimmed == "" {
		return ""
	}

	lines := strings.Split(trimmed, "\n")
	if len(lines) > serveLogLines {
		lines = lines[len(lines)-serveLogLines:]
	}

	return strings.Join(strings.Fields(strings.Join(lines, " ")), " ")
}

func serverReachable(addr string) bool {
	conn, err := net.DialTimeout("tcp", addr, 150*time.Millisecond)
	if err != nil {
		return false
	}

	_ = conn.Close()
	return true
}

func serveLogPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "claude-agents-serve.log"
	}

	return filepath.Join(home, ".claude", "claude-agents-serve.log")
}

func selfPath() string {
	path, err := os.Executable()
	if err != nil {
		return "claude-agents"
	}

	return path
}
