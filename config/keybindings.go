package config

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
)

// Keybindings scopes bindings to the screen they act in. Each binding is a list of
// bubbletea key strings such as "enter", "ctrl+d", or "esc". Every default is a
// single key, leaving the alternatives to you. A binding left unset falls back to
// its built-in default.
type Keybindings struct {
	// The agents list, the panel's main screen.
	Agents AgentsKeys `mapstructure:"agents"`
	// The archive screen.
	Archive ArchiveKeys `mapstructure:"archive"`
	// The archive menu, nested under the menu on the hovered row of the archive.
	ArchiveMenu ArchiveMenuKeys `mapstructure:"archive_menu"`
	// The details under whichever list is on screen: the key that hands them the
	// cursor, and the key that copies the field it lands on.
	Details DetailsKeys `mapstructure:"details"`
	// A diff with a cursor in it, shared by the review and a document's history.
	Diff DiffKeys `mapstructure:"diff"`
	// The documents view, which lists every document agents have written, and the document types.
	Documents DocumentsKeys `mapstructure:"documents"`
	// The menu on the hovered document, over the documents view's own entries.
	DocumentMenu DocumentMenuKeys `mapstructure:"document_menu"`
	// The menu on the document type under the cursor of the documents view.
	DocumentTypesMenu DocumentTypesMenuKeys `mapstructure:"document_types_menu"`
	// The filter over whichever list is on screen, shared by every screen that
	// draws one. The key that opens it is "/" and is not configured.
	Filter FilterKeys `mapstructure:"filter"`
	// The key that draws the legend under a list, shared by every screen for the
	// same reason navigation is.
	Help HelpKeys `mapstructure:"help"`
	// The git menu, nested under the menu on the hovered row, and the actions inside it.
	Git GitKeys `mapstructure:"git"`
	// The jump menu, nested under the menu on a task, a document or a worktree.
	JumpMenu JumpMenuKeys `mapstructure:"jump_menu"`
	// The hide menu, nested under the menu on the agents list.
	HideMenu HideMenuKeys `mapstructure:"hide_menu"`
	// The merge menu, nested under the git menu, which merges the hovered task's pull request.
	Merge MergeKeys `mapstructure:"merge"`
	// The logs screen, which tails what the server logged about one agent.
	Logs LogsKeys `mapstructure:"logs"`
	// The messages komrad pushed, read in the main pane.
	Messages MessagesKeys `mapstructure:"messages"`
	// Navigation, shared by every menu so movement is the same everywhere.
	Nav NavKeys `mapstructure:"nav"`
	// The review screen, which draws a task's diff in the stage and its review
	// threads in the sidebar.
	Review ReviewKeys `mapstructure:"review"`
	// The keys the mouse wheel presses over the panel, shared by every screen.
	Column ColumnKeys `mapstructure:"column"`
	// The schedules screen.
	Schedules SchedulesKeys `mapstructure:"schedules"`
	// The menu on the hovered schedule.
	ScheduleMenu ScheduleMenuKeys `mapstructure:"schedule_menu"`
	// The keys that move the sidebar's edge, shared by every screen.
	Sidebar SidebarKeys `mapstructure:"sidebar"`
	// Choosing several agents at once, shared by the two screens that draw a tree.
	Selection SelectionKeys `mapstructure:"selection"`
	// The menu on the hovered row, which lists the screens that belong to it and the menus nested
	// under it.
	Menu MenuKeys `mapstructure:"menu"`
	// The row of tabs a screen draws over its list, and the key that hands it the cursor.
	Tabs TabsKeys `mapstructure:"tabs"`
	// The row of kinds a screen listing more than one kind of resource draws under its tabs.
	Kinds KindsKeys `mapstructure:"kinds"`
	// The switcher at the top of the panel, which moves between the agents, query,
	// schedules, archive and worktrees from any of them.
	Views ViewsKeys `mapstructure:"views"`
	// The bindings that create and change workflows, from the agents list.
	Workflows WorkflowsKeys `mapstructure:"workflows"`
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

// KindsKeys is the binding for the row of kinds a screen draws under its tabs.
type KindsKeys struct {
	// Hand the keys to the row of kinds, where tab and shift+tab switch the kind listed; escape
	// hands them back.
	Focus []string `mapstructure:"focus" default:"F"`
}

// ViewsKeys are the bindings for the switcher at the top of the panel. Next and Previous open the
// next and previous view and hand the switcher the keys, where they keep stepping through the
// views and esc hands the keys back.
type ViewsKeys struct {
	// Open the next view.
	Next []string `mapstructure:"next" default:"tab"`
	// Open the previous view.
	Previous []string `mapstructure:"previous" default:"shift+tab"`
}

// FixedKeys are the keys every screen, menu and form answers the same way, and so not something a
// config sets.
type FixedKeys struct {
	// Leave the screen, menu or form for the one it was opened from.
	Back []string
	// Open the message being written in $EDITOR, in every box that takes prose.
	Editor []string
	// Open the row under the cursor.
	Open []string
	// Quit the panel.
	Quit []string
	// Submit the form from any field.
	Submit []string
}

// Fixed is the set every screen, menu and form answers to.
func Fixed() FixedKeys {
	return FixedKeys{
		Back:   []string{"esc"},
		Editor: []string{"ctrl+e"},
		Open:   []string{"enter"},
		Quit:   []string{"ctrl+c"},
		Submit: []string{"enter"},
	}
}

// AgentsKeys are the bindings for the agents list.
type AgentsKeys struct {
	// Jump to the first agent in the triage zone at the top of the list.
	Triage []string `mapstructure:"triage" default:"t"`
	// Rename the hovered agent, or the slug of the hovered task.
	Rename []string `mapstructure:"rename" default:"r"`
}

// GitKeys are the bindings for the git menu, which opens from the menu on the hovered task and
// holds the actions that act on its branch.
type GitKeys struct {
	// Copy the address of the hovered task's current pull request to your clipboard.
	CopyPullRequestURL []string `mapstructure:"copy_pull_request_url" default:"c"`
	// Open the merge menu to the right of this one, for merging the hovered task's pull request
	// on the forge and archiving the task after.
	Merge []string `mapstructure:"merge" default:"m"`
	// Open the hovered task's current pull request in your browser: the newest one opened from
	// its branch that is not finished, else the newest finished one. The entry says so rather
	// than acting on a task with none.
	OpenPullRequest []string `mapstructure:"open_pull_request" default:"o"`
	// Send the hovered task's branch to origin, which asks first and says how many commits go
	// up, whether origin's copy is overwritten after a rebase, and what stays uncommitted.
	Push []string `mapstructure:"push" default:"p"`
	// Undo the rebase, am, merge, cherry-pick or revert part-way through the hovered task's
	// worktree, which asks first. The entry names the kind and is off when nothing is in progress.
	Abort []string `mapstructure:"abort" default:"x"`
	// Bring the hovered task's worktree up to date on the latest base, which asks first and
	// says what it would do to the branch and to the files.
	Rebase []string `mapstructure:"rebase" default:"r"`
	// Ask everyone who requested changes on the hovered task's pull request to review it again.
	RequestRereview []string `mapstructure:"request_rereview" default:"v"`
	// Mark the hovered task's pull request a draft, or ready for review when it is one.
	ToggleDraft []string `mapstructure:"toggle_draft" default:"d"`
}

// HideMenuKeys are the bindings for the hide menu, which opens from the menu on the agents list.
type HideMenuKeys struct {
	// Hide the hovered task or every chosen task, or unhide them when they are all hidden.
	Hide []string `mapstructure:"hide" default:"h"`
	// Show the hidden tasks alongside the rest, or put them away again.
	ShowHidden []string `mapstructure:"show_hidden" default:"H"`
}

// MergeKeys are the bindings for the merge menu, which opens from the git menu.
type MergeKeys struct {
	// Merge the pull request and leave the task where it is.
	Merge []string `mapstructure:"merge" default:"m"`
	// Merge the pull request, then archive the task once the forge has merged it.
	MergeArchive []string `mapstructure:"merge_archive" default:"a"`
	// Have the forge merge the pull request on its own once its checks and reviews pass, or stop it
	// when it is already waiting to.
	AutoMerge []string `mapstructure:"auto_merge" default:"u"`
	// Merge the pull request on the forge, then bring the task's worktree up to date on the base,
	// which now holds its work.
	MergeRebase []string `mapstructure:"merge_rebase" default:"r"`
}

// MenuKeys are the bindings for the menu, which lists the hovered row's entries over the
// screen's own. The rows with entries are a task, an agent, a workflow, a schedule, a worktree,
// and every row of the archive. On the agents list the work is acted on from its task, and an
// agent's own menu holds only what concerns its session.
type MenuKeys struct {
	// Open the menu: the hovered row's entries, or the chosen tasks', over the screen's own.
	// The key opens nothing on a screen with neither.
	Open []string `mapstructure:"open" default:"space"`
	// Open the review of the task's diff. The entry is gray on a task with no worktree, and in
	// the archive.
	Review []string `mapstructure:"review" default:"r"`
	// Open the agent's logs, the server log lines about it.
	Logs []string `mapstructure:"logs" default:"l"`
	// Open the status checks run on the task's pull request.
	StatusChecks []string `mapstructure:"status_checks" default:"x"`
	// Open a shell in the pane below the stage, rooted at the hovered task's or workflow's directory,
	// or hand the keyboard to the one already rooted there. The entry is gray in the archive and
	// where there is no directory.
	Terminal []string `mapstructure:"terminal" default:"t"`
	// Open the git menu to the right of this one, for rebasing the task's branch or opening or
	// merging its pull request.
	Git []string `mapstructure:"git" default:"g"`
	// Open the jump menu to the right of this one, for landing on the hovered row's agent or task,
	// its worktree or its documents.
	Jump []string `mapstructure:"jump" default:"J"`
	// Open the archive menu to the right of this one, for archiving or deleting the hovered task
	// or every chosen task, for archiving the hovered agent, workflow or schedule, for restoring
	// or deleting a row of the archive, or for deleting worktrees.
	Archive []string `mapstructure:"archive" default:"a"`
	// Open the hide menu to the right of this one, for hiding the hovered task or every chosen
	// task, or showing the hidden ones. The entry is only on the agents list.
	Hide []string `mapstructure:"hide" default:"h"`
	// Open your editor on the files of the hovered task or agent, in place of the pane beside
	// the list. The agent working there is paused while the editor is open, and quitting the
	// editor, picking the entry again or leaving the agents list closes it and unpauses the agent.
	// The entry is only on the agents list.
	Edit []string `mapstructure:"edit" default:"e"`
	// Retry the launch of an agent that failed to create, opening a form where the flags can be
	// corrected before confirming. The entry is only on such an agent.
	Retry []string `mapstructure:"retry" default:"R"`
	// Snooze the hovered task in triage, which takes its agents out of the zone until what they
	// wait on changes, or wake it when it is snoozed. The entry is only on a task or an
	// orchestrator in triage.
	Snooze []string `mapstructure:"snooze" default:"z"`
	// Edit the hovered workflow, or the hovered task's title, goal and notes.
	EditRow []string `mapstructure:"edit_row" default:"E"`
	// Archive the hovered task's writer and its children and start a new writer in the same
	// checkout, from a form asking for a note to it. The entry is only on a task, and does nothing
	// on one with no live writer.
	HandOff []string `mapstructure:"hand_off" default:"o"`
	// Put an agent on a branch that already exists, in the hovered workflow.
	Adopt []string `mapstructure:"adopt" default:"A"`
	// Start a new task on the hovered workflow. On the hovered task it starts a child of the live
	// writer, or else a writer in the task's checkout, and it is not offered once the worktree was
	// removed. The entry is only on the agents list.
	NewAgent []string `mapstructure:"new_agent" default:"c"`
	// Create a workflow. The entry is only on a workflow in the agents list.
	NewWorkflow []string `mapstructure:"new_workflow" default:"w"`
}

// WorkflowsKeys are the bindings that act on a workflow.
type WorkflowsKeys struct {
	// Rename the workflow under the cursor.
	Rename []string `mapstructure:"rename" default:"r"`
}

// DocumentTypesMenuKeys are the bindings for the menu on the document type under the cursor of
// the documents view, which the menu key opens over it.
type DocumentTypesMenuKeys struct {
	// Edit the type's name and context, from a form holding them as they stand.
	Edit []string `mapstructure:"edit" default:"e"`
	// Define a new document type, from a form asking its name and the context an agent reads
	// before writing one.
	New []string `mapstructure:"new" default:"c"`
	// Delete the type, once you confirm.
	Delete []string `mapstructure:"delete" default:"d"`
}

// DocumentMenuKeys are the bindings for the menu on the hovered document.
type DocumentMenuKeys struct {
	// Open the document in your editor, in place of the text drawn beside the list. Quitting the
	// editor, or picking the entry again to close it, saves what it has written.
	Edit []string `mapstructure:"edit" default:"e"`
	// Rename the document, typing over its name in the list.
	Rename []string `mapstructure:"rename" default:"r"`
	// Write a new document for the agent whose documents are listed, from a form. The entry is
	// only on the documents view opened from an agent.
	New []string `mapstructure:"new" default:"c"`
}

// DocumentsKeys are the bindings for the documents view, which lists every document agents
// have written, or the documents of one agent when opened from its menu, and draws the one
// under the cursor beside the list. Editing, renaming and writing a new one are in the menu on
// the hovered document.
type DocumentsKeys struct {
	// Step back to an earlier write of the document under the cursor, drawing what that write
	// changed.
	Older []string `mapstructure:"older" default:"<"`
	// Step forward to a later write, and past the newest back to the document as it stands.
	Newer []string `mapstructure:"newer" default:">"`
}

// MessagesKeys are the bindings for the messages komrad pushed.
type MessagesKeys struct {
	// Open every message in the main pane, and close it again.
	Open []string `mapstructure:"open" default:"ctrl+g"`
	// Dismiss the message under the cursor.
	Dismiss []string `mapstructure:"dismiss" default:"d"`
	// Dismiss every message.
	DismissAll []string `mapstructure:"dismiss_all" default:"D"`
}

// LogsKeys are the bindings for the logs screen. Movement is the shared navigation, and
// moving up stops following the newest line until the key that jumps to the last row.
type LogsKeys struct {
	// Copy the log line the cursor is on to the system clipboard.
	Copy []string `mapstructure:"copy" default:"y"`
}

// JumpMenuKeys are the bindings for the jump menu, nested under the menu on a task, a document
// or a worktree, which lands on the rows related to the hovered one.
type JumpMenuKeys struct {
	// Land on the agent, on the agents list or in the archive.
	Agent []string `mapstructure:"agent" default:"a"`
	// Land on the task, on the agents list or in the archive.
	Task []string `mapstructure:"task" default:"t"`
	// Land on the agent's worktree on the worktrees screen.
	Worktree []string `mapstructure:"worktree" default:"w"`
	// Open the documents view on the task's documents from a task's menu, and on the agent's elsewhere.
	Documents []string `mapstructure:"documents" default:"d"`
}

// ReviewKeys are the bindings for the review screen, where the diff is in the stage and the
// threads are in the sidebar. Paging and panning are the screen's own, since the shared
// navigation keys move the cursor a row at a time. While a message is being composed or a
// pattern typed, every other key is a character, so the keys that close those are fixed rather
// than configured.
type ReviewKeys struct {
	// From the menu, pass a held comment from the forge on to the writer, copied into your review as your own
	// words. A comment is held when its author cannot push to the repository, and the writer
	// reads nothing in its thread until the comment opening it is accepted and the review sent.
	Accept []string `mapstructure:"accept" default:"A"`
	// From the menu, show only the change that answered the comment under the cursor, from the work as
	// it stood just before the writer's addressed reply to the work that reply carried, or
	// go back to all changes from there.
	Answer []string `mapstructure:"answer" default:"a"`
	// Walk back a chapter through the review, from the whole change to the newest chapter and
	// on into the older ones, stopping on the oldest.
	Base []string `mapstructure:"base" default:"<"`
	// Walk the other way through the chapters, towards the newest work and on to the whole
	// change.
	BaseForward []string `mapstructure:"base_forward" default:">"`
	// From the menu, write a message on the line under the cursor, replying in the thread already there
	// or opening a new one.
	Compose []string `mapstructure:"compose" default:"c"`
	// From the menu, open the file under the cursor in your editor, beside the diff, on the line the
	// cursor is on. This is the task's own worktree rather than a copy of it, so what
	// you write is what the writer finds there, and the key is refused while the writer is
	// working rather than letting you both write at once. It is refused on a round the
	// writer has written past, since those lines have moved on.
	EditFile []string `mapstructure:"edit_file" default:"e"`
	// From the menu, throw away a reply of yours the forge refused on the thread under the cursor. Nothing is
	// posted.
	ForgeDiscard []string `mapstructure:"forge_discard" default:"x"`
	// From the menu, try again with a reply of yours the forge refused on the thread under the cursor.
	ForgePost []string `mapstructure:"forge_post" default:"p"`
	// From the menu, write a reply on the forge thread under the cursor, posted on the pull request where
	// anyone can read it.
	ForgeReply []string `mapstructure:"forge_reply" default:"C"`
	// From the menu, jump from the comment under the cursor to the diff it was written against, with the
	// cursor on it, or back to where you were from there. Anywhere else, move to the next
	// comment from the forge on the pull request.
	Jump []string `mapstructure:"jump" default:"J"`
	// Jump to the next line carrying a review thread.
	NextThread []string `mapstructure:"next_thread" default:"t"`
	// Move the cursor to the next comment nobody has settled. It stops on the last one.
	NextOpenThread []string `mapstructure:"next_open_thread" default:"u"`
	// Move the cursor to the previous comment nobody has settled. It stops on the first one.
	PrevOpenThread []string `mapstructure:"prev_open_thread" default:"U"`
	// Jump to the previous line carrying a review thread.
	PrevThread []string `mapstructure:"prev_thread" default:"T"`
	// From the menu, catch up with the writer, once the band says it has written since you opened the
	// diff. What you are reading never moves on its own, since that would take the code out
	// from under a comment being written, so catching up is always yours to ask for.
	Reload []string `mapstructure:"reload" default:"R"`
	// From the menu, show or hide the conversations agents opened, which are hidden whenever a
	// review opens.
	AgentThreads []string `mapstructure:"agent_threads" default:"b"`
	// From the menu, settle the thread under the cursor, which marks it resolved without deleting it.
	Resolve []string `mapstructure:"resolve" default:"r"`
	// From the menu, send the comments waiting in your review to the task's writer, after asking for a note on the work as a whole.
	Send []string `mapstructure:"send" default:"S"`
	// From the menu, fold away the file the cursor is in once you have read it, or open it again. A file
	// the writer writes to afterwards comes back on its own.
	ToggleViewed []string `mapstructure:"toggle_viewed" default:"v"`
}

// DiffKeys walk a diff once the keys are on it, on the review and on a document's history alike.
type DiffKeys struct {
	// Draw the whole of the file the cursor is in rather than the changed lines and the
	// three around them, or go back to those. The lines this adds behave like any other:
	// the cursor rests on them and, on the review, a comment can be written on one. A file
	// stays open while you are reading this change and is drawn tight again when you leave.
	Expand []string `mapstructure:"expand" default:"E"`
	// Jump to the next file in the diff.
	NextFile []string `mapstructure:"next_file" default:"]"`
	// Jump to the next line matching the pattern the filter key searched for.
	NextMatch []string `mapstructure:"next_match" default:"n"`
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
	// Wrap lines wider than the pane, or go back to cutting them off and panning.
	Wrap []string `mapstructure:"wrap" default:"w"`
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
// rather than part of the agents tree, and act on the hovered schedule.
type SchedulesKeys struct {
	// Rename the schedule under the cursor, typed over its row.
	Rename []string `mapstructure:"rename" default:"r"`
}

// ScheduleMenuKeys are the bindings for the menu on the hovered schedule.
type ScheduleMenuKeys struct {
	// Pause the schedule, or resume it when it is paused.
	Toggle []string `mapstructure:"toggle" default:"p"`
	// Edit the hovered schedule.
	Edit []string `mapstructure:"edit" default:"e"`
	// Write a new schedule, offered on the schedules screen whatever is hovered.
	New []string `mapstructure:"new" default:"c"`
}

// ArchiveKeys are the bindings for the archive screen, which lists the agents, workflows and
// schedules archived from the panel. Restoring and deleting are in the menu on the hovered row.
type ArchiveKeys struct {
	// Rename the agent or live workflow under the cursor, typed over its row.
	Rename []string `mapstructure:"rename" default:"r"`
}

// ArchiveMenuKeys are the bindings for the archive menu, which opens from the menu on the hovered
// row and acts on every chosen agent while some are.
type ArchiveMenuKeys struct {
	// Archive the agent, task, workflow or schedule, or every chosen task.
	Archive []string `mapstructure:"archive" default:"a"`
	// Destroy the row for good, with its transcript or its run history, archiving a live task
	// first, which takes its agents, documents, reviews and worktree with it. On the worktrees it
	// deletes the directory, through the task delete when an archived task answers for it.
	// Nothing undoes it, so it asks first.
	Delete []string `mapstructure:"delete" default:"d"`
	// Put the row back in the panel.
	Restore []string `mapstructure:"restore" default:"r"`
	// Put the agent back in the panel and open it, which relaunches its conversation in a pane
	// and takes you to it. The entry is gray on an agent that never got as far as starting.
	RestoreOpen []string `mapstructure:"restore_open" default:"o"`
	// Delete every worktree that can go: every directory whose agent is archived, through the
	// agent delete and its hooks, and every directory no agent answers for. A directory with a
	// live agent in it is never deleted. The entry is only on the worktrees screen, and is gray
	// when nothing there can go.
	Clear []string `mapstructure:"clear" default:"c"`
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
