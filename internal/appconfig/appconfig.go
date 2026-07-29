// Package appconfig loads the user's customization from a TOML file via viper.
// It covers the default claude flags, the marks an agent can be given, and the
// panel keybindings.
package appconfig

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

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
	// Move the hovered agent to the next mark in the marks list, and past the last
	// one back to unmarked. Nothing but you ever sets a mark.
	Mark []string `mapstructure:"mark"`
	// Hide the hovered agent from this list, or unhide one while the hidden agents
	// are shown. Hiding stops nothing: the agent keeps running and every other view
	// still holds it. This holds h, which is why back on this screen is esc and left
	// alone.
	Hide []string `mapstructure:"hide"`
	// Show the hidden agents alongside the rest, and press it again to put them
	// away. While they are away each workflow header carries the count of what it is
	// not showing.
	ShowHidden []string `mapstructure:"show_hidden"`
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

// Layout is how the panel divides the popup between its own list and the stage
// pane beside it.
type Layout struct {
	// The share of the width the panel's list takes, as a fraction above zero and
	// below one. The stage showing whatever the cursor is on takes the rest, so the
	// default leaves the greater part to the preview, which is where there is
	// something to read.
	Split float64 `mapstructure:"split"`
}

// Config is the user's customization.
type Config struct {
	// Flags appended to the claude command for every new session.
	ClaudeFlags string `mapstructure:"claude_flags"`
	// How the popup's width is divided between the panel and the stage beside it.
	Layout Layout `mapstructure:"layout"`
	// The marks the mark key cycles through, in order. A mark is a glyph and means
	// whatever you decide, so the glyph is the label. Pressing mark advances the
	// hovered agent to the next one here, and past the last one back to unmarked,
	// which leaves a one-mark list behaving as a toggle.
	Marks []Mark `mapstructure:"marks"`
	// Panel keybindings, scoped to the screen each action acts in.
	Keybindings Keybindings `mapstructure:"keybindings"`
}

// Defaults is the configuration a user who has written nothing runs on. It is also
// what the generated config.toml spells out, so the file and an install without one
// agree by construction.
func Defaults() *Config {
	return &Config{
		Layout:      DefaultLayout(),
		Marks:       DefaultMarks(),
		Keybindings: DefaultKeybindings(),
	}
}

// DefaultSplit is the share of the width the panel's list takes when the config
// names none.
const DefaultSplit = 0.2

// DefaultLayout is the layout used when the config omits it.
func DefaultLayout() Layout {
	return Layout{Split: DefaultSplit}
}

// Mark is one of the marks the mark key steps through. The glyph is what a row draws
// and what the store keeps, so it is the mark's identity, while the colour is the
// panel's alone and is never stored.
//
// The json tags are the names get_config reports a mark under, spelled out because
// the encoder behind it follows the mapstructure tags as far as the slice and no
// further.
type Mark struct {
	Glyph string `mapstructure:"glyph" json:"glyph"`
	// A hex colour such as "#DCA561", either three or six digits. Left empty the
	// mark takes DefaultMarkColor.
	Color string `mapstructure:"color" json:"color"`
}

// DefaultMarkColor is the yellow a mark takes when its own colour is unset. The panel
// uses it for nothing else, so a mark left on it cannot be read as a lifecycle state.
// Marks coloured apart from each other are worth having, and keeping them clear of
// the state colours is then yours to do.
const DefaultMarkColor = "#DCA561"

// DefaultMarks are the marks stepped through when the config names none. A glyph says
// what it means to whoever reads the row, so a dot reads as worth coming back to, a
// half-filled circle as underway, and a check as dealt with. They share one colour,
// leaving the glyph to carry the meaning until someone colours them apart.
func DefaultMarks() []Mark {
	return []Mark{
		{Glyph: "●", Color: DefaultMarkColor},
		{Glyph: "◐", Color: DefaultMarkColor},
		{Glyph: "✓", Color: DefaultMarkColor},
	}
}

// DefaultKeybindings are the bindings used when the config omits them.
func DefaultKeybindings() Keybindings {
	return Keybindings{
		Nav: NavKeys{
			Up:   []string{"up", "ctrl+p"},
			Down: []string{"down", "ctrl+n"},
		},
		Agents: AgentsKeys{
			Quit:       []string{"ctrl+c", "q"},
			Back:       []string{"esc", "left"},
			Open:       []string{"enter", "l", "right"},
			New:        []string{"n"},
			Rename:     []string{"r"},
			Delete:     []string{"d"},
			Search:     []string{"/"},
			Info:       []string{"i"},
			Mark:       []string{"m"},
			Hide:       []string{"h"},
			ShowHidden: []string{"H"},
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

// maxMarkLen bounds a mark in bytes. A glyph runs to a handful of bytes even with
// modifiers on it, so anything longer is not the one character the panel can draw.
const maxMarkLen = 16

// hexColor matches the colours lipgloss reads, three or six digits behind a hash.
// Anything else it renders as no colour at all, so a typo would silently lose the
// mark's colour rather than say so.
var hexColor = regexp.MustCompile(`^#([0-9a-fA-F]{3}|[0-9a-fA-F]{6})$`)

// validateMarks rejects marks the panel could not draw. A glyph goes straight into a
// rendered frame, so whitespace or a control character would break the row it sits
// on rather than show up in it, and a glyph listed twice would never be steppable
// past its first appearance.
func validateMarks(marks []Mark) error {
	unprintable := func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }
	seen := make(map[string]bool, len(marks))

	for _, mark := range marks {
		switch {
		case mark.Glyph == "":
			return errors.New("a mark cannot be empty")
		case len(mark.Glyph) > maxMarkLen:
			return fmt.Errorf("mark %q is longer than %d bytes, so it is not one glyph", mark.Glyph, maxMarkLen)
		case strings.ContainsFunc(mark.Glyph, unprintable):
			return fmt.Errorf("mark %q carries whitespace or control characters", mark.Glyph)
		case seen[mark.Glyph]:
			return fmt.Errorf("mark %q is listed twice", mark.Glyph)
		case mark.Color != "" && !hexColor.MatchString(mark.Color):
			return fmt.Errorf("mark %q has colour %q, which is not a hex colour such as \"#DCA561\"", mark.Glyph, mark.Color)
		}

		seen[mark.Glyph] = true
	}

	return nil
}

// validateLayout rejects a split tmux could not lay the two panes out at. A share
// of one or more leaves the panel nothing, and a negative one is not a share at
// all. Zero is how an omitted key reads, so it is left to fall back to the default
// rather than reported as a mistake.
func validateLayout(layout Layout) error {
	if layout.Split < 0 || layout.Split >= 1 {
		return fmt.Errorf("layout split %v is not a fraction of the width above zero and below one", layout.Split)
	}

	return nil
}

func fillKeys(configured, def []string) []string {
	if len(configured) == 0 {
		return def
	}
	return configured
}

// validate rejects the settings the panel could not run on, so a config that
// cannot work says so when it is read rather than once something tries to draw
// with it.
func (c Config) validate() error {
	if err := validateMarks(c.Marks); err != nil {
		return err
	}

	return validateLayout(c.Layout)
}

// withDefaults fills anything the file left unset from the built-ins, so a user
// who writes part of a config keeps the defaults for the rest. An omitted value
// reads as its zero, which is why a blank marks list and a zero split are the
// unset cases here rather than choices to be honoured.
func (c Config) withDefaults() Config {
	c.Keybindings = c.Keybindings.withDefaults()
	if len(c.Marks) == 0 {
		c.Marks = DefaultMarks()
	}
	if c.Layout.Split == 0 {
		c.Layout = DefaultLayout()
	}

	return c
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
	k.Agents.Hide = fillKeys(k.Agents.Hide, def.Agents.Hide)
	k.Agents.ShowHidden = fillKeys(k.Agents.ShowHidden, def.Agents.ShowHidden)

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
			return Defaults(), nil
		}
		if os.IsNotExist(err) {
			return Defaults(), nil
		}

		return nil, fmt.Errorf("failed to read config %s: %w", file, err)
	}

	var cfg Config
	if err := viperCfg.Unmarshal(&cfg, func(dc *mapstructure.DecoderConfig) {
		dc.ErrorUnused = true
	}); err != nil {
		return nil, fmt.Errorf("failed to parse config %s: %w", file, err)
	}

	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("failed to parse config %s: %w", file, err)
	}

	cfg = cfg.withDefaults()

	return &cfg, nil
}
