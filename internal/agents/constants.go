package agents

import (
	"os"
	"path/filepath"
)

const ManagerSession = "weave"

const defaultServerAddr = "127.0.0.1:8787"

// DefaultFallbackResumeRoot is where an agent runs when the directory it was created in
// cannot be brought back. It sits beside Claude's own state rather than in the
// system temp directory, so a stand-in an agent is still living in does not
// vanish under it on the next cleanup sweep.
func DefaultFallbackResumeRoot() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = ""
	}

	return filepath.Join(home, ".claude", "weave", "restored")
}
