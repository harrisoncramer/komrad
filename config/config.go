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
}

// Load reads the panel's configuration from the environment.
func Load() (*Config, error) {
	cfg := &Config{}
	if err := env.Parse(cfg); err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	if cfg.CacheDir == "" {
		cfg.CacheDir = defaultCacheDir()
	}

	return cfg, nil
}

// LogPath is the file the background service writes to.
func (c *Config) LogPath() string {
	return filepath.Join(c.CacheDir, "weave.log")
}

// defaultCacheDir is where weave keeps what it generates, falling back to the working directory when the user's cache directory cannot be resolved at all.
func defaultCacheDir() string {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "."
	}

	return filepath.Join(dir, "weave")
}
