// Package config defines the panel's configuration, loaded from the environment
// via ultra so secrets and overrides resolve the same way as the rest of the
// toolchain.
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/harrisoncramer/ultra"
)

// Config holds the panel's runtime configuration.
type Config struct {
	DBPath     string `env:"CLAUDE_AGENTS_DB"`
	Addr       string `env:"CLAUDE_AGENTS_ADDR" envDefault:"127.0.0.1:8787"`
	TmuxSocket string `env:"CLAUDE_AGENTS_TMUX_SOCKET" envDefault:"default"`
}

// Load reads the panel's configuration from the environment, defaulting the
// database to ~/.claude/claude-agents.db when unset.
func Load() (*Config, error) {
	cfg, err := ultra.Load(&Config{})
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	if cfg.DBPath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("failed to resolve home directory: %w", err)
		}

		cfg.DBPath = filepath.Join(home, ".claude", "claude-agents.db")
	}

	return cfg, nil
}
