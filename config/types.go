package config

import (
	"fmt"
	"strings"
	"time"
)

// Where each region of the panel's layout can sit.
const (
	SidebarSideLeft  = "left"
	SidebarSideRight = "right"
	InfoSideBelow    = "below"
	InfoSideAbove    = "above"
)

// Actions is where weave finds the scripts a workflow, a schedule, or an agent runs.
type Actions struct {
	// Directory for actions: one executable file per action, named after the action.
	// Files here override built-in actions with the same name.
	// Actions run in the agent's directory with agent info in WEAVE_AGENT as JSON. Default is ~/.config/weave/actions.
	Dir string `mapstructure:"dir"`
}

// Worktrees is where a workflow's generated git worktree setup action puts what it creates.
type Worktrees struct {
	// Directory generated worktrees are created under, one subdirectory per agent.
	// Left empty this is ~/.local/state/weave/worktrees.
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

// Layout is how the panel divides the popup between its own list, the details under that
// list, the stage beside them, and the info pane that opens under the stage. A size below
// one is read as a share of what it divides, and one at or above it as a count of cells.
type Layout struct {
	// Which side of the popup the panel's sidebar sits on, "left" or "right".
	SidebarSide string `mapstructure:"sidebar_side"`
	// How wide the panel's sidebar is, either a share of the popup's width below one or a whole number of columns.
	SidebarWidth float64 `mapstructure:"sidebar_width"`
	// Which side of the stage the info pane opens on, "below" or "above". That pane is where the server log and an editor are drawn.
	InfoSide string `mapstructure:"info_side"`
	// How tall the info pane opens, either a share of the stage's height below one or a whole number of rows.
	InfoHeight float64 `mapstructure:"info_height"`
	// How many lines of fields the details under the list draw, either a share of the sidebar's height below one or a whole number of lines. The chrome around them is added on top, and the two together never take more than half the sidebar's height.
	DetailsHeight float64 `mapstructure:"details_height"`
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

// Mail is how long weave keeps trying to hand mail to the agent it was addressed
// to. Delivery waits on the recipient: an agent that is busy gets its mail as its turn
// ends, and one that has stopped is nudged, so an agent nobody is running holds mail until
// it comes back or the deadline passes.
type Mail struct {
	// How long mail stays deliverable, as a Go duration such as "3h" or "45m". Mail past
	// it is neither delivered nor readable, and the row is kept as a record that it never
	// arrived.
	DeliveryDeadline string `mapstructure:"delivery_deadline"`
	// How many pieces of mail may pass along one reply thread before a send is refused. A
	// reply takes its parent's hop count plus one, so this caps a back-and-forth rather
	// than the total mail between two agents.
	MaxHops int `mapstructure:"max_hops"`
}

// GetDeliveryDeadline is how long mail stays deliverable, parsed. A duration that will
// not parse is an error rather than the default, since a deadline somebody wrote and weave
// quietly ignored is worse than one it refuses.
func (m Mail) GetDeliveryDeadline() (time.Duration, error) {
	if m.DeliveryDeadline == "" {
		m.DeliveryDeadline = DefaultDeliveryDeadline
	}

	parsed, err := time.ParseDuration(strings.TrimSpace(m.DeliveryDeadline))
	if err != nil {
		return 0, fmt.Errorf("unreadable delivery deadline %q: %w", m.DeliveryDeadline, err)
	}
	if parsed <= 0 {
		return 0, fmt.Errorf("the delivery deadline must be positive, not %q", m.DeliveryDeadline)
	}

	return parsed, nil
}

// GetMaxHops is how many pieces of mail may pass along one reply thread. An unset limit
// takes the default; a negative one is an error rather than the default, for the same
// reason an unparseable deadline is: a config somebody wrote and weave quietly ignored is
// worse than one it refuses.
func (m Mail) GetMaxHops() (int, error) {
	if m.MaxHops == 0 {
		m.MaxHops = DefaultMaxHops
	}
	if m.MaxHops < 0 {
		return 0, fmt.Errorf("the mail hop limit must be positive, not %d", m.MaxHops)
	}

	return m.MaxHops, nil
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
	// The viewer for a setup or teardown action's own output, plain text rather than the
	// structured log's JSON lines.
	ProcessViewer string `mapstructure:"process_viewer"`
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
	// Where the panel's sidebar, the stage beside it, and the info pane under the stage sit, and how much of the popup each takes.
	Layout Layout `mapstructure:"layout"`
	// The tags the tag key cycles through, in order.
	// A colour is either one of the sixteen your terminal's own scheme maps, named as "red" or "brightred",
	// or a hex value such as "#DCA561". The panel draws itself from the sixteen,
	// so a name is what keeps a tag in the same scheme as everything around it.
	Tags []Tag `mapstructure:"tags"`
	// Panel keybindings, scoped to the screen each binding acts in.
	Keybindings Keybindings `mapstructure:"keybindings"`
	// Where weave finds the scripts a workflow, a schedule, or an agent runs.
	Actions Actions `mapstructure:"actions"`
	// Where a workflow's generated worktree setup action creates its worktrees.
	Worktrees Worktrees `mapstructure:"worktrees"`
	// The standing text every agent is started with, whatever workflow it is in.
	Context Context `mapstructure:"context"`
	// How mail between agents is delivered.
	Mail Mail `mapstructure:"mail"`
	// The log the background service writes, which the panel's log key pages.
	Log Log `mapstructure:"log"`
	// Which weave this is: the database, the address, the tmux socket, the cache.
	Connection Connection `mapstructure:"connection"`
}

// Tag is one of the tags the tag key steps through. The glyph is what a row draws
// and what the store keeps, so it is the tag's identity, while the colour is the
// panel's alone and is never stored.
//
// The json tags are the names get_config reports a tag under, spelled out because
// the encoder behind it follows the mapstructure tags as far as the slice and no
// further.
type Tag struct {
	Glyph string `mapstructure:"glyph" json:"glyph"`
	// The colour the glyph is drawn in, either one of the sixteen a terminal's own
	// scheme maps, named as "red" or "brightred", or a hex value such as "#DCA561"
	Color string `mapstructure:"color" json:"color"`
}

// ansiColors are the sixteen colours a terminal's scheme maps, keyed by the name a config
// names one by and valued by the index lipgloss draws it as. The panel is drawn from the
// same palette, so a tag named this way sits in the scheme around it.
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
