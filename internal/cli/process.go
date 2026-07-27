package cli

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
)

// The commands that talk to the service over HTTP share these, locating this binary
// so it can respawn itself and making sure something is listening before a request
// is sent.

// ensureServer makes sure a server is reachable at addr, spawning a detached serve
// process if not. It returns an error if the server never becomes reachable.
func ensureServer(addr, self string) error {
	if serverReachable(addr) {
		return nil
	}

	cmd := exec.Command(self, "serve", "--addr", addr)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if logFile, err := os.OpenFile(serveLogPath(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
		cmd.Stdout = logFile
		cmd.Stderr = logFile
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start the agent server: %w", err)
	}

	for range 50 {
		if serverReachable(addr) {
			return nil
		}

		time.Sleep(40 * time.Millisecond)
	}

	return fmt.Errorf("the agent server did not become reachable at %s", addr)
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
