// Package claude runs Claude Code as a komrad agent, building the command that starts or
// resumes a conversation, reading its JSONL transcript, and telling from a captured frame
// whether it is working, waiting on a person, or idle.
package claude

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"komrad/config"
	"komrad/internal/harness"
)

// HarnessID is the id an agent running Claude Code is recorded under.
const HarnessID = "claude"

const configDirEnv = "CLAUDE_CONFIG_DIR"

type NewHarnessParams struct {
	// ConfigDir is the directory named in place of ~/.claude, empty when the CLI is left to
	// its default.
	ConfigDir string
	// CacheDir holds the socket each agent's CLI listens on for messages.
	CacheDir string
}

type Harness struct {
	configDir string
	cacheDir  string
	mu        sync.Mutex
	// logPaths remembers where each session's log was found, keyed by session id.
	logPaths map[string]string
}

// NewHarness builds a harness rooted at configDir.
func NewHarness(params NewHarnessParams) *Harness {
	return &Harness{
		configDir: params.ConfigDir,
		cacheDir:  params.CacheDir,
		logPaths:  map[string]string{},
	}
}

var _ harness.Harness = (*Harness)(nil)

// ID is the name an agent running Claude Code is recorded under.
func (h *Harness) ID() string {
	return HarnessID
}

// Binary is the CLI a pane's shell has to resolve to start an agent.
func (h *Harness) Binary() string {
	return HarnessID
}

// SessionNaming is SessionMinted.
func (h *Harness) SessionNaming() harness.SessionNaming {
	return harness.SessionMinted
}

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
