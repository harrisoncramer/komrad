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
const defaultDatabaseURL = "postgres://weave:weave@localhost:5460/weave?sslmode=disable"

// Config holds the panel's runtime configuration. TmuxSocket names the manager's
// own tmux server, which is deliberately not the user's. A server only lists its
// own sessions, so the manager session stays out of the user's session list and
// cannot be attached to outside the popup, which is the only place it behaves.
//
// Editor is what the panel's editor key runs. It reads EDITOR rather than being a
// setting of the panel's own, so the key opens whatever the rest of the user's shell
// opens.
//
// Tmux is what tmux exports into the pane it spawns. It holds the socket path of that
// pane's server, the server's pid, and the id of its session. The open command reads it
// to find the user's server, because nothing can be handed in from the key binding.
// tmux expands no format in a popup's command or its environment.
type Config struct {
	DatabaseURL string `env:"DATABASE_URL"`
	Addr        string `env:"WEAVE_ADDR" envDefault:"127.0.0.1:8787"`
	TmuxSocket  string `env:"WEAVE_TMUX_SOCKET" envDefault:"weave"`
	Editor      string `env:"EDITOR" envDefault:"vi"`
	Tmux        string `env:"TMUX"`
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
