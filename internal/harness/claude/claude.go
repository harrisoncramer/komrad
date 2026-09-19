// Package claude runs Claude Code as a weave agent, building the command that starts or
// resumes a conversation, reading its JSONL transcript, and telling from a captured frame
// whether it is working, waiting on a person, or idle.
package claude

import (
	"fmt"
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
	// ConfigDir is the directory named in place of ~/.claude, empty when the CLI is left to
	// its default.
	ConfigDir string
}

// NewHarness builds a harness rooted at configDir.
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

// NamedConfigDir reads the directory the environment names in place of ~/.claude, empty when
// it names none.
func NamedConfigDir() string {
	dir, _ := config.LookupEnv(configDirEnv)

	return dir
}

func (h *Harness) resolveConfigDir() (string, error) {
	if h.configDir != "" {
		return h.configDir, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}

	return filepath.Join(home, ".claude"), nil
}
