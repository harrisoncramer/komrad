package codex

import (
	"fmt"
	"strings"

	"weave/internal/harness"
)

// ForksOnResume reports that Codex cannot, since it takes no session id on the command line and
// a resume appends to the rollout it was given, so adopting one takes it over.
func (h *Harness) ForksOnResume() bool { return false }

// DescribeSession reads where a conversation was held out of the opening record of its rollout,
// which is the only place Codex writes it down.
func (h *Harness) DescribeSession(sessionID string) (harness.Session, error) {
	path := h.GetLogPath(sessionID)
	if path == "" {
		return harness.Session{}, fmt.Errorf("%w for session %q", harness.ErrSessionNotFound, sessionID)
	}

	meta, err := readSessionMeta(path)
	if err != nil {
		return harness.Session{}, err
	}

	dir := strings.TrimSpace(meta.Cwd)
	if dir == "" {
		return harness.Session{}, fmt.Errorf("%w: the rollout for session %q records no working directory", harness.ErrSessionNotFound, sessionID)
	}

	// Codex records no branch, so an adopted agent's branch is whatever its checkout is on.
	return harness.Session{Dir: dir}, nil
}
