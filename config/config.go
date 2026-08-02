// Package config defines the panel's runtime configuration, loaded from the
// environment.
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/caarlos0/env/v10"
)

// Config holds the panel's runtime configuration.
type Config struct {
	DatabaseURL         string `env:"DATABASE_URL" envDefault:"postgres://weave:weave@localhost:5460/weave?sslmode=disable"`
	ReadOnlyDatabaseURL string `env:"WEAVE_READONLY_DATABASE_URL" envDefault:"postgres://weave_readonly:weave_readonly@localhost:5460/weave?sslmode=disable"`
	Addr                string `env:"WEAVE_ADDR" envDefault:"127.0.0.1:8787"`
	TmuxSocket          string `env:"WEAVE_TMUX_SOCKET" envDefault:"weave"`
	Editor              string `env:"EDITOR" envDefault:"vi"`
	Tmux                string `env:"TMUX"`
	CacheDir            string `env:"WEAVE_CACHE_DIR"`
	TmuxPane            string `env:"TMUX_PANE"`
	// Set in the environment of every pane the panel launches, so a call this binary makes
	// on an agent's behalf is logged as that agent rather than as nobody.
	AgentID   string `env:"WEAVE_AGENT_ID"`
	SessionID string `env:"WEAVE_SESSION_ID"`
}

// Load reads the panel's configuration from the environment.
func Load() (*Config, error) {
	cfg := &Config{}
	if err := env.Parse(cfg); err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	if cfg.CacheDir == "" {
		dir, err := defaultCacheDir()
		if err != nil {
			return nil, err
		}

		cfg.CacheDir = dir
	}

	return cfg, nil
}

// LogPath is the file the background service writes to.
func (c *Config) LogPath() string {
	return filepath.Join(c.CacheDir, "weave.log")
}

// defaultCacheDir is where weave keeps what it generates. A directory that cannot be found
// is refused rather than stood in for, since the working directory is whatever the caller
// happened to be in and everything under a cache directory is weave's to delete and rewrite.
func defaultCacheDir() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("failed to find a cache directory: %w: set WEAVE_CACHE_DIR to an absolute path weave can write to", err)
	}

	return filepath.Join(dir, "weave"), nil
}
