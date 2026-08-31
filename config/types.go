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

// Keybindings scopes bindings to the screen they act in. Each binding is a list of
// bubbletea key strings such as "enter", "ctrl+d", or "esc". Every default is a
// single key, leaving the alternatives to you. A binding left unset falls back to
// its built-in default.
type Keybindings struct {
	// The agents list, the panel's main screen.
	Agents AgentsKeys `mapstructure:"agents"`
	// The archive screen, and the key that opens it from the agents list.
	Archive ArchiveKeys `mapstructure:"archive"`
	// The details under whichever list is on screen: the key that hands them the
	// cursor, and the key that copies the field it lands on.
	Details DetailsKeys `mapstructure:"details"`
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
	// The review screen, which draws an agent's diff in the stage and its review
	// threads in the sidebar.
	Review ReviewKeys `mapstructure:"review"`
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

// DetailsKeys are the bindings for the details drawn under a list. Focus moves the cursor
// between the list and the record below it, and while the record holds it the shared
// navigation keys run down its fields rather than the rows above them. The agents list, the
// schedules, and the archive all draw one.
type DetailsKeys struct {
	// Copy the field the details cursor is on to the system clipboard.
	Copy []string `mapstructure:"copy"`
	// Move the cursor between the list on screen and the details under it.
	Focus []string `mapstructure:"focus"`
}

// LogsKeys is the binding that cycles the hovered agent's own logs through the info pane:
// its setup log, its teardown log, then the background service's own log filtered to
// that agent, and a press past the last one closes the pane.
type LogsKeys struct {
	// Cycle the hovered agent's logs in the info pane.
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

// AgentsKeys are the bindings for the agents list.
type AgentsKeys struct {
	// Archive the hovered agent, which stops it and moves it to the archive screen rather than destroying it.
	Archive []string `mapstructure:"archive"`
	// Step out of a workflow scope, or quit at the root.
	Back []string `mapstructure:"back"`
	// Review what the hovered agent has changed in its worktree, with the diff in the stage and
	// the review threads in the sidebar. The key does nothing for an agent whose workflow does
	// not make worktrees, since there is no base branch to measure one against, and the legend
	// leaves it off there.
	Review []string `mapstructure:"review"`
	// Hide the hovered agent from this list.
	Hide []string `mapstructure:"hide"`
	// Move the hovered agent to the next tag in the tags list, and past the last
	// one back to untagged. Nothing but you ever sets a tag.
	Tag []string `mapstructure:"tag"`
	// Start a new agent.
	New []string `mapstructure:"new"`
	// Attach to the hovered agent.
	Open []string `mapstructure:"open"`
	// Open the pull request the hovered agent recorded, in your browser. The key does
	// nothing on an agent with none recorded, and the legend leaves it off there.
	OpenPR []string `mapstructure:"open_pr"`
	// Quit the panel.
	Quit []string `mapstructure:"quit"`
	// Rename the hovered agent.
	Rename []string `mapstructure:"rename"`
	// Search every conversation the panel holds for what you type, the archived ones included.
	Search []string `mapstructure:"search"`
	// Toggles visibility of hidden agents alongside the rest.
	ShowHidden []string `mapstructure:"show_hidden"`
}

// WorkflowsKeys are the bindings that act on a workflow. New is global. Rename,
// edit, and archive apply while the cursor is on a workflow header.
type WorkflowsKeys struct {
	// Put an agent on a branch or a pull request that already exists, in the
	// workflow under the cursor.
	Adopt []string `mapstructure:"adopt"`
	// Archive the workflow under the cursor, which archives the agents inside it.
	Archive []string `mapstructure:"archive"`
	// Edit the workflow under the cursor.
	Edit []string `mapstructure:"edit"`
	// Create a workflow. Works from anywhere in the list.
	New []string `mapstructure:"new"`
	// Rename the workflow under the cursor.
	Rename []string `mapstructure:"rename"`
}

// ReviewKeys are the bindings for the review screen, where the diff is in the stage and the
// threads are in the sidebar. Paging and panning are the screen's own, since the shared
// navigation keys move the cursor a row at a time. While a message is being composed or a
// pattern typed, every other key is a character, so the keys that close those are fixed rather
// than configured, as is the vim recentring chord zz, zt, zb.
type ReviewKeys struct {
	// Leave the review for the agents list.
	Back []string `mapstructure:"back"`
	// Write a message on the line under the cursor, replying in the thread already there
	// or opening a new one.
	Compose []string `mapstructure:"compose"`
	// Delete the thread under the cursor, with every message in it.
	DeleteThread []string `mapstructure:"delete_thread"`
	// Open the file under the cursor in your editor, on the line the cursor is on.
	EditFile []string `mapstructure:"edit_file"`
	// Scroll down half a screen.
	HalfPageDown []string `mapstructure:"half_page_down"`
	// Scroll up half a screen.
	HalfPageUp []string `mapstructure:"half_page_up"`
	// Jump to the next file in the diff.
	NextFile []string `mapstructure:"next_file"`
	// Jump to the next hunk.
	NextHunk []string `mapstructure:"next_hunk"`
	// Jump to the next line matching the pattern the filter key searched for.
	NextMatch []string `mapstructure:"next_match"`
	// Jump to the next line carrying a review thread.
	NextThread []string `mapstructure:"next_thread"`
	// Scroll down a screen.
	PageDown []string `mapstructure:"page_down"`
	// Scroll up a screen.
	PageUp []string `mapstructure:"page_up"`
	// Pan the diff left, for a line wider than the pane.
	PanLeft []string `mapstructure:"pan_left"`
	// Pan back to the start of the line.
	PanReset []string `mapstructure:"pan_reset"`
	// Pan the diff right.
	PanRight []string `mapstructure:"pan_right"`
	// Jump to the previous file in the diff.
	PrevFile []string `mapstructure:"prev_file"`
	// Jump to the previous hunk.
	PrevHunk []string `mapstructure:"prev_hunk"`
	// Jump to the previous line matching the pattern.
	PrevMatch []string `mapstructure:"prev_match"`
	// Jump to the previous line carrying a review thread.
	PrevThread []string `mapstructure:"prev_thread"`
	// Quit the panel.
	Quit []string `mapstructure:"quit"`
	// Settle the thread under the cursor, which marks it resolved without deleting it.
	Resolve []string `mapstructure:"resolve"`
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
	// Destroy every archived agent for good, however many there are. The panel hands
	// the work to a background job, since tearing down that many agents takes a while.
	// It asks first.
	Empty []string `mapstructure:"empty"`
	// Open the archive from the agents list.
	Open []string `mapstructure:"open"`
	// Quit the panel.
	Quit []string `mapstructure:"quit"`
	// Put the agent under the cursor back in the panel.
	Restore []string `mapstructure:"restore"`
}

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
