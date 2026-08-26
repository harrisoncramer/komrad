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

// GetInterruptKeys stops Claude where it stands. Escape interrupts what it is doing, and the
// second one dismisses the prompt the first can leave behind, so a message typed after them
// lands in the composer rather than in whatever was on screen.
func (h *Harness) GetInterruptKeys() []string { return []string{"Escape", "Escape"} }

// MintSessionID names the conversation a fresh launch will hold. Claude takes one on
// the command line, so the panel names it before it exists, and every later resume,
// log lookup, and relocation keys on the name it chose.
func (h *Harness) MintSessionID() string { return uuid.NewString() }

// FindSession has nothing to find. Claude is told the id of every conversation it
// starts, so a session is never discovered after the fact. This is only relevant for Codex.
func (h *Harness) FindSession(string) (string, error) { return "", nil }

// DefaultProjectsDir is where Claude Code writes its JSONL logs. A home that
// cannot be read yields a relative path, which matches no logs rather than erroring.
func DefaultProjectsDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = ""
	}

	return filepath.Join(home, ".claude", "projects")
}
