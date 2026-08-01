package claude

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"weave/internal/harness"
)

// Claude Code keeps a file per running process under ~/.claude/sessions, named for the
// pid and rewritten whenever the session changes what it is doing. It is the whole of
// what the panel knows about a Claude agent, and it settles what a capture cannot: an
// escape interrupt leaves the status line on the pane but returns the session to idle
// here.
//
// The files are keyed by conversation, which survives a resume and a compaction but not
// a /clear. Clearing mints a new conversation under the same process, leaving the id the
// panel recorded matching nothing, so an agent that has been cleared stops reporting.
const (
	statusBusy    = "busy"
	statusWaiting = "waiting"
	statusIdle    = "idle"
	// statusShell is idle with a shell still open under the session, which is the
	// user's turn either way.
	statusShell = "shell"
)

// scanTTL is how long one read of the session directory is reused for. A pass asks about
// every agent in turn and the directory holds a file per agent, so scanning it per agent
// would cost a read of every session for every session.
const scanTTL = 500 * time.Millisecond

// sessionFile is the part of a status file the panel reads.
type sessionFile struct {
	PID       int    `json:"pid"`
	SessionID string `json:"sessionId"`
	Status    string `json:"status"`
}

// sessions reads what Claude Code says its running conversations are doing. The format is
// undocumented, so an unreadable directory, an unparsable file, or a status this build
// does not know all answer nothing and leave the caller on the frame.
type sessions struct {
	dir   string
	alive func(pid int) bool
	ttl   time.Duration

	mu     sync.Mutex
	readAt time.Time
	byID   map[string]string
}

// newSessions builds a reader over dir, which is Claude's own location when empty.
func newSessions(dir string) *sessions {
	if dir == "" {
		dir = DefaultSessionsDir()
	}

	return &sessions{dir: dir, alive: processAlive, ttl: scanTTL}
}

// status is what Claude says the conversation is doing, or false when it says nothing.
func (s *sessions) status(sessionID string) (string, bool) {
	if sessionID == "" {
		return "", false
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if time.Since(s.readAt) >= s.ttl {
		s.byID = s.scan()
		s.readAt = time.Now()
	}

	status, ok := s.byID[sessionID]

	return status, ok
}

// scan reads the whole directory into conversation ids and their statuses.
func (s *sessions) scan() map[string]string {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil
	}

	found := make(map[string]string, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}

		data, err := os.ReadFile(filepath.Join(s.dir, entry.Name()))
		if err != nil {
			continue
		}

		var file sessionFile
		if err := json.Unmarshal(data, &file); err != nil {
			continue
		}

		// A killed process leaves its last status behind, since only a graceful exit
		// removes the file, so the pid decides whether it still describes anything.
		if file.SessionID == "" || !s.alive(file.PID) {
			continue
		}

		found[file.SessionID] = file.Status
	}

	return found
}

// statusActivity maps a status onto an activity, reporting false for one this build does
// not know so the caller falls back to reading the frame.
func statusActivity(status string) (harness.Activity, bool) {
	switch status {
	case statusBusy:
		return harness.ActivityWorking, true
	case statusWaiting:
		return harness.ActivityWaiting, true
	case statusIdle, statusShell:
		return harness.ActivityIdle, true
	default:
		return harness.ActivityUnknown, false
	}
}

// processAlive reports whether a pid names a running process. Signal zero delivers
// nothing and reports only whether it could have been delivered, and the permission it
// can be refused with is a process running as somebody else rather than an absent one.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}

	err := syscall.Kill(pid, 0)

	return err == nil || errors.Is(err, syscall.EPERM)
}

// DefaultSessionsDir is where Claude Code writes its per-process status files. An
// unresolvable home yields a relative path, which matches nothing rather than erroring.
func DefaultSessionsDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = ""
	}

	return filepath.Join(home, ".claude", "sessions")
}
