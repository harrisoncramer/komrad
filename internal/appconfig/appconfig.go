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
	List   ListKeys   `mapstructure:"list"`
	Chats  ChatsKeys  `mapstructure:"chats"`
	Search SearchKeys `mapstructure:"search"`
	Form   FormKeys   `mapstructure:"form"`
}

// FormKeys are the bindings for the new-chat and rename forms. Back and Quit
// both abort the form. The split mirrors the other panels so users can rebind
// them separately.
type FormKeys struct {
	Quit   []string `mapstructure:"quit"`
	Back   []string `mapstructure:"back"`
	Submit []string `mapstructure:"submit"`
}

// ListKeys are the bindings for the picker list. New, Chats, and Search jump
// straight to a section from anywhere in the list, in addition to moving the
// cursor and selecting it.
type ListKeys struct {
	Quit   []string `mapstructure:"quit"`
	Back   []string `mapstructure:"back"`
	Up     []string `mapstructure:"up"`
	Down   []string `mapstructure:"down"`
	Select []string `mapstructure:"select"`
	New    []string `mapstructure:"new"`
	Chats  []string `mapstructure:"chats"`
	Search []string `mapstructure:"search"`
}

// SearchKeys are the bindings for the search screen. The query is a live text
// input, so navigation avoids letter keys, which are typed into the query.
type SearchKeys struct {
	Quit []string `mapstructure:"quit"`
	Back []string `mapstructure:"back"`
	Up   []string `mapstructure:"up"`
	Down []string `mapstructure:"down"`
	Open []string `mapstructure:"open"`
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
			New:    []string{"n"},
			Chats:  []string{"c"},
			Search: []string{"/"},
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
		Search: SearchKeys{
			Quit: []string{"ctrl+c"},
			Back: []string{"esc"},
			Up:   []string{"up", "ctrl+p"},
			Down: []string{"down", "ctrl+n"},
			Open: []string{"enter"},
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

	k.List.Quit = fillKeys(k.List.Quit, d.List.Quit)
	k.List.Back = fillKeys(k.List.Back, d.List.Back)
	k.List.Up = fillKeys(k.List.Up, d.List.Up)
	k.List.Down = fillKeys(k.List.Down, d.List.Down)
	k.List.Select = fillKeys(k.List.Select, d.List.Select)
	k.List.New = fillKeys(k.List.New, d.List.New)
	k.List.Chats = fillKeys(k.List.Chats, d.List.Chats)
	k.List.Search = fillKeys(k.List.Search, d.List.Search)

	k.Search.Quit = fillKeys(k.Search.Quit, d.Search.Quit)
	k.Search.Back = fillKeys(k.Search.Back, d.Search.Back)
	k.Search.Up = fillKeys(k.Search.Up, d.Search.Up)
	k.Search.Down = fillKeys(k.Search.Down, d.Search.Down)
	k.Search.Open = fillKeys(k.Search.Open, d.Search.Open)

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
