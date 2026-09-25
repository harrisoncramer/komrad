package logread

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeLog(t *testing.T, lines ...string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "komrad.log")
	body := ""
	for _, line := range lines {
		body += line + "\n"
	}

	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))

	return path
}

const (
	archiveStarted = `{"timestamp":"2026-08-06T22:47:32.101Z","severity_text":"INFO","body":"rpc served",` +
		`"trace.id":"trace-one","rpc.method":"ArchiveAgents","komrad.agent.id":"a1"}`
	archiveFailed = `{"timestamp":"2026-08-06T22:47:32.293Z","severity_text":"ERROR","body":"rpc failed",` +
		`"trace.id":"trace-one","rpc.method":"ArchiveAgents","error.message":"pane already gone"}`
	otherRPC = `{"timestamp":"2026-08-06T22:47:33.001Z","severity_text":"INFO","body":"rpc served",` +
		`"trace.id":"trace-two","rpc.method":"ListAgents"}`
	loopPass = `{"timestamp":"2026-08-06T22:47:34.500Z","severity_text":"ERROR","body":"loop pass failed",` +
		`"trace.id":"trace-three","loop":"reconcile"}`
	untraced = `{"timestamp":"2026-08-06T22:47:30.000Z","severity_text":"INFO","body":"the service is listening"}`
)

// TestLines covers the lines of one trace coming back whole and in order with no other
// trace's lines among them.
func TestLines(t *testing.T) {
	t.Parallel()

	reader := New(writeLog(t, untraced, archiveStarted, otherRPC, archiveFailed, loopPass))

	lines, err := reader.Lines("trace-one")
	require.NoError(t, err)
	require.Len(t, lines, 2)
	assert.Equal(t, "rpc served", lines[0].Message)
	assert.Equal(t, "rpc failed", lines[1].Message)
	assert.Equal(t, "ERROR", lines[1].Level)
	assert.Equal(t, "pane already gone", lines[1].Fields["error.message"], "what the line carried comes with it")
	assert.Equal(t, "2026-08-06T22:47:32.101Z", lines[0].Time)
}

// TestLinesUnderNoSuchTrace covers a mistyped or expired id, which has to say so rather than
// answer an empty trace where nothing happened.
func TestLinesUnderNoSuchTrace(t *testing.T) {
	t.Parallel()

	_, err := New(writeLog(t, archiveStarted)).Lines("trace-nine")
	require.ErrorIs(t, err, ErrNoSuchTrace)
	assert.Contains(t, err.Error(), "trace-nine")
}

// TestRecentTraces covers each trace being named by what it was and counted, oldest first, with a
// line carrying no trace left out and the limit cutting the oldest.
func TestRecentTraces(t *testing.T) {
	t.Parallel()

	reader := New(writeLog(t, untraced, archiveStarted, otherRPC, archiveFailed, loopPass))

	recent, err := reader.RecentTraces(0)
	require.NoError(t, err)
	require.Len(t, recent, 3)
	assert.Equal(t, Summary{
		Trace:     "trace-one",
		StartedAt: "2026-08-06T22:47:32.101Z",
		What:      "ArchiveAgents",
		Lines:     2,
	}, recent[0])
	assert.Equal(t, "reconcile loop", recent[2].What, "a loop pass is named by its loop")

	last, err := reader.RecentTraces(1)
	require.NoError(t, err)
	require.Len(t, last, 1)
	assert.Equal(t, "trace-three", last[0].Trace, "the limit keeps the newest")
}

// TestLinesSurviveAHalfWrittenLine covers reading a log the service is still appending to,
// where the last line can be cut in half.
func TestLinesSurviveAHalfWrittenLine(t *testing.T) {
	t.Parallel()

	lines, err := New(writeLog(t, archiveStarted, `{"timestamp":"2026-08-06T22:47:35.0`)).Lines("trace-one")
	require.NoError(t, err)
	assert.Len(t, lines, 1)
}

// TestLinesWithoutALog covers asking before the service has ever written anything.
func TestLinesWithoutALog(t *testing.T) {
	t.Parallel()

	_, err := New(filepath.Join(t.TempDir(), "missing.log")).Lines("trace-one")
	require.ErrorIs(t, err, ErrNoLog)
	assert.Contains(t, err.Error(), "missing.log")
}
