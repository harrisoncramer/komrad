// Package appconfig loads the user's customization from a TOML file via viper.
// It covers the default claude flags and the panel keybindings.
package appconfig

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/viper"
)

// Keybindings scopes bindings to the panel they act in. Each panel's actions
// are a list of bubbletea key strings (e.g. "enter", "ctrl+d", "esc"), and any
// action left unset falls back to its built-in default.
type Keybindings struct {
	Nav       NavKeys       `mapstructure:"nav"`
	Agents    AgentsKeys    `mapstructure:"agents"`
	Form      FormKeys      `mapstructure:"form"`
	Workflows WorkflowsKeys `mapstructure:"workflows"`
}

// NavKeys are the shared navigation bindings used by every menu, so movement is
// the same everywhere rather than configured per menu. Up and Down move the
// cursor.
type NavKeys struct {
	Up   []string `mapstructure:"up"`
	Down []string `mapstructure:"down"`
}

// FormKeys are the bindings for the new-agent and rename forms. Back and Quit
// both abort the form. The split mirrors the other panels so users can rebind
// them separately.
type FormKeys struct {
	Quit   []string `mapstructure:"quit"`
	Back   []string `mapstructure:"back"`
	Submit []string `mapstructure:"submit"`
}

// AgentsKeys are the bindings for the agents list, the panel's only view. Info
// toggles the hovered agent's stored record into the stage in place of the agent
// itself.
type AgentsKeys struct {
	Quit   []string `mapstructure:"quit"`
	Back   []string `mapstructure:"back"`
	Open   []string `mapstructure:"open"`
	New    []string `mapstructure:"new"`
	Rename []string `mapstructure:"rename"`
	Delete []string `mapstructure:"delete"`
	Search []string `mapstructure:"search"`
	Info   []string `mapstructure:"info"`
}

// WorkflowsKeys are the bindings that act on a workflow. New is global. Rename,
// edit, and delete apply while the cursor is on a workflow header.
type WorkflowsKeys struct {
	New    []string `mapstructure:"new"`
	Rename []string `mapstructure:"rename"`
	Edit   []string `mapstructure:"edit"`
	Delete []string `mapstructure:"delete"`
}

// Config is the user's customization.
type Config struct {
	ClaudeFlags string      `mapstructure:"claude_flags"`
	Keybindings Keybindings `mapstructure:"keybindings"`
}

// DefaultKeybindings are the bindings used when the config omits them.
func DefaultKeybindings() Keybindings {
	return Keybindings{
		Nav: NavKeys{
			Up:   []string{"up", "ctrl+p"},
			Down: []string{"down", "ctrl+n"},
		},
		Agents: AgentsKeys{
			Quit:   []string{"ctrl+c", "q"},
			Back:   []string{"esc", "left", "h"},
			Open:   []string{"enter", "l", "right"},
			New:    []string{"n"},
			Rename: []string{"r"},
			Delete: []string{"d"},
			Search: []string{"/"},
			Info:   []string{"i"},
		},
		Workflows: WorkflowsKeys{
			New:    []string{"W"},
			Rename: []string{"r"},
			Edit:   []string{"e"},
			Delete: []string{"d"},
		},
		Form: FormKeys{
			Quit:   []string{"ctrl+c"},
			Back:   []string{"esc"},
			Submit: []string{"shift+enter"},
		},
	}
}

func fillKeys(configured, def []string) []string {
	if len(configured) == 0 {
		return def
	}
	return configured
}

// withDefaults fills any unset binding from the defaults, so a user who sets
// only some keys keeps the built-ins for the rest.
func (k Keybindings) withDefaults() Keybindings {
	def := DefaultKeybindings()

	k.Nav.Up = fillKeys(k.Nav.Up, def.Nav.Up)
	k.Nav.Down = fillKeys(k.Nav.Down, def.Nav.Down)

	k.Agents.Quit = fillKeys(k.Agents.Quit, def.Agents.Quit)
	k.Agents.Back = fillKeys(k.Agents.Back, def.Agents.Back)
	k.Agents.Open = fillKeys(k.Agents.Open, def.Agents.Open)
	k.Agents.New = fillKeys(k.Agents.New, def.Agents.New)
	k.Agents.Rename = fillKeys(k.Agents.Rename, def.Agents.Rename)
	k.Agents.Delete = fillKeys(k.Agents.Delete, def.Agents.Delete)
	k.Agents.Search = fillKeys(k.Agents.Search, def.Agents.Search)
	k.Agents.Info = fillKeys(k.Agents.Info, def.Agents.Info)

	k.Workflows.New = fillKeys(k.Workflows.New, def.Workflows.New)
	k.Workflows.Rename = fillKeys(k.Workflows.Rename, def.Workflows.Rename)
	k.Workflows.Edit = fillKeys(k.Workflows.Edit, def.Workflows.Edit)
	k.Workflows.Delete = fillKeys(k.Workflows.Delete, def.Workflows.Delete)

	k.Form.Quit = fillKeys(k.Form.Quit, def.Form.Quit)
	k.Form.Back = fillKeys(k.Form.Back, def.Form.Back)
	k.Form.Submit = fillKeys(k.Form.Submit, def.Form.Submit)

	return k
}

// Load reads the config from path when given, otherwise from the default
// location ~/.config/claude-agents/config.toml. A missing file is not an error
// and yields the default keybindings.
func Load(path string) (*Config, error) {
	viperCfg := viper.New()
	viperCfg.SetConfigType("toml")

	if path != "" {
		viperCfg.SetConfigFile(path)
	} else {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("failed to resolve home directory: %w", err)
		}

		viperCfg.SetConfigFile(filepath.Join(home, ".config", "claude-agents", "config.toml"))
	}

	if err := viperCfg.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); ok {
			return &Config{Keybindings: DefaultKeybindings()}, nil
		}
		if os.IsNotExist(err) {
			return &Config{Keybindings: DefaultKeybindings()}, nil
		}

		return nil, fmt.Errorf("failed to read config: %w", err)
	}

	var cfg Config
	if err := viperCfg.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	cfg.Keybindings = cfg.Keybindings.withDefaults()

	return &cfg, nil
}
