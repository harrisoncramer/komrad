// Package appconfig loads the user's customization from a TOML file via viper.
// It covers the default claude flags and the lifecycle hook script paths.
package appconfig

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/viper"
)

// Hooks are paths to scripts run around a session's lifecycle. Each is optional.
// PreCreate runs before launch with the session name and requested directory,
// and may print the directory to actually use (e.g. a freshly created worktree).
type Hooks struct {
	PreCreate  string `mapstructure:"pre_create"`
	PostCreate string `mapstructure:"post_create"`
	OnDelete   string `mapstructure:"on_delete"`
}

// Config is the user's customization.
type Config struct {
	ClaudeFlags string `mapstructure:"claude_flags"`
	// AgentWidth and AgentHeight lock every agent session to a fixed size so its
	// capture fits the preview box exactly.
	AgentWidth  int   `mapstructure:"agent_width"`
	AgentHeight int   `mapstructure:"agent_height"`
	Hooks       Hooks `mapstructure:"hooks"`
}

const (
	defaultAgentWidth  = 120
	defaultAgentHeight = 40
)

// Load reads the config from path when given, otherwise from the default
// location (~/.config/claude-agents/config.toml). A missing file is not an
// error; it yields a zero-value Config.
func Load(path string) (*Config, error) {
	v := viper.New()
	v.SetConfigType("toml")

	if path != "" {
		v.SetConfigFile(path)
	} else {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("failed to resolve home directory: %w", err)
		}

		v.SetConfigFile(filepath.Join(home, ".config", "claude-agents", "config.toml"))
	}

	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); ok {
			return withDefaults(&Config{}), nil
		}
		if os.IsNotExist(err) {
			return withDefaults(&Config{}), nil
		}

		return nil, fmt.Errorf("failed to read config: %w", err)
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	return withDefaults(&cfg), nil
}

func withDefaults(cfg *Config) *Config {
	if cfg.AgentWidth <= 0 {
		cfg.AgentWidth = defaultAgentWidth
	}
	if cfg.AgentHeight <= 0 {
		cfg.AgentHeight = defaultAgentHeight
	}

	return cfg
}
