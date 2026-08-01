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
	"slices"
	"strings"
	"unicode"

	"github.com/go-viper/mapstructure/v2"
	"github.com/spf13/viper"
)

// Keybindings scopes bindings to the screen they act in. Each binding is a list of
// bubbletea key strings such as "enter", "ctrl+d", or "esc". Every default is a
// single key, leaving the alternatives to you. A binding left unset falls back to
// its built-in default.
type Keybindings struct {
	// Navigation, shared by every menu so movement is the same everywhere.
	Nav NavKeys `mapstructure:"nav"`
	// The key that draws the legend under a list, shared by every screen for the same
	// reason navigation is.
	Help HelpKeys `mapstructure:"help"`
	// The agents list, the panel's main screen.
	Agents AgentsKeys `mapstructure:"agents"`
	// The new-agent, rename, and workflow forms.
	Form FormKeys `mapstructure:"form"`
	// The bindings that create and change workflows, from the agents list.
	Workflows WorkflowsKeys `mapstructure:"workflows"`
	// The schedules screen, and the key that opens it from the agents list.
	Schedules SchedulesKeys `mapstructure:"schedules"`
	// The archive screen, and the key that opens it from the agents list.
	Archive ArchiveKeys `mapstructure:"archive"`
	// The actions list, and the key that opens it on the hovered agent.
	Actions ActionsKeys `mapstructure:"actions"`
}

// NavKeys are the shared navigation bindings every menu uses, so movement is the
// same everywhere rather than configured per menu.
type NavKeys struct {
	// Move the cursor up.
	Up []string `mapstructure:"up"`
	// Move the cursor down.
	Down []string `mapstructure:"down"`
}

// HelpKeys is the binding that draws the legend under a list. It is its own group
// rather than one of a screen's, since it acts the same on all of them.
type HelpKeys struct {
	// Toggle for the keybindings
	Toggle []string `mapstructure:"toggle"`
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
	// Open the hovered agent's directory in whatever EDITOR names. The window is created
	// on your own tmux server rather than the panel's, and the panel closes behind it, so
	// you are left in the editor.
	Editor []string `mapstructure:"editor"`
	// Start a new agent.
	New []string `mapstructure:"new"`
	// Rename the hovered agent.
	Rename []string `mapstructure:"rename"`
	// Archive the hovered agent, which stops it and moves it to the archive screen
	// rather than destroying it.
	Archive []string `mapstructure:"archive"`
	// Filter the list by name.
	Search []string `mapstructure:"search"`
	// Swap the hovered agent's stored record into the stage in place of the agent.
	Info []string `mapstructure:"info"`
	// Move the hovered agent to the next mark in the marks list, and past the last
	// one back to unmarked. Nothing but you ever sets a mark.
	Mark []string `mapstructure:"mark"`
	// Hide the hovered agent from this list, or unhide one while the hidden agents
	// are shown. Hiding stops nothing: the agent keeps running and every other view
	// still holds it.
	Hide []string `mapstructure:"hide"`
	// Show the hidden agents alongside the rest, and press it again to put them
	// away. While they are away each workflow header carries the count of what it is
	// not showing.
	ShowHidden []string `mapstructure:"show_hidden"`
	// Show the background service's log in the stage, in place of the row under the cursor,
	// and press it again to put the row back. The service is spawned detached and writes to
	// a file, so nothing else shows what it is doing.
	Logs []string `mapstructure:"logs"`
}

// WorkflowsKeys are the bindings that act on a workflow. New is global. Rename,
// edit, and archive apply while the cursor is on a workflow header.
type WorkflowsKeys struct {
	// Create a workflow. Works from anywhere in the list.
	New []string `mapstructure:"new"`
	// Rename the workflow under the cursor.
	Rename []string `mapstructure:"rename"`
	// Edit the workflow under the cursor.
	Edit []string `mapstructure:"edit"`
	// Archive the workflow under the cursor, which archives the agents inside it.
	Archive []string `mapstructure:"archive"`
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
	// Archive the schedule under the cursor, which stops it firing. The agents it
	// launched are kept, and nothing brings the schedule back.
	Archive []string `mapstructure:"archive"`
}

// ArchiveKeys are the bindings for the archive screen, which lists the agents
// archived from the panel. Open enters it from the agents list, and the rest act
// on the hovered agent once inside.
type ArchiveKeys struct {
	// Open the archive from the agents list.
	Open []string `mapstructure:"open"`
	// Quit the panel.
	Quit []string `mapstructure:"quit"`
	// Leave the archive for the agents list.
	Back []string `mapstructure:"back"`
	// Put the agent under the cursor back in the panel.
	Restore []string `mapstructure:"restore"`
	// Destroy the agent under the cursor for good, with its transcript. This is
	// the one thing in the panel nothing undoes, so it asks first.
	Delete []string `mapstructure:"delete"`
}

// ActionsKeys are the bindings for the actions list, the scripts you have written for
// the panel to run. Open enters it on the hovered agent, and the rest act on the script
// under the cursor once inside.
type ActionsKeys struct {
	// Open the actions list on the hovered agent. It acts on a row rather than on the
	// list, so this does nothing on a workflow header.
	Open []string `mapstructure:"open"`
	// Quit the panel.
	Quit []string `mapstructure:"quit"`
	// Leave the actions list for the agents list.
	Back []string `mapstructure:"back"`
	// Run the script under the cursor on the agent the list was opened on.
	Run []string `mapstructure:"run"`
}

// Actions is where the panel finds the scripts its action key runs.
type Actions struct {
	// The directory holding them, one file per action, named by the file. Every file
	// directly inside is offered and the directory is read on each press, so writing an
	// action is dropping an executable in rather than an edit here. An action runs on the
	// agent it was chosen on, in that agent's directory, with the agent as JSON in
	// WEAVE_AGENT. Left empty this is ~/.config/weave/actions, beside this file.
	Dir string `mapstructure:"dir"`
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

// Log is how much the background service writes about itself. Where it writes is not
// configurable. The process that spawns the service points its output at the log and the
// panel's log key pages the same file, and a path the two could disagree about is a log
// nobody can find.
type Log struct {
	// How much is written, one of debug, info, warn, or error. Served requests are logged
	// at debug and failures at error, so the default keeps the file to what went wrong.
	// Debug is worth turning on while chasing something. It is loud, because the panel
	// calls four times a second while it is open.
	Level string `mapstructure:"level"`
	// What the log key opens the log in. It runs in the stage pane through a shell, so a
	// pipeline is as good as a single command. Two variables are exported into it:
	// WEAVE_BIN is this binary, whose "logs" subcommand prints the file a line per record
	// rather than as the JSON it is stored as, and WEAVE_LOG is the file itself, for a
	// reader that would rather have it raw.
	//
	// Nothing in the command is rewritten, so a tool's own placeholders are its own:
	// "$WEAVE_BIN logs --color=always | fzf --ansi --tac" gives fzf the whole of its "{}".
	//
	// Whatever it runs has to hold the pane, the way a pager does. A command that prints
	// and exits takes the pane down with it, and the stage comes back empty.
	Viewer string `mapstructure:"viewer"`
}

// Config is the user's customization.
type Config struct {
	// Which agent CLI a new agent runs when neither the agent nor its workflow names
	// one. It has to be a harness this binary implements, so it is one of the names
	// under [harness] below.
	DefaultHarness string `mapstructure:"default_harness"`
	// How many unarchived agents may exist at once, past which a create is refused.
	MaxAgents int `mapstructure:"max_agents"`
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
	// Where the panel finds the scripts its action key runs.
	Actions Actions `mapstructure:"actions"`
	// The log the background service writes, which the panel's log key pages.
	Log Log `mapstructure:"log"`
}

// Defaults is the configuration a user who has written nothing runs on, and what the
// generated config.toml spells out, so the two agree by construction.
func Defaults() *Config {
	return &Config{
		DefaultHarness: DefaultHarness,
		MaxAgents:      DefaultMaxAgents,
		Layout:         DefaultLayout(),
		Marks:          DefaultMarks(),
		Keybindings:    DefaultKeybindings(),
		Log:            Log{Level: DefaultLogLevel, Viewer: DefaultLogViewer},
	}
}

// DefaultLogLevel is how much the service writes when the config says nothing. Failures
// are written at error and everything else at debug, so the file stays empty while the
// service works.
const DefaultLogLevel = "info"

// DefaultLogViewer is what the log key opens the log in when the config names nothing. It
// reads through this binary's own logs command, so what lands in the pane is a line per
// record rather than the JSON the file holds, and it pages that with the flags a preview
// uses plus -S, which leaves a long line running off the edge rather than wrapping it into
// four rows of one record.
const DefaultLogViewer = `"$WEAVE_BIN" logs --color=always | less -RSX +G`

// DefaultMaxAgents is how many agents may exist at once when the config names no number,
// well above what anyone opens by hand and low enough to catch a caller in a loop.
const DefaultMaxAgents = 50

// DefaultHarness is the CLI a new agent runs when the config names none, spelled here
// rather than taken from the registry since this is a default a user overrides.
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
// names one by and valued by the index lipgloss draws it as. The panel is drawn from the
// same palette, so a mark named this way sits in the scheme around it.
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

// ResolveColor is a configured colour in the form lipgloss reads it: a name becomes the
// index lipgloss maps onto the terminal's scheme, and a hex value passes through.
func ResolveColor(color string) string {
	if index, ok := ansiColors[color]; ok {
		return index
	}

	return color
}

// DefaultMarks are the marks stepped through when the config names none. They share one
// colour, leaving the glyph to carry the meaning until someone colours them apart.
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
			Up:   []string{"k"},
			Down: []string{"j"},
		},
		Help: HelpKeys{
			Toggle: []string{"?"},
		},
		Agents: AgentsKeys{
			Quit:       []string{"ctrl+c"},
			Back:       []string{"esc"},
			Open:       []string{"enter"},
			Editor:     []string{"e"},
			New:        []string{"n"},
			Rename:     []string{"r"},
			Archive:    []string{"z"},
			Search:     []string{"/"},
			Info:       []string{"i"},
			Mark:       []string{"m"},
			Hide:       []string{"h"},
			ShowHidden: []string{"H"},
			Logs:       []string{"L"},
		},
		Workflows: WorkflowsKeys{
			New:     []string{"W"},
			Rename:  []string{"r"},
			Edit:    []string{"e"},
			Archive: []string{"z"},
		},
		Schedules: SchedulesKeys{
			Open:    []string{"s"},
			Quit:    []string{"ctrl+c"},
			Back:    []string{"esc"},
			New:     []string{"n"},
			Edit:    []string{"enter"},
			Toggle:  []string{" "},
			Archive: []string{"z"},
		},
		Archive: ArchiveKeys{
			Open:    []string{"Z"},
			Quit:    []string{"ctrl+c"},
			Back:    []string{"esc"},
			Restore: []string{"r"},
			Delete:  []string{"d"},
		},
		Actions: ActionsKeys{
			Open: []string{"a"},
			Quit: []string{"ctrl+c"},
			Back: []string{"esc"},
			Run:  []string{"enter"},
		},
		Form: FormKeys{
			Quit:   []string{"ctrl+c"},
			Back:   []string{"esc"},
			Submit: []string{"ctrl+s"},
		},
	}
}

// maxMarkLen bounds a mark in bytes. A glyph runs to a handful even with modifiers on it,
// so anything longer is not the one character the panel can draw.
const maxMarkLen = 16

// hexColor matches the colours lipgloss reads, three or six digits behind a hash. Anything
// else renders as no colour at all, so a typo would silently lose the mark's colour.
var hexColor = regexp.MustCompile(`^#([0-9a-fA-F]{3}|[0-9a-fA-F]{6})$`)

// validColor reports whether the panel could draw a colour. The names are a closed set, so
// a colour spelled wrongly is refused here rather than drawn as no colour.
func validColor(color string) bool {
	if _, ok := ansiColors[color]; ok {
		return true
	}

	return hexColor.MatchString(color)
}

// validateMarks rejects marks the panel could not draw. A glyph goes straight into a
// rendered frame, so whitespace or a control character would break its row, and a glyph
// listed twice could never be stepped past.
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

// validateLayout rejects a split tmux could not lay the two panes out at. Zero is how an
// omitted key reads, so it falls back to the default rather than being called a mistake.
func validateLayout(layout Layout) error {
	if layout.Split < 0 || layout.Split >= 1 {
		return fmt.Errorf("layout split %v is not a fraction of the width above zero and below one", layout.Split)
	}

	return nil
}

// validate rejects the settings the panel could not run on, so a config that cannot work
// says so when it is read rather than when something tries to draw with it.
func (c Config) validate() error {
	if err := validateMarks(c.Marks); err != nil {
		return err
	}
	if err := validateLog(c.Log); err != nil {
		return err
	}
	// A negative ceiling would read as zero and take the default, leaving the file saying
	// one thing while the panel does another.
	if c.MaxAgents < 0 {
		return fmt.Errorf("max_agents: %d is not a number of agents", c.MaxAgents)
	}

	return validateLayout(c.Layout)
}

// logLevels are the levels the service understands. One it does not know decodes as debug,
// so an unchecked typo turns the log up instead of saying the word was wrong.
var logLevels = []string{"debug", "info", "warn", "error"}

// validateLog rejects a level the service could not act on.
func validateLog(log Log) error {
	level := strings.TrimSpace(log.Level)
	if level == "" || slices.Contains(logLevels, strings.ToLower(level)) {
		return nil
	}

	return fmt.Errorf("log.level: %q is not one of %s", log.Level, strings.Join(logLevels, ", "))
}

// withDefaults fills anything the file left unset from the built-ins. An omitted value
// reads as its zero, which is why a blank marks list and a zero split are unset here
// rather than choices to be honoured.
func (c Config) withDefaults() Config {
	c.Keybindings = c.Keybindings.withDefaults()
	// Every launch resolves through this id, so leaving it empty would refuse every
	// create rather than fall back.
	if strings.TrimSpace(c.DefaultHarness) == "" {
		c.DefaultHarness = DefaultHarness
	}
	if c.MaxAgents == 0 {
		c.MaxAgents = DefaultMaxAgents
	}
	if len(c.Marks) == 0 {
		c.Marks = DefaultMarks()
	}
	if c.Layout.Split == 0 {
		c.Layout = DefaultLayout()
	}
	if strings.TrimSpace(c.Log.Level) == "" {
		c.Log.Level = DefaultLogLevel
	}
	if strings.TrimSpace(c.Log.Viewer) == "" {
		c.Log.Viewer = DefaultLogViewer
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

// fillBindings copies the default into every key list the config left empty, walking the
// groups as it goes: a group is a struct and a binding a slice of keys. It reads the struct
// rather than naming each binding, so adding one is an edit to the struct and nothing else.
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

// configHome is the directory the panel keeps its own files in, ~/.config/weave.
func configHome() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to resolve home directory: %w", err)
	}

	return filepath.Join(home, ".config", "weave"), nil
}

// ActionsDir is where the actions live: the configured directory, or
// ~/.config/weave/actions when the config names none. The default is resolved here rather
// than filled into Config, since the generated config.toml spells every default out and one
// machine's home directory would be wrong on every other machine.
func (c Config) ActionsDir() (string, error) {
	if dir := strings.TrimSpace(c.Actions.Dir); dir != "" {
		return dir, nil
	}

	home, err := configHome()
	if err != nil {
		return "", err
	}

	return filepath.Join(home, "actions"), nil
}

// Load reads the config from path when given, otherwise from
// ~/.config/weave/config.toml. A missing file is not an error and yields the defaults, but
// a symlink resolving to nothing is, since that is a config someone meant to have.
//
// A key the Config struct does not know about is an error rather than silently dropped, so
// a typo or a binding left behind by an older version says so.
func Load(path string) (*Config, error) {
	viperCfg := viper.New()
	viperCfg.SetConfigType("toml")

	// The resolved path is named in the errors below, since what went wrong is not
	// actionable without knowing which file to edit.
	file := path
	if file == "" {
		dir, err := configHome()
		if err != nil {
			return nil, err
		}

		file = filepath.Join(dir, "config.toml")
	}

	viperCfg.SetConfigFile(file)

	if err := viperCfg.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); ok {
			return Defaults(), nil
		}
		if os.IsNotExist(err) {
			// A dangling symlink and a path with nothing at it fail the same way, so
			// both arrive here. Readlink does not follow the link and so succeeds only
			// in the dangling case, which is the one worth naming: a broken link is a
			// config the user believes they have.
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
