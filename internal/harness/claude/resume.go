package claude

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Relocate moves a conversation's log into the projects folder that belongs to
// dir, so a claude started there resumes it rather than opening an empty session.
// Claude keeps a conversation under a folder named after the directory it ran in
// and resolves --resume against that folder alone, so a session is otherwise only
// reachable from the path the agent was created in.
//
// It moves rather than copies because a log is found by globbing for the session
// id, and two files answering to the same id would leave that reading whichever
// sorted first.
func (h *Harness) Relocate(sessionID, dir string) error {
	if h.projectsDir == "" || sessionID == "" {
		return nil
	}

	current := h.GetLogPath(sessionID)
	if current == "" {
		return nil
	}

	target := filepath.Join(h.projectsDir, projectFolder(dir))
	if err := os.MkdirAll(target, 0o755); err != nil {
		return fmt.Errorf("failed to create the projects folder for %q: %w", dir, err)
	}

	if err := os.Rename(current, filepath.Join(target, sessionID+".jsonl")); err != nil {
		return fmt.Errorf("failed to move the log for session %q: %w", sessionID, err)
	}

	return nil
}

// projectFolder renders the folder name Claude files a directory's conversations
// under, every character outside letters and digits replaced by a dash. This is the
// only place that reproduces Claude's encoding, and it is reached only when a log
// has to be moved between folders. Everything that reads a log globs for the
// session id instead, so an encoding that changes upstream degrades to a resume
// that starts fresh rather than a restore that fails.
func projectFolder(dir string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			return r
		default:
			return '-'
		}
	}, dir)
}
