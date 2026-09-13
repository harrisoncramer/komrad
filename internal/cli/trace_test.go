package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeLog(t *testing.T, lines ...string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "weave.log")
	body := ""
	for _, line := range lines {
		body += line + "\n"
	}

	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))

	return path
}

const (
	archiveStarted = `{"timestamp":"2026-08-06T22:47:32.101Z","severity_text":"INFO","body":"rpc served",` +
		`"trace.id":"trace-one","rpc.method":"ArchiveAgents","weave.agent.id":"a1"}`
	archiveFailed = `{"timestamp":"2026-08-06T22:47:32.293Z","severity_text":"ERROR","body":"rpc failed",` +
		`"trace.id":"trace-one","rpc.method":"ArchiveAgents","error.message":"pane already gone"}`
	otherRPC = `{"timestamp":"2026-08-06T22:47:33.001Z","severity_text":"INFO","body":"rpc served",` +
		`"trace.id":"trace-two","rpc.method":"ListAgents"}`
	loopPass = `{"timestamp":"2026-08-06T22:47:34.500Z","severity_text":"ERROR","body":"loop pass failed",` +
		`"trace.id":"trace-three","loop":"reconcile"}`
	untraced = `{"timestamp":"2026-08-06T22:47:30.000Z","severity_text":"INFO","body":"the service is listening"}`
)

// TestShowTrace covers the lines of one trace coming back whole and in order with no other
// trace's lines among them.
func TestShowTrace(t *testing.T) {
	path := writeLog(t, untraced, archiveStarted, otherRPC, archiveFailed, loopPass)

	var out bytes.Buffer
	require.NoError(t, showTrace(&out, path, "trace-one"))

	written := out.String()
	assert.Contains(t, written, "rpc served")
	assert.Contains(t, written, "rpc failed")
	assert.Contains(t, written, "pane already gone", "what the line carried is shown, not just its message")
	assert.Contains(t, written, "22:47:32", "the clock, since one trace happens in a moment")
	assert.NotContains(t, written, "ListAgents", "another trace's lines stay out of it")
	assert.NotContains(t, written, "loop pass failed")
	assert.NotContains(t, written, "trace.id", "the id is what was asked for, so it is not repeated on every line")
}

// TestShowTraceThatNamesNothing covers a mistyped or expired id, which has to say so rather
// than print nothing and read as a trace where nothing happened.
func TestShowTraceThatNamesNothing(t *testing.T) {
	path := writeLog(t, archiveStarted)

	var out bytes.Buffer
	err := showTrace(&out, path, "trace-nine")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "trace-nine")
	assert.Empty(t, out.String())
}

// TestListRecentTraces covers each trace being named by what it was and counted, with a line
// carrying no trace left out.
func TestListRecentTraces(t *testing.T) {
	path := writeLog(t, untraced, archiveStarted, otherRPC, archiveFailed, loopPass)

	var out bytes.Buffer
	require.NoError(t, listRecentTraces(&out, path))

	written := out.String()
	assert.Contains(t, written, "trace-one")
	assert.Contains(t, written, "ArchiveAgents")
	assert.Contains(t, written, "2 line(s)", "the two lines of one trace are counted together")
	assert.Contains(t, written, "reconcile loop", "a loop pass is named by its loop")
	assert.NotContains(t, written, "the service is listening", "a line under no trace is not a trace")
}

// TestTraceSurvivesAHalfWrittenLine covers reading a log the service is still appending to,
// where the last line can be cut in half.
func TestTraceSurvivesAHalfWrittenLine(t *testing.T) {
	path := writeLog(t, archiveStarted, `{"timestamp":"2026-08-06T22:47:35.0`)

	var out bytes.Buffer
	require.NoError(t, showTrace(&out, path, "trace-one"))

	assert.Contains(t, out.String(), "rpc served")
}

// TestTraceWithoutALog covers asking before the service has ever run.
func TestTraceWithoutALog(t *testing.T) {
	var out bytes.Buffer
	err := showTrace(&out, filepath.Join(t.TempDir(), "missing.log"), "trace-one")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing.log")
}
