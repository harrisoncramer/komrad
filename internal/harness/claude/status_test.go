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

// newTestHarness builds a harness over dir with every pid alive and the scan reused for
// nothing, so a test sees each write on the next read.
func newTestHarness(t *testing.T, dir string) *Harness {
	t.Helper()

	h := New(Options{ProjectsDir: t.TempDir(), SessionsDir: dir})
	h.sessions.alive = func(int) bool { return true }
	h.sessions.ttl = 0

	return h
}

// TestReadActivityPrefersReportedStatus covers the statuses Claude writes about its own
// running sessions, which beat the frame because they are what it says rather than what
// its chrome implies. Each case pairs a status with a frame that would read the other
// way, so a pass proves which source won.
func TestReadActivityPrefersReportedStatus(t *testing.T) {
	tests := []struct {
		name   string
		status string
		frame  string
		want   harness.Activity
	}{
		{
			name:   "busy over an idle frame",
			status: statusBusy,
			frame:  idleFrame,
			want:   harness.ActivityWorking,
		},
		{
			name:   "waiting over an idle frame",
			status: statusWaiting,
			frame:  idleFrame,
			want:   harness.ActivityWaiting,
		},
		{
			// The interrupted turn. Claude leaves the status line on screen for the
			// moment the frame is captured, but says it is done.
			name:   "idle over a working frame",
			status: statusIdle,
			frame:  busyFrame,
			want:   harness.ActivityIdle,
		},
		{
			name:   "a shell left open under the session is the user's turn",
			status: statusShell,
			frame:  busyFrame,
			want:   harness.ActivityIdle,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			writeStatus(t, dir, sessionFile{PID: 1234, SessionID: "sess", Status: tt.status})

			reading := newTestHarness(t, dir).ReadActivity(harness.Observation{Frame: tt.frame, SessionID: "sess"})
			assert.Equal(t, harness.Reading{Activity: tt.want, Reported: true}, reading)
		})
	}
}

// TestReadActivityFallsBackToTheFrame covers everything that leaves the status files
// saying nothing about a conversation. All of it has to land on the frame, since the
// format is undocumented and a build that stops writing it must not strand every agent.
func TestReadActivityFallsBackToTheFrame(t *testing.T) {
	tests := []struct {
		name string
		// file is the status file to write, or nil to leave the directory empty.
		file *sessionFile
		// alive answers whether the pid in that file names a running process.
		alive bool
		// sessionID is the conversation the agent is recorded against.
		sessionID string
	}{
		{
			name:      "no status file at all, which is a build too old to write them",
			alive:     true,
			sessionID: "sess",
		},
		{
			name:      "a file for somebody else's conversation",
			file:      &sessionFile{PID: 1234, SessionID: "other", Status: statusIdle},
			alive:     true,
			sessionID: "sess",
		},
		{
			name:      "an agent with no conversation yet",
			file:      &sessionFile{PID: 1234, SessionID: "sess", Status: statusIdle},
			alive:     true,
			sessionID: "",
		},
		{
			// Only a graceful exit removes the file, so a killed process leaves its
			// last status behind to be believed.
			name:      "a status left behind by a process that is gone",
			file:      &sessionFile{PID: 1234, SessionID: "sess", Status: statusIdle},
			sessionID: "sess",
		},
		{
			name:      "a status this build does not know",
			file:      &sessionFile{PID: 1234, SessionID: "sess", Status: "compacting"},
			alive:     true,
			sessionID: "sess",
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

			reading := h.ReadActivity(harness.Observation{Frame: busyFrame, SessionID: tt.sessionID})
			assert.Equal(t, harness.Reading{Activity: harness.ActivityWorking}, reading,
				"the frame decides, and an inferred verdict is never reported")
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
		h := newTestHarness(t, sessionsDir)
		reading := h.ReadActivity(harness.Observation{Frame: idleFrame, SessionID: "sess"})
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
