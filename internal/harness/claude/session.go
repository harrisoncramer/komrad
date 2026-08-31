package claude

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"weave/internal/harness"
)

// ForksOnResume reports that Claude can carry a conversation into a session id of weave's own,
// which is what lets an adoption leave the person's own conversation untouched.
func (h *Harness) ForksOnResume() bool { return true }

// DescribeSession reads where a conversation was held out of its own log, since the projects
// folder it sits in maps every character a path cannot hold to a dash and does not decode.
func (h *Harness) DescribeSession(sessionID string) (harness.Session, error) {
	path := h.GetLogPath(sessionID)
	if path == "" {
		return harness.Session{}, fmt.Errorf("%w for session %q", harness.ErrSessionNotFound, sessionID)
	}

	file, err := os.Open(path)
	if err != nil {
		return harness.Session{}, fmt.Errorf("failed to open the log for session %q: %w", sessionID, err)
	}
	defer func() { _ = file.Close() }()

	var found harness.Session

	lines := bufio.NewScanner(file)
	lines.Buffer(make([]byte, 0, 64*1024), maxLogLine)
	for lines.Scan() {
		var line sessionLine
		if err := json.Unmarshal(lines.Bytes(), &line); err != nil {
			continue
		}

		// The last turn to record either wins, since a conversation carried to another directory
		// keeps writing the log it started in and only its newest lines say where it is now.
		if dir := strings.TrimSpace(line.Cwd); dir != "" {
			found.Dir = dir
		}
		// A detached checkout records HEAD, which names no branch and would have the panel
		// hunting for a pull request on one.
		if branch := strings.TrimSpace(line.GitBranch); branch != "" && branch != "HEAD" {
			found.Branch = branch
		}
	}
	if err := lines.Err(); err != nil {
		return harness.Session{}, fmt.Errorf("failed to read the log for session %q: %w", sessionID, err)
	}

	if found.Dir == "" {
		return harness.Session{}, fmt.Errorf("%w: the log for session %q records no working directory", harness.ErrSessionNotFound, sessionID)
	}

	return found, nil
}

// maxLogLine is what one line of a conversation log may run to, which is generous because a
// single turn carrying a pasted file is one line and a truncated read reports no directory.
const maxLogLine = 8 * 1024 * 1024

type sessionLine struct {
	Cwd       string `json:"cwd"`
	GitBranch string `json:"gitBranch"`
}
