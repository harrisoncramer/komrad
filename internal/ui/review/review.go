package review

import (
	"time"

	"komrad/config"
	"komrad/internal/diff"
	"komrad/internal/glyph"
	"komrad/internal/ui/api"
	"komrad/internal/ui/editor"
	"komrad/internal/ui/field"
	"komrad/internal/ui/filter"
	"komrad/internal/ui/keys"
	"komrad/internal/ui/panel"
	"komrad/internal/ui/screen"
	"komrad/internal/vim"
	agentv1 "komrad/proto/gen/agent/v1"

	tea "charm.land/bubbletea/v2"
)

const timeout = 30 * time.Second

const separated = " " + glyph.Separator + " "

// Deps is what the review screen is given at launch.
type Deps struct {
	Keys           config.Keybindings
	Glyphs         glyph.Set
	Register       *vim.Register
	Clients        api.Clients
	Patches        diff.Reader
	Panes          screen.Panes
	Top            *keys.Top
	Editor         editor.Editor
	EditorSettings editor.Settings
}

// New is the review screen built from what it is given at launch.
func New(deps Deps) Screen {
	return Screen{deps: deps}
}

// Screen is the review of one agent's diff, while that screen is up.
type Screen struct {
	deps      Deps
	agentID   string
	taskID    string
	agentName string
	dir       string
	root      string
	exclude   []string
	snapshot  *agentv1.ReviewSnapshot
	loading   bool
	// loads counts the reads of the diff started, and only the newest one lands.
	loads int
	// notice is what the last load had to say about the snapshot it settled on.
	notice string

	// diff is the patch on screen and where the reader is standing in it.
	diff diffPane

	// threads is the conversation in the sidebar and what the reader is writing into it.
	threads conversation

	// edit is the reader's own editor over the agent's worktree.
	edit  worktreeEdit
	focus focus

	// side is the row over the file tree the sidebar's cursor rests on, nil while it is on the tree.
	side *sideCursor
	// openDescription says the pull request's description is open.
	openDescription bool

	// filesFilter is the pattern the file tree is searched or narrowed by.
	filesFilter filter.State

	// closedFolders is the folders of the file tree whose files are hidden, by path.
	closedFolders map[string]bool

	// folderCursor is the folder of the file tree its cursor rests on.
	folderCursor folderCursor

	// notices is what the review has raised on the messages stack about the agent.
	notices notices
}

// focus is which of the diff and the sidebar holds the review's keys.
type focus int

const (
	focusDiff focus = iota
	focusFiles
)

// Parent is the agents view the review sits under.
func (r Screen) Parent() screen.ID {
	return screen.Agents
}

// Crumbs runs through the agent whose work is under review.
func (r Screen) Crumbs(screen.Inputs) screen.Crumbs {
	return screen.Crumbs{Trail: []string{r.agentName, "review"}}
}

// TakesEveryKey reports whether the review is mid-input, in which case a key bound elsewhere
// in the panel belongs here instead.
func (r Screen) TakesEveryKey() bool {
	return r.IsWriting() || r.IsSearchTyping()
}

// IsSearchTyping reports whether the diff's or the file tree's search is typed into.
func (r Screen) IsSearchTyping() bool {
	return r.diff.filter.IsTyping || r.filesFilter.IsTyping
}

// IsWriting reports whether the reader is composing a message.
func (r Screen) IsWriting() bool {
	return r.threads.composing
}

// IsBusy reports whether the reader is part way through something a restart would lose.
func (r Screen) IsBusy() bool {
	return r.IsWriting() || r.edit.atEditor
}

// IsInserting reports whether the message being written takes typed text rather than vim commands.
func (r Screen) IsInserting() bool {
	return r.threads.editor.isInserting()
}

// IsAtEditor reports whether the reader's own editor holds the stage.
func (r Screen) IsAtEditor() bool {
	return r.edit.atEditor
}

// IsDiffFocused is whether the patch holds the keys.
func (r Screen) IsDiffFocused(in screen.Inputs) bool {
	return in.Room.ListFocused && !r.IsWriting() && !r.IsBlockFocused(in)
}

// Update sends a key to the review's keys and follows the world the key left behind.
func (r *Screen) Update(in screen.Inputs, msg tea.Msg) screen.Request {
	var keyed screen.Request
	if key, ok := msg.(tea.KeyPressMsg); ok {
		keyed = r.updateKey(in, key)
	}

	return screen.Batch{keyed, r.followWorld(in)}
}

// Receive takes the editor's pane, the diff and writes as they land, and the listing moving under the agent.
func (r *Screen) Receive(in screen.Inputs, msg tea.Msg) (screen.Request, bool) {
	switch msg := msg.(type) {
	case editorStaged:
		return r.applyEditorStaged(msg), true
	case loadedMsg, wroteMsg, messageEditedMsg:
		if !in.Up {
			return nil, true
		}

		routed := r.route(in, msg)

		return screen.Batch{routed, r.followWorld(in)}, true
	case screen.Relisted:
		if !in.Up || !doesMoveReview(msg.Changed, r.agentID) {
			return nil, true
		}

		return r.followWorld(in), true
	}

	return nil, false
}

func (r *Screen) route(in screen.Inputs, msg tea.Msg) screen.Request {
	switch msg := msg.(type) {
	case loadedMsg:
		return r.applyLoaded(in, msg)
	case wroteMsg:
		return r.applyWrote(in, msg)
	case messageEditedMsg:
		return r.applyMessageEdited(in, msg)
	}

	return nil
}

// Enter leaves the review as it stands.
func (r *Screen) Enter(screen.Inputs, screen.Target) screen.Request {
	return nil
}

// IsLoading reports whether the diff is loading or the agent's pull request is running its checks.
func (r Screen) IsLoading(in screen.Inputs) bool {
	if r.loading {
		return true
	}

	item, ok := in.Listing.LiveAgent(r.agentID)

	return ok && in.Listing.Tasks.FindForAgent(item.Agent).PullRequest.IsChecksRunning()
}

// StageResting stages the terminal and marks the thread the cursor came to rest on read.
func (r *Screen) StageResting(in screen.Inputs) screen.Request {
	if r.edit.atEditor {
		return r.markThreadRead(in)
	}

	return screen.Batch{screen.Restage{}, r.markThreadRead(in)}
}

// DoesRenderFromListing reports that the review renders from its own read of the diff.
func (r *Screen) DoesRenderFromListing() bool {
	return false
}

// TypedLine is the file tree's search or the diff's, whichever is typed into.
func (r Screen) TypedLine() (field.Line, bool) {
	if r.filesFilter.IsTyping {
		return r.filesFilter.Input, true
	}

	return r.diff.filter.Input, r.diff.filter.IsTyping
}

// Dispatches is the diff's table, the file tree's and the table of the rows over it.
func (r *Screen) Dispatches() []keys.Table {
	return []keys.Table{r.diffActions(screen.Inputs{}), r.filesActions(screen.Inputs{}), r.sideActions(screen.Inputs{})}
}

// writerPane is the agent whose work is under review, which the stage keeps beside the diff while
// its pane is running, held for your editor or not.
func (r Screen) writerPane(in screen.Inputs) (panel.AgentRow, bool) {
	item, ok := r.agentRow(in)
	if !ok || !item.Alive || item.Agent.Pane == "" || item.Agent.State == agentv1.AgentState_AGENT_STATE_SETTING_UP || panel.IsFailed(item.Agent.State) {
		return panel.AgentRow{}, false
	}

	return item, true
}
