package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DefaultLogLevel is how much the service writes when the config says nothing. Failures
// are written at error and everything else at debug, so the file stays empty while the
// service works.
const DefaultLogLevel = "info"

// DefaultLogViewer is what the agent-log stop opens the log in when the config names
// nothing: filtered to the lines naming the agent WEAVE_AGENT_ID carries, followed live
// rather than read once. An empty WEAVE_AGENT_ID matches every line, which is what running
// this outside that stop degrades to.
const DefaultLogViewer = `tail -n +1 -F "$WEAVE_LOG" | grep --line-buffered "$WEAVE_AGENT_ID" | less -R +F`

const DefaultProcessViewer = `less -R -X "$WEAVE_LOG"`

// DefaultDeliveryDeadline is how long mail stays deliverable when the config names no
// duration. Long enough that an agent left alone over lunch still gets its mail, short
// enough that nothing chases a recipient nobody is coming back to.
const DefaultDeliveryDeadline = "3h"

// DefaultMaxHops is how many pieces of mail may pass along one reply thread when the config
// names no limit, ten rounds each way between two agents.
const DefaultMaxHops = 20

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

// The layout the panel lays itself out at when the config names none. The info pane opens
// tall enough to read a log in without pushing the agent above it off the screen.
const (
	DefaultSidebarSide  = SidebarSideLeft
	DefaultSidebarWidth = 0.2
	DefaultInfoSide     = InfoSideBelow
	DefaultInfoHeight   = 0.4
	// DefaultDetailsHeight leaves the list twice the room the record under it takes, which
	// holds an agent's fields without the list it belongs to shrinking to a handful of rows.
	DefaultDetailsHeight = 0.33
)

// DefaultTagColor is the yellow a tag takes when its own colour is unset. It is the
// plain yellow rather than the bright one the panel draws a thinking agent in, so a tag
// left on the default cannot be read as a lifecycle state.
const DefaultTagColor = "yellow"

// Defaults is the configuration a user who has written nothing runs on, and what the
// generated config.toml spells out, so the two agree by construction.
func Defaults() *Config {
	return &Config{
		DefaultHarness: DefaultHarness,
		MaxAgents:      DefaultMaxAgents,
		Layout:         DefaultLayout(),
		Tags:           DefaultTags(),
		Keybindings:    DefaultKeybindings(),
		Mail:           Mail{DeliveryDeadline: DefaultDeliveryDeadline, MaxHops: DefaultMaxHops},
		Log:            Log{Level: DefaultLogLevel, Viewer: DefaultLogViewer, ProcessViewer: DefaultProcessViewer},
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
	return Layout{
		SidebarSide:   DefaultSidebarSide,
		SidebarWidth:  DefaultSidebarWidth,
		InfoSide:      DefaultInfoSide,
		InfoHeight:    DefaultInfoHeight,
		DetailsHeight: DefaultDetailsHeight,
	}
}

// DefaultTags are the tags stepped through when the config names none. They share one
// colour, leaving the glyph to carry the meaning until someone colours them apart.
func DefaultTags() []Tag {
	return []Tag{
		{Glyph: "●", Color: DefaultTagColor},
		{Glyph: "◐", Color: DefaultTagColor},
		{Glyph: "✓", Color: DefaultTagColor},
	}
}

// DefaultKeybindings are the bindings used when the config omits them.
func DefaultKeybindings() Keybindings {
	return Keybindings{
		Agents: AgentsKeys{
			Archive:    []string{"z"},
			Back:       []string{"esc"},
			Review:     []string{"d"},
			Hide:       []string{"h"},
			Tag:        []string{"t"},
			New:        []string{"c"},
			Open:       []string{"enter"},
			OpenPR:     []string{"o"},
			Quit:       []string{"ctrl+c"},
			Rename:     []string{"r"},
			Search:     []string{"s"},
			ShowHidden: []string{"H"},
		},
		Archive: ArchiveKeys{
			Back:    []string{"esc"},
			Delete:  []string{"d"},
			Empty:   []string{"D"},
			Open:    []string{"Z"},
			Quit:    []string{"ctrl+c"},
			Restore: []string{"r"},
		},
		Details: DetailsKeys{
			Copy:  []string{"y"},
			Focus: []string{"ctrl+t"},
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
		Review: ReviewKeys{
			Back:         []string{"esc"},
			Compose:      []string{"c"},
			DeleteThread: []string{"D"},
			EditFile:     []string{"e"},
			HalfPageDown: []string{"ctrl+d"},
			HalfPageUp:   []string{"ctrl+u"},
			NextFile:     []string{"]"},
			NextHunk:     []string{"}"},
			NextMatch:    []string{"n"},
			NextThread:   []string{"t"},
			PageDown:     []string{"pgdown", "ctrl+n", "ctrl+f", " "},
			PageUp:       []string{"pgup", "ctrl+p", "ctrl+b"},
			PanLeft:      []string{"h", "left"},
			PanReset:     []string{"0"},
			PanRight:     []string{"l", "right"},
			PrevFile:     []string{"["},
			PrevHunk:     []string{"{"},
			PrevMatch:    []string{"N"},
			PrevThread:   []string{"T"},
			Quit:         []string{"ctrl+c"},
			Resolve:      []string{"r"},
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
			Adopt:   []string{"a"},
			Archive: []string{"z"},
			Edit:    []string{"e"},
			New:     []string{"w"},
			Rename:  []string{"r"},
		},
	}
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

// GetWorktreesDir is where a generated worktree setup action creates its worktrees: the
// configured directory, or ~/.local/state/weave/worktrees when the config names none. The
// default is filled in here rather than into Config for the reason the actions directory's
// is, since one machine's home directory spelled into the generated config would be wrong
// on every other machine.
func (c Config) GetWorktreesDir() (string, error) {
	if dir := strings.TrimSpace(c.Worktrees.Dir); dir != "" {
		return ExpandHome(dir), nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}

	return filepath.Join(home, ".local", "state", "weave", "worktrees"), nil
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
