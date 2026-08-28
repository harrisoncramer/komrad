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

// reviewState is the review of one agent's diff, while that screen is up.
type reviewState struct {
	agentID   string
	agentName string

	dir     string
	root    string
	base    string
	exclude []string

	view    *review.View
	threads []*agentv1.ReviewThread

	// cursor lives here rather than in the stage, since the sidebar draws the thread it stands on.
	cursor int
	pan    int
	// top is the first row the pane is showing, held rather than derived from the cursor, since
	// deriving it centres the cursor and repaints every row on every move.
	top int

	frames *reviewFrames

	composing bool
	editor    textarea.Model

	// filter is the pattern being searched for in the diff, kept apart from the agents list's own
	// filter so a pattern meant for code does not empty that list.
	filter filterState

	// pending is a key waiting on the one after it, which is only ever z today.
	pending string

	// pinTop holds the window where zt or zb put it until the cursor moves, since the next repaint
	// would otherwise drag it back to keep the cursor clear of the edge.
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

// reviewFrames is the size of the pane the diff is drawn into, kept here because the frame
// reports it once when it starts the drawer rather than on every repaint.
type reviewFrames struct {
	width  int
	height int
}

// window is the screenful starting at top, with the cursor's own row drawn as the cursor.
func (f *reviewFrames) window(view *review.View, top, cursor, pan int) string {
	height := max(f.height, 1)

	start := max(min(top, view.Len()-height), 0)

	// Only the rows the pane can show are drawn, since a nine thousand row diff takes a tenth of
	// a second to draw whole and panning would pay that on every keypress.
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

// reviewWroteMsg is one write and the review as it stands after it, since the panel keeps no
// copy of a thread it has just changed and reads the review back instead.
type reviewWroteMsg struct {
	agentID string

	notice string

	threads []*agentv1.ReviewThread

	err error
}

// enterReview opens the review of an agent's diff, with the threads in the sidebar and the
// diff in the stage.
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
// inside the diff.
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
func (m model) repaintReview() error {
	pane := m.frame.Pane(layout.RegionStage)
	frames := m.review.frames

	// Nothing to draw against until the frame has started the drawer and reported its pane size.
	if pane == "" || m.review.view == nil || frames.width == 0 {
		return nil
	}

	if !writeStageFile(reviewStagePath, frames.window(m.review.view, m.review.top, m.review.cursor, m.review.pan)) {
		return fmt.Errorf("failed to write the diff to %s", reviewStagePath)
	}

	// The pager is left running and re-reads on any key, since restarting it repainted the whole
	// stage as a dozen partial writes over twenty milliseconds and read as a flicker.
	return m.tmux.SendKeys(pane, "R")
}

// reviewContent starts the pager on the diff, keyed by the agent alone so that moving the
// cursor repaints rather than respawning the pane.
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

// reviewStageArgv runs weave's own drawer in the stage rather than a pager, since bubbletea's
// renderer writes only the rows that differ where a pager repaints its whole screen.
func (m model) reviewStageArgv(path string) []string {
	self := m.self
	if self == "" {
		self = "weave"
	}

	return []string{self, "stage", path}
}

// takesEveryKey reports whether the review is mid-input, which means a key bound elsewhere in
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

// updateReviewPending answers the key after a z prefix, which decides where in the pane the
// cursor's own row is put.
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

// reviewThreadAtCursor is the thread the cursor is standing on, and false while the diff is
// still loading, since the sidebar is drawn on frames that arrive before the patch does.
func (m model) reviewThreadAtCursor() (*agentv1.ReviewThread, bool) {
	if m.review.view == nil {
		return nil, false
	}

	return m.review.view.ThreadAt(m.review.cursor)
}

// reloadReview reads the worktree again, holding the place the cursor is standing so the diff
// comes back under it.
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

// scrollReviewToCursor moves the window only when the cursor is about to leave it, since
// scrolling on every move repaints every row of the pane and reads as a flicker.
func (m *model) scrollReviewToCursor() {
	height := m.review.frames.height
	if height <= 0 {
		return
	}

	// A recentring key has already said where the window goes, and the margin below would drag it
	// straight back off what it was asked to show.
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

// markReviewThreadReadCmd claims the thread the cursor has just landed on and only that one,
// since marking the whole list would claim threads were read that were never opened.
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

// reviewPanStep is how far sideways a press goes, a third of the pane, since a fixed few
// characters meant forty presses to cross a wide diff.
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
func (m model) composeReview() (tea.Model, tea.Cmd) {
	thread, replying := m.reviewThreadAtCursor()

	// A thread whose code is gone has no line to anchor a new thread to, but replying to it is
	// what it is still on screen for.
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
// settled.
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

// notifyReview tells the agent its review has something in it, without delivering any of it,
// since the agent reads the threads itself.
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
