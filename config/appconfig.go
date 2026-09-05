// Package config loads the user's customization from one or more TOML files via viper,
// merged in order so a later file wins. It covers which CLI agents run and the flags it
// takes, the tags an agent can be given, the panel keybindings, and which database, address
// and tmux server this weave is.
package config

import (
	"errors"
	"fmt"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"weave/lib/xslice"

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

// DefaultMaxExchange is how much mail two agents may write each other when the config names no
// limit, set at more than twice the longest exchange the panel has recorded finishing, since a
// cap set too high costs one long loop and a cap set too low severs a review as it concludes.
const DefaultMaxExchange = 40

// DefaultFrameHistory is how many frames a recording keeps for one agent when the config names
// no number. Since weave captures about one frame every two seconds, this equates roughly to 15 minutes
// of frames captured, which should be more than enough to replicate most issues related to state transitions
// or reading data off of an agent panel.
const DefaultFrameHistory = 500

// DefaultMaxAgents is how many agents may exist at once when the config names no number,
// well above what anyone opens by hand and low enough to catch a caller in a loop.
const DefaultMaxAgents = 50

// DefaultMaxChildren is how many children one agent may hold at once when the config names no
// number. It is far lower than the panel's own ceiling because they all work in the one
// checkout, so a third is usually a sign an orchestrator is fanning out where it wanted agents
// of their own.
const DefaultMaxChildren = 3

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
	// The mailbox of one agent, and the key that opens it from the agents list.
	Mail MailKeys `mapstructure:"mail"`
	// The marks a person puts on rows to jump between them, as vim's own marks work.
	Marks MarksKeys `mapstructure:"marks"`
	// Navigation, shared by every menu so movement is the same everywhere.
	Nav NavKeys `mapstructure:"nav"`
	// The review screen, which draws an agent's diff in the stage and its review
	// threads in the sidebar.
	Review ReviewKeys `mapstructure:"review"`
	// The schedules screen, and the key that opens it from the agents list.
	Schedules SchedulesKeys `mapstructure:"schedules"`
	// Choosing several agents at once, shared by the two screens that draw a tree.
	Selection SelectionKeys `mapstructure:"selection"`
	// The key that opens a shell in the hovered agent's directory, shared by every
	// screen that lists agents.
	Terminal TerminalKeys `mapstructure:"terminal"`
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

// LogsKeys are the bindings that read the hovered agent's own logs in the info pane. Open
// cycles the most recent run of one of its scripts, then the background service's own log
// filtered to that agent, and a press past the last one closes the pane. Older steps back
// through the runs before the one on screen, wrapping round to the most recent.
type LogsKeys struct {
	// Cycle the hovered agent's logs in the info pane.
	Open []string `mapstructure:"open"`
	// Step back to the run before the one the info pane is showing.
	Older []string `mapstructure:"older"`
}

// FilterKeys are the bindings for the search over the list on screen. It is its own group rather
// than one of a screen's, since it acts the same on all of them. While the input is open every
// other key types into the pattern, so the keys that close it are fixed rather than configured,
// and the keys that walk the matches answer only once the search stands.
type FilterKeys struct {
	// Search the list on screen for what you type, as a regular expression.
	Open []string `mapstructure:"open"`
	// Move the cursor to the next row matching the search, wrapping at the end of the list.
	NextMatch []string `mapstructure:"next_match"`
	// Move the cursor to the previous row matching the search.
	PrevMatch []string `mapstructure:"prev_match"`
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

// TerminalKeys is the binding that opens a shell in the info pane, rooted at the hovered
// agent's own directory. It is its own group rather than one of a screen's, since the
// archive lists agents with directories too. The shell stays where it opened as the cursor
// moves, since respawning its pane would kill whatever is running in it, so pressing the key
// hands it the keyboard and only moves it when the hovered agent is rooted somewhere else.
type TerminalKeys struct {
	// Open a shell in the hovered agent's directory, focus the one already there, or move it here.
	Open []string `mapstructure:"open"`
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
	// Start a new agent.
	New []string `mapstructure:"new"`
	// Attach a child agent to the hovered agent, which runs in that agent's own checkout
	// with no worktree of its own and is archived along with it.
	Child []string `mapstructure:"child"`
	// Put the hovered agent's unread mark back, or take it off, for the times weave's own
	// guess about what you have read is wrong. The mark you set by hand stands until you
	// move the cursor off the row.
	ToggleViewed []string `mapstructure:"toggle_viewed"`
	// Read the mail the hovered agent has sent and been sent, with the mailbox in the
	// sidebar and the message under the cursor in the stage.
	Mail []string `mapstructure:"mail"`
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

// MarksKeys are the bindings for the marks a person puts on rows, which work the way vim's own
// do: one lowercase letter, held by at most one row whether that is an agent or a workflow, set
// by the key and then the letter, and lost when the row is archived.
type MarksKeys struct {
	// Set a mark on the row under the cursor, agent or workflow, then the letter to set, or
	// the letter it already holds to take the mark off it.
	Set []string `mapstructure:"set"`
	// Jump the cursor to a marked row, then the letter to jump to, or the jump key again to
	// go back to where the last jump left from.
	Jump []string `mapstructure:"jump"`
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

// MailKeys are the bindings for the mailbox screen, which draws an agent's mail grouped
// into threads in the sidebar, and the message the cursor is standing on in the stage.
type MailKeys struct {
	// Leave the mailbox for the agents list.
	Back []string `mapstructure:"back"`
	// Quit the panel.
	Quit []string `mapstructure:"quit"`
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
	// Put the diff the panel is holding on screen, once the sidebar says the worktree has
	// moved under you. The diff is never swapped in on its own, since that would move the
	// code out from under a comment being written. With nothing held, this reads the
	// worktree again.
	Reload []string `mapstructure:"reload"`
	// Raise a doorbell again for every comment you released whose own ran out of time
	// before the agent read it, which is the only thing that reaches an agent that was
	// not listening when you let the review go.
	Resend []string `mapstructure:"resend"`
	// Settle the thread under the cursor, which marks it resolved without deleting it.
	Resolve []string `mapstructure:"resolve"`
	// Fold away the file the cursor is in once you have read it, or open it again. A file
	// the agent writes to afterwards comes back on its own.
	ToggleViewed []string `mapstructure:"toggle_viewed"`
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

// Hooks is where weave finds the scripts a workflow hangs on a moment in its agents' lives.
type Hooks struct {
	// Directory for hooks: one executable file per hook, named after the hook.
	// A hook is a script or program weave runs at a moment a workflow names it against, in the
	// agent's own directory, with agent info in WEAVE_AGENT as JSON and the moment in
	// WEAVE_TRIGGER. Default is ~/.config/weave/hooks.
	Dir string `mapstructure:"dir"`
}

// Worktrees is where a workflow's generated git worktree provisioner puts what it creates.
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
	// How much mail two agents may write each other before a send between them is
	// refused. It counts the pair in both directions over every thread, so opening a new
	// subject is not a way around it. When a send is refused weave marks a row for
	// attention and tells the recipient its mail was stopped, so an exchange that is cut
	// off does not leave the other side waiting on a reply that will never arrive.
	MaxExchange int `mapstructure:"max_exchange"`
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

// GetMaxExchange is how much mail two agents may write each other. An unset limit takes the
// default; a negative one is an error rather than the default, for the same reason an
// unparseable deadline is: a config somebody wrote and weave quietly ignored is worse than one
// it refuses.
func (m Mail) GetMaxExchange() (int, error) {
	if m.MaxExchange == 0 {
		m.MaxExchange = DefaultMaxExchange
	}
	if m.MaxExchange < 0 {
		return 0, fmt.Errorf("the mail exchange limit must be positive, not %d", m.MaxExchange)
	}

	return m.MaxExchange, nil
}

// Debug is what weave records about its own reading of an agent.
type Debug struct {
	// Whether the pane capture behind each reading of an agent is kept under the cache
	// directory.
	Frames bool `mapstructure:"frames"`
	// How many frames are kept for one agent before the oldest is dropped. A number at or
	// below zero takes the default.
	FrameHistory int `mapstructure:"frame_history"`
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
	// The viewer for a provisioner's or deprovisioner's own output, plain text rather than the
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
	// Where weave keeps what it generates: the service log, the built-in scripts it
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
	MaxAgents int `mapstructure:"max_agents"`
	// How many children one agent may hold at once, past which attaching another is refused.
	// Children share their parent's checkout, so this is deliberately a much lower ceiling
	// than max_agents.
	MaxChildren int       `mapstructure:"max_children"`
	Harness     Harnesses `mapstructure:"harness"`
	// Where the panel's sidebar, the stage beside it, and the info pane under the stage sit, and how much of the popup each takes.
	Layout Layout `mapstructure:"layout"`
	// Panel keybindings, scoped to the screen each binding acts in.
	Keybindings Keybindings `mapstructure:"keybindings"`
	// Where weave finds the scripts a workflow hangs on a moment in its agents' lives.
	Hooks Hooks `mapstructure:"hooks"`
	// Where a workflow's generated worktree provisioner creates its worktrees.
	Worktrees Worktrees `mapstructure:"worktrees"`
	// The standing text every agent is started with, whatever workflow it is in.
	Context Context `mapstructure:"context"`
	// How mail between agents is delivered.
	Mail Mail `mapstructure:"mail"`
	// The log the background service writes, which the panel's log key pages.
	Log Log `mapstructure:"log"`
	// Puts weave into debug mode which records additional information used to help diagnose issues.
	Debug Debug `mapstructure:"debug"`
	// Which weave this is: the database, the address, the tmux socket, the cache.
	Connection Connection `mapstructure:"connection"`
}

// Defaults is the configuration a user who has written nothing runs on, and what the
// generated config.toml spells out, so the two agree by construction.
func Defaults() *Config {
	return &Config{
		DefaultHarness: DefaultHarness,
		MaxAgents:      DefaultMaxAgents,
		MaxChildren:    DefaultMaxChildren,
		Layout:         DefaultLayout(),
		Keybindings:    DefaultKeybindings(),
		Mail:           Mail{DeliveryDeadline: DefaultDeliveryDeadline, MaxExchange: DefaultMaxExchange},
		Log: Log{
			Level:         DefaultLogLevel,
			Viewer:        DefaultLogViewer,
			ProcessViewer: DefaultProcessViewer,
		},
		Debug:      Debug{FrameHistory: DefaultFrameHistory},
		Connection: DefaultConnection(),
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

// DefaultKeybindings are the bindings used when the config omits them.
func DefaultKeybindings() Keybindings {
	return Keybindings{
		Agents: AgentsKeys{
			Archive:      []string{"z"},
			Back:         []string{"esc"},
			Review:       []string{"d"},
			Hide:         []string{"h"},
			New:          []string{"c"},
			Child:        []string{"C"},
			ToggleViewed: []string{"u"},
			Mail:         []string{"m"},
			Open:         []string{"enter"},
			OpenPR:       []string{"o"},
			Quit:         []string{"ctrl+c"},
			Rename:       []string{"r"},
			Search:       []string{"s"},
			ShowHidden:   []string{"H"},
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
			Open:      []string{"/"},
			NextMatch: []string{"n"},
			PrevMatch: []string{"N"},
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
			Open:  []string{"l"},
			Older: []string{"L"},
		},
		Nav: NavKeys{
			Bottom: []string{"G"},
			Down:   []string{"j"},
			Top:    []string{"g"},
			Up:     []string{"k"},
		},
		Mail: MailKeys{
			Back: []string{"esc"},
			Quit: []string{"ctrl+c"},
		},
		Marks: MarksKeys{
			Set:  []string{"t"},
			Jump: []string{"'"},
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
			Reload:       []string{"R"},
			Resend:       []string{"a"},
			Resolve:      []string{"r"},
			ToggleViewed: []string{"v"},
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
		Terminal: TerminalKeys{
			Open: []string{"T"},
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

// validateLayout rejects a layout tmux could not lay the panes out at. Zero is how an omitted
// size reads, so it falls back to the default rather than being called a mistake.
func validateLayout(layout Layout) error {
	if err := validatePaneSize("layout.sidebar_width", layout.SidebarWidth); err != nil {
		return err
	}
	if err := validatePaneSize("layout.info_height", layout.InfoHeight); err != nil {
		return err
	}
	if err := validatePaneSize("layout.details_height", layout.DetailsHeight); err != nil {
		return err
	}
	if err := validateSide("layout.sidebar_side", layout.SidebarSide, SidebarSideLeft, SidebarSideRight); err != nil {
		return err
	}

	return validateSide("layout.info_side", layout.InfoSide, InfoSideBelow, InfoSideAbove)
}

// validatePaneSize rejects a size that is neither a share of what it divides nor a whole
// number of cells, since tmux is given one or the other and 1.5 columns is neither.
func validatePaneSize(key string, size float64) error {
	switch {
	case size < 0:
		return fmt.Errorf("%s: %v is not a size", key, size)
	case size >= 1 && size != math.Trunc(size):
		return fmt.Errorf("%s: %v is neither a share below one nor a whole number of cells", key, size)
	}

	return nil
}

// validateSide rejects a side that names neither of the two a region can sit on.
func validateSide(key, side string, allowed ...string) error {
	trimmed := strings.TrimSpace(side)
	if trimmed == "" || slices.Contains(allowed, trimmed) {
		return nil
	}

	return fmt.Errorf("%s: %q is not one of %s", key, side, strings.Join(allowed, ", "))
}

// validate rejects the settings the panel could not run on, so a config that cannot work
// says so when it is read rather than when something tries to draw with it.
func (c Config) validate() error {
	if err := validateLog(c.Log); err != nil {
		return err
	}
	// A negative ceiling would read as zero and take the default, leaving the file saying
	// one thing while the panel does another.
	if c.MaxAgents < 0 {
		return fmt.Errorf("max_agents: %d is not a number of agents", c.MaxAgents)
	}
	if c.MaxChildren < 0 {
		return fmt.Errorf("max_children: %d is not a number of agents", c.MaxChildren)
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

// GetHooksDir is where weave looks for the scripts a workflow hangs on a moment: the
// configured directory, or ~/.config/weave/hooks when the config names none.
func (c Config) GetHooksDir() (string, error) {
	if dir := strings.TrimSpace(c.Hooks.Dir); dir != "" {
		return ExpandHome(dir), nil
	}

	home, err := configHome()
	if err != nil {
		return "", err
	}

	return filepath.Join(home, "hooks"), nil
}

// GetWorktreesDir is where a generated worktree provisioner creates its worktrees: the
// configured directory, or ~/.local/state/weave/worktrees when the config names none. The
// default is filled in here rather than into Config for the reason the hooks directory's
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
// the reason the hooks directory's is, since one machine's home directory spelled into
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
	SourceDerived     Source = "derived default"
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
	if c.MaxChildren == 0 {
		c.MaxChildren = DefaultMaxChildren
	}
	if c.Layout.SidebarWidth == 0 {
		c.Layout.SidebarWidth = DefaultSidebarWidth
	}
	if c.Layout.InfoHeight == 0 {
		c.Layout.InfoHeight = DefaultInfoHeight
	}
	if c.Layout.DetailsHeight == 0 {
		c.Layout.DetailsHeight = DefaultDetailsHeight
	}
	if strings.TrimSpace(c.Layout.SidebarSide) == "" {
		c.Layout.SidebarSide = DefaultSidebarSide
	}
	if strings.TrimSpace(c.Layout.InfoSide) == "" {
		c.Layout.InfoSide = DefaultInfoSide
	}
	if strings.TrimSpace(c.Log.Level) == "" {
		c.Log.Level = DefaultLogLevel
	}
	if strings.TrimSpace(c.Log.Viewer) == "" {
		c.Log.Viewer = DefaultLogViewer
	}
	if strings.TrimSpace(c.Log.ProcessViewer) == "" {
		c.Log.ProcessViewer = DefaultProcessViewer
	}
	if c.Debug.FrameHistory <= 0 {
		c.Debug.FrameHistory = DefaultFrameHistory
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
		if _, set := LookupEnv(variable); set {
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
	return strings.Join(xslice.Map(layers, func(layer Layer) string { return layer.Path }), ", ")
}
