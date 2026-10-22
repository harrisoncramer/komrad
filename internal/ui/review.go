package ui

import (
	"time"

	"komrad/config"
	"komrad/internal/diff"
	"komrad/internal/glyph"
	"komrad/internal/ui/api"
	"komrad/internal/ui/editor"
	"komrad/internal/ui/field"
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

	// hold is the reader's own hold over the work, and the editor it may hold open.
	hold  reviewHold
	focus reviewFocus

	// side is the row over the file tree the sidebar's cursor rests on, nil while it is on the tree.
	side *sideCursor
	// openDescription says the pull request's description is open.
	openDescription bool

	// filesFilter is the pattern the file tree is searched or narrowed by.
	filesFilter filterState

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

func (r reviewScreen) parent() screen.ID {
	return screen.Agents
}

// crumbs runs through the agent whose work is under review.
func (r reviewScreen) crumbs(*model) crumbs {
	return crumbs{trail: []string{r.agentName, "review"}}
}

// takesEveryKey reports whether the review is mid-input, in which case a key bound elsewhere
// in the panel belongs here instead.
func (r reviewScreen) takesEveryKey() bool {
	return r.isWriting() || r.isSearchTyping()
}

func (r reviewScreen) isSearchTyping() bool {
	return r.diff.filter.typing || r.filesFilter.typing
}

// isWriting reports whether the reader is composing a message.
func (r reviewScreen) isWriting() bool {
	return r.threads.composing
}

// isBusy reports whether the reader is part way through something a restart would lose.
func (r reviewScreen) isBusy() bool {
	return r.isWriting() || r.hold.isHeld()
}

// isDiffFocused is whether the patch holds the keys.
func (r reviewScreen) isDiffFocused(m *model) bool {
	return m.isListFocused() && !r.isWriting() && !r.isBlockFocused(m)
}

// update takes the review's own answers and, once nothing in the sidebar has them, the keys.
func (r *reviewScreen) update(m *model, msg tea.Msg) screen.Request {
	routed := r.route(m, msg)

	return screen.Batch{routed, r.followWorld(m)}
}

func (r *reviewScreen) receive(m *model, msg tea.Msg) (screen.Request, bool) {
	staged, ok := msg.(reviewEditorStaged)
	if !ok {
		return nil, false
	}

	return r.applyEditorStaged(m, staged), true
}

func (r *reviewScreen) route(m *model, msg tea.Msg) screen.Request {
	switch msg := msg.(type) {
	case reviewLoadedMsg:
		return r.applyLoaded(m, msg)
	case reviewPausedMsg:
		return r.applyPaused(m, msg)
	case reviewWroteMsg:
		return r.applyWrote(m, msg)
	case messageEditedMsg:
		return r.applyMessageEdited(m, msg)
	case tea.KeyPressMsg:
		return r.updateKey(m, msg)
	}

	return nil
}

// enter leaves the review as it stands.
func (r *reviewScreen) enter(*model, screen.Target) screen.Request {
	return nil
}

var (
	_ columnFocuser    = (*reviewScreen)(nil)
	_ crumbRenderer    = (*reviewScreen)(nil)
	_ loadingReporter  = (*reviewScreen)(nil)
	_ lineTyper        = (*reviewScreen)(nil)
	_ listScroller     = (*reviewScreen)(nil)
	_ editorReconciler = (*reviewScreen)(nil)
	_ agentPauser      = (*reviewScreen)(nil)
)

func (r *reviewScreen) doesRenderFromListing() bool {
	return false
}

func (r *reviewScreen) column() columnSpec {
	return reviewColumn()
}

func (r reviewScreen) typedLine() (field.Line, bool) {
	if r.filesFilter.typing {
		return r.filesFilter.input, true
	}

	return r.diff.filter.input, r.diff.filter.typing
}

func (r *reviewScreen) dispatches(*model) []keys.Table {
	return []keys.Table{r.diffActions(), r.filesActions()}
}

// writerPane is the agent whose work is under review, which the stage keeps beside the diff while
// its pane is running, held for your editor or not.
func (r reviewScreen) writerPane(m *model) (panel.AgentRow, bool) {
	item, ok := r.agentRow(m)
	if !ok || !item.Alive || item.Agent.Pane == "" || item.Agent.State == panel.StateSettingUp || item.Agent.State.IsFailed() {
		return panel.AgentRow{}, false
	}

	return item, true
}
