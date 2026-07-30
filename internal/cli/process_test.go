package cli

import (
	"fmt"
	"net"
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

// TestEnsureServerFindsARunningServer covers that nothing is spawned when the
// address already answers, so opening a second panel does not start a second
// server.
func TestEnsureServerFindsARunningServer(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = listener.Close() }()

	logPath := filepath.Join(t.TempDir(), "serve.log")

	require.NoError(t, ensureServer(listener.Addr().String(), fakeServe(t, "should not run"), logPath))

	_, statErr := os.Stat(logPath)
	assert.True(t, os.IsNotExist(statErr), "no server was spawned, so nothing was logged")
}

// TestServeLogTail covers how the log is folded into a reason. A dial failure spans
// several indented lines, and the error it becomes is rendered wrapped, so it is
// carried as one line rather than with the log's own breaks in it.
func TestServeLogTail(t *testing.T) {
	dialFailure := "failed to reach db: failed to connect:\n\t[::1]:5460: dial error: connection refused\n"

	tests := []struct {
		name     string
		contents string
		offset   int64
		want     string
	}{
		{
			name:     "an empty log has no reason to give",
			contents: "\n\n",
		},
		{
			name:     "a multi-line failure is folded onto one line",
			contents: dialFailure,
			want:     "failed to reach db: failed to connect: [::1]:5460: dial error: connection refused",
		},
		{
			name:     "reading past the end gives nothing rather than the older lines",
			contents: dialFailure,
			offset:   int64(len(dialFailure)),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "serve.log")
			require.NoError(t, os.WriteFile(path, []byte(tt.contents), 0o644))

			assert.Equal(t, tt.want, serveLogTail(path, tt.offset))
		})
	}
}

// TestServeLogTailIsBounded covers that a server which logged a great deal is cut
// down to its last lines, since the error is shown on a panel screen. The cut keeps
// the end, where the failure that stopped it is.
func TestServeLogTailIsBounded(t *testing.T) {
	path := filepath.Join(t.TempDir(), "serve.log")

	var log strings.Builder
	for i := range serveLogLines * 3 {
		fmt.Fprintf(&log, "line %d\n", i)
	}
	require.NoError(t, os.WriteFile(path, []byte(log.String()), 0o644))

	tail := serveLogTail(path, 0)

	assert.Len(t, strings.Split(tail, " "), serveLogLines*2, "each kept line is two words")
	assert.Contains(t, tail, fmt.Sprintf("line %d", serveLogLines*3-1), "the last line is kept")
	assert.NotContains(t, tail, "line 0 ", "the first is dropped")
}

// TestServeLogTailWithNoLog covers a log that could not be read at all, which is
// the case when the file could not be opened for the server to write to either.
func TestServeLogTailWithNoLog(t *testing.T) {
	assert.Empty(t, serveLogTail(filepath.Join(t.TempDir(), "absent.log"), 0))
}
