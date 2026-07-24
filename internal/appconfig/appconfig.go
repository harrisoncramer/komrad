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
	Chats     ChatsKeys     `mapstructure:"chats"`
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

// FormKeys are the bindings for the new-chat and rename forms. Back and Quit
// both abort the form. The split mirrors the other panels so users can rebind
// them separately.
type FormKeys struct {
	Quit   []string `mapstructure:"quit"`
	Back   []string `mapstructure:"back"`
	Submit []string `mapstructure:"submit"`
}

// ChatsKeys are the bindings for the split chats view.
type ChatsKeys struct {
	Quit   []string `mapstructure:"quit"`
	Back   []string `mapstructure:"back"`
	Open   []string `mapstructure:"open"`
	New    []string `mapstructure:"new"`
	Rename []string `mapstructure:"rename"`
	Delete []string `mapstructure:"delete"`
	Move   []string `mapstructure:"move"`
}

// WorkflowsKeys are the bindings for the workflow picker. It mirrors the chats
// view, since it is the top-level view scoped to one bucket of work.
type WorkflowsKeys struct {
	Quit   []string `mapstructure:"quit"`
	Back   []string `mapstructure:"back"`
	Open   []string `mapstructure:"open"`
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
		Chats: ChatsKeys{
			Quit:   []string{"ctrl+c", "q"},
			Back:   []string{"esc", "left", "h"},
			Open:   []string{"enter", "l", "right"},
			New:    []string{"n"},
			Rename: []string{"r"},
			Delete: []string{"d"},
			Move:   []string{"m"},
		},
		Workflows: WorkflowsKeys{
			Quit:   []string{"ctrl+c", "q"},
			Back:   []string{"esc", "left", "h"},
			Open:   []string{"enter", "l", "right"},
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

func fillKeys(v, def []string) []string {
	if len(v) == 0 {
		return def
	}
	return v
}

// withDefaults fills any unset binding from the defaults, so a user who sets
// only some keys keeps the built-ins for the rest.
func (k Keybindings) withDefaults() Keybindings {
	d := DefaultKeybindings()

	k.Nav.Up = fillKeys(k.Nav.Up, d.Nav.Up)
	k.Nav.Down = fillKeys(k.Nav.Down, d.Nav.Down)

	k.Chats.Quit = fillKeys(k.Chats.Quit, d.Chats.Quit)
	k.Chats.Back = fillKeys(k.Chats.Back, d.Chats.Back)
	k.Chats.Open = fillKeys(k.Chats.Open, d.Chats.Open)
	k.Chats.New = fillKeys(k.Chats.New, d.Chats.New)
	k.Chats.Rename = fillKeys(k.Chats.Rename, d.Chats.Rename)
	k.Chats.Delete = fillKeys(k.Chats.Delete, d.Chats.Delete)
	k.Chats.Move = fillKeys(k.Chats.Move, d.Chats.Move)

	k.Workflows.Quit = fillKeys(k.Workflows.Quit, d.Workflows.Quit)
	k.Workflows.Back = fillKeys(k.Workflows.Back, d.Workflows.Back)
	k.Workflows.Open = fillKeys(k.Workflows.Open, d.Workflows.Open)
	k.Workflows.New = fillKeys(k.Workflows.New, d.Workflows.New)
	k.Workflows.Rename = fillKeys(k.Workflows.Rename, d.Workflows.Rename)
	k.Workflows.Edit = fillKeys(k.Workflows.Edit, d.Workflows.Edit)
	k.Workflows.Delete = fillKeys(k.Workflows.Delete, d.Workflows.Delete)

	k.Form.Quit = fillKeys(k.Form.Quit, d.Form.Quit)
	k.Form.Back = fillKeys(k.Form.Back, d.Form.Back)
	k.Form.Submit = fillKeys(k.Form.Submit, d.Form.Submit)

	return k
}

// Load reads the config from path when given, otherwise from the default
// location (~/.config/claude-agents/config.toml). A missing file is not an
// error. It yields a zero-value Config.
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
			return &Config{Keybindings: DefaultKeybindings()}, nil
		}
		if os.IsNotExist(err) {
			return &Config{Keybindings: DefaultKeybindings()}, nil
		}

		return nil, fmt.Errorf("failed to read config: %w", err)
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	cfg.Keybindings = cfg.Keybindings.withDefaults()

	return &cfg, nil
}
