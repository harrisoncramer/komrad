// Package agents runs background Claude chats. It holds three services over the
// store and tmux: SessionService for a chat's lifecycle, WorkflowService for the
// buckets chats are grouped into, and Launcher for the command line that starts
// one. Reconciler and TranscriptSyncer are the background workers serve runs on a
// timer. Each declares only the store and tmux methods it actually uses.
package agents

import (
	"os"
	"path/filepath"
)

// ManagerSession is the persistent tmux session the panel runs in. The popup
// attaches to it rather than launching the panel fresh, so hiding the popup
// leaves the panel running and reopening restores it. The tmux plugin uses the
// same literal.
const ManagerSession = "claude-manager"

// defaultServerAddr matches the config package's default listen address, used
// when a caller does not pass one so a launched agent is still told a usable
// endpoint rather than a malformed URL.
const defaultServerAddr = "127.0.0.1:8787"

// DefaultTranscriptsDir is where Claude Code writes its JSONL logs. An
// unresolvable home yields a relative path, which matches no logs rather than
// erroring.
func DefaultTranscriptsDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = ""
	}

	return filepath.Join(home, ".claude", "projects")
}
