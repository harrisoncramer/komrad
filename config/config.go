// Package config defines the panel's runtime configuration, loaded from the
// environment via ultra.
package config

import (
	"fmt"

	"github.com/harrisoncramer/ultra"
)

const defaultDatabaseURL = "postgres://weave:weave@localhost:5460/weave?sslmode=disable"
const defaultReadOnlyDatabaseURL = "postgres://weave_readonly:weave_readonly@localhost:5460/weave?sslmode=disable"

// Config holds the panel's runtime configuration.
type Config struct {
	DatabaseURL         string `env:"DATABASE_URL"`
	ReadOnlyDatabaseURL string `env:"WEAVE_READONLY_DATABASE_URL"`
	Addr                string `env:"WEAVE_ADDR" envDefault:"127.0.0.1:8787"`
	TmuxSocket          string `env:"WEAVE_TMUX_SOCKET" envDefault:"weave"`
	Editor              string `env:"EDITOR" envDefault:"vi"`
	Tmux                string `env:"TMUX"`
}

// Load reads the panel's configuration from the environment, defaulting to the
// local development database when DATABASE_URL is unset.
func Load() (*Config, error) {
	cfg, err := ultra.Load(&Config{})
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	if cfg.DatabaseURL == "" {
		cfg.DatabaseURL = defaultDatabaseURL
	}

	// Keyed on the database being the development one rather than on DATABASE_URL
	// having been unset, since exporting that url by hand is the same database and
	// wants the same read-only login.
	if cfg.ReadOnlyDatabaseURL == "" && cfg.DatabaseURL == defaultDatabaseURL {
		cfg.ReadOnlyDatabaseURL = defaultReadOnlyDatabaseURL
	}

	if cfg.ReadOnlyDatabaseURL == "" {
		cfg.ReadOnlyDatabaseURL = cfg.DatabaseURL
	}

	return cfg, nil
}
