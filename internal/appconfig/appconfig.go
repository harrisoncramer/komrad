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

// Keybindings scopes bindings to the panel they act in. Each panel's actions
// are a list of bubbletea key strings (e.g. "enter", "ctrl+d", "esc"), and any
// action left unset falls back to its built-in default.
type Keybindings struct {
	List  ListKeys  `mapstructure:"list"`
	Chats ChatsKeys `mapstructure:"chats"`
	Form  FormKeys  `mapstructure:"form"`
}

// FormKeys are the bindings for the new-chat and rename forms. Back and Quit
// both abort the form; the split mirrors the other panels so users can rebind
// them separately.
type FormKeys struct {
	Quit []string `mapstructure:"quit"`
	Back []string `mapstructure:"back"`
}

// ListKeys are the bindings for the picker list.
type ListKeys struct {
	Quit   []string `mapstructure:"quit"`
	Back   []string `mapstructure:"back"`
	Up     []string `mapstructure:"up"`
	Down   []string `mapstructure:"down"`
	Select []string `mapstructure:"select"`
}

// ChatsKeys are the bindings for the split chats view.
type ChatsKeys struct {
	Quit        []string `mapstructure:"quit"`
	Back        []string `mapstructure:"back"`
	Up          []string `mapstructure:"up"`
	Down        []string `mapstructure:"down"`
	Open        []string `mapstructure:"open"`
	Rename      []string `mapstructure:"rename"`
	Delete      []string `mapstructure:"delete"`
	PreviewDown []string `mapstructure:"preview_down"`
	PreviewUp   []string `mapstructure:"preview_up"`
}

// Config is the user's customization.
type Config struct {
	ClaudeFlags string      `mapstructure:"claude_flags"`
	Hooks       Hooks       `mapstructure:"hooks"`
	Keybindings Keybindings `mapstructure:"keybindings"`
}

// DefaultKeybindings are the bindings used when the config omits them.
func DefaultKeybindings() Keybindings {
	return Keybindings{
		List: ListKeys{
			Quit:   []string{"ctrl+c", "q"},
			Back:   []string{"esc", "left", "h"},
			Up:     []string{"up", "k"},
			Down:   []string{"down", "j"},
			Select: []string{"enter"},
		},
		Chats: ChatsKeys{
			Quit:        []string{"ctrl+c", "q"},
			Back:        []string{"esc", "left", "h"},
			Up:          []string{"up", "k"},
			Down:        []string{"down", "j"},
			Open:        []string{"enter", "l", "right"},
			Rename:      []string{"r"},
			Delete:      []string{"d"},
			PreviewDown: []string{"ctrl+d"},
			PreviewUp:   []string{"ctrl+u"},
		},
		Form: FormKeys{
			Quit: []string{"ctrl+c"},
			Back: []string{"esc"},
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

	k.List.Quit = fillKeys(k.List.Quit, d.List.Quit)
	k.List.Back = fillKeys(k.List.Back, d.List.Back)
	k.List.Up = fillKeys(k.List.Up, d.List.Up)
	k.List.Down = fillKeys(k.List.Down, d.List.Down)
	k.List.Select = fillKeys(k.List.Select, d.List.Select)

	k.Chats.Quit = fillKeys(k.Chats.Quit, d.Chats.Quit)
	k.Chats.Back = fillKeys(k.Chats.Back, d.Chats.Back)
	k.Chats.Up = fillKeys(k.Chats.Up, d.Chats.Up)
	k.Chats.Down = fillKeys(k.Chats.Down, d.Chats.Down)
	k.Chats.Open = fillKeys(k.Chats.Open, d.Chats.Open)
	k.Chats.Rename = fillKeys(k.Chats.Rename, d.Chats.Rename)
	k.Chats.Delete = fillKeys(k.Chats.Delete, d.Chats.Delete)
	k.Chats.PreviewDown = fillKeys(k.Chats.PreviewDown, d.Chats.PreviewDown)
	k.Chats.PreviewUp = fillKeys(k.Chats.PreviewUp, d.Chats.PreviewUp)

	k.Form.Quit = fillKeys(k.Form.Quit, d.Form.Quit)
	k.Form.Back = fillKeys(k.Form.Back, d.Form.Back)

	return k
}

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
