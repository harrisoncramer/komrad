package config

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
