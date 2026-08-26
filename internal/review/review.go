// Package review draws an agent's diff, lets a cursor walk it one line at a time, and pins
// review threads to the lines it stops on.
package review

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"weave/internal/diff"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ErrNoThreadClient is answered rather than falling back to something that looks like a
// review and is thrown away when the popup closes.
var ErrNoThreadClient = errors.New("the diff viewer needs a review thread client")

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

	// Threads reaches the review threads on this agent, and is required: a review that lived
	// in the process drawing it would look saved and be gone when the popup closed.
	Threads ThreadClient
}

// Run draws the diff and returns once the viewer has been closed.
func Run(params RunParams) error {
	if params.Threads == nil {
		return ErrNoThreadClient
	}

	program := tea.NewProgram(newModel(params), tea.WithAltScreen())
	if _, err := program.Run(); err != nil {
		return fmt.Errorf("failed to draw the diff: %w", err)
	}

	return nil
}

type threadsLoadedMsg struct {
	threads []Thread
	err     error
}

type noticeMsg struct {
	text string
	err  error
}

type model struct {
	patch *diff.Patch
	view  *view

	agentName string
	threads   ThreadClient

	loaded []Thread

	// shown is which of the loaded threads the thread screen lists, as indices into loaded.
	// It is every thread most of the time, and one line's threads when the cursor was on a
	// line holding several and you asked to choose between them.
	shown []int
	// scoped says shown is one line's threads rather than all of them, which is only worth
	// knowing so the screen can say so.
	scoped bool

	// picked is which of shown the thread screen has selected.
	picked int

	// conversationTop is how far the open conversation is scrolled, since a long one does not
	// fit and every message has to be reachable.
	conversationTop int

	mode mode

	editor textarea.Model
	// editing is the thread a composed message is going into, empty when the message will
	// open a new thread on the line under the cursor.
	editing string

	notice string

	cursor int
	top    int

	leftColumn int

	width  int
	height int

	err error
}

func newModel(params RunParams) model {
	v := newView(params.Patch, lipgloss.HasDarkBackground())

	editor := textarea.New()
	editor.Placeholder = "what should the agent change here?"
	editor.ShowLineNumbers = false
	editor.SetHeight(editorHeight)

	return model{
		patch:     params.Patch,
		view:      v,
		agentName: params.AgentName,
		threads:   params.Threads,
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

		return m.adopt(msg.threads), nil
	case noticeMsg:
		if msg.err != nil {
			m.err = msg.err

			return m, nil
		}

		m.err = nil
		m.notice = msg.text

		return m, nil
	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	return m, nil
}

// adopt takes a freshly read review and keeps the screens pointing where they were: the same
// thread stays selected across a reload, and a line's chooser goes on showing that line.
func (m model) adopt(threads []Thread) model {
	selected := ""
	if picked := m.getPicked(); picked != nil {
		selected = picked.ID
	}

	m.loaded = threads
	m.view.pinThreads(threads)

	if m.scoped {
		m = m.scopeTo(m.anchorOf(selected))
	} else {
		m = m.scopeToAll()
	}

	m.picked = 0
	for index, position := range m.shown {
		if m.loaded[position].ID == selected {
			m.picked = index
		}
	}

	return m
}

// anchorOf is where a thread was pinned, used to rebuild a line's chooser after a reload.
func (m model) anchorOf(threadID string) anchorKey {
	for _, thread := range m.loaded {
		if thread.ID == threadID {
			return keyForThread(thread)
		}
	}

	if key, ok := m.view.getKey(m.cursor); ok {
		return key
	}

	return anchorKey{}
}

func (m model) scopeToAll() model {
	m.shown = make([]int, 0, len(m.loaded))
	for index := range m.loaded {
		m.shown = append(m.shown, index)
	}
	m.scoped = false

	return m
}

func (m model) scopeTo(key anchorKey) model {
	m.shown = nil
	for index, thread := range m.loaded {
		if keyForThread(thread) == key {
			m.shown = append(m.shown, index)
		}
	}
	m.scoped = true

	return m
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
		return m.openThreadsUnderCursor()
	case "C":
		return m.openAllThreads()
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

		return m, m.write(func(ctx context.Context) error {
			return threads.AddReviewMessage(ctx, threadID, body)
		})
	}

	anchor, ok := m.view.GetAnchor(m.cursor)
	if !ok {
		return m, nil
	}

	m.notice = "thread opened"

	return m, m.write(func(ctx context.Context) error {
		return threads.CreateReviewThread(ctx, *anchor, body)
	})
}

// write runs one change against the service and then reads the review back, since the viewer
// keeps no copy it could update itself.
func (m model) write(change func(context.Context) error) tea.Cmd {
	return tea.Sequence(
		func() tea.Msg {
			if err := change(context.Background()); err != nil {
				return threadsLoadedMsg{err: err}
			}

			return nil
		},
		m.loadThreads(),
	)
}

// openThreadsUnderCursor opens what is pinned to the line the cursor is on. One thread opens
// straight into the conversation; several open the list of them, since the gutter mark cannot
// say which of them you meant.
func (m model) openThreadsUnderCursor() (tea.Model, tea.Cmd) {
	here := m.view.getThreads(m.cursor)
	if len(here) == 0 {
		return m, nil
	}

	key, ok := m.view.getKey(m.cursor)
	if !ok {
		return m, nil
	}

	m = m.scopeTo(key)
	m.picked = 0

	if len(here) > 1 {
		m.mode = modeThreads

		return m, nil
	}

	return m.openConversation()
}

func (m model) openAllThreads() (tea.Model, tea.Cmd) {
	m = m.scopeToAll()
	m.picked = 0
	m.mode = modeThreads

	return m, nil
}

// openConversation shows the selected thread and tells the service it has been read, which is
// what stops a thread the agent replied in from being marked unread for ever.
func (m model) openConversation() (tea.Model, tea.Cmd) {
	thread := m.getPicked()
	if thread == nil {
		return m, nil
	}

	m.mode = modeThread
	m.conversationTop = 0

	if !thread.UnreadByHuman {
		return m, nil
	}

	threads, id := m.threads, thread.ID

	return m, m.write(func(ctx context.Context) error {
		return threads.MarkReviewThreadRead(ctx, id)
	})
}

func (m model) handleThreadsKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "esc", "q", "C":
		m.mode = modeDiff
	case "ctrl+c":
		return m, tea.Quit
	case "j", "down":
		m.picked = min(m.picked+1, max(0, len(m.shown)-1))
	case "k", "up":
		m.picked = max(0, m.picked-1)
	case "g":
		m.picked = 0
	case "G":
		m.picked = max(0, len(m.shown)-1)
	case "enter":
		return m.openConversation()
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

	found := m.view.findAnchor(*thread)
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
	if thread == nil {
		m.mode = modeThreads

		return m, nil
	}

	height := m.getScreenHeight()
	lines := len(m.conversationLines(*thread))

	switch key.String() {
	case "esc", "q":
		m.mode = modeThreads
	case "ctrl+c":
		return m, tea.Quit
	case "j", "down":
		m.conversationTop = clampTop(lines, height, m.conversationTop+1)
	case "k", "up":
		m.conversationTop = clampTop(lines, height, m.conversationTop-1)
	case "ctrl+d", "pgdown":
		m.conversationTop = clampTop(lines, height, m.conversationTop+height)
	case "ctrl+u", "pgup":
		m.conversationTop = clampTop(lines, height, m.conversationTop-height)
	case "g":
		m.conversationTop = 0
	case "G":
		m.conversationTop = clampTop(lines, height, lines)
	case "r":
		return m.compose(thread.ID)
	case "x":
		return m.resolvePicked(*thread)
	case "D":
		return m.deletePicked(*thread)
	}

	return m, nil
}

func (m model) resolvePicked(thread Thread) (tea.Model, tea.Cmd) {
	threads, id, resolved := m.threads, thread.ID, !thread.Resolved

	m.notice = "resolved"
	if !resolved {
		m.notice = "reopened"
	}

	return m, m.write(func(ctx context.Context) error {
		return threads.ResolveReviewThread(ctx, id, resolved)
	})
}

func (m model) deletePicked(thread Thread) (tea.Model, tea.Cmd) {
	threads, id := m.threads, thread.ID

	m.mode = modeThreads
	m.notice = "thread deleted"

	return m, m.write(func(ctx context.Context) error {
		return threads.DeleteReviewThread(ctx, id)
	})
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

// notify tells the agent there is something to read and says what that achieved. Whether the
// agent has actually been reached is worth reporting: one already waiting on an unread notice
// is not told twice, and one sitting at a prompt of its own is not typed through.
func (m model) notify(interrupt bool) (tea.Model, tea.Cmd) {
	threads := m.threads

	m.mode = modeDiff

	return m, tea.Sequence(
		func() tea.Msg {
			notified, err := threads.NotifyReviewThreads(context.Background(), interrupt)
			if err != nil {
				return noticeMsg{err: err}
			}

			return noticeMsg{text: describeNotification(notified, interrupt)}
		},
		m.loadThreads(),
	)
}

func describeNotification(notified Notification, interrupt bool) string {
	if notified.Unread == 0 {
		return "the agent has already read everything here"
	}

	said := fmt.Sprintf("told the agent about %d thread(s)", notified.Unread)
	if !notified.Rang {
		said = fmt.Sprintf("the agent was already holding a notice about %d thread(s)", notified.Unread)
	}

	switch {
	case notified.Waiting:
		return said + ", but it is waiting at a prompt and will not be told until it answers"
	case interrupt && !notified.Interrupted:
		return said + ", but it could not be stopped where it stands"
	case notified.Interrupted:
		return said + " and stopped it where it stood"
	}

	return said
}

// getPicked is the thread the thread screens are showing, which is the selected row of
// whichever set of threads is being listed.
func (m model) getPicked() *Thread {
	if m.picked < 0 || m.picked >= len(m.shown) {
		return nil
	}

	position := m.shown[m.picked]
	if position < 0 || position >= len(m.loaded) {
		return nil
	}

	return &m.loaded[position]
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
