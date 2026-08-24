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

const (
	serveWaitAttempts = 50
	serveWaitInterval = 40 * time.Millisecond
)

const stopWaitFor = 30 * time.Second

const stopPollInterval = 100 * time.Millisecond

type ensureServerParams struct {
	Addr        string
	Self        string
	LogPath     string
	ConfigPaths []string
}

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

	args := []string{"serve", "--addr", addr}
	for _, path := range params.ConfigPaths {
		args = append(args, "--config", path)
	}

	cmd := exec.Command(params.Self, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	logFile, err := openServeLog(logPath)
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

	for range serveWaitAttempts {
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

func replaceServer(addr, reported string) error {
	if _, err := stopServer(addr); err != nil {
		return fmt.Errorf("the server at %s is build %q and this is %s, so it has to be replaced: %w",
			addr, reported, version.Version(), err)
	}

	return nil
}

type serverProbe struct {
	answering bool

	ready bool

	version string
	current bool
}

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

func isProcessAlive(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}

	return process.Signal(syscall.Signal(0)) == nil
}
