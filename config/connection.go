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
// ReadOnlyDatabaseURL and CacheDir have no constant because neither is a fixed string: one
// is derived from whichever database url wins and the other from the platform's cache
// directory, both after the file and the environment have been resolved.
const (
	// The default engine's login on the Postgres `mise run docker:up` starts and provisions,
	// so a checkout works with nothing exported.
	defaultDatabaseURL = "postgres://komrad_engine_default:komrad@localhost:5460/komrad?sslmode=disable"
	// The login that owns that Postgres's schema, used while database_url is the default.
	defaultOwnerDatabaseURL = "postgres://komrad:komrad@localhost:5460/komrad?sslmode=disable"
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
	DatabaseURL string `mapstructure:"database_url"`
	// The login that owns the schema, used only by `komrad migrate` and `komrad engine ensure`.
	// Left empty this is database_url, or the local owner while database_url is the default.
	OwnerDatabaseURL string `mapstructure:"owner_database_url"`
	// The least-privilege login the MCP query tools connect as.
	// Left empty this is database_url with the engine's reader login, which is its own login
	// with _reader on the end and the same password.
	ReadOnlyDatabaseURL string `mapstructure:"readonly_database_url"`
	// Where the server listens, and where the panel, the CLI and the MCP
	// endpoint all reach it. Change it to run two komrads on one machine. It has to be a
	// loopback host, since the api has no auth and its writes spawn agents.
	Addr string `mapstructure:"addr"`
	// The tmux socket komrad runs its own server on. Nothing in your normal tmux sees
	// that server, which is why `tmux -L komrad ls` is how you look at it directly.
	TmuxSocket string `mapstructure:"tmux_socket"`
	// Where komrad keeps what it generates: the server log, the built-in scripts it
	// writes out on every start, and the temp files a harness passes a prompt through.
	// Left empty this is ~/Library/Caches/komrad on a Mac and ~/.cache/komrad on Linux.
	// Everything under it is komrad's to delete and rewrite, so nothing of yours belongs here.
	CacheDir string `mapstructure:"cache_dir"`
	// Where komrad keeps its own files about agents: generated worktrees, released reviews
	// and forge credentials.
	// Left empty this is ~/.local/state/komrad.
	// It holds credentials and is komrad's to manage, so nothing of yours belongs here.
	StateDir string `mapstructure:"state_dir"`
	// The ssh destination the engine runs on, like user@host, when it is not this machine.
	// Left empty the engine is local and an agent's terminal is shown by moving its own pane
	// into the panel, which only works while the panel and the agents share a tmux server.
	SSH string `mapstructure:"ssh"`
	// How to forward a port from this machine to the engine, as an argv, when ssh is not how you
	// reach it. "{{local_port}}" and "{{remote_port}}" are replaced with the two ends. It should
	// stay in the foreground for as long as the forward is wanted.
	Forward []string `mapstructure:"forward"`
}

// defaultConnection is the instance a config that names none points at.
func defaultConnection() Connection {
	return Connection{
		DatabaseURL: defaultDatabaseURL,
		Addr:        defaultAddr,
		TmuxSocket:  defaultTmuxSocket,
	}
}

func (c *Connection) normalize() {
	if strings.TrimSpace(c.Addr) == "" {
		c.Addr = defaultAddr
	}
	if strings.TrimSpace(c.DatabaseURL) == "" {
		c.DatabaseURL = defaultDatabaseURL
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

	if slices.Contains(loopbackHosts, host) {
		return nil
	}

	parsed := net.ParseIP(host)
	if parsed == nil {
		return fmt.Errorf("%q is not an address this machine can answer on", host)
	}
	if !parsed.IsLoopback() {
		return fmt.Errorf("%q reaches this machine from the network, and the api has no auth and its writes spawn agents", host)
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
	if parsed.Host == "" {
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

	if c.Connection.ReadOnlyDatabaseURL == "" {
		c.Connection.ReadOnlyDatabaseURL = deriveReadOnlyURL(c.Connection.DatabaseURL)
		report.Sources["readonly_database_url"] = sourceDerived
	}

	if c.Connection.OwnerDatabaseURL == "" {
		c.Connection.OwnerDatabaseURL = c.Connection.DatabaseURL
		if report.Sources["database_url"] == sourceDefault {
			c.Connection.OwnerDatabaseURL = defaultOwnerDatabaseURL
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

	password, _ := parsed.User.Password()
	parsed.User = url.UserPassword(parsed.User.Username()+readerSuffix, password)

	return parsed.String()
}
