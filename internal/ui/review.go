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
	"weave/internal/ui/layout"
	agentv1 "weave/proto/gen/agent/v1"
	"weave/proto/gen/agent/v1/agentv1connect"

	"connectrpc.com/connect"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
)

var reviewStagePath = filepath.Join(os.TempDir(), "weave-review.ansi")

const reviewTimeout = 30 * time.Second

const reviewPanStep = 8

const reviewEditorHeight = 4

// reviewState is the review of one agent's diff, while that screen is up. The cursor lives
// here rather than in the stage, because the sidebar draws the thread the cursor is standing
// on and only the panel knows where that is.
type reviewState struct {
	agentID   string
	agentName string

	dir  string
	base string

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
	// editing is the thread a composed message is going into, empty when it will open a new
	// thread on the line under the cursor.
	editing string

	loading bool
	notice  string
	err     error
}

// reviewFrames is the whole diff drawn once, and the size of the pane it is being drawn into.
// Drawing every row costs real time on a large patch, so it is done when the pane or the
// horizontal offset changes rather than on every cursor move.
type reviewFrames struct {
	width  int
	height int
	pan    int
	rows   []string
}

// invalidate throws the drawn rows away while keeping the size they were drawn at, so the next
// repaint draws them again. The size is kept because only the frame reports it, and the frame
// reports it when it starts the drawer rather than on every repaint.
func (f *reviewFrames) invalidate() { f.rows = nil }

func (f *reviewFrames) ensure(view *review.View, width, height, pan int) {
	if f.rows != nil && f.width == width && f.height == height && f.pan == pan {
		return
	}

	rows := make([]string, view.Len())
	for y := range rows {
		rows[y] = view.RenderRow(y, false, width, pan)
	}

	f.width, f.height, f.pan, f.rows = width, height, pan, rows
}

// window is the screenful starting at top, with the cursor's own row drawn as the cursor. The
// panel scrolls the diff itself rather than asking the pager to, which is what lets the pager
// stay running: it is handed exactly one screen, already positioned.
func (f *reviewFrames) window(view *review.View, top, cursor int) string {
	height := max(f.height, 1)

	start := max(min(top, len(f.rows)-height), 0)

	rows := make([]string, 0, height)
	for y := start; y < min(start+height, len(f.rows)); y++ {
		if y == cursor {
			rows = append(rows, view.RenderRow(y, true, f.width, f.pan))

			continue
		}

		rows = append(rows, f.rows[y])
	}

	// Padded out rather than left short, so a diff smaller than the pane is followed by blank
	// lines instead of the pager's end-of-file markers.
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

	editor := textarea.New()
	editor.Placeholder = "what should the agent change here?"
	editor.ShowLineNumbers = false
	editor.SetHeight(reviewEditorHeight)

	m.enterScreen(screenReview)
	m.review = reviewState{
		agentID:   agent.ID,
		agentName: agent.Name,
		dir:       agent.Cwd,
		base:      m.baseBranchFor(agent),
		frames:    &reviewFrames{},
		editor:    editor,
		loading:   true,
	}
	m.frame.Show(layout.RegionStage, newTextContent("_loading the diff…_"))

	return m, m.loadReviewCmd(m.review.agentID, m.review.dir, m.review.base)
}

func (m model) loadReviewCmd(agentID, dir, base string) tea.Cmd {
	agents := m.api.agents

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), reviewTimeout)
		defer cancel()

		patch, err := diff.Load(ctx, diff.LoadParams{Dir: dir, Base: base})
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

	m.review.view = review.NewView(msg.patch, m.isDark)
	m.review.view.PinThreads(msg.threads)
	m.review.threads = msg.threads
	m.review.cursor = m.review.view.First()
	m.review.frames = &reviewFrames{}

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
	m.review.view.PinThreads(msg.threads)
	// A gutter mark appearing or going means the rows themselves changed, not just the cursor.
	m.review.frames.invalidate()

	m.stageReview()
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
	// is. Everything after that is drawn here, including the rows themselves: the drawer is only
	// started once, so nothing else runs again to draw them.
	if pane == "" || m.review.view == nil || frames.width == 0 {
		return nil
	}

	frames.ensure(m.review.view, frames.width, frames.height, m.review.pan)

	if !writeStageFile(reviewStagePath, m.review.frames.window(m.review.view, m.review.top, m.review.cursor)) {
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
			width := max(target.Width, review.GutterWidth+20)
			state.frames.ensure(state.view, width, max(target.Height, 1), state.pan)

			if !writeStageFile(reviewStagePath, state.frames.window(state.view, state.top, state.cursor)) {
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

func (m model) updateReview(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.review.composing {
		return m.updateReviewComposer(key)
	}

	k := key.String()
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
	case k == "n":
		return m.moveReviewCursor(m.review.view.JumpThread(m.review.cursor, 1))
	case k == "N":
		return m.moveReviewCursor(m.review.view.JumpThread(m.review.cursor, -1))
	case k == "l", k == "right":
		return m.panReview(reviewPanStep)
	case k == "h", k == "left":
		return m.panReview(-reviewPanStep)
	case k == "0":
		return m.panReview(-m.review.pan)
	case k == "c":
		return m.composeReview()
	case k == "D":
		return m.deleteReviewThread()
	case k == "s":
		return m.notifyReview()
	}

	return m, nil
}

// reviewThreadAtCursor is the thread the cursor is standing on, and the one place that asks.
// It answers false while the diff is still loading, which is what a nil view stands for: the
// sidebar and its legend are drawn on every frame, including the frames before the patch has
// arrived, so anything reaching into the view has to survive not having one yet.
func (m model) reviewThreadAtCursor() (*agentv1.ReviewThread, bool) {
	if m.review.view == nil {
		return nil, false
	}

	return m.review.view.ThreadAt(m.review.cursor)
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
	if _, ok := m.review.view.GetAnchor(m.review.cursor); !ok {
		return m, nil
	}

	m.review.editing = ""
	if thread, ok := m.reviewThreadAtCursor(); ok {
		m.review.editing = thread.GetId()
	}

	m.review.composing = true
	m.review.editor.SetValue("")
	m.review.editor.SetWidth(max(20, m.width-4))

	return m, m.review.editor.Focus()
}

func (m model) updateReviewComposer(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "esc":
		m.review.editor.Blur()
		m.review.composing = false

		return m, nil
	case "ctrl+c":
		return m, m.hide()
	case "ctrl+s":
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
		return m, m.reviewWriteCmd("replied", func(ctx context.Context) error {
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

	return m, m.reviewWriteCmd("agent told", func(ctx context.Context) error {
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

func (m model) baseBranchFor(agent agentstore.Agent) string {
	for _, workflow := range m.listing.workflows {
		if workflow.ID == agent.WorkflowID {
			return workflow.Worktree.BaseBranch
		}
	}

	return ""
}
