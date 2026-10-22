package ui

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

const reviewTimeout = 30 * time.Second

// reviewDeps is what the review screen is given at launch.
type reviewDeps struct {
	keys           config.Keybindings
	glyphs         glyph.Set
	register       *vim.Register
	clients        api.Clients
	patches        diff.Reader
	panes          screen.Panes
	top            *keys.Top
	editor         editor.Editor
	editorSettings editor.Settings
}

// reviewScreen is the review of one agent's diff, while that screen is up.
type reviewScreen struct {
	deps      reviewDeps
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
	diff reviewDiff

	// threads is the conversation in the sidebar and what the reader is writing into it.
	threads reviewThreads

	// edit is the reader's own editor over the agent's worktree.
	edit  reviewEdit
	focus reviewFocus

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
	notices reviewNotices
}

// reviewFocus is which of the diff and the sidebar holds the review's keys.
type reviewFocus int

const (
	reviewFocusDiff reviewFocus = iota
	reviewFocusFiles
)

func (r reviewScreen) Parent() screen.ID {
	return screen.Agents
}

// Crumbs runs through the agent whose work is under review.
func (r reviewScreen) Crumbs(screen.Inputs) screen.Crumbs {
	return screen.Crumbs{Trail: []string{r.agentName, "review"}}
}

// TakesEveryKey reports whether the review is mid-input, in which case a key bound elsewhere
// in the panel belongs here instead.
func (r reviewScreen) TakesEveryKey() bool {
	return r.isWriting() || r.IsSearchTyping()
}

func (r reviewScreen) IsSearchTyping() bool {
	return r.diff.filter.Typing || r.filesFilter.Typing
}

// isWriting reports whether the reader is composing a message.
func (r reviewScreen) isWriting() bool {
	return r.threads.composing
}

// isBusy reports whether the reader is part way through something a restart would lose.
func (r reviewScreen) isBusy() bool {
	return r.isWriting() || r.edit.atEditor
}

// isDiffFocused is whether the patch holds the keys.
func (r reviewScreen) isDiffFocused(in screen.Inputs) bool {
	return in.Room.ListFocused && !r.isWriting() && !r.IsBlockFocused(in)
}

func (r *reviewScreen) Update(in screen.Inputs, msg tea.Msg) screen.Request {
	var keyed screen.Request
	if key, ok := msg.(tea.KeyPressMsg); ok {
		keyed = r.updateKey(in, key)
	}

	return screen.Batch{keyed, r.followWorld(in)}
}

func (r *reviewScreen) Receive(in screen.Inputs, msg tea.Msg) (screen.Request, bool) {
	switch msg := msg.(type) {
	case reviewEditorStaged:
		return r.applyEditorStaged(msg), true
	case reviewLoadedMsg, reviewWroteMsg, messageEditedMsg:
		if !in.Up {
			return nil, true
		}

		routed := r.route(in, msg)

		return screen.Batch{routed, r.followWorld(in)}, true
	}

	return nil, false
}

func (r *reviewScreen) route(in screen.Inputs, msg tea.Msg) screen.Request {
	switch msg := msg.(type) {
	case reviewLoadedMsg:
		return r.applyLoaded(in, msg)
	case reviewWroteMsg:
		return r.applyWrote(in, msg)
	case messageEditedMsg:
		return r.applyMessageEdited(in, msg)
	}

	return nil
}

// Enter leaves the review as it stands.
func (r *reviewScreen) Enter(screen.Inputs, screen.Target) screen.Request {
	return nil
}

var (
	_ columnFocuser    = (*reviewScreen)(nil)
	_ crumbRenderer    = (*reviewScreen)(nil)
	_ loadingReporter  = (*reviewScreen)(nil)
	_ lineTyper        = (*reviewScreen)(nil)
	_ listScroller     = (*reviewScreen)(nil)
	_ editorReconciler = (*reviewScreen)(nil)
	_ popupHost        = (*reviewScreen)(nil)
)

func (r *reviewScreen) DoesRenderFromListing() bool {
	return false
}

func (r reviewScreen) TypedLine() (field.Line, bool) {
	if r.filesFilter.Typing {
		return r.filesFilter.Input, true
	}

	return r.diff.filter.Input, r.diff.filter.Typing
}

func (r *reviewScreen) Dispatches() []keys.Table {
	return []keys.Table{r.diffActions(screen.Inputs{}), r.filesActions(screen.Inputs{}), r.sideActions(screen.Inputs{})}
}

// writerPane is the agent whose work is under review, which the stage keeps beside the diff while
// its pane is running, held for your editor or not.
func (r reviewScreen) writerPane(in screen.Inputs) (panel.AgentRow, bool) {
	item, ok := r.agentRow(in)
	if !ok || !item.Alive || item.Agent.Pane == "" || item.Agent.State == panel.StateSettingUp || item.Agent.State.IsFailed() {
		return panel.AgentRow{}, false
	}

	return item, true
}
