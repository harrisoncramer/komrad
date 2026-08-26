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

// reviewFrames is the whole diff drawn once. Drawing every row costs real time on a large
// patch, so it is done when the width changes rather than on every cursor move, and moving
// redraws only the row the cursor left and the one it landed on.
type reviewFrames struct {
	width int
	pan   int
	rows  []string
}

func (f *reviewFrames) ensure(view *review.View, width, pan int) {
	if f.rows != nil && f.width == width && f.pan == pan {
		return
	}

	rows := make([]string, view.Len())
	for y := range rows {
		rows[y] = view.RenderRow(y, false, width, pan)
	}

	f.width, f.pan, f.rows = width, pan, rows
}

func (f *reviewFrames) body(view *review.View, cursor int) string {
	if cursor < 0 || cursor >= len(f.rows) {
		return strings.Join(f.rows, "\n")
	}

	rows := make([]string, len(f.rows))
	copy(rows, f.rows)
	rows[cursor] = view.RenderRow(cursor, true, f.width, f.pan)

	return strings.Join(rows, "\n")
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
	m.review.frames = &reviewFrames{}

	m.stageReview()
}

func (m *model) stageReview() {
	if m.review.view == nil {
		return
	}

	m.frame.Show(layout.RegionStage, m.reviewContent())
}

// reviewContent draws the diff into the stage. The whole patch is written out and the pager is
// opened centred on the cursor, so the row under the cursor is redrawn and repositioned in one
// respawn rather than the stage holding a program of its own.
func (m model) reviewContent() layout.Content {
	state := m.review

	return layout.Content{
		Key:   fmt.Sprintf("review:%s:%d:%d", state.agentID, state.cursor, state.pan),
		Wraps: true,
		Render: func(target layout.Target) ([]string, bool) {
			width := max(target.Width, review.GutterWidth+20)
			state.frames.ensure(state.view, width, state.pan)

			if !writeStageFile(reviewStagePath, state.frames.body(state.view, state.cursor)) {
				return nil, false
			}

			return reviewPagerArgv(reviewStagePath, state.cursor+1, target.Height), true
		},
	}
}

// reviewPagerArgv opens the pager with the cursor's row held half way down the pane, so moving
// the cursor scrolls the diff under it rather than walking it off the bottom.
//
// The row is named absolutely rather than as a fraction, because a fraction asks for the line
// to be centred even when there is nothing above it to show: the pager honours that by padding
// the top of the pane with tildes, which wastes half the stage on the first screen of every
// diff. Naming the row lets the target sit where it naturally falls until the diff is deep
// enough to scroll.
func reviewPagerArgv(path string, line, height int) []string {
	row := line
	if middle := height / 2; middle > 0 && row > middle {
		row = middle
	}

	return []string{"less", "-R", "-X", "-Ps", fmt.Sprintf("-j%d", max(row, 1)), fmt.Sprintf("+%d", line), path}
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
	case k == "pgdown", k == "ctrl+f", k == " ":
		return m.pageReview(m.reviewPage())
	case k == "pgup", k == "ctrl+b":
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

// reviewPage is how many rows a page is. The stage is the pane beside the panel in the same
// split, so it is as tall as the panel, less the pager's own prompt line.
func (m model) reviewPage() int {
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
