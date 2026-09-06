package agents

import (
	"os"
	"path/filepath"
)

const ManagerSession = "weave"

const defaultServerAddr = "127.0.0.1:8787"

// defaultFallbackResumeRoot is where an agent runs when the directory it was created in cannot
// be brought back, kept beside Claude's own state so that a sweep of the system temp directory
// cannot delete it while an agent is still living there.
func defaultFallbackResumeRoot() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = ""
	}

	return filepath.Join(home, ".claude", "weave", "restored")
}
