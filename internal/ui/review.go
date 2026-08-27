package ui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"weave/internal/diff"
	"weave/internal/review"
	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"
	"weave/internal/ui/layout"
	agentv1 "weave/proto/gen/agent/v1"
	"weave/proto/gen/agent/v1/agentv1connect"

	"connectrpc.com/connect"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
)

var reviewStagePath = filepath.Join(os.TempDir(), "weave-review.ansi")

const reviewTimeout = 30 * time.Second

// reviewPanStep is the smallest sideways jump, used until the pane has been measured.
const reviewPanStep = 8

const reviewEditorHeight = 4

// reviewState is the review of one agent's diff, while that screen is up. The cursor lives
// here rather than in the stage, because the sidebar draws the thread the cursor is standing
// on and only the panel knows where that is.
type reviewState struct {
	agentID   string
	agentName string

	dir     string
	root    string
	base    string
	exclude []string

	view    *review.View
	threads []*agentv1.ReviewThread

	cursor int
	pan    int
	// top is the first row of the diff the pane is showing. It is held rather than derived from
	// the cursor because deriving it centres the cursor, which scrolls the whole pane on every
	// move and repaints every row of it. Holding it means an ordinary move changes two rows.
	top int

	frames *reviewFrames

	composing bool
	editor    textarea.Model

	// filter is the pattern being searched for in the diff. It is the panel's filter input, kept
	// here rather than shared with the screen behind it: the agents list filters its own rows by
	// the model's filter, and a pattern meant for code would empty it.
	filter filterState

	// pending is a key waiting on the one after it, which is only ever z today.
	pending string

	// pinTop holds the window where a recentring key put it, until the cursor moves. Without it
	// the next repaint drags the window back to keep the cursor a few rows clear of the edge,
	// which is exactly what zt and zb are asking it not to do.
	pinTop bool
	// editing is the thread a composed message is going into, empty when it will open a new
	// thread on the line under the cursor.
	editing string

	loading bool
	notice  string
	err     error

	// restore is where the cursor was standing when the patch was read again, so a reload lands
	// the reader back where they left rather than at the top of the diff.
	restore *review.Place
}

// reviewFrames is the size of the pane the diff is being drawn into. Only the frame knows it,
// and it says so when it starts the drawer rather than on every repaint, so it is kept here.
type reviewFrames struct {
	width  int
	height int
}

// window is the screenful starting at top, with the cursor's own row drawn as the cursor.
//
// Only the rows the pane can show are drawn, and they are drawn afresh every time. Holding the
// whole patch drawn instead buys nothing and costs a great deal: a screen is under a millisecond
// whatever the patch's size, where a nine thousand row diff takes a tenth of a second to draw
// whole, and the horizontal offset changes every row, so panning paid that on every keypress.
func (f *reviewFrames) window(view *review.View, top, cursor, pan int) string {
	height := max(f.height, 1)

	start := max(min(top, view.Len()-height), 0)

	rows := make([]string, 0, height)
	for y := start; y < min(start+height, view.Len()); y++ {
		rows = append(rows, view.RenderRow(y, y == cursor, f.width, pan))
	}

	// Padded out rather than left short, so a diff smaller than the pane is followed by blank
	// lines instead of the drawer being handed fewer rows than it has.
	for len(rows) < height {
		rows = append(rows, "")
	}

	return strings.Join(rows, "\n") + "\n"
}

type reviewLoadedMsg struct {
	agentID string

	patch   *diff.Patch
	threads []*agentv1.ReviewThread

	err error
}

// reviewWroteMsg is one write and the review as it stands after it. The two arrive together
// because the panel keeps no copy of a thread it has just changed: every write is followed by
// reading the review back, and there is nothing useful to draw in between.
type reviewWroteMsg struct {
	agentID string

	notice string

	threads []*agentv1.ReviewThread

	err error
}

// enterReview opens the review of an agent's diff: the threads in the sidebar, the diff in
// the stage. The patch is loaded off the UI thread, so the screen comes up saying so.
func (m model) enterReview(agent agentstore.Agent) (tea.Model, tea.Cmd) {
	if !m.isReviewable(agent) {
		return m, nil
	}

	worktree := m.worktreeFor(agent)

	editor := textarea.New()
	editor.Placeholder = "what should the agent change here?"
	editor.ShowLineNumbers = false
	editor.SetHeight(reviewEditorHeight)

	m.enterScreen(screenReview)
	m.review = reviewState{
		agentID:   agent.ID,
		agentName: agent.Name,
		dir:       agent.Cwd,
		base:      worktree.BaseBranch,
		exclude:   worktree.DiffExclude,
		frames:    &reviewFrames{},
		editor:    editor,
		loading:   true,
	}
	m.frame.Show(layout.RegionStage, newTextContent("_loading the diff…_"))

	return m, m.loadReviewCmd(m.review.agentID, m.review.dir, m.review.base, m.review.exclude)
}

func (m model) loadReviewCmd(agentID, dir, base string, exclude []string) tea.Cmd {
	agents := m.api.agents

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), reviewTimeout)
		defer cancel()

		patch, err := diff.Load(ctx, diff.LoadParams{Dir: dir, Base: base, Exclude: exclude})
		if err != nil {
			return reviewLoadedMsg{agentID: agentID, err: err}
		}

		threads, err := listReviewThreads(ctx, agents, agentID)

		return reviewLoadedMsg{agentID: agentID, patch: patch, threads: threads, err: err}
	}
}

func listReviewThreads(
	ctx context.Context,
	agents agentv1connect.AgentServiceClient,
	agentID string,
) ([]*agentv1.ReviewThread, error) {
	resp, err := agents.ListReviewThreads(ctx, connect.NewRequest(&agentv1.ListReviewThreadsRequest{
		AgentId: &agentID,
	}))
	if err != nil {
		return nil, fmt.Errorf("failed to read the review of %q: %w", agentID, err)
	}

	return resp.Msg.GetThreads(), nil
}

func (m *model) applyReviewLoaded(msg reviewLoadedMsg) {
	if msg.agentID != m.review.agentID {
		return
	}

	m.review.loading = false
	m.review.err = msg.err
	if msg.patch == nil {
		return
	}

	m.review.root = msg.patch.Root
	m.review.view = review.NewView(msg.patch, m.isDark)
	m.review.view.PinThreads(msg.threads)
	m.review.threads = msg.threads
	m.review.cursor = m.review.view.First()
	if place := m.review.restore; place != nil {
		if found, ok := m.review.view.Locate(*place); ok {
			m.review.cursor = found
		}

		m.review.restore = nil
	}

	m.stageReview()
}

func (m *model) applyReviewWrote(msg reviewWroteMsg) {
	if msg.agentID != m.review.agentID || m.review.view == nil {
		return
	}

	m.review.err = msg.err
	if msg.err != nil {
		return
	}

	m.review.notice = msg.notice
	m.review.threads = msg.threads

	// Where the cursor is standing is taken before the threads are pinned again, since pinning
	// rebuilds the rows the threads with nowhere to stand are drawn on, and a thread settled or
	// deleted takes one of those rows away from under the cursor.
	place, ok := m.review.view.GetPlace(m.review.cursor)
	m.review.view.PinThreads(msg.threads)
	m.keepReviewCursor(place, ok)

	m.stageReview()
}

// keepReviewCursor puts the cursor back on what it was standing on, and failing that, back
// inside the diff. Only the rows past the end of the patch move, so this is the one write that
// can leave the cursor pointing at a row that is no longer there.
func (m *model) keepReviewCursor(place review.Place, ok bool) {
	was := m.review.cursor

	m.review.cursor = min(m.review.cursor, m.review.view.Last())
	if ok {
		if found, located := m.review.view.Locate(place); located {
			m.review.cursor = found
		}
	}

	if m.review.cursor != was {
		m.review.pinTop = false
	}
}

func (m *model) stageReview() {
	if m.review.view == nil {
		return
	}

	// Showing is what measures the pane, so the window is only settled afterwards.
	m.frame.Show(layout.RegionStage, m.reviewContent())
	m.scrollReviewToCursor()

	if err := m.repaintReview(); err != nil {
		m.report("Could not draw the diff", err)
	}
}

// repaintReview rewrites the screenful the stage is showing and tells the pager to re-read it.
//
// The pager is deliberately left running. Restarting it for every cursor move made it paint the
// whole stage from scratch, and tmux forwarded that to the client as a dozen partial writes
// spread over twenty milliseconds, which is what read as a flicker while scrolling. Re-reading
// one already-positioned screen changes two rows, so that is all that reaches the client.
func (m model) repaintReview() error {
	pane := m.frame.Pane(layout.RegionStage)
	frames := m.review.frames

	// Nothing to draw against until the frame has started the drawer and told us how big its pane
	// is. Everything after that is drawn here, since the drawer is started once and nothing else
	// runs again.
	if pane == "" || m.review.view == nil || frames.width == 0 {
		return nil
	}

	if !writeStageFile(reviewStagePath, frames.window(m.review.view, m.review.top, m.review.cursor, m.review.pan)) {
		return fmt.Errorf("failed to write the diff to %s", reviewStagePath)
	}

	// Any key tells the drawer the file has changed; it holds the pane and re-reads on one.
	return m.tmux.SendKeys(pane, "R")
}

// reviewContent starts the pager on the diff. The key names only the agent, so moving the
// cursor does not restart it: the pane is respawned when the review opens or the pane changes
// size, and every move after that is a repaint. The pane's size is measured here, since this is
// where the frame reports it, and held for the repaints to draw against.
func (m model) reviewContent() layout.Content {
	state := m.review

	return layout.Content{
		Key:   "review:" + state.agentID,
		Wraps: true,
		Render: func(target layout.Target) ([]string, bool) {
			state.frames.width = max(target.Width, review.GutterWidth+20)
			state.frames.height = max(target.Height, 1)

			if !writeStageFile(reviewStagePath, state.frames.window(state.view, state.top, state.cursor, state.pan)) {
				return nil, false
			}

			return m.reviewStageArgv(reviewStagePath), true
		},
	}
}

// reviewStageArgv runs weave's own drawer in the stage rather than a pager. A pager repaints its
// whole screen for any change, which reaches the client in installments and reads as a flicker on
// every cursor move; the drawer holds the frame in a bubbletea model, whose renderer writes only
// the rows that differ. There is nothing to position either: the file is exactly one screen with
// the cursor already on the right row of it.
func (m model) reviewStageArgv(path string) []string {
	self := m.self
	if self == "" {
		self = "weave"
	}

	return []string{self, "stage", path}
}

// takesEveryKey reports whether the review is mid-input: a message being composed, a pattern
// being typed, or a prefix waiting on the key after it. All three mean a key bound elsewhere in
// the panel belongs here instead.
func (r reviewState) takesEveryKey() bool {
	return r.composing || r.filter.typing || r.pending != ""
}

func (m model) updateReview(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.review.composing {
		return m.updateReviewComposer(key)
	}

	if m.review.filter.typing {
		return m.updateReviewFilter(key)
	}

	k := key.String()

	if m.review.pending != "" {
		return m.updateReviewPending(k)
	}
	if m.review.view == nil {
		if k == "esc" || isBound(k, m.keys.Agents.Back) {
			return m.leaveReview()
		}

		return m, nil
	}

	m.review.notice = ""

	switch {
	case k == "esc", isBound(k, m.keys.Agents.Back):
		return m.leaveReview()
	case k == "ctrl+c":
		return m, m.hide()
	case isBound(k, m.keys.Nav.Down):
		return m.moveReviewCursor(m.review.view.Move(m.review.cursor, 1))
	case isBound(k, m.keys.Nav.Up):
		return m.moveReviewCursor(m.review.view.Move(m.review.cursor, -1))
	case isBound(k, m.keys.Nav.Top):
		return m.moveReviewCursor(m.review.view.First())
	case isBound(k, m.keys.Nav.Bottom):
		return m.moveReviewCursor(m.review.view.Last())
	case k == "pgdown", k == "ctrl+n", k == "ctrl+f", k == " ":
		return m.pageReview(m.reviewPage())
	case k == "pgup", k == "ctrl+p", k == "ctrl+b":
		return m.pageReview(-m.reviewPage())
	case k == "ctrl+d":
		return m.pageReview(max(1, m.reviewPage()/2))
	case k == "ctrl+u":
		return m.pageReview(-max(1, m.reviewPage()/2))
	case k == "]":
		return m.moveReviewCursor(m.review.view.JumpFile(m.review.cursor, 1))
	case k == "[":
		return m.moveReviewCursor(m.review.view.JumpFile(m.review.cursor, -1))
	case k == "}":
		return m.moveReviewCursor(m.review.view.JumpHunk(m.review.cursor, 1))
	case k == "{":
		return m.moveReviewCursor(m.review.view.JumpHunk(m.review.cursor, -1))
	case k == "t":
		return m.moveReviewCursor(m.review.view.JumpThread(m.review.cursor, 1))
	case k == "T":
		return m.moveReviewCursor(m.review.view.JumpThread(m.review.cursor, -1))
	case isBound(k, m.keys.Filter.Open):
		m.review.filter.typing = true

		return m, nil
	case k == "n":
		return m.jumpReviewMatch(1)
	case k == "N":
		return m.jumpReviewMatch(-1)
	case k == "z":
		m.review.pending = "z"

		return m, nil
	case k == "l", k == "right":
		return m.panReview(m.reviewPanStep())
	case k == "h", k == "left":
		return m.panReview(-m.reviewPanStep())
	case k == "0":
		return m.panReview(-m.review.pan)
	case k == "e":
		return m.editReviewFile()
	case k == "c":
		return m.composeReview()
	case k == "r":
		return m.resolveReviewThread()
	case k == "D":
		return m.deleteReviewThread()
	case isBound(k, m.keys.Form.Submit):
		return m.notifyReview()
	}

	return m, nil
}

// updateReviewPending answers the key after a prefix. Only z has one, and it decides where in
// the pane the cursor's own row is put.
func (m model) updateReviewPending(k string) (tea.Model, tea.Cmd) {
	m.review.pending = ""

	height := m.review.frames.height
	if height <= 0 {
		return m, nil
	}

	switch k {
	case "z":
		return m.putReviewWindow(m.review.cursor - height/2)
	case "t":
		return m.putReviewWindow(m.review.cursor)
	case "b":
		return m.putReviewWindow(m.review.cursor - height + 1)
	}

	return m, nil
}

// putReviewWindow scrolls the diff so the cursor's row lands where a recentring key asked for,
// without moving the cursor itself.
func (m model) putReviewWindow(top int) (tea.Model, tea.Cmd) {
	m.review.top = max(min(top, m.review.view.Len()-1), 0)
	m.review.pinTop = true
	m.stageReview()

	return m, nil
}

// updateReviewFilter is the search prompt taking keys, which is the panel's filter input drawn
// in the review's own sidebar.
func (m model) updateReviewFilter(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if key.String() == "ctrl+c" {
		return m, m.hide()
	}

	if !m.review.filter.takeKey(key) {
		return m, nil
	}

	m.searchReview()

	return m, nil
}

// searchReview runs the pattern over the diff and moves to the first thing it finds, which is
// what makes typing one worth watching.
func (m *model) searchReview() {
	if m.review.view == nil {
		return
	}

	found := m.review.view.SetPattern(m.review.filter.pattern)
	if found > 0 {
		if next, ok := m.review.view.JumpMatch(m.review.cursor-1, 1); ok {
			m.review.cursor = next
			m.review.pinTop = false
		}
	}

	m.stageReview()
}

// jumpReviewMatch steps to the next or previous line the search found, wrapping at the ends.
func (m model) jumpReviewMatch(direction int) (tea.Model, tea.Cmd) {
	next, ok := m.review.view.JumpMatch(m.review.cursor, direction)
	if !ok {
		m.review.notice = "nothing to search for yet"
		if m.review.filter.standing() {
			m.review.notice = "no matches"
		}

		return m, nil
	}

	return m.moveReviewCursor(next)
}

// reviewThreadAtCursor is the thread the cursor is standing on. It answers false while the
// diff is still loading, since the sidebar and its legend are drawn on frames that arrive
// before the patch does.
func (m model) reviewThreadAtCursor() (*agentv1.ReviewThread, bool) {
	if m.review.view == nil {
		return nil, false
	}

	return m.review.view.ThreadAt(m.review.cursor)
}

// reloadReview reads the worktree again, holding the place the cursor is standing so the diff
// comes back under it. The editor is where files get changed and commits get made, so what the
// panel is drawing is stale the moment it closes.
//
// The old diff stays on screen until the new one arrives rather than blanking to a loading
// line: the read is quick and the reader is mid-thought.
func (m model) reloadReview() (model, tea.Cmd) {
	if m.review.view == nil {
		return m, nil
	}

	if place, ok := m.review.view.GetPlace(m.review.cursor); ok {
		m.review.restore = &place
	}

	return m, m.loadReviewCmd(m.review.agentID, m.review.dir, m.review.base, m.review.exclude)
}

func (m model) leaveReview() (tea.Model, tea.Cmd) {
	m.review = reviewState{}

	return m.returnToAgents()
}

func (m model) moveReviewCursor(next int) (tea.Model, tea.Cmd) {
	if next == m.review.cursor {
		return m, nil
	}

	m.review.cursor = next
	m.review.pinTop = false
	m.stageReview()

	return m, m.markReviewThreadReadCmd()
}

// reviewScrollMargin is how close to the edge of the pane the cursor gets before the diff
// scrolls under it, so there is always some of what is coming into view.
const reviewScrollMargin = 4

// scrollReviewToCursor moves the window only when the cursor is about to leave it. Scrolling on
// every move would be simpler, and is what centring the cursor amounts to, but it repaints every
// row of the pane for a one-line move and that is what reads as a flicker.
func (m *model) scrollReviewToCursor() {
	height := m.review.frames.height
	if height <= 0 {
		return
	}

	// A recentring key has already said where the window goes, and the margin below would drag
	// it straight back off what it was asked to show. It holds until the cursor moves, since
	// every repaint until then is drawing the same screen.
	if m.review.pinTop {
		return
	}

	margin := min(reviewScrollMargin, max((height-1)/2, 0))

	if low := m.review.cursor - margin; low < m.review.top {
		m.review.top = max(low, 0)
	}

	if high := m.review.cursor - height + 1 + margin; high > m.review.top {
		m.review.top = high
	}
}

// markReviewThreadReadCmd claims the thread the cursor has just landed on, and only that one.
// Nothing else advances the human side of the watermark, so without this a thread the agent
// has replied in stays marked unread for good. Marking the whole list instead would claim
// threads were read that were never opened.
func (m model) markReviewThreadReadCmd() tea.Cmd {
	thread, ok := m.reviewThreadAtCursor()
	if !ok || !thread.GetUnreadByHuman() {
		return nil
	}

	agents, agentID, id := m.api.agents, m.review.agentID, thread.GetId()

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), reviewTimeout)
		defer cancel()

		_, err := agents.MarkReviewThreadRead(ctx, connect.NewRequest(&agentv1.MarkReviewThreadReadRequest{Id: id}))
		if err != nil {
			return reviewWroteMsg{agentID: agentID, err: err}
		}

		threads, err := listReviewThreads(ctx, agents, agentID)

		return reviewWroteMsg{agentID: agentID, threads: threads, err: err}
	}
}

// reviewPanStep is how far sideways a press goes: a third of the pane, the way a page is a pane
// of rows. A fixed few characters means forty presses to cross a wide diff, which is the sort of
// slow that reads as the key not working.
func (m model) reviewPanStep() int {
	return max(m.review.frames.width/3, reviewPanStep)
}

func (m model) pageReview(delta int) (tea.Model, tea.Cmd) {
	return m.moveReviewCursor(m.review.view.Page(m.review.cursor, delta))
}

// reviewPage is how many rows a page is, which is the pane the diff is drawn into once that has
// been measured, and the panel's own height until then, since the stage is its sibling in the
// same split and as tall as it.
func (m model) reviewPage() int {
	if height := m.review.frames.height; height > 0 {
		return height
	}

	return max(1, m.height-1)
}

func (m model) panReview(delta int) (tea.Model, tea.Cmd) {
	next := max(0, m.review.pan+delta)
	if next == m.review.pan {
		return m, nil
	}

	m.review.pan = next
	m.stageReview()

	return m, nil
}

// composeReview opens the editor on the line under the cursor, replying in the thread already
// there or opening a new one.
//
// A thread whose code is gone has no line left to open a new thread on, and answering it is
// exactly what it is still on the screen for, so the anchor is only asked for when there is no
// thread here to reply to.
func (m model) composeReview() (tea.Model, tea.Cmd) {
	thread, replying := m.reviewThreadAtCursor()

	if _, ok := m.review.view.GetAnchor(m.review.cursor); !ok && !replying {
		return m, nil
	}

	m.review.editing = ""
	if replying {
		m.review.editing = thread.GetId()
	}

	m.review.composing = true
	m.review.editor.SetValue("")
	m.review.editor.SetWidth(max(20, m.width-4))

	return m, m.review.editor.Focus()
}

func (m model) updateReviewComposer(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := key.String()

	switch {
	case k == "esc":
		m.review.editor.Blur()
		m.review.composing = false

		return m, nil
	case k == "ctrl+c":
		return m, m.hide()
	case isBound(k, m.keys.Form.Submit):
		return m.saveReviewMessage()
	}

	editor, cmd := m.review.editor.Update(key)
	m.review.editor = editor

	return m, cmd
}

func (m model) saveReviewMessage() (tea.Model, tea.Cmd) {
	body := strings.TrimSpace(m.review.editor.Value())

	m.review.editor.Blur()
	m.review.composing = false

	if body == "" {
		return m, nil
	}

	agents, agentID, threadID := m.api.agents, m.review.agentID, m.review.editing
	if threadID != "" {
		// Nothing is said back: the reply is drawn in the thread it went into, which is already on
		// the screen the moment it lands.
		return m, m.reviewWriteCmd("", func(ctx context.Context) error {
			_, err := agents.AddReviewMessage(ctx, connect.NewRequest(&agentv1.AddReviewMessageRequest{
				ThreadId: threadID,
				Body:     body,
			}))

			return err
		})
	}

	anchor, ok := m.review.view.GetAnchor(m.review.cursor)
	if !ok {
		return m, nil
	}

	return m, m.reviewWriteCmd("thread opened", func(ctx context.Context) error {
		_, err := agents.CreateReviewThread(ctx, connect.NewRequest(&agentv1.CreateReviewThreadRequest{
			AgentId:     agentID,
			FilePath:    anchor.FilePath,
			OldStart:    int32(anchor.OldStart),
			OldEnd:      int32(anchor.OldEnd),
			NewStart:    int32(anchor.NewStart),
			NewEnd:      int32(anchor.NewEnd),
			QuotedLines: anchor.QuotedLines,
			Body:        body,
		}))

		return err
	})
}

// resolveReviewThread settles the thread the cursor is standing on, or reopens one already
// settled. It is the only thing that says a conversation is finished with.
//
// Nothing about where the thread is drawn touches this. Code moving under a question does not
// answer it, and a thread that has come adrift is settled the same way as any other.
func (m model) resolveReviewThread() (tea.Model, tea.Cmd) {
	thread, ok := m.reviewThreadAtCursor()
	if !ok {
		return m, nil
	}

	agents, id, settling := m.api.agents, thread.GetId(), thread.GetResolvedAt() == ""

	notice := "thread reopened"
	if settling {
		notice = "thread settled"
	}

	return m, m.reviewWriteCmd(notice, func(ctx context.Context) error {
		_, err := agents.SetReviewThreadResolved(ctx, connect.NewRequest(&agentv1.SetReviewThreadResolvedRequest{
			Id:       id,
			Resolved: settling,
		}))

		return err
	})
}

func (m model) deleteReviewThread() (tea.Model, tea.Cmd) {
	thread, ok := m.reviewThreadAtCursor()
	if !ok {
		return m, nil
	}

	agents, id := m.api.agents, thread.GetId()

	return m, m.reviewWriteCmd("thread deleted", func(ctx context.Context) error {
		_, err := agents.DeleteReviewThread(ctx, connect.NewRequest(&agentv1.DeleteReviewThreadRequest{Id: id}))

		return err
	})
}

// notifyReview tells the agent its review has something in it. Nothing of the review is
// delivered: the mail is a doorbell, and the agent reads the threads itself.
//
// The mail is never made to interrupt. A working agent picks it up when it next asks, and a
// dormant one is nudged by its stop hook, so interrupting only ever meant delivering the same
// doorbell twice.
func (m model) notifyReview() (tea.Model, tea.Cmd) {
	agents, agentID := m.api.agents, m.review.agentID

	// Nothing is said back: the doorbell landing in the agent's mailbox is what the conversation
	// state at the top of the sidebar reads, and it says so until the agent picks it up.
	return m, m.reviewWriteCmd("", func(ctx context.Context) error {
		_, err := agents.NotifyReviewThreads(ctx, connect.NewRequest(&agentv1.NotifyReviewThreadsRequest{
			AgentId: agentID,
		}))

		return err
	})
}

// reviewWriteCmd runs one write and reads the review back in the same breath.
func (m model) reviewWriteCmd(notice string, write func(context.Context) error) tea.Cmd {
	agents, agentID := m.api.agents, m.review.agentID

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), reviewTimeout)
		defer cancel()

		if err := write(ctx); err != nil {
			return reviewWroteMsg{agentID: agentID, err: err}
		}

		threads, err := listReviewThreads(ctx, agents, agentID)

		return reviewWroteMsg{agentID: agentID, notice: notice, threads: threads, err: err}
	}
}

func (m model) worktreeFor(agent agentstore.Agent) workflowstore.WorktreeConfig {
	for _, workflow := range m.listing.workflows {
		if workflow.ID == agent.WorkflowID {
			return workflow.Worktree
		}
	}

	return workflowstore.WorktreeConfig{}
}
