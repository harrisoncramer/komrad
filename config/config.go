// Package config defines the panel's runtime configuration, loaded from the
// environment via ultra.
package config

import (
	"fmt"

	"github.com/harrisoncramer/ultra"
)

// defaultDatabaseURL points at the Postgres that docker-compose brings up, so a
// checkout works after `mise run docker:up` with nothing exported. A deployed
// instance sets DATABASE_URL to its own database and never falls back here.
const defaultDatabaseURL = "postgres://claude_agents:claude_agents@localhost:5460/claude_agents?sslmode=disable"

// Config holds the panel's runtime configuration.
type Config struct {
	DatabaseURL string `env:"DATABASE_URL"`
	Addr        string `env:"CLAUDE_AGENTS_ADDR" envDefault:"127.0.0.1:8787"`
	TmuxSocket  string `env:"CLAUDE_AGENTS_TMUX_SOCKET" envDefault:"default"`
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

	return cfg, nil
}
