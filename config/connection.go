package config

import (
	"cmp"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/spf13/viper"
)

// readerSuffix turns an engine's login into the name of its read-only login.
const readerSuffix = "_reader"

// The connection settings a config that names none runs on. They live here rather than as
// envDefault tags on the runtime config so that the built-in default sits underneath the
// file, which is where the precedence chain wants it. A tag default would sit beside the
// environment instead and there would be no way to tell it apart from a value someone set.
//
// DatabaseURL, OwnerDatabaseURL, ReadOnlyDatabaseURL and CacheDir have no constant because each
// is derived from the state directory, another url or the platform once the file and the
// environment have been resolved.
const (
	// The default engine's login, which `komrad setup` creates in the embedded Postgres.
	defaultEngineLogin = "komrad_engine_default"
	// Loopback, because the endpoint has no auth and its writes can spawn agents.
	defaultAddr = "127.0.0.1:8787"
	// The socket komrad's own tmux server listens on, named for the tool.
	defaultTmuxSocket = "komrad"
)

// Connection is which komrad you are talking to: the database it writes, the address it
// serves, the tmux server it runs agents on, and the directory it generates into. Every
// one of these also has a KOMRAD_-prefixed environment variable, and the variable wins when
// it is set, so one shell or one checkout can point somewhere else without editing a file.
//
// These are the settings a second instance is isolated by, which is why they are a table
// rather than four keys at the top level. Overriding the table is overriding the instance.
type Connection struct {
	// The Postgres komrad keeps every agent, workflow, schedule and transcript in, reached as
	// this engine's own login, which is what keeps its rows apart from every other engine's.
	// `komrad engine ensure` creates the login and prints this url.
	// Left empty this is the default engine's login on the embedded Postgres under state_dir,
	// which `komrad start` brings up.
	DatabaseURL string `mapstructure:"database_url"`
	// The login that owns the schema, used only by `komrad migrate` and the `komrad engine` commands.
	// Left empty this is database_url, or the embedded Postgres's owner while database_url is empty.
	OwnerDatabaseURL string `mapstructure:"owner_database_url"`
	// The least-privilege login the MCP query tools connect as.
	// Left empty this is database_url with the engine's reader login, which is its own login
	// with _reader on the end and the same password.
	ReadOnlyDatabaseURL string `mapstructure:"readonly_database_url"`
	// The address the server listens on, which the panel, the CLI and the MCP
	// endpoint all reach it at. Change it to run two komrads on one machine. It has to be a
	// loopback host, since the api has no auth and its writes spawn agents.
	Addr string `mapstructure:"addr"`
	// The tmux socket komrad runs its own server on. Nothing in your normal tmux sees
	// that server, which is why `tmux -L komrad ls` is how you look at it directly.
	TmuxSocket string `mapstructure:"tmux_socket"`
	// The directory where komrad keeps what it generates, such as the server log, the built-in
	// scripts it writes out on every start, and the temp files a harness passes a prompt through.
	// Left empty this is ~/Library/Caches/komrad on a Mac and ~/.cache/komrad on Linux.
	// Everything under it is komrad's to delete and rewrite, so nothing of yours belongs here.
	CacheDir string `mapstructure:"cache_dir"`
	// The directory where komrad keeps its own files about agents, such as generated worktrees,
	// released reviews and forge credentials.
	// Left empty this is ~/.local/state/komrad.
	// It holds credentials and is komrad's to manage, so nothing of yours belongs here.
	StateDir string `mapstructure:"state_dir"`
	// The https address the engine is served on, like https://komrad.example.com. Left empty the
	// engine is local and an agent's terminal is shown by moving its own pane into the panel. It
	// may be plain http when its host is this machine.
	URL string `mapstructure:"url"`
}

// defaultConnection is the instance a config that names none points at.
func defaultConnection() Connection {
	return Connection{
		Addr:       defaultAddr,
		TmuxSocket: defaultTmuxSocket,
	}
}

func (c *Connection) normalize() {
	c.URL = strings.TrimRight(c.URL, "/")
	c.DatabaseURL = strings.TrimSpace(c.DatabaseURL)
	if strings.TrimSpace(c.Addr) == "" {
		c.Addr = defaultAddr
	}
	if strings.TrimSpace(c.TmuxSocket) == "" {
		c.TmuxSocket = defaultTmuxSocket
	}
}

// validate rejects an address the server could not listen on or should not, and a
// database url that is not one Postgres would answer.
func (c Connection) validate() error {
	if err := ValidateAddr(c.Addr); err != nil {
		return fmt.Errorf("connection.addr: %w", err)
	}
	if err := c.validateURL(); err != nil {
		return fmt.Errorf("connection.url: %w", err)
	}

	return validateDatabaseURL(c.DatabaseURL)
}

// loopbackHosts are the hosts that reach only this machine, spelled the ways a person writes them.
var loopbackHosts = []string{"localhost", "::1"}

// ValidateAddr rejects an address the server could not listen on, or should not. The caller
// names where the address came from, since it is a flag as often as it is a setting.
func ValidateAddr(addr string) error {
	trimmed := strings.TrimSpace(addr)
	if trimmed == "" {
		return errors.New("names nothing, which listens on every interface, and the api has no auth")
	}

	host, port, err := net.SplitHostPort(trimmed)
	if err != nil {
		return fmt.Errorf("%q is not a host and port: %w", addr, err)
	}
	if number, err := strconv.Atoi(port); err != nil || number < 1 || number > 65535 {
		return fmt.Errorf("%q is not a port", port)
	}
	if host == "" {
		return fmt.Errorf("%q names no host, which answers on every interface, and the api has no auth", addr)
	}

	if isLoopbackHost(host) {
		return nil
	}

	if net.ParseIP(host) == nil {
		return fmt.Errorf("%q is not an address this machine can answer on", host)
	}

	return fmt.Errorf("%q reaches this machine from the network, and the api has no auth and its writes spawn agents", host)
}

// isLoopbackHost reports whether a host reaches only this machine.
func isLoopbackHost(host string) bool {
	if slices.Contains(loopbackHosts, host) {
		return true
	}

	parsed := net.ParseIP(host)

	return parsed != nil && parsed.IsLoopback()
}

// validateURL rejects an engine address that is neither https nor on this machine.
func (c Connection) validateURL() error {
	if c.URL == "" {
		return nil
	}

	if err := requireSecureURL(c.URL); err != nil {
		return err
	}

	parsed, err := url.Parse(c.URL)
	if err != nil {
		return fmt.Errorf("%q is not a url: %w", c.URL, err)
	}
	if strings.Trim(parsed.Path, "/") != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("%q has more than a scheme and a host", c.URL)
	}

	return nil
}

// validateDatabaseURL rejects a url Postgres would not answer.
func validateDatabaseURL(databaseURL string) error {
	parsed, err := url.Parse(strings.TrimSpace(databaseURL))
	if err != nil {
		return fmt.Errorf("connection.database_url: %q is not a url: %w", databaseURL, err)
	}
	if parsed.Scheme != "postgres" && parsed.Scheme != "postgresql" {
		return fmt.Errorf("connection.database_url: %q is a %s url, and komrad stores its records in Postgres",
			databaseURL, cmp.Or(parsed.Scheme, "schemeless"))
	}
	if parsed.Host == "" && parsed.Query().Get("host") == "" {
		return fmt.Errorf("connection.database_url: %q names no server to connect to", databaseURL)
	}

	return nil
}

// Source is the precedence layer that supplied a resolved connection setting.
type Source string

const (
	sourceDefault     Source = "built-in default"
	sourceFile        Source = "config file"
	sourceEnvironment Source = "environment"
	sourceDerived     Source = "derived default"
)

// UsesEmbeddedPostgres reports whether database_url was left empty, which names the embedded Postgres.
func (r *Report) UsesEmbeddedPostgres() bool {
	return r.Sources["database_url"] == sourceDerived
}

var connectionEnvironment = map[string]string{
	"connection.database_url":          "KOMRAD_DATABASE_URL",
	"connection.readonly_database_url": "KOMRAD_READONLY_DATABASE_URL",
	"connection.owner_database_url":    "KOMRAD_OWNER_DATABASE_URL",
	"connection.addr":                  "KOMRAD_ADDR",
	"connection.tmux_socket":           "KOMRAD_TMUX_SOCKET",
	"connection.cache_dir":             "KOMRAD_CACHE_DIR",
	"connection.state_dir":             "KOMRAD_STATE_DIR",
}

func bindEnvironment(cfg *viper.Viper) error {
	for key, variable := range connectionEnvironment {
		if err := cfg.BindEnv(key, variable); err != nil {
			return fmt.Errorf("bind %s to %s: %w", key, variable, err)
		}
	}

	return nil
}

func connectionSources(cfg *viper.Viper) map[string]Source {
	sources := make(map[string]Source, len(connectionEnvironment))
	for path, variable := range connectionEnvironment {
		key := strings.TrimPrefix(path, "connection.")
		if _, set := LookupEnv(variable); set {
			sources[key] = sourceEnvironment
		} else if cfg.InConfig(path) {
			sources[key] = sourceFile
		} else {
			sources[key] = sourceDefault
		}
	}

	return sources
}

func (c *Config) resolveDerived(report *Report) error {
	if c.Connection.CacheDir == "" {
		dir, err := os.UserCacheDir()
		if err != nil {
			return fmt.Errorf("failed to find a cache directory, set KOMRAD_CACHE_DIR to an absolute path komrad can write to: %w", err)
		}

		c.Connection.CacheDir = filepath.Join(dir, "komrad")
		report.Sources["cache_dir"] = sourceDerived
	} else {
		c.Connection.CacheDir = ExpandHome(c.Connection.CacheDir)
	}

	if c.Connection.StateDir == "" {
		dir, err := getDefaultStateDir()
		if err != nil {
			return err
		}

		c.Connection.StateDir = dir
		report.Sources["state_dir"] = sourceDerived
	} else {
		c.Connection.StateDir = ExpandHome(c.Connection.StateDir)
	}

	embeddedPostgres := GetEmbeddedPostgresDir(c.Connection.StateDir)
	if c.Connection.DatabaseURL == "" {
		c.Connection.DatabaseURL = BuildEmbeddedPostgresURL(embeddedPostgres, defaultEngineLogin, EmbeddedPostgresDatabase)
		report.Sources["database_url"] = sourceDerived
	}

	if c.Connection.ReadOnlyDatabaseURL == "" {
		c.Connection.ReadOnlyDatabaseURL = deriveReadOnlyURL(c.Connection.DatabaseURL)
		report.Sources["readonly_database_url"] = sourceDerived
	}

	if c.Connection.OwnerDatabaseURL == "" {
		c.Connection.OwnerDatabaseURL = c.Connection.DatabaseURL
		if report.UsesEmbeddedPostgres() {
			c.Connection.OwnerDatabaseURL = BuildEmbeddedPostgresURL(embeddedPostgres, EmbeddedPostgresOwner, EmbeddedPostgresDatabase)
		}
		report.Sources["owner_database_url"] = sourceDerived
	}

	return nil
}

// deriveReadOnlyURL is the url of the reader login belonging to the engine login databaseURL names.
func deriveReadOnlyURL(databaseURL string) string {
	parsed, err := url.Parse(databaseURL)
	if err != nil {
		return ""
	}

	if parsed.Scheme != "postgres" && parsed.Scheme != "postgresql" {
		return ""
	}

	if parsed.User == nil || parsed.User.Username() == "" {
		return ""
	}

	login := parsed.User.Username() + readerSuffix
	if password, set := parsed.User.Password(); set {
		parsed.User = url.UserPassword(login, password)
	} else {
		parsed.User = url.User(login)
	}

	return parsed.String()
}
