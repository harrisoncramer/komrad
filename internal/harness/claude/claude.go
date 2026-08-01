// Package claude runs Claude Code as a weave harness. Everything here is
// true of that CLI and of nothing else: the flags it takes, the chrome its TUI
// draws while it works, where it files a conversation, and the JSONL it writes
// there.
package claude

import (
	"os"
	"path/filepath"

	"weave/internal/harness"

	"github.com/google/uuid"
)

// Harness puts Claude Code up in a pane and reads back what happened in it.
type Harness struct {
	// projectsDir is Claude's ~/.claude/projects, where it writes one conversation
	// log per session. It is a field rather than a constant so a test can point it
	// at a temp directory instead of the user's home.
	projectsDir string
	// sessions is Claude's own account of what each running conversation is doing.
	sessions *sessions
}

// Options configures the Claude harness. Both paths default to Claude's own locations.
type Options struct {
	ProjectsDir string
	SessionsDir string
}

// New builds a harness reading conversation logs and session statuses from the
// configured directories.
func New(opts Options) *Harness {
	projects := opts.ProjectsDir
	if projects == "" {
		projects = DefaultProjectsDir()
	}

	return &Harness{projectsDir: projects, sessions: newSessions(opts.SessionsDir)}
}

// The panel reaches Claude Code only through the interface, so a method missing here
// is a compile error rather than a nil call on one of serve's loops.
var _ harness.Harness = (*Harness)(nil)

// GetID is the name an agent running Claude Code is recorded under.
func (h *Harness) GetID() string { return "claude" }

// MintSessionID names the conversation a fresh launch will hold. Claude takes one on
// the command line, so the panel names it before it exists, and every later resume,
// log lookup, and relocation keys on the name it chose.
func (h *Harness) MintSessionID() string { return uuid.NewString() }

// FindSession has nothing to find. Claude is told the id of every conversation it
// starts, so a session is never discovered after the fact.
func (h *Harness) FindSession(string) (string, error) { return "", nil }

// DefaultProjectsDir is where Claude Code writes its JSONL logs. An unresolvable
// home yields a relative path, which matches no logs rather than erroring.
func DefaultProjectsDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = ""
	}

	return filepath.Join(home, ".claude", "projects")
}
