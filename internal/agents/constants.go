package agents

import (
	"os"
	"path/filepath"
)

// ManagerSession is the persistent tmux session the panel runs in. The popup
// attaches to it rather than launching the panel fresh, so hiding the popup
// leaves the panel running and reopening restores it. The tmux plugin uses the
// same literal.
const ManagerSession = "weave"

// defaultServerAddr matches the config package's default listen address. It is
// used when a caller passes none, so a launched agent is still told a usable
// endpoint rather than a malformed URL.
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
