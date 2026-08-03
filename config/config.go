// Package config defines the panel's runtime configuration, loaded from the
// environment.
package config

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"

	"github.com/caarlos0/env/v10"
)

// The login the MCP query tools connect as, when it is not configured. Only the name and
// the password are weave's to pick. Where it connects is DATABASE_URL's to say, since this
// is a second role on the same rows rather than a second store.
const (
	readOnlyUser     = "weave_readonly"
	readOnlyPassword = "weave_readonly"
)

// Config holds the panel's runtime configuration.
type Config struct {
	DatabaseURL         string `env:"DATABASE_URL" envDefault:"postgres://weave:weave@localhost:5460/weave?sslmode=disable"`
	ReadOnlyDatabaseURL string `env:"WEAVE_READONLY_DATABASE_URL"`
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

	if cfg.ReadOnlyDatabaseURL == "" {
		// A DATABASE_URL this cannot rewrite is left as no read-only url at all, rather
		// than refused here. Refusing would take `weave doctor` down with it, and doctor is
		// the command a connection string that does not parse is diagnosed with. The
		// service and doctor both name an empty one for what it is.
		cfg.ReadOnlyDatabaseURL = deriveReadOnlyURL(cfg.DatabaseURL)
	}

	return cfg, nil
}

// deriveReadOnlyURL is DATABASE_URL with the least-privilege login in place of the owner's.
// The host, port, database, and connection options are kept exactly as they were written,
// since the two logins reach the same rows on the same server and differ only in what they
// may do to them.
//
// Empty when the url is not one this can rewrite, which is anything that is not a
// postgres:// url. Postgres also accepts a space-separated keyword list, and weave has no
// way to swap a login into one of those without parsing and rebuilding a string whose
// options it would then have to reproduce.
func deriveReadOnlyURL(databaseURL string) string {
	parsed, err := url.Parse(databaseURL)
	if err != nil {
		return ""
	}

	if parsed.Scheme != "postgres" && parsed.Scheme != "postgresql" {
		return ""
	}

	parsed.User = url.UserPassword(readOnlyUser, readOnlyPassword)

	return parsed.String()
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
		return "", fmt.Errorf("failed to find a cache directory, set WEAVE_CACHE_DIR to an absolute path weave can write to: %w", err)
	}

	return filepath.Join(dir, "weave"), nil
}
