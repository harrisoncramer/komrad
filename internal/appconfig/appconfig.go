// Package appconfig loads the user's customization from a TOML file via viper.
// It covers which CLI agents run and the flags it takes, the marks an agent can be
// given, and the panel keybindings.
package appconfig

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"unicode"

	"github.com/go-viper/mapstructure/v2"
	"github.com/spf13/viper"
)

// Keybindings scopes bindings to the screen they act in. Each binding is a list of
// bubbletea key strings such as "enter", "ctrl+d", or "esc". A binding left unset
// falls back to its built-in default.
type Keybindings struct {
	// Navigation, shared by every menu so movement is the same everywhere. These
	// avoid letter keys so they still work in the search text field.
	Nav NavKeys `mapstructure:"nav"`
	// The agents list, the panel's main screen.
	Agents AgentsKeys `mapstructure:"agents"`
	// The new-agent, rename, and workflow forms.
	Form FormKeys `mapstructure:"form"`
	// The bindings that create and change workflows, from the agents list.
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
	// the one thing in the panel nothing undoes, so it asks first.
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

// Harness is how one agent CLI is configured.
type Harness struct {
	// Flags appended to every agent's command line for this CLI, ahead of the flags
	// that agent carries itself. Flag vocabularies do not overlap between CLIs, which
	// is why they are configured per harness rather than once.
	Flags string `mapstructure:"flags"`
}

// Codex is how the Codex CLI is configured. It carries one setting of its own,
// since Codex asks a question at startup that no other CLI here does.
type Codex struct {
	// Flags appended to every Codex agent's command line, ahead of the flags that
	// agent carries itself.
	Flags string `mapstructure:"flags"`
	// Whether to record each agent's directory as trusted in Codex's own config
	// before launching it. Codex asks whether a directory is trusted the first time it
	// runs there and waits for an answer, which it reads only from that file, so an
	// agent in a fresh worktree otherwise starts blocked and shows here as waiting.
	// Turning this on edits ~/.codex/config.toml on your behalf, adding nothing but a
	// trust entry per directory and never changing one you set yourself.
	TrustLaunchDirs bool `mapstructure:"trust_launch_dirs"`
}

// Harnesses is the per-CLI configuration. There is a field per harness the binary
// implements rather than an open table, so a misspelled name is refused by the same
// unknown-key check as everything else here, and this file lists what can be run.
type Harnesses struct {
	Claude Harness `mapstructure:"claude"`
	Codex  Codex   `mapstructure:"codex"`
}

// Config is the user's customization.
type Config struct {
	// Which agent CLI a new agent runs when neither the agent nor its workflow names
	// one. It has to be a harness this binary implements, so it is one of the names
	// under [harness] below.
	DefaultHarness string `mapstructure:"default_harness"`
	// Per-CLI configuration, which is where the flags an agent of that CLI inherits
	// live.
	Harness Harnesses `mapstructure:"harness"`
	// How the popup's width is divided between the panel and the stage beside it.
	Layout Layout `mapstructure:"layout"`
	// The marks the mark key cycles through, in order. A mark is a glyph and means
	// whatever you decide, so the glyph is the label. Pressing mark advances the
	// hovered agent to the next one here, and past the last one back to unmarked,
	// which leaves a one-mark list behaving as a toggle. A colour is either one of the
	// sixteen your terminal's own scheme maps, named as "red" or "brightred", or a hex
	// value such as "#DCA561". The panel draws itself from the sixteen, so a name is
	// what keeps a mark in the same scheme as everything around it.
	Marks []Mark `mapstructure:"marks"`
	// Panel keybindings, scoped to the screen each binding acts in.
	Keybindings Keybindings `mapstructure:"keybindings"`
}

// Defaults is the configuration a user who has written nothing runs on. It is also
// what the generated config.toml spells out, so the file and an install without one
// agree by construction.
func Defaults() *Config {
	return &Config{
		DefaultHarness: DefaultHarness,
		Layout:         DefaultLayout(),
		Marks:          DefaultMarks(),
		Keybindings:    DefaultKeybindings(),
	}
}

// DefaultHarness is the CLI a new agent runs when the config names none. It is
// spelled here rather than taken from the registry because the registry is built
// from what the binary implements, and this is a default a user overrides.
const DefaultHarness = "claude"

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
	// The colour the glyph is drawn in, either one of the sixteen a terminal's own
	// scheme maps, named as "red" or "brightred", or a hex value such as "#DCA561" with
	// three or six digits. A name follows whatever your colour scheme holds in that
	// slot, the way the rest of the panel does, while hex is the same in every terminal.
	// Left empty the mark takes DefaultMarkColor.
	Color string `mapstructure:"color" json:"color"`
}

// DefaultMarkColor is the yellow a mark takes when its own colour is unset. It is the
// plain yellow rather than the bright one the panel draws a thinking agent in, so a mark
// left on the default cannot be read as a lifecycle state.
const DefaultMarkColor = "yellow"

// ansiColors are the sixteen colours a terminal's scheme maps, keyed by the name a config
// names one by and valued by the index lipgloss draws it as. They are the palette the
// panel itself is drawn from, so a mark named this way sits in the same scheme as the
// rows around it.
var ansiColors = map[string]string{
	"black":         "0",
	"red":           "1",
	"green":         "2",
	"yellow":        "3",
	"blue":          "4",
	"magenta":       "5",
	"cyan":          "6",
	"white":         "7",
	"brightblack":   "8",
	"brightred":     "9",
	"brightgreen":   "10",
	"brightyellow":  "11",
	"brightblue":    "12",
	"brightmagenta": "13",
	"brightcyan":    "14",
	"brightwhite":   "15",
}

// ResolveColor is a configured colour in the form lipgloss reads it. A name becomes its
// index, which is what lipgloss maps onto the terminal's own scheme, and a hex value
// passes through as it is.
func ResolveColor(color string) string {
	if index, ok := ansiColors[color]; ok {
		return index
	}

	return color
}

// DefaultMarks are the marks stepped through when the config names none. They share
// one colour, leaving the glyph to carry the meaning until someone colours them
// apart.
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
			Submit: []string{"ctrl+s"},
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

// validColor reports whether the panel could draw a colour. The names are a closed set
// rather than anything the terminal might accept, so a colour spelled wrongly is refused
// here instead of drawing as no colour at all.
func validColor(color string) bool {
	if _, ok := ansiColors[color]; ok {
		return true
	}

	return hexColor.MatchString(color)
}

// validateMarks rejects marks the panel could not draw. A glyph goes straight into a
// rendered frame, so whitespace or a control character would break the row it sits on,
// and a glyph listed twice would never be steppable past its first appearance.
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
		case mark.Color != "" && !validColor(mark.Color):
			return fmt.Errorf("mark %q has colour %q, which is neither one of the sixteen such as \"yellow\" nor a hex colour such as \"#DCA561\"", mark.Glyph, mark.Color)
		}

		seen[mark.Glyph] = true
	}

	return nil
}

// validateLayout rejects a split tmux could not lay the two panes out at. Zero is how
// an omitted key reads, so it falls back to the default rather than being reported as
// a mistake.
func validateLayout(layout Layout) error {
	if layout.Split < 0 || layout.Split >= 1 {
		return fmt.Errorf("layout split %v is not a fraction of the width above zero and below one", layout.Split)
	}

	return nil
}

// validate rejects the settings the panel could not run on, so a config that cannot
// work says so when it is read rather than once something tries to draw with it.
func (c Config) validate() error {
	if err := validateMarks(c.Marks); err != nil {
		return err
	}

	return validateLayout(c.Layout)
}

// withDefaults fills anything the file left unset from the built-ins. An omitted value
// reads as its zero, which is why a blank marks list and a zero split are the unset
// cases here rather than choices to be honoured.
func (c Config) withDefaults() Config {
	c.Keybindings = c.Keybindings.withDefaults()
	// Every launch resolves through this id, so leaving it empty would refuse every
	// create rather than fall back to anything.
	if strings.TrimSpace(c.DefaultHarness) == "" {
		c.DefaultHarness = DefaultHarness
	}
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
	filled := k
	fillBindings(reflect.ValueOf(&filled).Elem(), reflect.ValueOf(DefaultKeybindings()))

	return filled
}

// fillBindings copies the default into every key list the config left empty, walking
// the groups of bindings as it goes. A group is a struct and a binding is a slice of
// keys, which are the two cases below. It reads the struct rather than naming each
// binding, so adding one is an edit to the struct and nothing else.
func fillBindings(configured, def reflect.Value) {
	for i := range configured.NumField() {
		field := configured.Field(i)

		if field.Kind() == reflect.Struct {
			fillBindings(field, def.Field(i))

			continue
		}
		if field.Kind() == reflect.Slice && field.Len() == 0 {
			field.Set(def.Field(i))
		}
	}
}

// Load reads the config from path when given, otherwise from the default
// location ~/.config/weave/config.toml. A missing file is not an error
// and yields the default keybindings, but a symlink standing where the file
// should be and resolving to nothing is, since that is a config someone meant
// to have rather than one they never wrote.
//
// A key the Config struct does not know about is an error rather than something
// silently dropped, so a typo or a binding left behind by an older version says
// so instead of quietly doing nothing.
func Load(path string) (*Config, error) {
	viperCfg := viper.New()
	viperCfg.SetConfigType("toml")

	// The resolved path is named in the errors below, since what went wrong is not
	// actionable without knowing which file to go and edit.
	file := path
	if file == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("failed to resolve home directory: %w", err)
		}

		file = filepath.Join(home, ".config", "weave", "config.toml")
	}

	viperCfg.SetConfigFile(file)

	if err := viperCfg.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); ok {
			return Defaults(), nil
		}
		if os.IsNotExist(err) {
			// Reading through a symlink that resolves to nothing fails the same way as
			// reading a path with nothing at it, so both arrive here and the defaults
			// would go back for either. Readlink does not follow the link, and so
			// succeeds only in the dangling case, which is the one worth naming: a
			// broken link is a config the user believes they have, and silence about it
			// looks exactly like a machine that was never configured.
			if target, linkErr := os.Readlink(file); linkErr == nil {
				return nil, fmt.Errorf("failed to read config %s: it is a symlink to %s, which does not exist", file, target)
			}

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
