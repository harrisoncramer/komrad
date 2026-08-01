package claude

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"weave/internal/harness"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeStatus puts one of Claude's status files in dir, named for the pid the way
// Claude names them.
func writeStatus(t *testing.T, dir string, file sessionFile) {
	t.Helper()

	data, err := json.Marshal(file)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "1234.json"), data, 0o600))
}

// newTestHarness builds a harness over dir with every pid alive and no scan reused, so
// a test sees each write on the next read.
func newTestHarness(t *testing.T, dir string) *Harness {
	t.Helper()

	h := New(Options{ProjectsDir: t.TempDir(), SessionsDir: dir})
	h.sessions.alive = func(int) bool { return true }
	h.sessions.ttl = 0

	return h
}

// TestReadActivity covers the verdict each of Claude's own statuses settles on. All of
// them are reported rather than inferred, since the CLI is stating them about itself.
func TestReadActivity(t *testing.T) {
	tests := []struct {
		name   string
		status string
		want   harness.Activity
	}{
		{
			name:   "busy is working",
			status: statusBusy,
			want:   harness.ActivityWorking,
		},
		{
			name:   "waiting is a prompt the user has to answer",
			status: statusWaiting,
			want:   harness.ActivityWaiting,
		},
		{
			name:   "idle is the ordinary finished agent",
			status: statusIdle,
			want:   harness.ActivityIdle,
		},
		{
			name:   "a shell left open under the session is the user's turn",
			status: statusShell,
			want:   harness.ActivityIdle,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			writeStatus(t, dir, sessionFile{PID: 1234, SessionID: "sess", Status: tt.status})

			reading := newTestHarness(t, dir).ReadActivity(harness.Observation{SessionID: "sess"})
			assert.Equal(t, harness.Reading{Activity: tt.want, Reported: true}, reading)
		})
	}
}

// TestReadActivityWithNoLiveSession covers a conversation no running process holds,
// which is what a finished agent, a crashed one, and a launch that has not written its
// first status yet all look like. Idle is the answer, left unreported so the
// reconciler's debounce rules out the launch case before demoting anything.
func TestReadActivityWithNoLiveSession(t *testing.T) {
	tests := []struct {
		name string
		// file is the status file to write, or nil to leave the directory empty.
		file *sessionFile
		// alive answers whether the pid in that file names a running process.
		alive bool
	}{
		{
			name:  "no status file at all, which is the moment after a launch",
			alive: true,
		},
		{
			name:  "a file for somebody else's conversation",
			file:  &sessionFile{PID: 1234, SessionID: "other", Status: statusBusy},
			alive: true,
		},
		{
			// Only a graceful exit removes the file, so a process that was killed
			// leaves its last status behind to be believed.
			name: "a status left behind by a process that is gone",
			file: &sessionFile{PID: 1234, SessionID: "sess", Status: statusBusy},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			if tt.file != nil {
				writeStatus(t, dir, *tt.file)
			}

			h := newTestHarness(t, dir)
			h.sessions.alive = func(int) bool { return tt.alive }

			reading := h.ReadActivity(harness.Observation{SessionID: "sess"})
			assert.Equal(t, harness.Reading{Activity: harness.ActivityIdle}, reading)
		})
	}
}

// TestReadActivityHoldsTheState covers the two cases where something is plainly running
// but this build cannot say what it is doing. Calling either of them idle would report
// live work as finished, so the agent keeps the state it already holds.
func TestReadActivityHoldsTheState(t *testing.T) {
	tests := []struct {
		name      string
		status    string
		sessionID string
	}{
		{
			name:      "an agent with no conversation recorded yet",
			status:    statusBusy,
			sessionID: "",
		},
		{
			name:      "a status this build does not know",
			status:    "compacting",
			sessionID: "sess",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			writeStatus(t, dir, sessionFile{PID: 1234, SessionID: "sess", Status: tt.status})

			reading := newTestHarness(t, dir).ReadActivity(harness.Observation{SessionID: tt.sessionID})
			assert.Equal(t, harness.Reading{Activity: harness.ActivityUnknown}, reading)
		})
	}
}

// TestReadActivitySurvivesJunk covers a directory holding things Claude did not write
// and a directory that is not there. Neither can be an error, since the panel reads a
// location it does not own.
func TestReadActivitySurvivesJunk(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("ignored"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "9.json"), []byte("{not json"), 0o600))
	require.NoError(t, os.Mkdir(filepath.Join(dir, "nested.json"), 0o750))

	for _, sessionsDir := range []string{dir, filepath.Join(dir, "gone")} {
		reading := newTestHarness(t, sessionsDir).ReadActivity(harness.Observation{SessionID: "sess"})
		assert.Equal(t, harness.Reading{Activity: harness.ActivityIdle}, reading)
	}
}

// TestSessionScanIsReusedWithinTheTTL pins the reason the scan is cached at all. A pass
// asks about every agent in turn and the directory holds a file per agent, so a scan per
// agent would cost a read of every session for every session.
func TestSessionScanIsReusedWithinTheTTL(t *testing.T) {
	dir := t.TempDir()
	writeStatus(t, dir, sessionFile{PID: 1234, SessionID: "sess", Status: statusBusy})

	h := New(Options{ProjectsDir: t.TempDir(), SessionsDir: dir})
	h.sessions.alive = func(int) bool { return true }

	status, ok := h.sessions.status("sess")
	require.True(t, ok)
	require.Equal(t, statusBusy, status)

	writeStatus(t, dir, sessionFile{PID: 1234, SessionID: "sess", Status: statusIdle})

	status, _ = h.sessions.status("sess")
	assert.Equal(t, statusBusy, status, "the second read of the pass reuses the first one's scan")

	h.sessions.ttl = 0
	status, _ = h.sessions.status("sess")
	assert.Equal(t, statusIdle, status, "the next pass reads the directory again")
}

// TestProcessAlive covers the liveness check the stale files are dropped by. The pid of
// the running test is the only one guaranteed to be alive.
func TestProcessAlive(t *testing.T) {
	assert.True(t, processAlive(os.Getpid()))
	assert.False(t, processAlive(0), "a pid nothing can be signalled at is not a process")
	assert.False(t, processAlive(-1))
}
