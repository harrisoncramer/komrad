// Package config loads the user's customization from one or more TOML files via viper,
// merged in order so a later file wins. It covers which CLI agents run and the flags it
// takes, the tags an agent can be given, the panel keybindings, and which database, address
// and tmux server this weave is.
package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"weave/lib/xslice"

	"github.com/go-viper/mapstructure/v2"
	"github.com/spf13/viper"
)

// errLayerAbsent marks a config layer that is not on disk. It is answered rather than
// raised for the default path, since not having written a config is the ordinary install.
var errLayerAbsent = errors.New("config layer is absent")

const (
	readOnlyUser     = "weave_readonly"
	readOnlyPassword = "weave_readonly"
)

// Layer is one file in the stack, and whether it was asked for by name. A named layer that
// is not there is an error, since a path someone typed is a config they believe they have,
// while the default path is allowed to be absent because not writing one is the ordinary
// install.
type Layer struct {
	Path  string
	Named bool
}

// ResolveLayers is the files Load would read, weakest first. Given paths, that is exactly
// those paths, since naming any replaces the default rather than adding to it: being able
// to say "this file and nothing else" is what lets a second instance be isolated from the
// first one's config. Given none, it is the one default path.
//
// Weave never looks for a file it was not asked for. There is no sibling convention and
// nothing per directory, so which files are read depends on what was asked for rather than
// on what happens to be lying around.
//
// It is exported because a caller reporting on a config has to name the files, and a
// missing one has no error to read the path out of.
func ResolveLayers(paths []string) ([]Layer, error) {
	if len(paths) > 0 {
		layers := make([]Layer, 0, len(paths))
		for _, path := range paths {
			if strings.TrimSpace(path) == "" {
				return nil, errors.New("--config was given an empty path")
			}

			layers = append(layers, Layer{Path: ExpandHome(path), Named: true})
		}

		return layers, nil
	}

	dir, err := configHome()
	if err != nil {
		return nil, err
	}

	return []Layer{{Path: filepath.Join(dir, "config.toml")}}, nil
}

// Load reads the layers in order and merges them, weakest first, so a later file wins where
// two overlap. A missing file among them is not an error unless it was named, and yields
// nothing rather than the defaults, but a symlink pointing at nothing is, since that is a
// config someone meant to have.
//
// A key the Config struct does not know about is an error rather than silently dropped, so
// a typo is reported rather than quietly doing nothing. The error names the ordered file
// stack beside what the key's table does take.
func Load(paths []string) (*Config, error) {
	cfg, _, err := LoadWithReport(paths)

	return cfg, err
}

// Source is the precedence layer that supplied a resolved connection setting.
type Source string

const (
	SourceDefault     Source = "built-in default"
	SourceFile        Source = "config file"
	SourceEnvironment Source = "environment"
	SourceDerived     Source = "derived default"
)

// Report describes how a configuration was resolved without mixing provenance into Config.
type Report struct {
	Layers  []Layer
	Sources map[string]Source
}

// LoadWithReport resolves the configuration and reports where connection values came from.
func LoadWithReport(paths []string) (*Config, *Report, error) {
	layers, err := ResolveLayers(paths)
	if err != nil {
		return nil, nil, err
	}

	merged := viper.New()
	merged.SetConfigType("toml")
	if err := registerDefaults(merged); err != nil {
		return nil, nil, err
	}
	if err := bindEnvironment(merged); err != nil {
		return nil, nil, err
	}

	for _, layer := range layers {
		merged.SetConfigFile(layer.Path)

		if err := merged.MergeInConfig(); err != nil {
			if readErr := classifyLayerRead(layer, err); !errors.Is(readErr, errLayerAbsent) {
				return nil, nil, readErr
			}

			continue
		}
	}

	var cfg Config
	var decoded mapstructure.Metadata
	if err := merged.Unmarshal(&cfg, func(dc *mapstructure.DecoderConfig) {
		dc.Metadata = &decoded
	}); err != nil {
		return nil, nil, fmt.Errorf("failed to parse config %s: %w", describeLayers(layers), err)
	}
	if err := reportUnknownKeys(decoded.Unused); err != nil {
		return nil, nil, fmt.Errorf("failed to parse config %s: %w", describeLayers(layers), err)
	}

	cfg.normalizeZeroValues()

	report := &Report{Layers: layers, Sources: connectionSources(merged)}
	if err := cfg.resolveDerived(report); err != nil {
		return nil, nil, err
	}

	if err := cfg.validate(); err != nil {
		return nil, nil, fmt.Errorf("failed to parse config %s: %w", describeLayers(layers), err)
	}

	return &cfg, report, nil
}

// normalizeZeroValues keeps the settings whose zero has historically meant "unset" on
// their built-in values. Collections are deliberately excluded: an explicit empty list
// clears a list supplied by a default or lower layer.
func (c *Config) normalizeZeroValues() {
	if strings.TrimSpace(c.DefaultHarness) == "" {
		c.DefaultHarness = DefaultHarness
	}
	if c.MaxAgents == 0 {
		c.MaxAgents = DefaultMaxAgents
	}
	if c.Layout.SidebarWidth == 0 {
		c.Layout.SidebarWidth = DefaultSidebarWidth
	}
	if c.Layout.InfoHeight == 0 {
		c.Layout.InfoHeight = DefaultInfoHeight
	}
	if c.Layout.DetailsHeight == 0 {
		c.Layout.DetailsHeight = DefaultDetailsHeight
	}
	if strings.TrimSpace(c.Layout.SidebarSide) == "" {
		c.Layout.SidebarSide = DefaultSidebarSide
	}
	if strings.TrimSpace(c.Layout.InfoSide) == "" {
		c.Layout.InfoSide = DefaultInfoSide
	}
	if strings.TrimSpace(c.Log.Level) == "" {
		c.Log.Level = DefaultLogLevel
	}
	if strings.TrimSpace(c.Log.Viewer) == "" {
		c.Log.Viewer = DefaultLogViewer
	}
	if strings.TrimSpace(c.Log.ProcessViewer) == "" {
		c.Log.ProcessViewer = DefaultProcessViewer
	}
}

func registerDefaults(cfg *viper.Viper) error {
	values := map[string]any{}
	if err := mapstructure.Decode(Defaults(), &values); err != nil {
		return fmt.Errorf("register config defaults: %w", err)
	}

	registerDefaultValues(cfg, "", values)

	return nil
}

func registerDefaultValues(cfg *viper.Viper, prefix string, values map[string]any) {
	for key, value := range values {
		path := key
		if prefix != "" {
			path = prefix + "." + key
		}

		if nested, ok := value.(map[string]any); ok {
			registerDefaultValues(cfg, path, nested)
			continue
		}

		cfg.SetDefault(path, value)
	}
}

var connectionEnvironment = map[string]string{
	"connection.database_url":          "WEAVE_DATABASE_URL",
	"connection.readonly_database_url": "WEAVE_READONLY_DATABASE_URL",
	"connection.addr":                  "WEAVE_ADDR",
	"connection.tmux_socket":           "WEAVE_TMUX_SOCKET",
	"connection.cache_dir":             "WEAVE_CACHE_DIR",
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
			sources[key] = SourceEnvironment
		} else if cfg.InConfig(path) {
			sources[key] = SourceFile
		} else {
			sources[key] = SourceDefault
		}
	}

	return sources
}

func (c *Config) resolveDerived(report *Report) error {
	if c.Connection.CacheDir == "" {
		dir, err := os.UserCacheDir()
		if err != nil {
			return fmt.Errorf("failed to find a cache directory, set WEAVE_CACHE_DIR to an absolute path weave can write to: %w", err)
		}

		c.Connection.CacheDir = filepath.Join(dir, "weave")
		report.Sources["cache_dir"] = SourceDerived
	}

	if c.Connection.ReadOnlyDatabaseURL == "" {
		c.Connection.ReadOnlyDatabaseURL = deriveReadOnlyURL(c.Connection.DatabaseURL)
		report.Sources["readonly_database_url"] = SourceDerived
	}

	return nil
}

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

// classifyLayerRead turns a failure to read one layer into what the caller should do about
// it: errLayerAbsent when the file is simply not there and the layer contributes nothing,
// and anything else is fatal.
func classifyLayerRead(layer Layer, err error) error {
	var notFound viper.ConfigFileNotFoundError
	if errors.As(err, &notFound) && !layer.Named {
		return errLayerAbsent
	}

	if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("failed to read config %s: %w", layer.Path, err)
	}

	// A dangling symlink and a path with nothing at it fail the same way, so both arrive
	// here. Readlink does not follow the link and so succeeds only in the dangling case,
	// which is the one worth naming: a broken link is a config the user believes they have.
	if target, linkErr := os.Readlink(layer.Path); linkErr == nil {
		return fmt.Errorf("failed to read config %s: it is a symlink to %s, which does not exist", layer.Path, target)
	}

	// A file the caller named has to be there. Not having written one is the ordinary
	// install, but a path someone typed is a config they believe they have, and coming up
	// on the defaults instead looks exactly like a file that was read and then ignored.
	if layer.Named {
		return fmt.Errorf("failed to read config %s: there is no file there", layer.Path)
	}

	return errLayerAbsent
}

// describeLayers names the files a value could have come from, for the errors raised on the
// merged config where no single file is to blame.
func describeLayers(layers []Layer) string {
	return strings.Join(xslice.Map(layers, func(layer Layer) string { return layer.Path }), ", ")
}
