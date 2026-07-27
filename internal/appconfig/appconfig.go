// Package appconfig loads the user's customization from a TOML file via viper.
// It covers the default claude flags and the panel keybindings.
package appconfig

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/go-viper/mapstructure/v2"
	"github.com/spf13/viper"
)

// Keybindings scopes bindings to the screen they act in. Each action is a list of
// bubbletea key strings such as "enter", "ctrl+d", or "esc". An action left unset
// falls back to its built-in default.
type Keybindings struct {
	// Navigation, shared by every menu so movement is the same everywhere. These
	// avoid letter keys so they still work in the search text field.
	Nav NavKeys `mapstructure:"nav"`
	// The agents list, the panel's main screen.
	Agents AgentsKeys `mapstructure:"agents"`
	// The new-agent, rename, and workflow forms.
	Form FormKeys `mapstructure:"form"`
	// The actions that create and change workflows, from the agents list.
	Workflows WorkflowsKeys `mapstructure:"workflows"`
}

// NavKeys are the shared navigation bindings every menu uses, so movement is the
// same everywhere rather than configured per menu.
type NavKeys struct {
	// Move the cursor up.
	Up []string `mapstructure:"up"`
	// Move the cursor down.
	Down []string `mapstructure:"down"`
}

// FormKeys are the bindings for the new-agent and rename forms. Back and Quit both
// abort the form, split apart so they can be rebound separately.
type FormKeys struct {
	// Abort the form and quit the panel.
	Quit []string `mapstructure:"quit"`
	// Abort the form without quitting the panel.
	Back []string `mapstructure:"back"`
	// Submit the form. Plain enter belongs to the text inputs, so this is
	// deliberately something else.
	Submit []string `mapstructure:"submit"`
}

// AgentsKeys are the bindings for the agents list. Info toggles the hovered agent's
// stored record into the stage in place of the agent itself.
type AgentsKeys struct {
	// Quit the panel.
	Quit []string `mapstructure:"quit"`
	// Step out of a workflow scope, or quit at the root.
	Back []string `mapstructure:"back"`
	// Attach to the hovered agent.
	Open []string `mapstructure:"open"`
	// Start a new agent.
	New []string `mapstructure:"new"`
	// Rename the hovered agent.
	Rename []string `mapstructure:"rename"`
	// Delete the hovered agent.
	Delete []string `mapstructure:"delete"`
	// Filter the list by name.
	Search []string `mapstructure:"search"`
	// Swap the hovered agent's stored record into the stage in place of the agent.
	Info []string `mapstructure:"info"`
}

// WorkflowsKeys are the bindings that act on a workflow. New is global. Rename,
// edit, and delete apply while the cursor is on a workflow header.
type WorkflowsKeys struct {
	// Create a workflow. Works from anywhere in the list.
	New []string `mapstructure:"new"`
	// Rename the workflow under the cursor.
	Rename []string `mapstructure:"rename"`
	// Edit the workflow under the cursor.
	Edit []string `mapstructure:"edit"`
	// Delete the workflow under the cursor.
	Delete []string `mapstructure:"delete"`
}

// Config is the user's customization.
type Config struct {
	// Flags appended to the claude command for every new session.
	ClaudeFlags string `mapstructure:"claude_flags"`
	// Panel keybindings, scoped to the screen each action acts in.
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
//
// A key the Config struct does not know about is an error rather than something
// silently dropped, so a typo or a binding left behind by an older version says
// so instead of quietly doing nothing.
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
	if err := viperCfg.Unmarshal(&cfg, func(dc *mapstructure.DecoderConfig) {
		dc.ErrorUnused = true
	}); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	cfg.Keybindings = cfg.Keybindings.withDefaults()

	return &cfg, nil
}
