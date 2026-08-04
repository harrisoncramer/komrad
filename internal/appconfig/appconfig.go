// Package appconfig loads the user's customization from one or more TOML files via viper,
// merged in order so a later file wins. It covers which CLI agents run and the flags it
// takes, the marks an agent can be given, the panel keybindings, and which database, address
// and tmux server this weave is.
package appconfig

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"unicode"

	"weave/config"

	"github.com/go-viper/mapstructure/v2"
	"github.com/spf13/viper"
)

// errLayerAbsent marks a config layer that is not on disk. It is answered rather than
// raised for the default path, since not having written a config is the ordinary install.
var errLayerAbsent = errors.New("config layer is absent")

const (
	readOnlyUser     = "weave_readonly"
	readOnlyPassword = "weave_readonly"
)

// DefaultLogLevel is how much the service writes when the config says nothing. Failures
// are written at error and everything else at debug, so the file stays empty while the
// service works.
const DefaultLogLevel = "info"

// DefaultLogViewer is what the log key opens the log in when the config names nothing: a
// pager on the file itself, opened in the pager's own follow so the log is watched rather
// than read once.
const DefaultLogViewer = `less -R +F "$WEAVE_LOG"`

// DefaultMaxAgents is how many agents may exist at once when the config names no number,
// well above what anyone opens by hand and low enough to catch a caller in a loop.
const DefaultMaxAgents = 50

// The connection settings a config that names none runs on. They live here rather than as
// envDefault tags on the runtime config so that the built-in default sits underneath the
// file, which is where the precedence chain wants it. A tag default would sit beside the
// environment instead and there would be no way to tell it apart from a value someone set.
//
// ReadOnlyDatabaseURL and CacheDir have no constant because neither is a fixed string: one
// is derived from whichever database url wins and the other from the platform's cache
// directory, both after the file and the environment have been resolved.
const (
	// The Postgres `mise run docker:up` starts, so a checkout works with nothing exported.
	DefaultDatabaseURL = "postgres://weave:weave@localhost:5460/weave?sslmode=disable"
	// Loopback, because the endpoint has no auth and its writes can spawn agents.
	DefaultAddr = "127.0.0.1:8787"
	// The socket weave's own tmux server listens on, named for the tool.
	DefaultTmuxSocket = "weave"
)

// DefaultHarness is the CLI a new agent runs when the config names none, spelled here
// rather than taken from the registry since this is a default a user overrides.
const DefaultHarness = "claude"

// DefaultSplit is the share of the width the panel's list takes when the config
// names none.
const DefaultSplit = 0.2

// DefaultSecondarySplit is the share of the stage's height the pane under it opens at when
// the config names none. It is enough to work in without pushing the agent it is about off
// the screen.
const DefaultSecondarySplit = 0.4

// DefaultMarkColor is the yellow a mark takes when its own colour is unset. It is the
// plain yellow rather than the bright one the panel draws a thinking agent in, so a mark
// left on the default cannot be read as a lifecycle state.
const DefaultMarkColor = "yellow"

// Keybindings scopes bindings to the screen they act in. Each binding is a list of
// bubbletea key strings such as "enter", "ctrl+d", or "esc". Every default is a
// single key, leaving the alternatives to you. A binding left unset falls back to
// its built-in default.
type Keybindings struct {
	// The actions list, and the key that opens it on the hovered agent.
	Actions ActionsKeys `mapstructure:"actions"`
	// The agents list, the panel's main screen.
	Agents AgentsKeys `mapstructure:"agents"`
	// The archive screen, and the key that opens it from the agents list.
	Archive ArchiveKeys `mapstructure:"archive"`
	// The filter over whichever list is on screen, shared by every screen that
	// draws one.
	Filter FilterKeys `mapstructure:"filter"`
	// The new-agent, rename, and workflow forms.
	Form FormKeys `mapstructure:"form"`
	// The key that draws the legend under a list, shared by every screen for the
	// same reason navigation is.
	Help HelpKeys `mapstructure:"help"`
	// The key that pages the service's log, shared by every screen since the log is
	// about the service rather than about anything a screen lists.
	Logs LogsKeys `mapstructure:"logs"`
	// Navigation, shared by every menu so movement is the same everywhere.
	Nav NavKeys `mapstructure:"nav"`
	// The schedules screen, and the key that opens it from the agents list.
	Schedules SchedulesKeys `mapstructure:"schedules"`
	// Choosing several agents at once, shared by the two screens that draw a tree.
	Selection SelectionKeys `mapstructure:"selection"`
	// The bindings that create and change workflows, from the agents list.
	Workflows WorkflowsKeys `mapstructure:"workflows"`
}

// NavKeys are the shared navigation bindings every menu uses, so movement is the
// same everywhere rather than configured per menu.
type NavKeys struct {
	// Jump to the last row.
	Bottom []string `mapstructure:"bottom"`
	// Move the cursor down.
	Down []string `mapstructure:"down"`
	// Jump to the first row, pressed twice the way vim's gg is.
	Top []string `mapstructure:"top"`
	// Move the cursor up.
	Up []string `mapstructure:"up"`
}

// HelpKeys is the binding that draws the legend under a list. It is its own group
// rather than one of a screen's, since it acts the same on all of them.
type HelpKeys struct {
	// Toggle for the keybindings
	Toggle []string `mapstructure:"toggle"`
}

// LogsKeys is the binding that pages the background service's log. It is its own group
// rather than one of a screen's, since the log is about the service and is wanted from
// wherever the panel is when the service misbehaves.
type LogsKeys struct {
	// Show the background service's log in the pane under the stage.
	Open []string `mapstructure:"open"`
}

// FilterKeys is the binding that narrows the list on screen. It is its own group rather than
// one of a screen's, since it acts the same on all of them. While the input is open every
// other key types into the pattern, so the keys that close it are fixed rather than
// configured.
type FilterKeys struct {
	// Narrow the list on screen to the rows matching what you type, as a regular expression.
	Open []string `mapstructure:"open"`
}

// SelectionKeys are the bindings for choosing several agents at once. They are shared by
// the agents list and the archive rather than set per screen, since the gesture is the same
// on either tree. While anything is selected that screen is in multi-select mode, where a
// gutter marks the chosen rows and the keys that act on one agent stand down.
type SelectionKeys struct {
	// Drop the whole selection, which is the way back out of multi-select mode.
	Clear []string `mapstructure:"clear"`
	// Take in every agent between the cursor and the nearest already-selected one.
	Range []string `mapstructure:"range"`
	// Take the row under the cursor into the selection, or back out of it.
	Toggle []string `mapstructure:"toggle"`
}

// FormKeys are the bindings for the new-agent and rename forms. Back and Quit both
// abort the form, split apart so they can be rebound separately.
type FormKeys struct {
	// Abort the form without quitting the panel.
	Back []string `mapstructure:"back"`
	// Abort the form and quit the panel.
	Quit []string `mapstructure:"quit"`
	// Submit the form. Plain enter belongs to the text inputs, so this is
	// deliberately something else.
	Submit []string `mapstructure:"submit"`
}

// AgentsKeys are the bindings for the agents list. Info toggles the hovered agent's
// stored record into the stage in place of the agent itself.
type AgentsKeys struct {
	// Archive the hovered agent, which stops it and moves it to the archive screen rather than destroying it.
	Archive []string `mapstructure:"archive"`
	// Step out of a workflow scope, or quit at the root.
	Back []string `mapstructure:"back"`
	// Open the hovered agent's directory in your EDITOR.
	// the key does.
	Editor []string `mapstructure:"editor"`
	// Hide the hovered agent from this list.
	Hide []string `mapstructure:"hide"`
	// Show the hovered agent's stored record in the pane under the stage.
	Info []string `mapstructure:"info"`
	// Move the hovered agent to the next mark in the marks list, and past the last
	// one back to unmarked. Nothing but you ever sets a mark.
	Mark []string `mapstructure:"mark"`
	// Start a new agent.
	New []string `mapstructure:"new"`
	// Attach to the hovered agent.
	Open []string `mapstructure:"open"`
	// Quit the panel.
	Quit []string `mapstructure:"quit"`
	// Rename the hovered agent.
	Rename []string `mapstructure:"rename"`
	// Search every conversation the panel holds for what you type, the archived ones included.
	Search []string `mapstructure:"search"`
	// Toggles visibility of hidden agents alongside the rest.
	ShowHidden []string `mapstructure:"show_hidden"`
	// Open a shell in the pane under the stage, in the hovered agent's directory.
	// This runs the _terminal action, write your own _terminal in your actions directory.
	Terminal []string `mapstructure:"terminal"`
}

// WorkflowsKeys are the bindings that act on a workflow. New is global. Rename,
// edit, and archive apply while the cursor is on a workflow header.
type WorkflowsKeys struct {
	// Archive the workflow under the cursor, which archives the agents inside it.
	Archive []string `mapstructure:"archive"`
	// Edit the workflow under the cursor.
	Edit []string `mapstructure:"edit"`
	// Create a workflow. Works from anywhere in the list.
	New []string `mapstructure:"new"`
	// Rename the workflow under the cursor.
	Rename []string `mapstructure:"rename"`
}

// SchedulesKeys are the bindings for the schedules screen, which is its own view
// rather than part of the agents tree. Open enters it from the agents list, and
// the rest act on the hovered schedule once inside.
type SchedulesKeys struct {
	// Archive the schedule under the cursor, which stops it firing. The agents it
	// launched are kept, and nothing brings the schedule back.
	Archive []string `mapstructure:"archive"`
	// Leave the schedules screen for the agents list.
	Back []string `mapstructure:"back"`
	// Edit the schedule under the cursor.
	Edit []string `mapstructure:"edit"`
	// Write a new schedule.
	New []string `mapstructure:"new"`
	// Open the schedules screen from the agents list.
	Open []string `mapstructure:"open"`
	// Quit the panel.
	Quit []string `mapstructure:"quit"`
	// Pause or resume the schedule under the cursor.
	Toggle []string `mapstructure:"toggle"`
}

// ArchiveKeys are the bindings for the archive screen, which lists the agents
// archived from the panel. Open enters it from the agents list, and the rest act
// on the hovered agent once inside.
type ArchiveKeys struct {
	// Leave the archive for the agents list.
	Back []string `mapstructure:"back"`
	// Destroy the agent under the cursor for good, with its transcript. This is
	// the one thing in the panel nothing undoes, so it asks first.
	Delete []string `mapstructure:"delete"`
	// Open the archive from the agents list.
	Open []string `mapstructure:"open"`
	// Quit the panel.
	Quit []string `mapstructure:"quit"`
	// Put the agent under the cursor back in the panel.
	Restore []string `mapstructure:"restore"`
}

// ActionsKeys are the bindings for the actions list, the scripts you have written for
// the panel to run. Open enters it on the hovered agent, and the rest act on the script
// under the cursor once inside.
type ActionsKeys struct {
	// Leave the actions list for the agents list.
	Back []string `mapstructure:"back"`
	// Open the actions list on the hovered agent. It acts on a row rather than on
	// the list, so this does nothing on a workflow header.
	Open []string `mapstructure:"open"`
	// Quit the panel.
	Quit []string `mapstructure:"quit"`
	// Run the script under the cursor on the agent the list was opened on.
	Run []string `mapstructure:"run"`
}

// Actions is where the panel finds the scripts its action key runs.
type Actions struct {
	// Directory for actions: one executable file per action, named after the action.
	// Files here override built-in actions with the same name.
	// Actions run in the agent's directory with agent info in WEAVE_AGENT as JSON. Default is ~/.config/weave/actions.
	Dir string `mapstructure:"dir"`
}

// Context is the standing text every agent is started with, above whatever its workflow
// adds. It is where what is true of your machine and how you want to be worked with goes,
// so it does not have to be pasted into every workflow.
type Context struct {
	// The file holding global context, read at each launch.
	// Left empty this is ~/.config/weave/context.md, but will not trigger a failure if left blank.
	File string `mapstructure:"file"`
}

// Layout is how the panel divides the popup between its own list and the stage
// pane beside it.
type Layout struct {
	// The share of the width the panel's list takes.
	Split float64 `mapstructure:"split"`
	// The share of the stage's height taken by the pane that opens under it.
	SecondarySplit float64 `mapstructure:"secondary_split"`
}

// Harness is how one agent CLI is configured.
type Harness struct {
	// Flags appended to every agent's command line for this CLI, ahead of the flags that agent carries itself.
	Flags string `mapstructure:"flags"`
}

// Codex is how the Codex CLI is configured. It carries one setting of its own,
// since Codex asks a question at startup that no other CLI here does.
type Codex struct {
	// Flags appended to every Codex agent's command line, ahead of the flags that agent carries itself.
	Flags string `mapstructure:"flags"`
	// Whether to record each agent's directory as trusted in Codex's own config
	// before launching it. Codex asks whether a directory is trusted the first
	// time it runs there and waits for an answer, which it reads only from that
	// file, so an agent in a fresh worktree otherwise starts blocked and shows
	// here as waiting. Turning this on edits ~/.codex/config.toml on your behalf.
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
	// How much is written, one of debug, info, warn, or error.
	Level string `mapstructure:"level"`
	// The default log viewer. Switch with fzf, rg, etc.
	Viewer string `mapstructure:"viewer"`
}

// Connection is which weave you are talking to: the database it writes, the address it
// serves, the tmux server it runs agents on, and the directory it generates into. Every
// one of these also has a WEAVE_-prefixed environment variable, and the variable wins when
// it is set, so one shell or one checkout can point somewhere else without editing a file.
//
// These are the settings a second instance is isolated by, which is why they are a table
// rather than four keys at the top level. Overriding the table is overriding the instance.
type Connection struct {
	// The Postgres weave keeps every agent, workflow, schedule and transcript in.
	// Weave migrates the schema itself on the way in, so an empty database is enough.
	DatabaseURL string `mapstructure:"database_url"`
	// The least-privilege login the MCP query tools connect as.
	// Left empty this is database_url with the weave_readonly login in place of yours,
	// on the same server and the same database, so moving one moves both.
	// Set it when that login needs its own password or reaches the database another way.
	ReadOnlyDatabaseURL string `mapstructure:"readonly_database_url"`
	// Where the background service listens, and where the panel, the CLI and the MCP
	// endpoint all reach it. Change it to run two weaves on one machine.
	Addr string `mapstructure:"addr"`
	// The tmux socket weave runs its own server on. Nothing in your normal tmux sees
	// that server, which is why `tmux -L weave ls` is how you look at it directly.
	TmuxSocket string `mapstructure:"tmux_socket"`
	// Where weave keeps what it generates: the service log, the built-in actions it
	// writes out on every start, and the temp files a harness passes a prompt through.
	// Left empty this is ~/Library/Caches/weave on a Mac and ~/.cache/weave on Linux.
	// Everything under it is weave's to delete and rewrite, so nothing of yours belongs here.
	CacheDir string `mapstructure:"cache_dir"`
}

// Config is the user's customization.
type Config struct {
	// Which agent CLI a new agent runs when neither the agent nor its workflow
	// names one. It has to be a harness this binary implements, so it is one of
	// the names under [harness] below.
	DefaultHarness string `mapstructure:"default_harness"`
	// How many unarchived agents may exist at once, past which a create is refused.
	MaxAgents int       `mapstructure:"max_agents"`
	Harness   Harnesses `mapstructure:"harness"`
	// How the popup's width is divided between the panel and the stage beside it.
	Layout Layout `mapstructure:"layout"`
	// The marks the mark key cycles through, in order.
	// A colour is either one of the sixteen your terminal's own scheme maps, named as "red" or "brightred",
	// or a hex value such as "#DCA561". The panel draws itself from the sixteen,
	// so a name is what keeps a mark in the same scheme as everything around it.
	Marks []Mark `mapstructure:"marks"`
	// Panel keybindings, scoped to the screen each binding acts in.
	Keybindings Keybindings `mapstructure:"keybindings"`
	// Where the panel finds the scripts its action key runs.
	Actions Actions `mapstructure:"actions"`
	// The standing text every agent is started with, whatever workflow it is in.
	Context Context `mapstructure:"context"`
	// The log the background service writes, which the panel's log key pages.
	Log Log `mapstructure:"log"`
	// Which weave this is: the database, the address, the tmux socket, the cache.
	Connection Connection `mapstructure:"connection"`
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
		Connection:     DefaultConnection(),
	}
}

// DefaultConnection is the instance a config that names none points at.
func DefaultConnection() Connection {
	return Connection{
		DatabaseURL: DefaultDatabaseURL,
		Addr:        DefaultAddr,
		TmuxSocket:  DefaultTmuxSocket,
	}
}

// DefaultLayout is the layout used when the config omits it.
func DefaultLayout() Layout {
	return Layout{Split: DefaultSplit, SecondarySplit: DefaultSecondarySplit}
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

// GetColor is a configured colour in the form lipgloss reads it: a name becomes the
// index lipgloss maps onto the terminal's scheme, and a hex value passes through.
func GetColor(color string) string {
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
		Actions: ActionsKeys{
			Back: []string{"esc"},
			Open: []string{"a"},
			Quit: []string{"ctrl+c"},
			Run:  []string{"enter"},
		},
		Agents: AgentsKeys{
			Archive:    []string{"z"},
			Back:       []string{"esc"},
			Editor:     []string{"e"},
			Hide:       []string{"h"},
			Info:       []string{"i"},
			Mark:       []string{"m"},
			New:        []string{"n"},
			Open:       []string{"enter"},
			Quit:       []string{"ctrl+c"},
			Rename:     []string{"r"},
			Search:     []string{"s"},
			ShowHidden: []string{"H"},
			Terminal:   []string{"t"},
		},
		Archive: ArchiveKeys{
			Back:    []string{"esc"},
			Delete:  []string{"d"},
			Open:    []string{"Z"},
			Quit:    []string{"ctrl+c"},
			Restore: []string{"r"},
		},
		Filter: FilterKeys{
			Open: []string{"/"},
		},
		Form: FormKeys{
			Back:   []string{"esc"},
			Quit:   []string{"ctrl+c"},
			Submit: []string{"ctrl+s"},
		},
		Help: HelpKeys{
			Toggle: []string{"?"},
		},
		Logs: LogsKeys{
			Open: []string{"l"},
		},
		Nav: NavKeys{
			Bottom: []string{"G"},
			Down:   []string{"j"},
			Top:    []string{"g"},
			Up:     []string{"k"},
		},
		Schedules: SchedulesKeys{
			Archive: []string{"z"},
			Back:    []string{"esc"},
			Edit:    []string{"enter"},
			New:     []string{"n"},
			Open:    []string{"S"},
			Quit:    []string{"ctrl+c"},
			Toggle:  []string{" "},
		},
		Selection: SelectionKeys{
			Clear:  []string{"esc"},
			Range:  []string{"V"},
			Toggle: []string{"v"},
		},
		Workflows: WorkflowsKeys{
			Archive: []string{"z"},
			Edit:    []string{"e"},
			New:     []string{"w"},
			Rename:  []string{"r"},
		},
	}
}

// maxMarkLen bounds a mark in bytes. A glyph runs to a handful even with modifiers on it,
// so anything longer is not the one character the panel can draw.
const maxMarkLen = 16

// hexColor matches the colours lipgloss reads, three or six digits behind a hash. Anything
// else renders as no colour at all, so a typo would silently lose the mark's colour.
var hexColor = regexp.MustCompile(`^#([0-9a-fA-F]{3}|[0-9a-fA-F]{6})$`)

// isValidColor reports whether the panel could draw a colour. The names are a closed set, so
// a colour spelled wrongly is refused here rather than drawn as no colour.
func isValidColor(color string) bool {
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
		case mark.Color != "" && !isValidColor(mark.Color):
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
	if layout.SecondarySplit < 0 || layout.SecondarySplit >= 1 {
		return fmt.Errorf("layout secondary split %v is not a fraction of the stage's height above zero and below one", layout.SecondarySplit)
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

// configHome is the directory the panel keeps its own files in, ~/.config/weave.
func configHome() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}

	return filepath.Join(home, ".config", "weave"), nil
}

// ExpandHome expands a leading ~ or ~/ to the user's home directory, since nothing below a
// config does it for itself. tmux's new-session -c takes the path literally, and so does
// every open of a file weave was pointed at. A home directory that cannot be read leaves
// the path as written.
func ExpandHome(path string) string {
	if path != "~" && !strings.HasPrefix(path, "~/") {
		return path
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	if path == "~" {
		return home
	}

	return filepath.Join(home, path[len("~/"):])
}

// GetActionsDir is where the actions live: the configured directory, or
// ~/.config/weave/actions when the config names none. The default is filled in here rather
// than filled into Config, since the generated config.toml spells every default out and one
// machine's home directory would be wrong on every other machine.
func (c Config) GetActionsDir() (string, error) {
	if dir := strings.TrimSpace(c.Actions.Dir); dir != "" {
		return ExpandHome(dir), nil
	}

	home, err := configHome()
	if err != nil {
		return "", err
	}

	return filepath.Join(home, "actions"), nil
}

// GetContextFile is the standing text every agent is started with: the configured file, or
// ~/.config/weave/context.md when the config names none. The default is filled in here for
// the reason the actions directory's is, since one machine's home directory spelled into
// the generated config would be wrong on every other machine.
func (c Config) GetContextFile() (string, error) {
	if file := strings.TrimSpace(c.Context.File); file != "" {
		return ExpandHome(file), nil
	}

	home, err := configHome()
	if err != nil {
		return "", err
	}

	return filepath.Join(home, "context.md"), nil
}

// Layer is one file in the stack, and whether it was asked for by name. A named layer that
// is not there is an error, since a path someone typed is a config they believe they have,
// while the default path is allowed to be absent because not writing one is the ordinary
// install.
type Layer struct {
	Path  string
	Named bool
}

// ResolveLayers is the files Load would read, weakest first. Given paths, that is exactly
// those paths, since naming any replaces the default rather than adding to it: being able
// to say "this file and nothing else" is what lets a second instance be isolated from the
// first one's config. Given none, it is the one default path.
//
// Weave never looks for a file it was not asked for. There is no sibling convention and
// nothing per directory, so which files are read depends on what was asked for rather than
// on what happens to be lying around.
//
// It is exported because a caller reporting on a config has to name the files, and a
// missing one has no error to read the path out of.
func ResolveLayers(paths []string) ([]Layer, error) {
	if len(paths) > 0 {
		layers := make([]Layer, 0, len(paths))
		for _, path := range paths {
			if strings.TrimSpace(path) == "" {
				return nil, errors.New("--config was given an empty path")
			}

			layers = append(layers, Layer{Path: ExpandHome(path), Named: true})
		}

		return layers, nil
	}

	dir, err := configHome()
	if err != nil {
		return nil, err
	}

	return []Layer{{Path: filepath.Join(dir, "config.toml")}}, nil
}

// Load reads the layers in order and merges them, weakest first, so a later file wins where
// two overlap. A missing file among them is not an error unless it was named, and yields
// nothing rather than the defaults, but a symlink pointing at nothing is, since that is a
// config someone meant to have.
//
// A key the Config struct does not know about is an error rather than silently dropped, so
// a typo is reported rather than quietly doing nothing. The error names the ordered file
// stack beside what the key's table does take.
func Load(paths []string) (*Config, error) {
	cfg, _, err := LoadWithReport(paths)

	return cfg, err
}

// Source is the precedence layer that supplied a resolved connection setting.
type Source string

const (
	SourceDefault     Source = "built-in default"
	SourceFile        Source = "config file"
	SourceEnvironment Source = "environment"
	SourceDerived     Source = "derived"
)

// Report describes how a configuration was resolved without mixing provenance into Config.
type Report struct {
	Layers  []Layer
	Sources map[string]Source
}

// LoadWithReport resolves the configuration and reports where connection values came from.
func LoadWithReport(paths []string) (*Config, *Report, error) {
	layers, err := ResolveLayers(paths)
	if err != nil {
		return nil, nil, err
	}

	merged := viper.New()
	merged.SetConfigType("toml")
	if err := registerDefaults(merged); err != nil {
		return nil, nil, err
	}
	if err := bindEnvironment(merged); err != nil {
		return nil, nil, err
	}

	for _, layer := range layers {
		merged.SetConfigFile(layer.Path)

		if err := merged.MergeInConfig(); err != nil {
			if readErr := classifyLayerRead(layer, err); !errors.Is(readErr, errLayerAbsent) {
				return nil, nil, readErr
			}

			continue
		}
	}

	var cfg Config
	var decoded mapstructure.Metadata
	if err := merged.Unmarshal(&cfg, func(dc *mapstructure.DecoderConfig) {
		dc.Metadata = &decoded
	}); err != nil {
		return nil, nil, fmt.Errorf("failed to parse config %s: %w", describeLayers(layers), err)
	}
	if err := reportUnknownKeys(decoded.Unused); err != nil {
		return nil, nil, fmt.Errorf("failed to parse config %s: %w", describeLayers(layers), err)
	}

	cfg.normalizeZeroValues()

	report := &Report{Layers: layers, Sources: connectionSources(merged)}
	if err := cfg.resolveDerived(report); err != nil {
		return nil, nil, err
	}

	if err := cfg.validate(); err != nil {
		return nil, nil, fmt.Errorf("failed to parse config %s: %w", describeLayers(layers), err)
	}

	return &cfg, report, nil
}

// normalizeZeroValues keeps the settings whose zero has historically meant "unset" on
// their built-in values. Collections are deliberately excluded: an explicit empty list
// clears a list supplied by a default or lower layer.
func (c *Config) normalizeZeroValues() {
	if strings.TrimSpace(c.DefaultHarness) == "" {
		c.DefaultHarness = DefaultHarness
	}
	if c.MaxAgents == 0 {
		c.MaxAgents = DefaultMaxAgents
	}
	if c.Layout.Split == 0 {
		c.Layout.Split = DefaultSplit
	}
	if c.Layout.SecondarySplit == 0 {
		c.Layout.SecondarySplit = DefaultSecondarySplit
	}
	if strings.TrimSpace(c.Log.Level) == "" {
		c.Log.Level = DefaultLogLevel
	}
	if strings.TrimSpace(c.Log.Viewer) == "" {
		c.Log.Viewer = DefaultLogViewer
	}
}

func registerDefaults(cfg *viper.Viper) error {
	values := map[string]any{}
	if err := mapstructure.Decode(Defaults(), &values); err != nil {
		return fmt.Errorf("register config defaults: %w", err)
	}

	registerDefaultValues(cfg, "", values)

	return nil
}

func registerDefaultValues(cfg *viper.Viper, prefix string, values map[string]any) {
	for key, value := range values {
		path := key
		if prefix != "" {
			path = prefix + "." + key
		}

		if nested, ok := value.(map[string]any); ok {
			registerDefaultValues(cfg, path, nested)
			continue
		}

		cfg.SetDefault(path, value)
	}
}

var connectionEnvironment = map[string]string{
	"connection.database_url":          "WEAVE_DATABASE_URL",
	"connection.readonly_database_url": "WEAVE_READONLY_DATABASE_URL",
	"connection.addr":                  "WEAVE_ADDR",
	"connection.tmux_socket":           "WEAVE_TMUX_SOCKET",
	"connection.cache_dir":             "WEAVE_CACHE_DIR",
}

func bindEnvironment(cfg *viper.Viper) error {
	for key, variable := range connectionEnvironment {
		if err := cfg.BindEnv(key, variable); err != nil {
			return fmt.Errorf("bind %s to %s: %w", key, variable, err)
		}
	}

	return nil
}

func connectionSources(cfg *viper.Viper) map[string]Source {
	sources := make(map[string]Source, len(connectionEnvironment))
	for path, variable := range connectionEnvironment {
		key := strings.TrimPrefix(path, "connection.")
		if _, set := config.LookupEnv(variable); set {
			sources[key] = SourceEnvironment
		} else if cfg.InConfig(path) {
			sources[key] = SourceFile
		} else {
			sources[key] = SourceDefault
		}
	}

	return sources
}

func (c *Config) resolveDerived(report *Report) error {
	if c.Connection.CacheDir == "" {
		dir, err := os.UserCacheDir()
		if err != nil {
			return fmt.Errorf("failed to find a cache directory, set WEAVE_CACHE_DIR to an absolute path weave can write to: %w", err)
		}

		c.Connection.CacheDir = filepath.Join(dir, "weave")
		report.Sources["cache_dir"] = SourceDerived
	}

	if c.Connection.ReadOnlyDatabaseURL == "" {
		c.Connection.ReadOnlyDatabaseURL = deriveReadOnlyURL(c.Connection.DatabaseURL)
		report.Sources["readonly_database_url"] = SourceDerived
	}

	return nil
}

func deriveReadOnlyURL(databaseURL string) string {
	parsed, err := url.Parse(databaseURL)
	if err != nil {
		return ""
	}

	if parsed.Scheme != "postgres" && parsed.Scheme != "postgresql" {
		return ""
	}

	parsed.User = url.UserPassword(readOnlyUser, readOnlyPassword)

	return parsed.String()
}

// classifyLayerRead turns a failure to read one layer into what the caller should do about
// it: errLayerAbsent when the file is simply not there and the layer contributes nothing,
// and anything else is fatal.
func classifyLayerRead(layer Layer, err error) error {
	var notFound viper.ConfigFileNotFoundError
	if errors.As(err, &notFound) && !layer.Named {
		return errLayerAbsent
	}

	if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("failed to read config %s: %w", layer.Path, err)
	}

	// A dangling symlink and a path with nothing at it fail the same way, so both arrive
	// here. Readlink does not follow the link and so succeeds only in the dangling case,
	// which is the one worth naming: a broken link is a config the user believes they have.
	if target, linkErr := os.Readlink(layer.Path); linkErr == nil {
		return fmt.Errorf("failed to read config %s: it is a symlink to %s, which does not exist", layer.Path, target)
	}

	// A file the caller named has to be there. Not having written one is the ordinary
	// install, but a path someone typed is a config they believe they have, and coming up
	// on the defaults instead looks exactly like a file that was read and then ignored.
	if layer.Named {
		return fmt.Errorf("failed to read config %s: there is no file there", layer.Path)
	}

	return errLayerAbsent
}

// describeLayers names the files a value could have come from, for the errors raised on the
// merged config where no single file is to blame.
func describeLayers(layers []Layer) string {
	paths := make([]string, 0, len(layers))
	for _, layer := range layers {
		paths = append(paths, layer.Path)
	}

	return strings.Join(paths, ", ")
}
