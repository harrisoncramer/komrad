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

	"weave/internal/version"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeServe writes a script that stands in for the serve subcommand, logging what it
// is given and then failing. It is used in place of the real binary so the tests
// never need a database, and it always fails because a server that starts cleanly is
// not what any of these cases is about.
func fakeServe(t *testing.T, output string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "fake-serve")
	script := fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' \"%s\" >&2\nexit 1\n", output)
	require.NoError(t, os.WriteFile(path, []byte(script), 0o700))

	return path
}

// fakeServerParams is what a stand-in server answers with, where status and body are the
// health route and stopPid is what its stop route reports having stopped.
type fakeServerParams struct {
	status  int
	body    string
	stopPid int
}

// fakeServer stands in for whatever holds the address, answering the health route with a body
// of the test's choosing and the stop route the way serve does.
func fakeServer(t *testing.T, params fakeServerParams) string {
	t.Helper()

	var server *httptest.Server

	mux := http.NewServeMux()
	mux.HandleFunc(HealthPath, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(params.status)
		_, _ = w.Write([]byte(params.body))
	})

	mux.HandleFunc(StopPath, func(w http.ResponseWriter, _ *http.Request) {
		// The stand-in stops by dropping its listener and this connection with it, since the
		// caller's pool would otherwise keep answering over a socket already accepted.
		_ = server.Listener.Close()
		w.Header().Set("Connection", "close")

		w.WriteHeader(http.StatusAccepted)
		_, _ = fmt.Fprint(w, params.stopPid)
	})

	server = httptest.NewServer(mux)
	t.Cleanup(server.Close)

	return strippedHost(t, server.URL)
}

// healthJSON is a health body reporting the given build, so a test can serve one that looks
// like this build and one that does not.
func healthJSON(t *testing.T, build string) string {
	t.Helper()

	body, err := json.Marshal(health{Status: "ok", Version: build})
	require.NoError(t, err)

	return string(body)
}

// deadPid is the pid of a process that has already exited, so the wait after a stop ends on
// finding it gone rather than on its own timeout.
func deadPid(t *testing.T) int {
	t.Helper()

	cmd := exec.Command("sh", "-c", "exit 0")
	require.NoError(t, cmd.Start())

	pid := cmd.Process.Pid
	require.NoError(t, cmd.Wait())

	return pid
}

// freeAddr returns an address nothing is listening on, so ensureServer takes its
// spawning path rather than finding a server already up.
func freeAddr(t *testing.T) string {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	addr := listener.Addr().String()
	require.NoError(t, listener.Close())

	return addr
}

// TestEnsureServerReportsWhyItDied covers that a server which exits during startup
// is reported with the reason it logged. The reason is written to a file by a
// process this one cannot see into, so without reading it back the caller has only
// the address that never answered, which says nothing about an unreachable
// database.
func TestEnsureServerReportsWhyItDied(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "serve.log")
	self := fakeServe(t, "failed to reach db: connection refused")

	err := ensureServer(freeAddr(t), self, logPath)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to reach db: connection refused")
}

// TestEnsureServerIgnoresOlderLogLines covers that only what this server wrote is
// reported. The log is appended to across every run, so the previous run's failure
// is sitting above the current one and would otherwise be read as the reason.
func TestEnsureServerIgnoresOlderLogLines(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "serve.log")
	require.NoError(t, os.WriteFile(logPath, []byte("failed to migrate db: an older run\n"), 0o644))

	self := fakeServe(t, "failed to reach db: this run")

	err := ensureServer(freeAddr(t), self, logPath)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "this run")
	assert.NotContains(t, err.Error(), "an older run")
}

// TestEnsureServerWithoutAReason covers a server that dies saying nothing. The
// error still has to point at the log, since a message with no reason in it and no
// idea where to look for one is not worth reading.
func TestEnsureServerWithoutAReason(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "serve.log")
	self := fakeServe(t, "")

	err := ensureServer(freeAddr(t), self, logPath)

	require.Error(t, err)
	assert.Contains(t, err.Error(), logPath)
}

// TestEnsureServerWithAnUnwritableLog covers a log that cannot be opened. Nothing
// is spawned, since a server whose output goes nowhere is one whose failure cannot
// be reported at all, and the reader is told which path could not be opened.
func TestEnsureServerWithAnUnwritableLog(t *testing.T) {
	// A regular file where a directory is expected, so making the log's parent fails.
	blocked := filepath.Join(t.TempDir(), "not-a-directory")
	require.NoError(t, os.WriteFile(blocked, nil, 0o644))

	logPath := filepath.Join(blocked, "serve.log")

	err := ensureServer(freeAddr(t), fakeServe(t, "should not run"), logPath)

	require.Error(t, err)
	assert.Contains(t, err.Error(), logPath, "the path that could not be opened is named")
}

// TestEnsureServerReusesItsOwnBuild covers that nothing is spawned when this build is already
// serving, so opening a second panel does not start a second server. A database that is down
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

			require.NoError(t, ensureServer(addr, fakeServe(t, "should not run"), logPath))

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

	err := ensureServer(addr, fakeServe(t, "the replacement was spawned"), logPath)

	require.Error(t, err, "the stand-in serve always fails")
	assert.Contains(t, err.Error(), "the replacement was spawned",
		"the server that was there is stopped and a new one started over it")
}

// TestProbeServer covers what the panel decides on. Ready is more than a bound socket, since
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
			want:   serverProbe{answering: true, ready: true, version: "v0.0.0-20260701000000-0000000000ab"},
		},
		{
			name:   "this build with a database it cannot reach",
			serve:  true,
			status: http.StatusServiceUnavailable,
			body:   healthJSON(t, version.Version()),
			want:   serverProbe{answering: true, version: version.Version(), current: true},
		},
		{
			name:   "this build",
			serve:  true,
			status: http.StatusOK,
			body:   healthJSON(t, version.Version()),
			want:   serverProbe{answering: true, ready: true, version: version.Version(), current: true},
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
