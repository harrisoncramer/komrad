// Package config defines the panel's runtime configuration, loaded from the
// environment.
package config

import (
	"fmt"

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
}

// Load reads the panel's configuration from the environment.
func Load() (*Config, error) {
	cfg := &Config{}
	if err := env.Parse(cfg); err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	return cfg, nil
}
