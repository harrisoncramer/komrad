// Package review draws an agent's diff, lets a cursor walk it one line at a time, and pins
// review threads to the lines it stops on.
package review

import (
	"context"
	"fmt"
	"strings"

	"weave/internal/diff"
	agentv1 "weave/proto/gen/agent/v1"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// chromeHeight is the header above the diff and the footer under it.
const chromeHeight = 2

// composerHeight is the box a message is typed in, which the diff gives up while it is open.
const composerHeight = 9

const editorHeight = 5

type mode uint8

const (
	modeDiff mode = iota
	modeCompose
	modeThreads
	modeThread
	modeNotify
)

// RunParams is what the viewer needs to draw one agent's diff.
type RunParams struct {
	Patch *diff.Patch

	AgentName string

	// Threads reaches the review threads on this agent. Left unset the viewer runs against an
	// in-memory store, which is the shape of the API rather than the API.
	Threads ThreadClient
}

// Run draws the diff and returns once the viewer has been closed.
func Run(params RunParams) error {
	program := tea.NewProgram(newModel(params), tea.WithAltScreen())
	if _, err := program.Run(); err != nil {
		return fmt.Errorf("failed to draw the diff: %w", err)
	}

	return nil
}

type threadsLoadedMsg struct {
	threads []*agentv1.ReviewThread
	err     error
}

type model struct {
	patch *diff.Patch
	view  *view

	agentName string
	threads   ThreadClient

	loaded []*agentv1.ReviewThread

	mode mode

	editor textarea.Model
	// editing is the thread a composed message is going into, empty when the message will
	// open a new thread on the line under the cursor.
	editing string

	// picked is the row of the thread screen, and the thread the thread screen is showing.
	picked int

	notice string

	cursor int
	top    int

	leftColumn int

	width  int
	height int

	err error
}

func newModel(params RunParams) model {
	threads := params.Threads
	if threads == nil {
		threads = newMemoryThreadClient()
	}

	v := newView(params.Patch, lipgloss.HasDarkBackground())

	editor := textarea.New()
	editor.Placeholder = "what should the agent change here?"
	editor.ShowLineNumbers = false
	editor.SetHeight(editorHeight)

	return model{
		patch:     params.Patch,
		view:      v,
		agentName: params.AgentName,
		threads:   threads,
		editor:    editor,
		cursor:    v.findFirst(),
	}
}

func (m model) Init() tea.Cmd { return m.loadThreads() }

// loadThreads reads the review back from the service. The viewer holds no copy of its own, so
// this runs at startup and after every write.
func (m model) loadThreads() tea.Cmd {
	threads := m.threads

	return func() tea.Msg {
		loaded, err := threads.ListReviewThreads(context.Background())

		return threadsLoadedMsg{threads: loaded, err: err}
	}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.editor.SetWidth(max(20, m.width-4))

		return m.keepCursorVisible(), nil
	case threadsLoadedMsg:
		if msg.err != nil {
			m.err = msg.err

			return m, nil
		}

		m.err = nil
		m.loaded = msg.threads
		m.view.pinThreads(msg.threads)
		m.picked = min(m.picked, max(0, len(msg.threads)-1))

		return m, nil
	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	return m, nil
}

func (m model) handleKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.mode {
	case modeCompose:
		return m.handleComposeKey(key)
	case modeThreads:
		return m.handleThreadsKey(key)
	case modeThread:
		return m.handleThreadKey(key)
	case modeNotify:
		return m.handleNotifyKey(key)
	case modeDiff:
	}

	return m.handleDiffKey(key)
}

func (m model) handleDiffKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	page := m.getBodyHeight()
	half := max(1, page/2)

	m.notice = ""

	switch key.String() {
	case "q", "esc", "ctrl+c":
		return m, tea.Quit
	case "j", "down":
		m.cursor = m.view.move(m.cursor, 1)
	case "k", "up":
		m.cursor = m.view.move(m.cursor, -1)
	case "ctrl+d":
		m.cursor = m.pageBy(half)
	case "ctrl+u":
		m.cursor = m.pageBy(-half)
	case "pgdown", "ctrl+f", " ":
		m.cursor = m.pageBy(page)
	case "pgup", "ctrl+b":
		m.cursor = m.pageBy(-page)
	case "g":
		m.cursor = m.view.findFirst()
	case "G":
		m.cursor = m.view.findLast()
	case "]":
		m.cursor = m.view.jumpFile(m.cursor, 1)
	case "[":
		m.cursor = m.view.jumpFile(m.cursor, -1)
	case "n":
		m.cursor = m.view.jumpThread(m.cursor, 1)
	case "N":
		m.cursor = m.view.jumpThread(m.cursor, -1)
	case "l", "right":
		m.leftColumn += horizontalStep
	case "h", "left":
		m.leftColumn = max(0, m.leftColumn-horizontalStep)
	case "0":
		m.leftColumn = 0
	case "c":
		return m.compose("")
	case "enter":
		return m.openThreadUnderCursor()
	case "C":
		m.mode = modeThreads

		return m, nil
	case "S":
		return m.beginNotify()
	}

	return m.keepCursorVisible(), nil
}

// compose opens the editor. An empty thread id means the message will open a new thread on
// the line under the cursor.
func (m model) compose(threadID string) (tea.Model, tea.Cmd) {
	if threadID == "" {
		if _, ok := m.view.GetAnchor(m.cursor); !ok {
			return m, nil
		}
	}

	m.editing = threadID
	m.mode = modeCompose
	m.editor.SetValue("")
	m.editor.SetWidth(max(20, m.width-4))

	return m, m.editor.Focus()
}

func (m model) handleComposeKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "esc":
		m.editor.Blur()
		m.mode = modeDiff

		return m, nil
	case "ctrl+c":
		return m, tea.Quit
	case "ctrl+s":
		return m.saveMessage()
	}

	editor, cmd := m.editor.Update(key)
	m.editor = editor

	return m, cmd
}

func (m model) saveMessage() (tea.Model, tea.Cmd) {
	body := strings.TrimSpace(m.editor.Value())

	m.editor.Blur()
	m.mode = modeDiff

	if body == "" {
		return m, nil
	}

	threads, threadID := m.threads, m.editing
	if threadID != "" {
		m.notice = "replied"

		return m, tea.Sequence(
			func() tea.Msg {
				if _, err := threads.AddReviewMessage(context.Background(), threadID, body); err != nil {
					return threadsLoadedMsg{err: err}
				}

				return nil
			},
			m.loadThreads(),
		)
	}

	anchor, ok := m.view.GetAnchor(m.cursor)
	if !ok {
		return m, nil
	}

	m.notice = "thread opened"

	return m, tea.Sequence(
		func() tea.Msg {
			if _, err := threads.CreateReviewThread(context.Background(), *anchor, body); err != nil {
				return threadsLoadedMsg{err: err}
			}

			return nil
		},
		m.loadThreads(),
	)
}

func (m model) openThreadUnderCursor() (tea.Model, tea.Cmd) {
	thread, ok := m.view.getThread(m.cursor)
	if !ok {
		return m, nil
	}

	for index, candidate := range m.loaded {
		if candidate.GetId() == thread.GetId() {
			m.picked = index
		}
	}

	m.mode = modeThread

	return m, nil
}

func (m model) handleThreadsKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "esc", "q", "C":
		m.mode = modeDiff
	case "ctrl+c":
		return m, tea.Quit
	case "j", "down":
		m.picked = min(m.picked+1, max(0, len(m.loaded)-1))
	case "k", "up":
		m.picked = max(0, m.picked-1)
	case "enter":
		if len(m.loaded) > 0 {
			m.mode = modeThread
		}
	case "d":
		return m.jumpToPicked()
	}

	return m, nil
}

// jumpToPicked takes the cursor to the line a thread is pinned to, when its anchor still
// holds. A thread whose code has been rewritten has nowhere in the diff to go.
func (m model) jumpToPicked() (tea.Model, tea.Cmd) {
	thread := m.getPicked()
	if thread == nil {
		return m, nil
	}

	found := m.view.findAnchor(thread)
	if found < 0 {
		m.notice = "that line is not in the diff any more"

		return m, nil
	}

	m.cursor = found
	m.mode = modeDiff

	return m.keepCursorVisible(), nil
}

func (m model) handleThreadKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	thread := m.getPicked()

	switch key.String() {
	case "esc", "q":
		m.mode = modeThreads
	case "ctrl+c":
		return m, tea.Quit
	case "r":
		if thread != nil {
			return m.compose(thread.GetId())
		}
	case "x":
		if thread != nil {
			return m.resolvePicked(thread)
		}
	case "D":
		if thread != nil {
			return m.deletePicked(thread)
		}
	}

	return m, nil
}

func (m model) resolvePicked(thread *agentv1.ReviewThread) (tea.Model, tea.Cmd) {
	threads, id, resolved := m.threads, thread.GetId(), thread.GetResolvedAt() == ""

	m.notice = "resolved"
	if !resolved {
		m.notice = "reopened"
	}

	return m, tea.Sequence(
		func() tea.Msg {
			if _, err := threads.ResolveReviewThread(context.Background(), id, resolved); err != nil {
				return threadsLoadedMsg{err: err}
			}

			return nil
		},
		m.loadThreads(),
	)
}

func (m model) deletePicked(thread *agentv1.ReviewThread) (tea.Model, tea.Cmd) {
	threads, id := m.threads, thread.GetId()

	m.mode = modeThreads
	m.notice = "thread deleted"

	return m, tea.Sequence(
		func() tea.Msg {
			if err := threads.DeleteReviewThread(context.Background(), id); err != nil {
				return threadsLoadedMsg{err: err}
			}

			return nil
		},
		m.loadThreads(),
	)
}

func (m model) beginNotify() (tea.Model, tea.Cmd) {
	if len(m.loaded) == 0 {
		m.notice = "nothing to tell the agent about"

		return m, nil
	}

	m.mode = modeNotify

	return m, nil
}

func (m model) handleNotifyKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "esc", "q":
		m.mode = modeDiff

		return m, nil
	case "ctrl+c":
		return m, tea.Quit
	case "s":
		return m.notify(false)
	case "i":
		return m.notify(true)
	}

	return m, nil
}

func (m model) notify(interrupt bool) (tea.Model, tea.Cmd) {
	threads := m.threads

	m.mode = modeDiff
	m.notice = "told the agent"
	if interrupt {
		m.notice = "interrupted the agent and told it"
	}

	return m, tea.Sequence(
		func() tea.Msg {
			if _, err := threads.NotifyReviewThreads(context.Background(), interrupt); err != nil {
				return threadsLoadedMsg{err: err}
			}

			return nil
		},
		m.loadThreads(),
	)
}

func (m model) getPicked() *agentv1.ReviewThread {
	if m.picked < 0 || m.picked >= len(m.loaded) {
		return nil
	}

	return m.loaded[m.picked]
}

// pageBy moves the cursor a screenful at a time. The jump is measured in drawn rows rather
// than in lines the cursor can hold, so a page moves what the eye saw move, and the cursor
// then settles on the nearest row it is allowed to rest on.
func (m model) pageBy(distance int) int {
	target := max(0, min(m.cursor+distance, len(m.view.rows)-1))
	if m.view.isPinnable(target) {
		return target
	}

	step := 1
	if distance < 0 {
		step = -1
	}

	if next := m.view.scan(target, step); next >= 0 {
		return next
	}
	if next := m.view.scan(target, -step); next >= 0 {
		return next
	}

	return m.cursor
}

func (m model) keepCursorVisible() model {
	body := m.getBodyHeight()

	if m.cursor < m.top {
		m.top = m.cursor
	}
	if m.cursor >= m.top+body {
		m.top = m.cursor - body + 1
	}

	m.top = max(0, min(m.top, max(0, len(m.view.rows)-body)))

	return m
}

func (m model) getBodyHeight() int {
	height := m.height - chromeHeight
	if m.mode == modeCompose {
		height -= composerHeight
	}

	return max(1, height)
}
