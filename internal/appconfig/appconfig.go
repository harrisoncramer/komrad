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
	// The schedules screen, and the key that opens it from the agents list.
	Schedules SchedulesKeys `mapstructure:"schedules"`
	// The trash screen, and the key that opens it from the agents list.
	Trash TrashKeys `mapstructure:"trash"`
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
	// Mark the hovered agent, or clear the mark when it already carries one. The
	// mark means whatever you decide, and nothing else ever sets it.
	Mark []string `mapstructure:"mark"`
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

// SchedulesKeys are the bindings for the schedules screen, which is its own view
// rather than part of the agents tree. Open enters it from the agents list, and
// the rest act on the hovered schedule once inside.
type SchedulesKeys struct {
	// Open the schedules screen from the agents list.
	Open []string `mapstructure:"open"`
	// Quit the panel.
	Quit []string `mapstructure:"quit"`
	// Leave the schedules screen for the agents list.
	Back []string `mapstructure:"back"`
	// Write a new schedule.
	New []string `mapstructure:"new"`
	// Edit the schedule under the cursor.
	Edit []string `mapstructure:"edit"`
	// Pause or resume the schedule under the cursor.
	Toggle []string `mapstructure:"toggle"`
	// Delete the schedule under the cursor. The agents it launched are kept.
	Delete []string `mapstructure:"delete"`
}

// TrashKeys are the bindings for the trash screen, which lists the agents
// deleted from the panel. Open enters it from the agents list, and the rest act
// on the hovered agent once inside.
type TrashKeys struct {
	// Open the trash from the agents list.
	Open []string `mapstructure:"open"`
	// Quit the panel.
	Quit []string `mapstructure:"quit"`
	// Leave the trash for the agents list.
	Back []string `mapstructure:"back"`
	// Put the agent under the cursor back in the panel.
	Restore []string `mapstructure:"restore"`
	// Destroy the agent under the cursor for good, with its transcript. This is
	// the one action in the panel nothing undoes, so it asks first.
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
			Mark:   []string{"m"},
		},
		Workflows: WorkflowsKeys{
			New:    []string{"W"},
			Rename: []string{"r"},
			Edit:   []string{"e"},
			Delete: []string{"d"},
		},
		Schedules: SchedulesKeys{
			Open:   []string{"s"},
			Quit:   []string{"ctrl+c", "q"},
			Back:   []string{"esc", "left", "h"},
			New:    []string{"n"},
			Edit:   []string{"e", "enter"},
			Toggle: []string{" "},
			Delete: []string{"d"},
		},
		Trash: TrashKeys{
			Open:    []string{"t"},
			Quit:    []string{"ctrl+c", "q"},
			Back:    []string{"esc", "left", "h"},
			Restore: []string{"r"},
			Delete:  []string{"d"},
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
	k.Agents.Mark = fillKeys(k.Agents.Mark, def.Agents.Mark)

	k.Workflows.New = fillKeys(k.Workflows.New, def.Workflows.New)
	k.Workflows.Rename = fillKeys(k.Workflows.Rename, def.Workflows.Rename)
	k.Workflows.Edit = fillKeys(k.Workflows.Edit, def.Workflows.Edit)
	k.Workflows.Delete = fillKeys(k.Workflows.Delete, def.Workflows.Delete)

	k.Schedules.Open = fillKeys(k.Schedules.Open, def.Schedules.Open)
	k.Schedules.Quit = fillKeys(k.Schedules.Quit, def.Schedules.Quit)
	k.Schedules.Back = fillKeys(k.Schedules.Back, def.Schedules.Back)
	k.Schedules.New = fillKeys(k.Schedules.New, def.Schedules.New)
	k.Schedules.Edit = fillKeys(k.Schedules.Edit, def.Schedules.Edit)
	k.Schedules.Toggle = fillKeys(k.Schedules.Toggle, def.Schedules.Toggle)
	k.Schedules.Delete = fillKeys(k.Schedules.Delete, def.Schedules.Delete)

	k.Trash.Open = fillKeys(k.Trash.Open, def.Trash.Open)
	k.Trash.Quit = fillKeys(k.Trash.Quit, def.Trash.Quit)
	k.Trash.Back = fillKeys(k.Trash.Back, def.Trash.Back)
	k.Trash.Restore = fillKeys(k.Trash.Restore, def.Trash.Restore)
	k.Trash.Delete = fillKeys(k.Trash.Delete, def.Trash.Delete)

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

	// The resolved path is named in the errors below. What went wrong is not
	// actionable without knowing which file to go and edit, and the default
	// location is not something the reader has necessarily seen written down.
	file := path
	if file == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("failed to resolve home directory: %w", err)
		}

		file = filepath.Join(home, ".config", "claude-agents", "config.toml")
	}

	viperCfg.SetConfigFile(file)

	if err := viperCfg.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); ok {
			return &Config{Keybindings: DefaultKeybindings()}, nil
		}
		if os.IsNotExist(err) {
			return &Config{Keybindings: DefaultKeybindings()}, nil
		}

		return nil, fmt.Errorf("failed to read config %s: %w", file, err)
	}

	var cfg Config
	if err := viperCfg.Unmarshal(&cfg, func(dc *mapstructure.DecoderConfig) {
		dc.ErrorUnused = true
	}); err != nil {
		return nil, fmt.Errorf("failed to parse config %s: %w", file, err)
	}

	cfg.Keybindings = cfg.Keybindings.withDefaults()

	return &cfg, nil
}
