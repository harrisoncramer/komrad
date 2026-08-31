package cli

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"weave/internal/server"
	"weave/internal/version"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func fakeServe(t *testing.T, output string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "fake-serve")
	script := fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' \"%s\" >&2\nexit 1\n", output)
	require.NoError(t, os.WriteFile(path, []byte(script), 0o700))

	return path
}

func fakeServeEchoingArgs(t *testing.T) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "fake-serve")
	require.NoError(t, os.WriteFile(path, []byte("#!/bin/sh\necho \"$@\" >&2\nexit 1\n"), 0o700))

	return path
}

type fakeServerParams struct {
	status  int
	body    string
	stopPid int
}

func fakeServer(t *testing.T, params fakeServerParams) string {
	t.Helper()

	var testServer *httptest.Server

	mux := http.NewServeMux()
	mux.HandleFunc(server.HealthPath, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(params.status)
		_, _ = w.Write([]byte(params.body))
	})

	mux.HandleFunc(server.StopPath, func(w http.ResponseWriter, _ *http.Request) {
		_ = testServer.Listener.Close()
		w.Header().Set("Connection", "close")

		w.WriteHeader(http.StatusAccepted)
		_, _ = fmt.Fprint(w, params.stopPid)
	})

	testServer = httptest.NewServer(mux)
	t.Cleanup(testServer.Close)

	return strippedHost(t, testServer.URL)
}

func healthJSON(t *testing.T, build string) string {
	t.Helper()

	body, err := json.Marshal(server.Health{Status: "ok", Version: build})
	require.NoError(t, err)

	return string(body)
}

func deadPid(t *testing.T) int {
	t.Helper()

	cmd := exec.Command("sh", "-c", "exit 0")
	require.NoError(t, cmd.Start())

	pid := cmd.Process.Pid
	require.NoError(t, cmd.Wait())

	return pid
}

func freeAddr(t *testing.T) string {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	addr := listener.Addr().String()
	require.NoError(t, listener.Close())

	return addr
}

// TestEnsureServerReportsWhyItDied covers that a server which exits during startup is
// reported with the reason it logged, since the reason is written to a file by a process
// this one cannot see into, and without reading it back the caller has only an address that
// never answered.
func TestEnsureServerReportsWhyItDied(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "serve.log")
	self := fakeServe(t, "failed to reach db: connection refused")

	err := ensureServer(ensureServerParams{
		Addr:    freeAddr(t),
		Self:    self,
		LogPath: logPath,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to reach db: connection refused")
}

// TestEnsureServerIgnoresOlderLogLines covers that only what this server wrote is reported:
// the log is appended to across every run, so the previous run's failure sits above the
// current one and would otherwise be read as the reason.
func TestEnsureServerIgnoresOlderLogLines(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "serve.log")
	require.NoError(t, os.WriteFile(logPath, []byte("failed to migrate db: an older run\n"), 0o644))

	self := fakeServe(t, "failed to reach db: this run")

	err := ensureServer(ensureServerParams{
		Addr:    freeAddr(t),
		Self:    self,
		LogPath: logPath,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "this run")
	assert.NotContains(t, err.Error(), "an older run")
}

// TestEnsureServerWithoutAReason covers a server that dies saying nothing: the error still
// has to point at the log, since a message with no reason and nowhere to look is not worth
// reading.
func TestEnsureServerWithoutAReason(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "serve.log")
	self := fakeServe(t, "")

	err := ensureServer(ensureServerParams{
		Addr:    freeAddr(t),
		Self:    self,
		LogPath: logPath,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), logPath)
}

// TestEnsureServerWithAnUnwritableLog covers a log that cannot be opened: nothing is
// spawned, since a server whose output goes nowhere is one whose failure cannot be reported
// at all, and the reader is told which path could not be opened.
func TestEnsureServerWithAnUnwritableLog(t *testing.T) {
	blocked := filepath.Join(t.TempDir(), "not-a-directory")
	require.NoError(t, os.WriteFile(blocked, nil, 0o644))

	logPath := filepath.Join(blocked, "serve.log")

	err := ensureServer(ensureServerParams{
		Addr:    freeAddr(t),
		Self:    fakeServe(t, "should not run"),
		LogPath: logPath,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), logPath, "the path that could not be opened is named")
}

// TestEnsureServerCarriesTheConfigPath covers the spawned server being told which config file
// to read: it loads the file for itself rather than being handed what this process parsed, so
// a path left off here serves out of a different file than the one the caller named.
func TestEnsureServerCarriesTheConfigPath(t *testing.T) {
	tests := []struct {
		name        string
		configPaths []string
		want        string
	}{
		{
			name:        "no path given",
			configPaths: nil,
			want:        "",
		},
		{
			name:        "a path given",
			configPaths: []string{"/tmp/somewhere/config.toml"},
			want:        "--config /tmp/somewhere/config.toml",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logPath := filepath.Join(t.TempDir(), "serve.log")

			err := ensureServer(ensureServerParams{
				Addr:        freeAddr(t),
				Self:        fakeServeEchoingArgs(t),
				LogPath:     logPath,
				ConfigPaths: tt.configPaths,
			})

			require.Error(t, err, "the stand-in serve always fails")
			if tt.want == "" {
				assert.NotContains(t, err.Error(), "--config")

				return
			}

			assert.Contains(t, err.Error(), tt.want)
		})
	}
}

// TestEnsureServerReusesItsOwnBuild covers that nothing is spawned when this build is already
// serving, so opening a second panel does not start a second server; a database that is down
// is still this build, so it is waited for rather than replaced.
func TestEnsureServerReusesItsOwnBuild(t *testing.T) {
	tests := []struct {
		name   string
		status int
	}{
		{
			name:   "a server whose database is down",
			status: http.StatusServiceUnavailable,
		},
		{
			name:   "a server that is ready",
			status: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			addr := fakeServer(t, fakeServerParams{
				status:  tt.status,
				body:    healthJSON(t, version.Version()),
				stopPid: deadPid(t),
			})
			logPath := filepath.Join(t.TempDir(), "serve.log")

			require.NoError(t, ensureServer(ensureServerParams{
				Addr:    addr,
				Self:    fakeServe(t, "should not run"),
				LogPath: logPath,
			}))

			_, statErr := os.Stat(logPath)
			assert.True(t, os.IsNotExist(statErr), "no server was spawned, so nothing was logged")
		})
	}
}

// TestEnsureServerReplacesAnotherBuild covers the panel finding a server it cannot use, since
// talking to one means fields that come back empty and routes that answer 404.
func TestEnsureServerReplacesAnotherBuild(t *testing.T) {
	addr := fakeServer(t, fakeServerParams{
		status:  http.StatusOK,
		body:    healthJSON(t, "v0.0.0-20260701000000-0000000000ab"),
		stopPid: deadPid(t),
	})
	logPath := filepath.Join(t.TempDir(), "serve.log")

	err := ensureServer(ensureServerParams{
		Addr:    addr,
		Self:    fakeServe(t, "the replacement was spawned"),
		LogPath: logPath,
	})

	require.Error(t, err, "the stand-in serve always fails")
	assert.Contains(t, err.Error(), "the replacement was spawned",
		"the server that was there is stopped and a new one started over it")
}

// TestProbeServer covers what the panel decides on: ready is more than a bound socket, since
// the server listens before it can reach its database.
func TestProbeServer(t *testing.T) {
	tests := []struct {
		name   string
		serve  bool
		status int
		body   string
		want   serverProbe
	}{
		{
			name:   "nothing on the address",
			serve:  false,
			status: 0,
			body:   "",
			want:   serverProbe{},
		},
		{
			name:   "another build",
			serve:  true,
			status: http.StatusOK,
			body:   healthJSON(t, "v0.0.0-20260701000000-0000000000ab"),
			want: serverProbe{
				answering: true,
				ready:     true,
				version:   "v0.0.0-20260701000000-0000000000ab",
			},
		},
		{
			name:   "this build with a database it cannot reach",
			serve:  true,
			status: http.StatusServiceUnavailable,
			body:   healthJSON(t, version.Version()),
			want: serverProbe{
				answering: true,
				version:   version.Version(),
				current:   true,
			},
		},
		{
			name:   "this build",
			serve:  true,
			status: http.StatusOK,
			body:   healthJSON(t, version.Version()),
			want: serverProbe{
				answering: true,
				ready:     true,
				version:   version.Version(),
				current:   true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			addr := freeAddr(t)
			if tt.serve {
				addr = fakeServer(t, fakeServerParams{status: tt.status, body: tt.body})
			}

			assert.Equal(t, tt.want, probeServer(addr))
		})
	}
}
