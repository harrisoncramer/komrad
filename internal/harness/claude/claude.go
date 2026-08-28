package claude

import (
	"os"
	"path/filepath"

	"weave/internal/harness"

	"github.com/google/uuid"
)

const binary = "claude"

type Harness struct {
	projectsDir string
}

var _ harness.Harness = (*Harness)(nil)

type NewHarnessParams struct {
	ProjectsDir string
}

// NewHarness builds a harness reading conversation logs from projectsDir, which is
// Claude's own location when it is left empty.
func NewHarness(params NewHarnessParams) *Harness {
	return &Harness{
		projectsDir: params.ProjectsDir,
	}
}

// GetID is the name an agent running Claude Code is recorded under.
func (h *Harness) GetID() string { return binary }

// GetBinary is the CLI a pane's shell has to resolve to start an agent.
func (h *Harness) GetBinary() string { return binary }

// GetInterruptKeys stops Claude where it stands, the second Escape dismissing the prompt the
// first can leave behind so a later message lands in the composer.
func (h *Harness) GetInterruptKeys() []string { return []string{"Escape", "Escape"} }

// MintSessionID names the conversation a fresh launch will hold, since Claude takes one on the
// command line and every later resume keys on it.
func (h *Harness) MintSessionID() string { return uuid.NewString() }

// FindSession has nothing to find, since Claude is told the id of every conversation it starts.
func (h *Harness) FindSession(string) (string, error) { return "", nil }

// DefaultProjectsDir is where Claude Code writes its JSONL logs, returning a relative path that
// matches no logs when the home directory cannot be read.
func DefaultProjectsDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = ""
	}

	return filepath.Join(home, ".claude", "projects")
}
