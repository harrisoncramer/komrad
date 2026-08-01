package cli

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

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

// TestEnsureServerFindsARunningServer covers that nothing is spawned when the address is
// already served, so opening a second panel does not start a second server.
//
// A server that answers anything at all counts, including one too old to have a health
// route. It holds the port either way, and spawning over it achieves nothing but a process
// that fails to bind.
func TestEnsureServerFindsARunningServer(t *testing.T) {
	tests := []struct {
		name   string
		status int
	}{
		{name: "a server that is ready", status: http.StatusOK},
		{name: "a server whose database is down", status: http.StatusServiceUnavailable},
		{name: "a server from a build that has no health route", status: http.StatusNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
			}))
			defer server.Close()

			addr := strings.TrimPrefix(server.URL, "http://")
			logPath := filepath.Join(t.TempDir(), "serve.log")

			require.NoError(t, ensureServer(addr, fakeServe(t, "should not run"), logPath))

			_, statErr := os.Stat(logPath)
			assert.True(t, os.IsNotExist(statErr), "no server was spawned, so nothing was logged")
		})
	}
}

// TestServerReadyNeedsMoreThanAListener covers what the wait after a spawn waits for. A
// bound socket is not a working service: the server listens before it can reach its
// database, and a panel that started on the first successful dial came up against a
// service that failed every call it made.
func TestServerReadyNeedsMoreThanAListener(t *testing.T) {
	unreachable := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("database unreachable: connection refused"))
	}))
	defer unreachable.Close()

	ready := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer ready.Close()

	assert.False(t, serverReady(strings.TrimPrefix(unreachable.URL, "http://")))
	assert.True(t, serverReady(strings.TrimPrefix(ready.URL, "http://")))
}
