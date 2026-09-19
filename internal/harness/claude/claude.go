// Package claude runs Claude Code as a weave agent, building the command that starts or
// resumes a conversation, reading its JSONL transcript, and telling from a captured frame
// whether it is working, waiting on a person, or idle.
package claude

import (
	"os"
	"path/filepath"

	"weave/config"
	"weave/internal/harness"

	"github.com/google/uuid"
)

// HarnessID is the id an agent running Claude Code is recorded under.
const HarnessID = "claude"

const configDirEnv = "CLAUDE_CONFIG_DIR"

type Harness struct {
	configDir string
}

var _ harness.Harness = (*Harness)(nil)

type NewHarnessParams struct {
	ConfigDir string
}

// NewHarness builds a harness that reads conversation logs from configDir, writes its launch
// files there, and pins every agent it starts to that same directory.
func NewHarness(params NewHarnessParams) *Harness {
	return &Harness{
		configDir: params.ConfigDir,
	}
}

// ID is the name an agent running Claude Code is recorded under.
func (h *Harness) ID() string { return HarnessID }

// Binary is the CLI a pane's shell has to resolve to start an agent.
func (h *Harness) Binary() string { return HarnessID }

// InterruptKeys stops Claude where it stands, the second Escape dismissing the prompt the first
// can leave behind.
func (h *Harness) InterruptKeys() []string { return []string{"Escape", "Escape"} }

// MintSessionID names the conversation a fresh launch will hold, which Claude takes on the
// command line and every later resume keys on.
func (h *Harness) MintSessionID() string { return uuid.NewString() }

// FindSession has nothing to find, Claude being told the id of every conversation it starts.
func (h *Harness) FindSession(string) (string, error) { return "", nil }

// DefaultConfigDir resolves Claude Code's own directory the way the CLI does, CLAUDE_CONFIG_DIR
// when it is set and ~/.claude otherwise.
func DefaultConfigDir() string {
	if dir, set := config.LookupEnv(configDirEnv); set {
		return dir
	}

	home, err := os.UserHomeDir()
	if err != nil {
		home = ""
	}

	return filepath.Join(home, ".claude")
}

func (h *Harness) projectsDir() string {
	return filepath.Join(h.configDir, "projects")
}
