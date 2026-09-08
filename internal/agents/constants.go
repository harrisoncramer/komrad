package agents

import (
	"os"
	"path/filepath"
)

const ManagerSession = "weave"

const defaultServerAddr = "127.0.0.1:8787"

// defaultFallbackResumeRoot is where an agent runs when the directory it was created in cannot
// be brought back, kept beside Claude's own state and out of the system temp directory.
func defaultFallbackResumeRoot() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = ""
	}

	return filepath.Join(home, ".claude", "weave", "restored")
}
