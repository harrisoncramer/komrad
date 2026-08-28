package claude

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Relocate moves a conversation's log into the projects folder belonging to dir, since Claude
// matches --resume against the folder named after the directory it ran in.
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

	// A log is found by globbing the session id, so moving rather than copying keeps two files
	// from answering to it.
	if err := os.Rename(current, filepath.Join(target, sessionID+".jsonl")); err != nil {
		return fmt.Errorf("failed to move the log for session %q: %w", sessionID, err)
	}

	return nil
}

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
