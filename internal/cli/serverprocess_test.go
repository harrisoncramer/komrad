//go:build integration

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

	"komrade/config"
	"komrade/internal/errui"
	"komrade/internal/server"
	"komrade/internal/version"

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

func healthWithCommit(t *testing.T, build, commit string) string {
	t.Helper()

	body, err := json.Marshal(server.Health{
		Status:  "ok",
		Version: build,
		Commit:  commit,
	})
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

// TestIntegrationEnsureServerReportsWhyItDied covers that a server which exits during startup is reported
// with the reason it logged.
func TestIntegrationEnsureServerReportsWhyItDied(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "serve.log")
	self := fakeServe(t, "failed to reach db: connection refused")

	err := ensureServer(t.Context(), ensureServerParams{
		Addr:    freeAddr(t),
		Self:    self,
		LogPath: logPath,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to reach db: connection refused")
}

// TestIntegrationEnsureServerIgnoresOlderLogLines covers that only what this server wrote is reported:
// the log is appended to across every run, so the previous run's failure sits above the
// current one and would otherwise be read as the reason.
func TestIntegrationEnsureServerIgnoresOlderLogLines(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "serve.log")
	require.NoError(t, os.WriteFile(logPath, []byte("failed to migrate db: an older run\n"), 0o644))

	self := fakeServe(t, "failed to reach db: this run")

	err := ensureServer(t.Context(), ensureServerParams{
		Addr:    freeAddr(t),
		Self:    self,
		LogPath: logPath,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "this run")
	assert.NotContains(t, err.Error(), "an older run")
}

// TestIntegrationEnsureServerWithoutAReason covers a server that dies saying nothing, where the error still
// has to point at the log.
func TestIntegrationEnsureServerWithoutAReason(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "serve.log")
	self := fakeServe(t, "")

	err := ensureServer(t.Context(), ensureServerParams{
		Addr:    freeAddr(t),
		Self:    self,
		LogPath: logPath,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), logPath)
}

// TestIntegrationEnsureServerWithAnUnwritableLog covers a log that cannot be opened, where nothing is
// spawned and the reader is told which path failed.
func TestIntegrationEnsureServerWithAnUnwritableLog(t *testing.T) {
	blocked := filepath.Join(t.TempDir(), "not-a-directory")
	require.NoError(t, os.WriteFile(blocked, nil, 0o644))

	logPath := filepath.Join(blocked, "serve.log")

	err := ensureServer(t.Context(), ensureServerParams{
		Addr:    freeAddr(t),
		Self:    fakeServe(t, "should not run"),
		LogPath: logPath,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), logPath, "the path that could not be opened is named")
}

// TestIntegrationEnsureServerCarriesTheConfigPath covers the spawned server being told which config file
// to read: it loads the file for itself rather than being handed what this process parsed, so
// a path left off here serves out of a different file than the one the caller named.
func TestIntegrationEnsureServerCarriesTheConfigPath(t *testing.T) {
	tests := []struct {
		name  string
		stack config.Stack
		want  string
	}{
		{
			name:  "no path given",
			stack: config.Stack{},
			want:  "",
		},
		{
			name:  "a path given",
			stack: config.Stack{Paths: []string{"/tmp/somewhere/config.toml"}},
			want:  "--config /tmp/somewhere/config.toml",
		},
		{
			name:  "an engine given",
			stack: config.Stack{Engine: "remote"},
			want:  "--engine remote",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logPath := filepath.Join(t.TempDir(), "serve.log")

			err := ensureServer(t.Context(), ensureServerParams{
				Addr:       freeAddr(t),
				Self:       fakeServeEchoingArgs(t),
				LogPath:    logPath,
				ConfigArgs: configArgs(tt.stack),
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

// TestIntegrationEnsureServerReusesItsOwnBuild covers that nothing is spawned when this build is already
// serving, so opening a second panel does not start a second server; a database that is down
// is still this build, so it is waited for rather than replaced.
func TestIntegrationEnsureServerReusesItsOwnBuild(t *testing.T) {
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

			require.NoError(t, ensureServer(t.Context(), ensureServerParams{
				Addr:    addr,
				Self:    fakeServe(t, "should not run"),
				LogPath: logPath,
			}))

			_, statErr := os.Stat(logPath)
			assert.True(t, os.IsNotExist(statErr), "no server was spawned, so nothing was logged")
		})
	}
}

// TestIntegrationEnsureServerReplacesAnotherBuild covers the panel finding a server it cannot use.
func TestIntegrationEnsureServerReplacesAnotherBuild(t *testing.T) {
	addr := fakeServer(t, fakeServerParams{
		status:  http.StatusOK,
		body:    healthJSON(t, "v0.0.0-20260701000000-0000000000ab"),
		stopPid: deadPid(t),
	})
	logPath := filepath.Join(t.TempDir(), "serve.log")

	err := ensureServer(t.Context(), ensureServerParams{
		Addr:    addr,
		Self:    fakeServe(t, "the replacement was spawned"),
		LogPath: logPath,
	})

	require.Error(t, err, "the stand-in serve always fails")
	assert.Contains(t, err.Error(), "the replacement was spawned",
		"the server that was there is stopped and a new one started over it")
}

// TestIntegrationProbeServer covers what the panel decides on, where ready is more than a bound socket.
func TestIntegrationProbeServer(t *testing.T) {
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

			assert.Equal(t, tt.want, probeServer(t.Context(), addr))
		})
	}
}

// TestIntegrationRequireEngineBuild covers the panel's own check against a remote engine.
func TestIntegrationRequireEngineBuild(t *testing.T) {
	const engineCommit = "4b1db3cfa7f3eb6f9f5f4de5b4ba5f5e12f5a0c1"

	tests := []struct {
		name    string
		serve   bool
		body    string
		want    string
		wantFix string
	}{
		{
			name:    "an engine on a commit this panel was not built from",
			serve:   true,
			body:    healthWithCommit(t, "v0.0.0-engine.4b1db3cf", engineCommit),
			want:    "v0.0.0-engine.4b1db3cf (4b1db3c)",
			wantFix: "build komrade from 4b1db3c",
		},
		{
			name:    "an engine deployed from a dirty tree, which has no commit to build from",
			serve:   true,
			body:    healthJSON(t, "v0.0.0-engine.4b1db3cf+dirty.131311"),
			want:    "v0.0.0-engine.4b1db3cf+dirty.131311",
			wantFix: "dist/komrade-panel",
		},
		{
			name:  "an engine this panel matches",
			serve: true,
			body:  healthJSON(t, version.Version()),
		},
		{
			name:  "nothing answering, which the engine checks report on instead",
			serve: false,
		},
		{
			name:  "something answering that is not komrade",
			serve: true,
			body:  "{}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			addr := freeAddr(t)
			if tt.serve {
				addr = fakeServer(t, fakeServerParams{status: http.StatusOK, body: tt.body})
			}

			err := requireEngineBuild(t.Context(), addr)
			if tt.want == "" {
				assert.NoError(t, err)

				return
			}

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want, "the engine's build is named")
			assert.Contains(t, err.Error(), version.Describe(version.Version(), version.Commit()),
				"this panel's build is named beside it")
			assert.Contains(t, err.Error(), tt.wantFix, "the fix named is one the reader can follow")
			require.ErrorIs(t, err, errui.ErrEngineBuild, "the panel picks its screen off this")
		})
	}
}
