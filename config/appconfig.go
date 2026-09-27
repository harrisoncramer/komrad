// Package config loads the user's customization from one or more TOML files via viper,
// merged in order so a later file wins. It covers which CLI agents run and the flags it
// takes, the tags an agent can be given, the panel keybindings, and which database, address
// and tmux server this komrad is.
package config

import (
	"cmp"
	"errors"
	"fmt"
	"math"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	"komrad/lib/xslice"

	"github.com/go-viper/mapstructure/v2"
	"github.com/spf13/viper"
)

// errLayerAbsent marks a config layer that is not on disk. It is answered rather than
// raised for the default path, since not having written a config is the ordinary install.
var errLayerAbsent = errors.New("config layer is absent")

// readerSuffix turns an engine's login into the name of its read-only login.
const readerSuffix = "_reader"

// DefaultLogLevel is how much the service writes when the config says nothing. Failures
// are written at error and everything else at debug, so the file stays empty while the
// service works.
const DefaultLogLevel = "info"

// DefaultDeliveryDeadline is how long mail stays deliverable when the config names no
// duration. Long enough that an agent left alone over lunch still gets its mail, short
// enough that nothing chases a recipient nobody is coming back to.
const DefaultDeliveryDeadline = "3h"

// DefaultMaxExchange is how much mail two agents may write each other when the config names no
// limit, set at more than twice the longest exchange the panel has recorded finishing, since a
// cap set too high costs one long loop and a cap set too low severs a review as it concludes.
const DefaultMaxExchange = 40

// DefaultFrameHistory is how many frames a recording keeps for one agent when the config names
// no number. Since komrad captures about one frame every two seconds, this equates roughly to 15 minutes
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

// DefaultImplementerAutoCompaction is the conversation size an implementer compacts at.
const DefaultImplementerAutoCompaction = 250_000

// DefaultReviewerAutoCompaction is the conversation size a reviewer compacts at.
const DefaultReviewerAutoCompaction = 250_000

// DefaultOrchestratorAutoCompaction is the conversation size an orchestrator compacts at.
const DefaultOrchestratorAutoCompaction = 150_000

// The connection settings a config that names none runs on. They live here rather than as
// envDefault tags on the runtime config so that the built-in default sits underneath the
// file, which is where the precedence chain wants it. A tag default would sit beside the
// environment instead and there would be no way to tell it apart from a value someone set.
//
// ReadOnlyDatabaseURL and CacheDir have no constant because neither is a fixed string: one
// is derived from whichever database url wins and the other from the platform's cache
// directory, both after the file and the environment have been resolved.
const (
	// The default engine's login on the Postgres `mise run docker:up` starts and provisions,
	// so a checkout works with nothing exported.
	DefaultDatabaseURL = "postgres://komrad_engine_default:komrad@localhost:5460/komrad?sslmode=disable"
	// The login that owns that Postgres's schema, used while database_url is the default.
	DefaultOwnerDatabaseURL = "postgres://komrad:komrad@localhost:5460/komrad?sslmode=disable"
	// Loopback, because the endpoint has no auth and its writes can spawn agents.
	DefaultAddr = "127.0.0.1:8787"
	// The socket komrad's own tmux server listens on, named for the tool.
	DefaultTmuxSocket = "komrad"
)

// DefaultHarness is the CLI a new agent runs when the config names none, spelled here
// rather than taken from the registry since this is a default a user overrides.
const DefaultHarness = "claude"

// The layout the panel lays itself out at when the config names none. The info pane opens
// tall enough to read a log in without pushing the agent above it off the screen.
const (
	DefaultSidebarSide   = SidebarSideLeft
	DefaultSidebarWidth  = 33
	DefaultInfoSide      = InfoSideBelow
	DefaultInfoHeight    = 0.4
	DefaultDetailsHeight = 9
)

// Where each region of the panel's layout can sit.
const (
	SidebarSideLeft  = "left"
	SidebarSideRight = "right"
	InfoSideBelow    = "below"
	InfoSideAbove    = "above"
)

// cycleKeyChoices are the pairs the cycle keys may be set to, forward key first.
var cycleKeyChoices = [][]string{
	{"ctrl+a", "ctrl+q"},
	{"alt+n", "alt+p"},
	{"alt+j", "alt+k"},
}

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
	// The documents screen, which reads what the hovered agent has written for a person.
	Documents DocumentsKeys `mapstructure:"documents"`
	// The filter over whichever list is on screen, shared by every screen that
	// draws one. The key that opens it is "/" and is not configured.
	Filter FilterKeys `mapstructure:"filter"`
	// The key that draws the legend under a list, shared by every screen for the
	// same reason navigation is.
	Help HelpKeys `mapstructure:"help"`
	// The git menu on the agents list, and the actions inside it.
	Git GitKeys `mapstructure:"git"`
	// The pair of keys that move between the sidebar, the staged agent and the details.
	Cycle CycleKeys `mapstructure:"cycle"`
	// The marks a person puts on rows to jump between them, as vim's own marks work.
	Marks MarksKeys `mapstructure:"marks"`
	// Navigation, shared by every menu so movement is the same everywhere.
	Nav NavKeys `mapstructure:"nav"`
	// The review screen, which draws an agent's diff in the stage and its review
	// threads in the sidebar.
	Review ReviewKeys `mapstructure:"review"`
	// The keys the mouse wheel presses over the panel, shared by every screen.
	Column ColumnKeys `mapstructure:"column"`
	// The schedules screen, and the key that opens it from the agents list.
	Schedules SchedulesKeys `mapstructure:"schedules"`
	// The keys that move the sidebar's edge, shared by every screen.
	Sidebar SidebarKeys `mapstructure:"sidebar"`
	// Choosing several agents at once, shared by the two screens that draw a tree.
	Selection SelectionKeys `mapstructure:"selection"`
	// The row of tabs a screen draws over its list, and the key that hands it the cursor.
	Tabs TabsKeys `mapstructure:"tabs"`
	// The key that opens a shell in the hovered agent's directory, shared by every
	// screen that lists agents.
	Terminal TerminalKeys `mapstructure:"terminal"`
	// The bindings that create and change workflows, from the agents list.
	Workflows WorkflowsKeys `mapstructure:"workflows"`
	// The worktrees screen, which lists every directory komrad cut for an agent.
	Worktrees WorktreesKeys `mapstructure:"worktrees"`
}

// CycleKeys are the keys that move between the panel's views.
type CycleKeys struct {
	// Move to the next view, then the previous one. One of ["ctrl+a", "ctrl+q"],
	// ["alt+n", "alt+p"] or ["alt+j", "alt+k"].
	Views []string `mapstructure:"views" default:"ctrl+a,ctrl+q"`
}

// NavKeys are the shared navigation bindings every menu uses, so movement is the
// same everywhere rather than configured per menu.
type NavKeys struct {
	// Jump to the last row.
	Bottom []string `mapstructure:"bottom" default:"G"`
	// Move the cursor down.
	Down []string `mapstructure:"down" default:"j"`
	// Move the cursor down a chunk of rows, stopping on the last one.
	PageDown []string `mapstructure:"page_down" default:"ctrl+d"`
	// Move the cursor up a chunk of rows, stopping on the first one.
	PageUp []string `mapstructure:"page_up" default:"ctrl+u"`
	// Jump to the first row, pressed twice the way vim's gg is.
	Top []string `mapstructure:"top" default:"g"`
	// Move the cursor up.
	Up []string `mapstructure:"up" default:"k"`
}

// HelpKeys is the binding that draws the legend under a list. It is its own group
// rather than one of a screen's, since it acts the same on all of them.
type HelpKeys struct {
	// Toggle for the keybindings
	Toggle []string `mapstructure:"toggle" default:"?"`
}

// DetailsKeys are the bindings for the details drawn under a list. Focus moves the cursor
// between the list and the record below it, and while the record holds it the shared
// navigation keys run down its fields rather than the rows above them. The agents list, the
// schedules, and the archive all draw one.
type DetailsKeys struct {
	// Copy the field the details cursor is on to the system clipboard.
	Copy []string `mapstructure:"copy" default:"y"`
}

// FilterKeys are the bindings for the search over the list on screen. It is its own group rather
// than one of a screen's, since it acts the same on all of them. The key that opens it is fixed,
// as are the keys that close it, since while the input is open every other key types into the
// pattern, and the keys that walk the matches answer only once the search stands.
type FilterKeys struct {
	// Move the cursor to the next row matching the search, wrapping at the end of the list.
	NextMatch []string `mapstructure:"next_match" default:"n"`
	// Move the cursor to the previous row matching the search.
	PrevMatch []string `mapstructure:"prev_match" default:"N"`
}

// SelectionKeys are the bindings for choosing several agents at once. They are shared by
// the agents list and the archive rather than set per screen, since the gesture is the same
// on either tree. While anything is selected that screen is in multi-select mode, where a
// gutter marks the chosen rows and the keys that act on one agent stand down.
type SelectionKeys struct {
	// Drop the whole selection, which is the way back out of multi-select mode.
	Clear []string `mapstructure:"clear" default:"esc"`
	// Take in every agent between the cursor and the nearest already-selected one.
	Range []string `mapstructure:"range" default:"V"`
	// Take the row under the cursor into the selection, or back out of it.
	Toggle []string `mapstructure:"toggle" default:"v"`
}

// TabsKeys is the binding for the row of tabs a screen draws over its list.
type TabsKeys struct {
	// Hand the keys to the row of tabs over the list; escape hands them back.
	Focus []string `mapstructure:"focus" default:"f"`
}

// TerminalKeys is the binding that opens a shell in the info pane, rooted at the hovered
// agent's own directory. It is its own group rather than one of a screen's, since the
// archive lists agents with directories too. The shell stays where it opened as the cursor
// moves, since respawning its pane would kill whatever is running in it, so pressing the key
// hands it the keyboard and only moves it when the hovered agent is rooted somewhere else.
type TerminalKeys struct {
	// Open a shell in the hovered agent's directory, focus the one already there, or move it here.
	Open []string `mapstructure:"open" default:"T"`
}

// FormKeys are the keys a form answers, read before the key reaches the box being typed into and
// so not something a config sets.
type FormKeys struct {
	// Abort the form without quitting the panel.
	Back []string
	// Open the message being written in $EDITOR, in every box that takes prose.
	Editor []string
	// Abort the form and quit the panel.
	Quit []string
	// Submit the form, plain enter belonging to the text inputs.
	Submit []string
	// Send a review as it stands and stop the agent where it is, beside the submit key that
	// sends without interrupting.
	SendNow []string
}

// FixedFormKeys is the set every form answers to.
func FixedFormKeys() FormKeys {
	return FormKeys{
		Back:    []string{"esc"},
		Editor:  []string{"ctrl+e"},
		Quit:    []string{"ctrl+c"},
		Submit:  []string{"ctrl+s"},
		SendNow: []string{"ctrl+x"},
	}
}

// AgentsKeys are the bindings for the agents list.
type AgentsKeys struct {
	// Archive the hovered agent, which stops it and moves it to the archive screen rather than destroying it.
	Archive []string `mapstructure:"archive" default:"z"`
	// Step out of a workflow scope, or quit at the root.
	Back []string `mapstructure:"back" default:"esc"`
	// Review what the hovered agent has changed in its worktree, with the diff and the review
	// threads drawn beside each other across the window. The key does nothing for an agent whose
	// workflow does not make worktrees, since there is no base branch to measure one against, and
	// the legend leaves it off there.
	Review []string `mapstructure:"review" default:"d"`
	// Pause the hovered agent where it stands, or unpause one you have already paused. Komrad
	// waits for the agent to come to rest before it claims the worktree, and refuses rather than
	// claiming it when the agent will not stop or its pane cannot be read.
	Pause []string `mapstructure:"pause" default:"p"`
	// Unpause the hovered agent without typing anything at it, which is the way out of a pause
	// whose agent will not take the words komrad normally sends it.
	ReleaseSilently []string `mapstructure:"release_silently" default:"P"`
	// Hide the hovered agent from this list.
	Hide []string `mapstructure:"hide" default:"h"`
	// Start a new agent.
	New []string `mapstructure:"new" default:"c"`
	// Attach a child agent to the hovered agent, which runs in that agent's own checkout
	// with no worktree of its own and is archived along with it.
	Child []string `mapstructure:"child" default:"C"`
	// Attach to the hovered agent, or to the workflow's own orchestrator when the cursor is on a
	// workflow. Either one is started first if it is not running.
	Open []string `mapstructure:"open" default:"enter"`
	// Quit the panel.
	Quit []string `mapstructure:"quit" default:"ctrl+c"`
	// Rename the hovered agent.
	Rename []string `mapstructure:"rename" default:"r"`
	// Retry the launch of an agent that failed to create, opening a form where the flags can be
	// corrected before confirming.
	Retry []string `mapstructure:"retry" default:"R"`
	// Search every conversation the panel holds for what you type, the archived ones included.
	Search []string `mapstructure:"search" default:"s"`
	// Toggles visibility of hidden agents alongside the rest.
	ShowHidden []string `mapstructure:"show_hidden" default:"H"`
}

// MarksKeys are the bindings for the marks a person puts on rows, which work the way vim's own
// do: one lowercase letter, held by at most one row whether that is an agent or a workflow, set
// by the key and then the letter, and lost when the row is archived.
type MarksKeys struct {
	// Set a mark on the row under the cursor, agent or workflow, then the letter to set, or
	// the letter it already holds to take the mark off it.
	Set []string `mapstructure:"set" default:"m"`
	// Jump the cursor to a marked row, then the letter to jump to, or the jump key again to
	// go back to where the last jump left from.
	Jump []string `mapstructure:"jump" default:"'"`
}

// GitKeys are the bindings for the git menu, which opens beside the agents list and holds the
// actions that act on the hovered agent's branch.
type GitKeys struct {
	// Open the git menu on the hovered agent.
	Open []string `mapstructure:"open" default:"b"`
	// Open the hovered agent's current pull request in your browser: the newest one opened from
	// its branch that is not finished, else the newest finished one. The entry says so rather
	// than acting on an agent with none.
	OpenPullRequest []string `mapstructure:"open_pull_request" default:"o"`
	// Bring the hovered agent's worktree up to date on the latest base, which asks first and
	// says what it would do to the branch and to the files.
	Rebase []string `mapstructure:"rebase" default:"r"`
}

// WorkflowsKeys are the bindings that act on a workflow.
type WorkflowsKeys struct {
	// Put an agent on a branch that already exists, in the workflow under the
	// cursor.
	Adopt []string `mapstructure:"adopt" default:"a"`
	// Archive the workflow under the cursor, which archives the agents inside it.
	Archive []string `mapstructure:"archive" default:"z"`
	// Edit the workflow under the cursor.
	Edit []string `mapstructure:"edit" default:"e"`
	// Create a workflow. Works from anywhere in the list.
	New []string `mapstructure:"new" default:"w"`
	// Rename the workflow under the cursor.
	Rename []string `mapstructure:"rename" default:"r"`
}

// DocumentsKeys are the bindings for the documents screen, which lists the documents one
// agent has written and draws the one under the cursor beside the list.
type DocumentsKeys struct {
	// Leave the documents screen for the agents list.
	Back []string `mapstructure:"back" default:"esc"`
	// Open the document under the cursor in your editor, in place of the document drawn
	// beside the list. Quitting the editor, or pressing the key again to close it, saves what it
	// has written to the file.
	Edit []string `mapstructure:"edit" default:"e"`
	// Step back to an earlier write of the document under the cursor, drawing what that write
	// changed.
	Older []string `mapstructure:"older" default:"<"`
	// Step forward to a later write, and past the newest back to the document as it stands.
	Newer []string `mapstructure:"newer" default:">"`
	// Open the documents screen from the agents list, on the hovered agent's documents, or
	// its orchestrator's on a workflow's row.
	Open []string `mapstructure:"open" default:"i"`
	// Quit the panel.
	Quit []string `mapstructure:"quit" default:"ctrl+c"`
}

// WorktreesKeys are the bindings for the worktrees screen.
type WorktreesKeys struct {
	// Leave the worktrees screen for the agents list.
	Back []string `mapstructure:"back" default:"esc"`
	// Delete everything on the screen that can go: every directory whose agent is archived,
	// through the agent delete and its hooks, and every directory no agent answers for. A
	// directory with a live agent in it is never deleted.
	Clear []string `mapstructure:"clear" default:"D"`
	// Delete the directory under the cursor. One with an archived agent goes through the
	// agent delete, which runs the workflow's hooks and the cleanup; one with a live agent
	// offers to archive it first; one nothing answers for is removed on its own. It asks first.
	Delete []string `mapstructure:"delete" default:"d"`
	// Jump to the agent in the directory under the cursor, on the agents list or in the
	// archive.
	JumpToAgent []string `mapstructure:"jump_to_agent" default:"J"`
	// Open the worktrees screen from the agents list or the archive, landing on the hovered
	// agent's own directory.
	Open []string `mapstructure:"open" default:"t"`
	// Quit the panel.
	Quit []string `mapstructure:"quit" default:"ctrl+c"`
}

// ReviewKeys are the bindings for the review screen, where the diff is in the stage and the
// threads are in the sidebar. Paging and panning are the screen's own, since the shared
// navigation keys move the cursor a row at a time. While a message is being composed or a
// pattern typed, every other key is a character, so the keys that close those are fixed rather
// than configured.
type ReviewKeys struct {
	// Show the change that answered the comment under the cursor, from the round the
	// comment was written on to the round the agent's addressed reply carried, or go back
	// to all changes from there.
	Answer []string `mapstructure:"answer" default:"a"`
	// Leave the review for the agents list.
	Back []string `mapstructure:"back" default:"esc"`
	// Walk back a round through the review, from the whole change to the newest round and
	// on into the older ones, stopping on the oldest.
	Base []string `mapstructure:"base" default:"<"`
	// Walk the other way through the rounds, towards the newest work and on to the whole
	// change.
	BaseForward []string `mapstructure:"base_forward" default:">"`
	// Write a message on the line under the cursor, replying in the thread already there
	// or opening a new one.
	Compose []string `mapstructure:"compose" default:"c"`
	// Open the file under the cursor in your editor, beside the diff, on the line the
	// cursor is on. This is the agent's own worktree rather than a copy of it, so what
	// you write is what the agent finds there, and the key is refused while the agent is
	// working rather than letting you both write at once. It is refused on a round the
	// agent has written past, since those lines have moved on.
	EditFile []string `mapstructure:"edit_file" default:"e"`
	// Jump to the next file in the diff.
	NextFile []string `mapstructure:"next_file" default:"]"`
	// Jump to the next line matching the pattern the filter key searched for.
	NextMatch []string `mapstructure:"next_match" default:"n"`
	// Jump to the next line carrying a review thread.
	NextThread []string `mapstructure:"next_thread" default:"t"`
	// Move the cursor to the next comment nobody has settled. It stops on the last one.
	NextOpenThread []string `mapstructure:"next_open_thread" default:"u"`
	// Pan the diff left, for a line wider than the pane.
	PanLeft []string `mapstructure:"pan_left" default:"h,left"`
	// Pan back to the start of the line.
	PanReset []string `mapstructure:"pan_reset" default:"0"`
	// Pan the diff right.
	PanRight []string `mapstructure:"pan_right" default:"l,right"`
	// Jump to the previous file in the diff.
	PrevFile []string `mapstructure:"prev_file" default:"["`
	// Jump to the previous line matching the pattern.
	PrevMatch []string `mapstructure:"prev_match" default:"N"`
	// Move the cursor to the previous comment nobody has settled. It stops on the first one.
	PrevOpenThread []string `mapstructure:"prev_open_thread" default:"U"`
	// Jump to the previous line carrying a review thread.
	PrevThread []string `mapstructure:"prev_thread" default:"T"`
	// Quit the panel.
	Quit []string `mapstructure:"quit" default:"ctrl+c"`
	// Catch up with the agent, once the band says it has written since you opened the
	// diff. What you are reading never moves on its own, since that would take the code out
	// from under a comment being written, so catching up is always yours to ask for.
	Reload []string `mapstructure:"reload" default:"R"`
	// Settle the thread under the cursor, which marks it resolved without deleting it.
	Resolve []string `mapstructure:"resolve" default:"r"`
	// Draw the whole of the file the cursor is in rather than the changed lines and the
	// three around them, or go back to those. The lines this adds behave like any other:
	// the cursor rests on them and a comment can be written on one. A file stays open
	// while you are reading this change and is drawn tight again when you leave.
	Expand []string `mapstructure:"expand" default:"E"`
	// Fold away the file the cursor is in once you have read it, or open it again. A file
	// the agent writes to afterwards comes back on its own.
	ToggleViewed []string `mapstructure:"toggle_viewed" default:"v"`
}

// ColumnKeys are what the mouse wheel presses over the panel. On a list they move the
// cursor a row, and on the review they move the diff or the thread a few lines.
type ColumnKeys struct {
	// Scroll down.
	ScrollDown []string `mapstructure:"scroll_down" default:"ctrl+n"`
	// Scroll up.
	ScrollUp []string `mapstructure:"scroll_up" default:"ctrl+p"`
}

// SidebarKeys move the edge between the sidebar and whatever is beside it, whether that is
// another pane or a block the panel draws itself.
type SidebarKeys struct {
	// Make the sidebar narrower.
	Narrow []string `mapstructure:"narrow" default:"{"`
	// Make the sidebar wider.
	Widen []string `mapstructure:"widen" default:"}"`
}

// SchedulesKeys are the bindings for the schedules screen, which is its own view
// rather than part of the agents tree. Open enters it from the agents list, and
// the rest act on the hovered schedule once inside.
type SchedulesKeys struct {
	// Archive the schedule under the cursor, which stops it firing. The agents it
	// launched are kept, and the archive can restore it.
	Archive []string `mapstructure:"archive" default:"z"`
	// Leave the schedules screen for the agents list.
	Back []string `mapstructure:"back" default:"esc"`
	// Edit the schedule under the cursor.
	Edit []string `mapstructure:"edit" default:"e"`
	// Write a new schedule.
	New []string `mapstructure:"new" default:"c"`
	// Open the schedules screen from the agents list.
	Open []string `mapstructure:"open" default:"S"`
	// Quit the panel.
	Quit []string `mapstructure:"quit" default:"ctrl+c"`
	// Pause or resume the schedule under the cursor.
	Toggle []string `mapstructure:"toggle" default:"space"`
}

// ArchiveKeys are the bindings for the archive screen, which lists the agents
// and schedules archived from the panel. Open enters it from the agents list, and the rest act
// on the hovered agent once inside.
type ArchiveKeys struct {
	// Leave the archive for the agents list.
	Back []string `mapstructure:"back" default:"esc"`
	// Destroy the agent or schedule under the cursor for good, with its transcript
	// or its run history. Nothing undoes it, so it asks first.
	Delete []string `mapstructure:"delete" default:"d"`
	// Destroy every archived agent for good, however many there are. The panel hands
	// the work to a background job, since tearing down that many agents takes a while.
	// It asks first.
	Empty []string `mapstructure:"empty" default:"D"`
	// Switch the archive between its agents and its schedules.
	Kind []string `mapstructure:"kind" default:"tab"`
	// Open the archive from the agents list.
	Open []string `mapstructure:"open" default:"Z"`
	// Quit the panel.
	Quit []string `mapstructure:"quit" default:"ctrl+c"`
	// Put the agent or schedule under the cursor back in the panel.
	Restore []string `mapstructure:"restore" default:"r"`
	// Put the agent under the cursor back in the panel and open it, which relaunches its
	// conversation in a pane and takes you to it. An agent that never got as far as
	// starting is only restored.
	RestoreOpen []string `mapstructure:"restore_open" default:"R"`
}

// Hooks is where komrad finds the scripts a workflow hangs on a moment in its agents' lives.
type Hooks struct {
	// Directory for hooks: one executable file per hook, named after the hook.
	// A hook is a script or program komrad runs at a moment a workflow names it against, in the
	// agent's own directory, with agent info in KOMRAD_AGENT as JSON and the moment in
	// KOMRAD_TRIGGER. Default is ~/.config/komrad/hooks.
	Dir string `mapstructure:"dir"`
}

// Worktrees is where a workflow's generated git worktree provisioner puts what it creates.
type Worktrees struct {
	// Directory generated worktrees are created under, one subdirectory per agent.
	// Left empty this is ~/.local/state/komrad/worktrees.
	Dir string `mapstructure:"dir"`
}

// GitHubApp is the App an engine fetches and pushes as.
type GitHubApp struct {
	// The App's own id, from its settings page. Left empty the engine has no GitHub identity
	// and git is left to whatever credentials the account already has, which on a machine
	// nobody logs into is none.
	AppID string `mapstructure:"app_id"`
	// The installation the token is minted for, which is what scopes it to the repositories the
	// App was installed on rather than to everything it could reach. GitHub writes it as a
	// number, and it is in the URL of the installation's own settings page.
	InstallationID string `mapstructure:"installation_id"`
	// The PEM the App was created with. On a deployed engine this is fetched into a tmpfs at
	// boot rather than written to disk, so the key does not outlive the machine.
	PrivateKeyPath string `mapstructure:"private_key_path"`
}

// Context is the standing text every agent is started with, above whatever its workflow
// adds. It is where what is true of your machine and how you want to be worked with goes,
// so it does not have to be pasted into every workflow.
type Context struct {
	// The file holding global context, read at each launch.
	// Left empty this is ~/.config/komrad/context.md, but will not trigger a failure if left blank.
	File string `mapstructure:"file"`
}

// Layout is how the panel divides its window between its own list, the details under that
// list, the stage beside them, and the info pane that opens under the stage. A size below
// one is read as a share of what it divides, and one at or above it as a count of cells.
type Layout struct {
	// Which side of the window the panel's sidebar sits on, "left" or "right".
	SidebarSide string `mapstructure:"sidebar_side"`
	// How many columns wide the panel's sidebar opens at. Dragging the divider afterwards moves it, and the panel follows.
	SidebarWidth int `mapstructure:"sidebar_width"`
	// Which side of the stage the info pane opens on, "below" or "above". That pane is where the server log and an editor are drawn.
	InfoSide string `mapstructure:"info_side"`
	// How tall the info pane opens, either a share of the stage's height below one or a whole number of rows.
	InfoHeight float64 `mapstructure:"info_height"`
	// How many lines the details under the list draw, either a share of the sidebar's height below one or a whole number of lines. The marks the hovered row is carrying are spelled out in that same room, ahead of the fields. The chrome around them is added on top, and the two together never take more than half the sidebar's height. Once the details hold the keys they grow past this to show the whole record, as far as the sidebar allows.
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
	// here as waiting. Turning this on edits that file on your behalf.
	TrustLaunchDirs bool `mapstructure:"trust_launch_dirs"`
}

// Harnesses is the per-CLI configuration. There is a field per harness the binary
// implements rather than an open table, so a misspelled name is refused by the same
// unknown-key check as everything else here, and this file lists what can be run.
type Harnesses struct {
	Claude Harness `mapstructure:"claude"`
	Codex  Codex   `mapstructure:"codex"`
}

// Usage is how much agents may run and how large their conversations may grow.
type Usage struct {
	// How many unarchived agents this engine may run at once, past which a create is refused.
	MaxAgents int `mapstructure:"max_agents"`
	// What an implementer, the agent that lands work on a branch of its own, may use.
	Implementer ImplementerUsage `mapstructure:"implementer"`
	// What a reviewer, the child that reads another agent's work, may use.
	Reviewer ReviewerUsage `mapstructure:"reviewer"`
	// What a workflow's orchestrator may use.
	Orchestrator OrchestratorUsage `mapstructure:"orchestrator"`
}

// ImplementerUsage is what one implementer may use.
type ImplementerUsage struct {
	// The conversation size in tokens an agent is held under, summarizing its history a little
	// before it gets there. Lower costs less per turn and forgets more. 0 leaves the agent CLI's
	// own default, and a value the CLI will not take is moved to the nearest one it will.
	AutoCompaction int `mapstructure:"auto_compaction"`
	// How many children, such as its reviewer, one implementer may hold at once. Children share
	// their parent's checkout, so this is deliberately far lower than max_agents.
	MaxChildren int `mapstructure:"max_children"`
}

// ReviewerUsage is what one reviewer may use.
type ReviewerUsage struct {
	// The conversation size in tokens an agent is held under, summarizing its history a little
	// before it gets there. Lower costs less per turn and forgets more. 0 leaves the agent CLI's
	// own default, and a value the CLI will not take is moved to the nearest one it will.
	AutoCompaction int `mapstructure:"auto_compaction"`
}

// OrchestratorUsage is what one orchestrator may use.
type OrchestratorUsage struct {
	// The conversation size in tokens an agent is held under, summarizing its history a little
	// before it gets there. Lower costs less per turn and forgets more. 0 leaves the agent CLI's
	// own default, and a value the CLI will not take is moved to the nearest one it will.
	AutoCompaction int `mapstructure:"auto_compaction"`
	// How many agents one orchestrator may have spawned and running at once, not counting their
	// children. 0 sets no limit of its own, leaving max_agents as the cap.
	MaxChildren int `mapstructure:"max_children"`
}

// NewUsage is the usage limits a config that names none runs on.
func NewUsage() Usage {
	return Usage{
		MaxAgents: DefaultMaxAgents,
		Implementer: ImplementerUsage{
			AutoCompaction: DefaultImplementerAutoCompaction,
			MaxChildren:    DefaultMaxChildren,
		},
		Reviewer:     ReviewerUsage{AutoCompaction: DefaultReviewerAutoCompaction},
		Orchestrator: OrchestratorUsage{AutoCompaction: DefaultOrchestratorAutoCompaction},
	}
}

func (u Usage) validate() error {
	// A negative ceiling would read as zero and take the default, leaving the file saying
	// one thing while the panel does another.
	if u.MaxAgents < 0 {
		return fmt.Errorf("usage.max_agents: %d is not a number of agents", u.MaxAgents)
	}
	if u.Implementer.MaxChildren < 0 {
		return fmt.Errorf("usage.implementer.max_children: %d is not a number of agents", u.Implementer.MaxChildren)
	}
	if u.Orchestrator.MaxChildren < 0 {
		return fmt.Errorf("usage.orchestrator.max_children: %d is not a number of agents", u.Orchestrator.MaxChildren)
	}
	if u.Implementer.AutoCompaction < 0 {
		return fmt.Errorf("usage.implementer.auto_compaction: %d is not a number of tokens", u.Implementer.AutoCompaction)
	}
	if u.Reviewer.AutoCompaction < 0 {
		return fmt.Errorf("usage.reviewer.auto_compaction: %d is not a number of tokens", u.Reviewer.AutoCompaction)
	}
	if u.Orchestrator.AutoCompaction < 0 {
		return fmt.Errorf("usage.orchestrator.auto_compaction: %d is not a number of tokens", u.Orchestrator.AutoCompaction)
	}

	return nil
}

// Mail is how long komrad keeps trying to hand mail to the agent it was addressed
// to. Delivery waits on the recipient: an agent that is busy gets its mail as its turn
// ends, and one that has stopped is nudged, so an agent nobody is running holds mail until
// it comes back or the deadline passes.
type Mail struct {
	// How long mail stays deliverable, as a Go duration such as "3h" or "45m". Mail past
	// it is neither delivered nor readable, and the row is kept as a record that it never
	// arrived. A duration komrad cannot read, or one that is not positive, is refused.
	DeliveryDeadline string `mapstructure:"delivery_deadline"`
	// How much mail two agents may write each other before a send between them is
	// refused. It counts the pair in both directions over every thread, so opening a new
	// subject is not a way around it. When a send is refused komrad marks a row for
	// attention and tells the recipient its mail was stopped, so an exchange that is cut
	// off does not leave the other side waiting on a reply that will never arrive. A
	// negative limit is refused.
	MaxExchange int `mapstructure:"max_exchange"`
}

// GetDeliveryDeadline is how long mail stays deliverable, parsed. A duration that will
// not parse is an error rather than the default, since a deadline somebody wrote and komrad
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
// unparseable deadline is: a config somebody wrote and komrad quietly ignored is worse than one
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

// Debug is what komrad records about its own reading of an agent.
type Debug struct {
	// Whether the pane capture behind each reading of an agent is kept under the cache
	// directory.
	Frames bool `mapstructure:"frames"`
	// How many frames are kept for one agent before the oldest is dropped. A number at or
	// below zero takes the default.
	FrameHistory int `mapstructure:"frame_history"`
}

// Log is how much the background service writes about itself. Where it writes is not
// configurable.
type Log struct {
	// How much is written, one of debug, info, warn, or error.
	Level string `mapstructure:"level"`
}

// Connection is which komrad you are talking to: the database it writes, the address it
// serves, the tmux server it runs agents on, and the directory it generates into. Every
// one of these also has a KOMRAD_-prefixed environment variable, and the variable wins when
// it is set, so one shell or one checkout can point somewhere else without editing a file.
//
// These are the settings a second instance is isolated by, which is why they are a table
// rather than four keys at the top level. Overriding the table is overriding the instance.
type Connection struct {
	// The Postgres komrad keeps every agent, workflow, schedule and transcript in, reached as
	// this engine's own login, which is what keeps its rows apart from every other engine's.
	// `komrad engine ensure` creates the login and prints this url.
	DatabaseURL string `mapstructure:"database_url"`
	// The login that owns the schema, used only by `komrad migrate` and `komrad engine ensure`.
	// Left empty this is database_url, or the local owner while database_url is the default.
	OwnerDatabaseURL string `mapstructure:"owner_database_url"`
	// The least-privilege login the MCP query tools connect as.
	// Left empty this is database_url with the engine's reader login, which is its own login
	// with _reader on the end and the same password.
	ReadOnlyDatabaseURL string `mapstructure:"readonly_database_url"`
	// Where the background service listens, and where the panel, the CLI and the MCP
	// endpoint all reach it. Change it to run two komrads on one machine. It has to be a
	// loopback host, since the api has no auth and its writes spawn agents.
	Addr string `mapstructure:"addr"`
	// The tmux socket komrad runs its own server on. Nothing in your normal tmux sees
	// that server, which is why `tmux -L komrad ls` is how you look at it directly.
	TmuxSocket string `mapstructure:"tmux_socket"`
	// Where komrad keeps what it generates: the service log, the built-in scripts it
	// writes out on every start, and the temp files a harness passes a prompt through.
	// Left empty this is ~/Library/Caches/komrad on a Mac and ~/.cache/komrad on Linux.
	// Everything under it is komrad's to delete and rewrite, so nothing of yours belongs here.
	CacheDir string `mapstructure:"cache_dir"`
	// The ssh destination the engine runs on, like user@host, when it is not this machine.
	// Left empty the engine is local and an agent's terminal is shown by moving its own pane
	// into the panel, which only works while the panel and the agents share a tmux server.
	SSH string `mapstructure:"ssh"`
	// How to forward a port from this machine to the engine, as an argv, when ssh is not how you
	// reach it. "{{local_port}}" and "{{remote_port}}" are replaced with the two ends. It should
	// stay in the foreground for as long as the forward is wanted.
	Forward []string `mapstructure:"forward"`
}

// Config is the user's customization.
type Config struct {
	// Which agent CLI a new agent runs when neither the agent nor its workflow
	// names one. It has to be a harness this binary implements, so it is one of
	// the names under [harness] below.
	DefaultHarness string    `mapstructure:"default_harness"`
	Harness        Harnesses `mapstructure:"harness"`
	// How much agents may run and how large their conversations may grow.
	Usage Usage `mapstructure:"usage"`
	// Where the panel's sidebar, the stage beside it, and the info pane under the stage sit, and how much of the window each takes.
	Layout Layout `mapstructure:"layout"`
	// Panel keybindings, scoped to the screen each binding acts in.
	Keybindings Keybindings `mapstructure:"keybindings"`
	// Where komrad finds the scripts a workflow hangs on a moment in its agents' lives.
	Hooks Hooks `mapstructure:"hooks"`
	// Where a workflow's generated worktree provisioner creates its worktrees.
	Worktrees Worktrees `mapstructure:"worktrees"`
	// The GitHub App an engine fetches and pushes as.
	GitHubApp GitHubApp `mapstructure:"github_app"`
	// The standing text every agent is started with, whatever workflow it is in.
	Context Context `mapstructure:"context"`
	// How mail between agents is delivered.
	Mail Mail `mapstructure:"mail"`
	// The log the background service writes, which the panel's log key pages.
	Log Log `mapstructure:"log"`
	// Puts komrad into debug mode which records additional information used to help diagnose issues.
	Debug Debug `mapstructure:"debug"`
	// Which komrad this is: the database, the address, the tmux socket, the cache.
	Connection Connection `mapstructure:"connection"`
}

// Defaults is the configuration a user who has written nothing runs on, and what the
// generated config.toml spells out, so the two agree by construction.
func Defaults() *Config {
	return &Config{
		DefaultHarness: DefaultHarness,
		Usage:          NewUsage(),
		Layout:         DefaultLayout(),
		Keybindings:    DefaultKeybindings(),
		Mail:           Mail{DeliveryDeadline: DefaultDeliveryDeadline, MaxExchange: DefaultMaxExchange},
		Log:            Log{Level: DefaultLogLevel},
		Debug:          Debug{FrameHistory: DefaultFrameHistory},
		Connection:     defaultConnection(),
	}
}

// defaultConnection is the instance a config that names none points at.
func defaultConnection() Connection {
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

// DefaultKeybindings are the bindings used when the config omits them, read from the default tag
// on each field so that a binding and the key it ships with are written in one place.
func DefaultKeybindings() Keybindings {
	var keys Keybindings

	groups := reflect.ValueOf(&keys).Elem()
	for i := range groups.NumField() {
		group := groups.Field(i)
		for j := range group.NumField() {
			group.Field(j).Set(reflect.ValueOf(defaultKeys(group.Type().Field(j))))
		}
	}

	return keys
}

// GetBoundKeys is every key the bindings name, once each and in a settled order.
func (k Keybindings) GetBoundKeys() []string {
	var bound []string

	groups := reflect.ValueOf(k)
	for i := range groups.NumField() {
		group := groups.Field(i)
		for j := range group.NumField() {
			keys, ok := group.Field(j).Interface().([]string)
			if !ok {
				continue
			}

			for _, key := range keys {
				if !slices.Contains(bound, key) {
					bound = append(bound, key)
				}
			}
		}
	}

	slices.Sort(bound)

	return bound
}

// defaultKeys is one field's shipped binding, as the comma-separated list its tag holds. A field
// with no tag, or an empty one, panics rather than binding nothing, since a key that answers
// nowhere is the quietest way for this file to be wrong. TestEveryBindingShipsAKey catches it
// before anybody starts the panel.
func defaultKeys(field reflect.StructField) []string {
	tag, ok := field.Tag.Lookup("default")
	if !ok || tag == "" {
		panic(fmt.Sprintf("keybinding %s has no default tag", field.Name))
	}

	return strings.Split(tag, ",")
}

func validateCycleKeys(keys CycleKeys) error {
	isChoice := slices.ContainsFunc(cycleKeyChoices, func(choice []string) bool {
		return slices.Equal(choice, keys.Views)
	})
	if isChoice {
		return nil
	}

	return fmt.Errorf("keybindings.cycle.views: %q is not one of the pairs on offer: %q", keys.Views, cycleKeyChoices)
}

// validateLayout rejects a layout tmux could not lay the panes out at. Zero is how an omitted
// size reads, so it falls back to the default rather than being called a mistake.
func validateLayout(layout Layout) error {
	if layout.SidebarWidth < 0 {
		return fmt.Errorf("layout.sidebar_width: %d is not a width", layout.SidebarWidth)
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
	if err := c.Usage.validate(); err != nil {
		return err
	}
	if err := newKeyChecker().checkKeybindings(c.Keybindings); err != nil {
		return err
	}
	if err := validateCycleKeys(c.Keybindings.Cycle); err != nil {
		return err
	}
	if err := validateMail(c.Mail); err != nil {
		return err
	}
	if err := validateConnection(c.Connection); err != nil {
		return err
	}

	return validateLayout(c.Layout)
}

// logLevels are the levels the service understands. One it does not know decodes as debug,
// so an unchecked typo turns the log up instead of saying the word was wrong.
var logLevels = []string{"debug", "info", "warn", "error"}

// validateMail rejects the two mail settings the service reads at send time.
func validateMail(mail Mail) error {
	if _, err := mail.GetDeliveryDeadline(); err != nil {
		return fmt.Errorf("mail.delivery_deadline: %w", err)
	}
	if _, err := mail.GetMaxExchange(); err != nil {
		return fmt.Errorf("mail.max_exchange: %w", err)
	}

	return nil
}

// validateConnection rejects an address the service could not listen on or should not, and a
// database url that is not one Postgres would answer.
func validateConnection(connection Connection) error {
	if err := ValidateAddr(connection.Addr); err != nil {
		return fmt.Errorf("connection.addr: %w", err)
	}

	return validateDatabaseURL(connection.DatabaseURL)
}

// loopbackHosts are the hosts that reach only this machine, spelled the ways a person writes them.
var loopbackHosts = []string{"localhost", "::1"}

// ValidateAddr rejects an address the service could not listen on, or should not. The caller
// names where the address came from, since it is a flag as often as it is a setting.
func ValidateAddr(addr string) error {
	trimmed := strings.TrimSpace(addr)
	if trimmed == "" {
		return errors.New("names nothing, which listens on every interface, and the api has no auth")
	}

	host, port, err := net.SplitHostPort(trimmed)
	if err != nil {
		return fmt.Errorf("%q is not a host and port: %w", addr, err)
	}
	if number, err := strconv.Atoi(port); err != nil || number < 1 || number > 65535 {
		return fmt.Errorf("%q is not a port", port)
	}
	if host == "" {
		return fmt.Errorf("%q names no host, which answers on every interface, and the api has no auth", addr)
	}

	if slices.Contains(loopbackHosts, host) {
		return nil
	}

	parsed := net.ParseIP(host)
	if parsed == nil {
		return fmt.Errorf("%q is not an address this machine can answer on", host)
	}
	if !parsed.IsLoopback() {
		return fmt.Errorf("%q reaches this machine from the network, and the api has no auth and its writes spawn agents", host)
	}

	return nil
}

// validateDatabaseURL rejects a url Postgres would not answer.
func validateDatabaseURL(databaseURL string) error {
	parsed, err := url.Parse(strings.TrimSpace(databaseURL))
	if err != nil {
		return fmt.Errorf("connection.database_url: %q is not a url: %w", databaseURL, err)
	}
	if parsed.Scheme != "postgres" && parsed.Scheme != "postgresql" {
		return fmt.Errorf("connection.database_url: %q is a %s url, and komrad stores its records in Postgres",
			databaseURL, cmp.Or(parsed.Scheme, "schemeless"))
	}
	if parsed.Host == "" {
		return fmt.Errorf("connection.database_url: %q names no server to connect to", databaseURL)
	}

	return nil
}

// validateLog rejects a level the service could not act on.
func validateLog(log Log) error {
	level := strings.TrimSpace(log.Level)
	if level == "" || slices.Contains(logLevels, strings.ToLower(level)) {
		return nil
	}

	return fmt.Errorf("log.level: %q is not one of %s", log.Level, strings.Join(logLevels, ", "))
}

// configHome is the directory the panel keeps its own files in, ~/.config/komrad.
func configHome() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}

	return filepath.Join(home, ".config", "komrad"), nil
}

// ExpandHome expands a leading ~ or ~/ to the user's home directory, since nothing below a
// config does it for itself. tmux's new-session -c takes the path literally, and so does
// every open of a file komrad was pointed at. A home directory that cannot be read leaves
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

// GetHooksDir is where komrad looks for the scripts a workflow hangs on a moment: the
// configured directory, or ~/.config/komrad/hooks when the config names none.
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
// configured directory, or ~/.local/state/komrad/worktrees when the config names none. The
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

	return filepath.Join(home, ".local", "state", "komrad", "worktrees"), nil
}

// GetStateDir is where komrad keeps its own per-agent files, such as a released review for the agent to read. It is under
// komrad's own state directory rather than beside the agent's work, since a linked worktree's
// own info/exclude is not read by git and a file put in the worktree would show up in the
// diff being reviewed.
func GetStateDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}

	return filepath.Join(home, ".local", "state", "komrad"), nil
}

// GetContextFile is the standing text every agent is started with: the configured file, or
// ~/.config/komrad/context.md when the config names none. The default is filled in here for
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

// Stack is a config to read: the files named by hand, and the engine laid over them.
type Stack struct {
	Paths  []string
	Engine string
}

// ResolveLayers is the files Load would read, weakest first. Given paths, that is exactly
// those paths, since naming any replaces the default rather than adding to it: being able
// to say "this file and nothing else" is what lets a second instance be isolated from the
// first one's config. Given none, it is the one default path.
//
// An engine is laid over whichever of those two the stack came to, so naming one adds a
// file rather than replacing the config underneath it.
//
// Komrad never looks for a file it was not asked for. There is no sibling convention and
// nothing per directory, so which files are read depends on what was asked for rather than
// on what happens to be lying around.
//
// It is exported because a caller reporting on a config has to name the files, and a
// missing one has no error to read the path out of.
func ResolveLayers(stack Stack) ([]Layer, error) {
	layers, err := namedLayers(stack.Paths)
	if err != nil {
		return nil, err
	}

	if stack.Engine == "" {
		return layers, nil
	}

	overlay, err := EnginePath(stack.Engine)
	if err != nil {
		return nil, err
	}

	return append(layers, Layer{Path: overlay, Named: true}), nil
}

func namedLayers(paths []string) ([]Layer, error) {
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

// LoadStack reads the layers in order and merges them, weakest first, so a later file wins
// where two overlap. A missing file among them is not an error unless it was named, and yields
// nothing rather than the defaults, but a symlink pointing at nothing is, since that is a
// config someone meant to have.
//
// A key the Config struct does not know about is an error rather than silently dropped, so
// a typo is reported rather than quietly doing nothing. The error names the ordered file
// stack beside what the key's table does take.
func LoadStack(stack Stack) (*Config, error) {
	cfg, _, err := LoadWithReport(stack)

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
func LoadWithReport(stack Stack) (*Config, *Report, error) {
	layers, err := ResolveLayers(stack)
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
	if c.Usage.MaxAgents == 0 {
		c.Usage.MaxAgents = DefaultMaxAgents
	}
	if c.Usage.Implementer.MaxChildren == 0 {
		c.Usage.Implementer.MaxChildren = DefaultMaxChildren
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
	if strings.TrimSpace(c.Connection.Addr) == "" {
		c.Connection.Addr = DefaultAddr
	}
	if strings.TrimSpace(c.Connection.DatabaseURL) == "" {
		c.Connection.DatabaseURL = DefaultDatabaseURL
	}
	if strings.TrimSpace(c.Connection.TmuxSocket) == "" {
		c.Connection.TmuxSocket = DefaultTmuxSocket
	}
	if strings.TrimSpace(c.Log.Level) == "" {
		c.Log.Level = DefaultLogLevel
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
	"connection.database_url":          "KOMRAD_DATABASE_URL",
	"connection.readonly_database_url": "KOMRAD_READONLY_DATABASE_URL",
	"connection.owner_database_url":    "KOMRAD_OWNER_DATABASE_URL",
	"connection.addr":                  "KOMRAD_ADDR",
	"connection.tmux_socket":           "KOMRAD_TMUX_SOCKET",
	"connection.cache_dir":             "KOMRAD_CACHE_DIR",
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
			return fmt.Errorf("failed to find a cache directory, set KOMRAD_CACHE_DIR to an absolute path komrad can write to: %w", err)
		}

		c.Connection.CacheDir = filepath.Join(dir, "komrad")
		report.Sources["cache_dir"] = SourceDerived
	} else {
		c.Connection.CacheDir = ExpandHome(c.Connection.CacheDir)
	}

	if c.Connection.ReadOnlyDatabaseURL == "" {
		c.Connection.ReadOnlyDatabaseURL = DeriveReadOnlyURL(c.Connection.DatabaseURL)
		report.Sources["readonly_database_url"] = SourceDerived
	}

	if c.Connection.OwnerDatabaseURL == "" {
		c.Connection.OwnerDatabaseURL = c.Connection.DatabaseURL
		if report.Sources["database_url"] == SourceDefault {
			c.Connection.OwnerDatabaseURL = DefaultOwnerDatabaseURL
		}
		report.Sources["owner_database_url"] = SourceDerived
	}

	return nil
}

// DeriveReadOnlyURL is the url of the reader login belonging to the engine login databaseURL names.
func DeriveReadOnlyURL(databaseURL string) string {
	parsed, err := url.Parse(databaseURL)
	if err != nil {
		return ""
	}

	if parsed.Scheme != "postgres" && parsed.Scheme != "postgresql" {
		return ""
	}

	if parsed.User == nil || parsed.User.Username() == "" {
		return ""
	}

	password, _ := parsed.User.Password()
	parsed.User = url.UserPassword(parsed.User.Username()+readerSuffix, password)

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
