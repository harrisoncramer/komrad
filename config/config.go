// Package config resolves the panel's runtime configuration: the user's config files
// underneath, the environment on top.
package config

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"

	"weave/internal/appconfig"

	"github.com/caarlos0/env/v10"
)

// The login the MCP query tools connect as when none is configured. Only the name and the
// password are weave's to pick. Where it connects is the database url's to say, since this
// is a second role on the same rows rather than a second store.
const (
	readOnlyUser     = "weave_readonly"
	readOnlyPassword = "weave_readonly"
)

// layerVariable is the environment's way of saying --config, holding the same list of paths
// separated by colons the way PATH is.
//
// It exists because some things can set a variable and nothing else. mise.test.toml is an
// [env] block, so it can give the throwaway instance its own database and tmux socket but has
// no way to add a flag to whatever command you happen to run.
const layerVariable = "WEAVE_CONFIG"

func LayerPaths() []string {
	return appconfig.ParseLayerList(os.Getenv(layerVariable))
}

// Source is where a resolved value came from. Used for debugging.
type Source string

const (
	SourceDefault     Source = "built-in default"
	SourceFile        Source = "config file"
	SourceEnvironment Source = "environment"
	SourceDerived     Source = "derived"
)

// Config holds the panel's runtime configuration, resolved.
//
// The connection settings each have a config file key and a WEAVE_-prefixed environment
// variable. None of them carries an envDefault, deliberately: the built-in default belongs
// underneath the file, and a tag default would sit beside the environment where nothing
// could tell it apart from a value somebody set.
type Config struct {
	ConnectionSettingsConfig
	Tmux      string `env:"TMUX"`
	TmuxPane  string `env:"TMUX_PANE"`
	AgentID   string `env:"WEAVE_AGENT_ID"`
	SessionID string `env:"WEAVE_SESSION_ID"`
	Editor    string `env:"EDITOR" envDefault:"vi"`
	Sources   map[string]Source
}

type ConnectionSettingsConfig struct {
	DatabaseURL         string `env:"WEAVE_DATABASE_URL"`
	ReadOnlyDatabaseURL string `env:"WEAVE_READONLY_DATABASE_URL"`
	Addr                string `env:"WEAVE_ADDR"`
	TmuxSocket          string `env:"WEAVE_TMUX_SOCKET"`
	CacheDir            string `env:"WEAVE_CACHE_DIR"`
}

// Load resolves the panel's configuration from the file config and the environment, with the
// environment winning wherever a variable is genuinely set.
func Load(file *appconfig.Config) (*Config, error) {
	if file == nil {
		file = appconfig.Defaults()
	}

	cfg := &Config{}

	// OnSet is used only to learn which variables the library looked at. Its isDefault
	// argument answers "did this value come from envDefault", which is not the same question
	// as "was the variable set": for a field with no envDefault it is false either way. So
	// the answer comes from the environment itself. An exported empty string counts as
	// unset, which is how the library treats one for a field that does have a default.
	set := map[string]bool{}
	options := env.Options{
		OnSet: func(tag string, _ any, _ bool) {
			value, present := os.LookupEnv(tag)
			set[tag] = present && value != ""
		},
	}

	if err := env.ParseWithOptions(cfg, options); err != nil {
		return nil, fmt.Errorf("failed to load config from the environment: %w", err)
	}

	cfg.Sources = map[string]Source{}
	for _, setting := range cfg.connectionSettings(file) {
		if set[setting.variable] {
			cfg.Sources[setting.key] = SourceEnvironment

			continue
		}

		// The environment parse already wrote its answer into the field, so a variable that
		// was not genuinely set is overwritten here rather than merged.
		if setting.fromFile != "" {
			*setting.field = setting.fromFile

			// A file value equal to the built-in is reported as the built-in. The loader
			// fills the connection table with the defaults, so a value that came from
			// withDefaults arrives here looking exactly like one somebody wrote, and
			// claiming a file said something it did not is worse than under-reporting a
			// file that agrees with the default anyway.
			cfg.Sources[setting.key] = SourceFile
			if setting.fromFile == setting.builtin {
				cfg.Sources[setting.key] = SourceDefault
			}

			continue
		}

		*setting.field = setting.builtin
		cfg.Sources[setting.key] = SourceDefault
	}

	// The two derivations run after the file and the environment have both had their say, so
	// that a config naming only a database gets a cache directory and a read-only login that
	// follow it rather than ones built off the default connection string.
	if cfg.CacheDir == "" {
		dir, err := defaultCacheDir()
		if err != nil {
			return nil, err
		}

		cfg.CacheDir = dir
		cfg.Sources["cache_dir"] = SourceDerived
	}

	if cfg.ReadOnlyDatabaseURL == "" {
		// A database url this cannot rewrite leaves no read-only url rather than refusing
		// here. `weave doctor` is the command a connection string that does not parse is
		// diagnosed with, and refusing would take doctor down with it.
		cfg.ReadOnlyDatabaseURL = deriveReadOnlyURL(cfg.DatabaseURL)
		cfg.Sources["readonly_database_url"] = SourceDerived
	}

	return cfg, nil
}

// connectionSetting is one setting's three candidate values and where the winner goes. Every
// level of the precedence chain is named here, so the loop that resolves them is the chain
// written once rather than per setting.
type connectionSetting struct {
	key      string
	variable string
	field    *string
	fromFile string
	// builtin is empty for the two that are derived rather than defaulted, which happens
	// after this because both follow whichever value won above.
	builtin string
}

// connectionSettings is the five settings the file and the environment both speak to, in the
// order the generated reference lists them. Building the list rather than resolving each by
// hand means a sixth setting is one entry here and nothing else.
func (c *Config) connectionSettings(file *appconfig.Config) []connectionSetting {
	return []connectionSetting{
		{
			key:      "database_url",
			variable: "WEAVE_DATABASE_URL",
			field:    &c.DatabaseURL,
			fromFile: file.Connection.DatabaseURL,
			builtin:  appconfig.DefaultDatabaseURL,
		},
		{
			key:      "readonly_database_url",
			variable: "WEAVE_READONLY_DATABASE_URL",
			field:    &c.ReadOnlyDatabaseURL,
			fromFile: file.Connection.ReadOnlyDatabaseURL,
		},
		{
			key:      "addr",
			variable: "WEAVE_ADDR",
			field:    &c.Addr,
			fromFile: file.Connection.Addr,
			builtin:  appconfig.DefaultAddr,
		},
		{
			key:      "tmux_socket",
			variable: "WEAVE_TMUX_SOCKET",
			field:    &c.TmuxSocket,
			fromFile: file.Connection.TmuxSocket,
			builtin:  appconfig.DefaultTmuxSocket,
		},
		{
			key:      "cache_dir",
			variable: "WEAVE_CACHE_DIR",
			field:    &c.CacheDir,
			fromFile: file.Connection.CacheDir,
		},
	}
}

// deriveReadOnlyURL is the database url with the least-privilege login in place of the
// owner's. The host, port, database, and connection options are kept exactly as they were
// written, since the two logins reach the same rows on the same server and differ only in
// what they may do to them.
//
// Empty for anything that is not a postgres:// url. Postgres also accepts a space-separated
// keyword list, and swapping a login into one of those means rebuilding a string whose
// options weave would then have to reproduce.
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
