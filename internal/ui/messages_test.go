package ui

import (
	"strings"
	"testing"

	"weave/internal/ui/paint"
	"weave/lib/dates"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newestMessage is the message a write's refusal was reported as, which is the one the region
// draws and the one a test asking what the panel said is asking about.
func newestMessage(m model) message {
	if len(m.messages.items) == 0 {
		return message{}
	}

	return m.messages.items[0]
}

func oneMessage(summary, detail string) messagesState {
	return messagesState{items: []message{{
		kind:    messageError,
		summary: summary,
		detail:  detail,
	}}}
}

// TestMessagesRegionFitsTheMessage covers the rows the region takes: never more than the pane
// keeps back for it, and one fewer while there is nothing to say, which is a row the list gets.
func TestMessagesRegionFitsTheMessage(t *testing.T) {
	m := detailsModel(&recordingTmux{})
	m.height, m.width = 40, 60

	record, help := m.detailsView(), m.renderHelp(m.agentHelp())

	idle := m.messagesRegion(record, help)
	require.NotEmpty(t, idle)
	assert.Contains(t, ansi.Strip(idle), "No messages")

	m.pushMessage(message{
		kind:    messageError,
		summary: "Nothing to review in fix-ci",
		detail:  "the worktree holds no change against the base",
	})
	filled := m.messagesRegion(record, help)

	assert.Equal(t, strings.Count(idle, "\n")+1, strings.Count(filled, "\n"), "the idle region gives a row back")
	assert.LessOrEqual(t, strings.Count(filled, "\n"), messagesRegionRows)
	assert.Contains(t, ansi.Strip(filled), "Nothing to review in fix-ci")
}

// TestMessagesRegionGivesUpAShortPane covers a pane with no rows to spare, where the region is
// dropped rather than drawn past the bottom of the list.
func TestMessagesRegionGivesUpAShortPane(t *testing.T) {
	m := detailsModel(&recordingTmux{})
	m.height, m.width = 12, 60

	assert.Empty(t, m.messagesRegion(m.detailsView(), m.renderHelp(m.agentHelp())))
}

// TestMessagesStackKeepsWhatCameBefore covers the stack: a second failure does not erase the
// first, and the tally says how much is behind the one on screen.
func TestMessagesStackKeepsWhatCameBefore(t *testing.T) {
	m := detailsModel(&recordingTmux{})
	m.height, m.width = 40, 60

	m.report("Could not archive agent", errNoWorkflow)
	m.report("Could not review agent", errNoWorkflow)

	require.Len(t, m.messages.items, 2)
	assert.Equal(t, "Could not review agent", newestMessage(m).summary)
	assert.Contains(t, ansi.Strip(m.messagesRegion(m.detailsView(), m.renderHelp(m.agentHelp()))), "+1")
}

// TestMessagesFocusTakesTheBlock covers focus moving the keys off the list, which is what draws
// the whole message beside it rather than the three rows the region keeps.
func TestMessagesFocusTakesTheBlock(t *testing.T) {
	m := detailsModel(&recordingTmux{})
	m.height, m.width = 40, 120
	m.pushMessage(message{
		kind:    messageError,
		summary: "Nothing to review in fix-ci",
		detail:  "the worktree holds no change against the base",
		trace:   "7c1f2a94",
	})

	focused := m.toggleMessagesFocus()

	require.True(t, focused.isMessagesFocused())
	assert.False(t, focused.isListFocused(), "the list goes dormant while the region holds the keys")

	doc := focused.messagesDoc()
	assert.Contains(t, doc, "Nothing to review in fix-ci")
	assert.Contains(t, doc, "the worktree holds no change against the base")
	assert.Contains(t, doc, "weave trace 7c1f2a94")

	spec, ok := focused.columnOf()
	require.True(t, ok, "the region takes the block beside the list")
	require.NotNil(t, spec.doc)
}

// TestMessagesDismissalWalksTheStack covers esc taking the message on screen away and landing on
// the one behind it, handing the keys back once there is nothing left to read.
func TestMessagesDismissalWalksTheStack(t *testing.T) {
	m := detailsModel(&recordingTmux{})
	m.height, m.width = 40, 120
	m.report("Could not archive agent", errNoWorkflow)
	m.report("Could not review agent", errNoWorkflow)

	focused := m.toggleMessagesFocus()
	dismissed := focused.dismissMessage()

	require.Len(t, dismissed.messages.items, 1)
	assert.Equal(t, "Could not archive agent", newestMessage(dismissed).summary)
	assert.True(t, dismissed.isMessagesFocused(), "the region keeps the keys while there is more to read")

	empty := dismissed.dismissMessage()

	assert.Empty(t, empty.messages.items)
	assert.False(t, empty.isMessagesFocused(), "an empty region hands the keys back")
}

// TestMessagesFocusGoesDormantOffItsScreen covers the review screen, which draws no region, so a
// focus taken on the agents list does not follow the reader onto a screen without one.
func TestMessagesFocusGoesDormantOffItsScreen(t *testing.T) {
	m := detailsModel(&recordingTmux{})
	m.height, m.width = 40, 120
	m.pushMessage(message{
		kind:    messageError,
		summary: "Nothing to review in fix-ci",
		detail:  "no change",
	})

	focused := m.toggleMessagesFocus()
	require.True(t, focused.isMessagesFocused())

	// The search screen draws no record and so no message region either, which is what makes the
	// focus dormant rather than followed.
	focused.screen = screenSearch

	assert.False(t, focused.isMessagesFocused())
	assert.True(t, focused.isListFocused())
}

// TestMessagesTallySaysWhereTheKeysAre covers the region's own focus indicator: the tally is dim
// while the list has the keys and the accent once it does not.
func TestMessagesTallySaysWhereTheKeysAre(t *testing.T) {
	restore := lipgloss.ColorProfile()
	t.Cleanup(func() { lipgloss.SetColorProfile(restore) })
	lipgloss.SetColorProfile(termenv.TrueColor)

	m := detailsModel(&recordingTmux{})
	m.height, m.width = 40, 60
	m.pushMessage(message{
		kind:    messageError,
		summary: "Nothing to review in fix-ci",
		detail:  "no change",
	})

	blurred := m.messagesTally(0)
	assert.Empty(t, ansi.Strip(blurred), "one message on its own counts nothing while it is unread")

	focused := m.toggleMessagesFocus().messagesTally(0)

	assert.Contains(t, ansi.Strip(focused), "1 of 1")
	assert.Contains(t, focused, "\x1b[1;34m", "the accent says the region holds the keys")
}

// TestMessagesTallyCountsFromTheOldest covers which way the stack is read: the newest message is
// the one on screen and the last of them, so < walks back through time and counts down.
func TestMessagesTallyCountsFromTheOldest(t *testing.T) {
	m := detailsModel(&recordingTmux{})
	m.height, m.width = 30, 66
	m.pushMessage(message{
		kind:    messageInfo,
		summary: "First",
		detail:  "one",
	})
	m.pushMessage(message{
		kind:    messageInfo,
		summary: "Second",
		detail:  "two",
	})
	m.pushMessage(message{
		kind:    messageInfo,
		summary: "Newest",
		detail:  "three",
	})

	focused := m.toggleMessagesFocus()

	assert.Equal(t, "Newest", focused.messages.items[focused.messagesCursor()].summary, "the newest message is the one on screen")
	assert.Contains(t, ansi.Strip(focused.messagesTally(0)), "3 of 3", "and it is the last of them")

	back := send(t, focused, "<")
	assert.Equal(t, "Second", back.messages.items[back.messagesCursor()].summary, "< walks back through time")
	assert.Contains(t, ansi.Strip(back.messagesTally(0)), "2 of 3")

	oldest := send(t, back, "<")
	assert.Equal(t, "First", oldest.messages.items[oldest.messagesCursor()].summary)
	assert.Contains(t, ansi.Strip(oldest.messagesTally(0)), "1 of 3")

	forward := send(t, oldest, ">")
	assert.Equal(t, "Second", forward.messages.items[forward.messagesCursor()].summary, "and > comes back towards the newest")
}

// TestMessagesRegionExpandsWhenFocused covers the region growing to hold the whole message
// rather than sending the reader to a block beside the list for the rest of it.
func TestMessagesRegionExpandsWhenFocused(t *testing.T) {
	m := detailsModel(&recordingTmux{})
	m.height, m.width = 30, 66
	m.pushMessage(message{
		kind:    messageError,
		summary: "Cannot reach the server",
		detail:  "dial tcp 127.0.0.1:8787: connect: connection refused",
		hint:    "Start it with: weave serve",
		trace:   "7c1f2a94",
	})

	record, help := m.detailsView(), m.renderHelp(m.agentHelp())
	blurred, ok := m.messagesFit(record, help)
	require.True(t, ok)
	assert.Equal(t, messagesRows, blurred, "the region keeps its three rows while the list has the keys")

	focused := m.toggleMessagesFocus()
	rows, ok := focused.messagesFit(record, help)
	require.True(t, ok)
	assert.Greater(t, rows, messagesRows, "and takes the rows the message needs once it has them")

	region := ansi.Strip(focused.messagesRegion(record, help))
	assert.Contains(t, region, "1 of 1", "the tally is on the summary row")
	assert.Contains(t, region, "connect: connection refused")
	assert.Contains(t, region, "Start it with: weave serve")
	assert.Contains(t, region, "weave trace 7c1f2a94")
	assert.Contains(t, region, "error ·", "the footer says what kind of message this is")

	assert.Contains(t, ansi.Strip(focused.View()), "Agents and Workflows", "the list is still above it")
}

// TestMessagesRegionLeavesTheReasonPlain covers the reason being the one part of an expanded
// message drawn in the plain foreground, which is what makes it the part read first.
func TestMessagesRegionLeavesTheReasonPlain(t *testing.T) {
	restore := lipgloss.ColorProfile()
	t.Cleanup(func() { lipgloss.SetColorProfile(restore) })
	lipgloss.SetColorProfile(termenv.TrueColor)

	m := detailsModel(&recordingTmux{})
	m.height, m.width = 30, 66
	m.pushMessage(message{
		kind:    messageError,
		summary: "Could not open a review round",
		detail:  "git rev-parse --show-toplevel: exit status 128",
		hint:    "The worktree is gone.",
	})

	for _, row := range m.messagesSlot() {
		if strings.Contains(ansi.Strip(row), "exit status 128") {
			assert.NotEqual(t, ansi.Strip(row), row, "the collapsed row stays dim")
		}
	}

	reason, hint := "", ""
	for _, row := range m.toggleMessagesFocus().messagesRender().rows {
		switch {
		case strings.Contains(ansi.Strip(row), "exit status 128"):
			reason = row
		case strings.Contains(ansi.Strip(row), "The worktree is gone."):
			hint = row
		}
	}

	require.NotEmpty(t, reason)
	require.NotEmpty(t, hint)

	assert.Equal(t, ansi.Strip(reason), reason, "the reason carries no colour of its own")
	assert.NotEqual(t, ansi.Strip(hint), hint, "what is said around it stays dim")
}

// TestMessagesRegionPinsTheSummaryWhileMoving covers a message longer than the rows the region
// can take, where the cursor moves the window the way the record's does and the summary keeps
// its row throughout.
func TestMessagesRegionPinsTheSummaryWhileMoving(t *testing.T) {
	m := detailsModel(&recordingTmux{})
	m.height, m.width = 20, 66
	m.pushMessage(message{
		kind:    messageError,
		summary: "Cannot reach the server",
		detail:  strings.Repeat("the reason it gave ran on and on. ", 12),
		trace:   "7c1f2a94",
	})

	focused := m.toggleMessagesFocus()
	record, help := focused.detailsView(), focused.renderHelp(focused.agentHelp())

	top := ansi.Strip(focused.messagesRegion(record, help))
	assert.Contains(t, top, "Cannot reach the server")
	assert.Contains(t, top, "more", "what is past the bottom is counted")

	moved := send(t, focused, "G")
	assert.Equal(t, len(moved.messagePieces())-1, moved.messagesPiece(), "the cursor is on the last piece")

	window := ansi.Strip(moved.messagesRegion(record, help))
	assert.Contains(t, window, "Cannot reach the server", "the summary keeps its row")
	assert.Contains(t, window, "↑", "and what is above the window is counted")
	assert.Contains(t, window, "error ·", "the piece the cursor moved to is on screen")
}

// TestMessagesPiecesAreCopied covers y putting the piece under the cursor on the clipboard, which
// is the record's own key doing the record's own thing.
func TestMessagesPiecesAreCopied(t *testing.T) {
	clip := &fakeClipboard{}

	m := detailsModel(&recordingTmux{})
	m.height, m.width = 30, 66
	m.clip = clip
	m.pushMessage(message{
		kind:    messageError,
		summary: "Could not open a review round",
		detail:  "git rev-parse --show-toplevel: exit status 128",
		hint:    "The worktree is gone.",
		trace:   "7c1f2a94",
	})

	focused := m.toggleMessagesFocus()

	want := []string{
		"Could not open a review round",
		"git rev-parse --show-toplevel: exit status 128",
		"The worktree is gone.",
		"7c1f2a94",
		dates.Friendly(m.messages.items[0].at),
	}

	for i, value := range want {
		copied, cmd := focused.updateMessages(keyMsg("y"))
		panel, ok := copied.(model)
		require.True(t, ok)
		require.NotNil(t, cmd)
		require.NoError(t, copyResultFrom(t, cmd).err)

		assert.Equal(t, value, clip.written[len(clip.written)-1], "the piece under the cursor is what y copies")
		assert.Equal(t, i+1, panel.messages.flashed, "and it flashes while the copy lands")

		focused = send(t, focused, "j")
	}
}

// TestMessagesRegionIsColouredByKind covers the expanded region drawing a message in the colour
// its row carries.
func TestMessagesRegionIsColouredByKind(t *testing.T) {
	tests := []struct {
		name string
		kind messageKind
		want string
	}{
		{
			name: "an error is the scheme's red",
			kind: messageError,
			want: "\x1b[91",
		},
		{
			name: "a warning is its yellow",
			kind: messageWarning,
			want: "\x1b[93",
		},
		{
			name: "a success is its green",
			kind: messageSuccess,
			want: "\x1b[92",
		},
		{
			name: "an info is its muted grey",
			kind: messageInfo,
			want: "\x1b[90",
		},
	}

	restore := lipgloss.ColorProfile()
	t.Cleanup(func() { lipgloss.SetColorProfile(restore) })
	lipgloss.SetColorProfile(termenv.TrueColor)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := detailsModel(&recordingTmux{})
			m.height, m.width = 40, 120
			m.pushMessage(message{
				kind:    tt.kind,
				summary: "Nothing to review in fix-ci",
				detail:  "no change against the base",
			})

			head := ""
			for _, row := range m.toggleMessagesFocus().messagesRender().rows {
				if strings.Contains(ansi.Strip(row), "Nothing to review in fix-ci") {
					head = row
				}
			}

			require.NotEmpty(t, head, "the region draws what happened")
			assert.Contains(t, head, tt.want)
		})
	}
}

// TestMessagesRefuseFocusWithNothingToRead covers the key being refused rather than opening a
// region whose block would say there is nothing in it.
func TestMessagesRefuseFocusWithNothingToRead(t *testing.T) {
	m := detailsModel(&recordingTmux{})
	m.height, m.width = 40, 60

	assert.False(t, m.toggleMessagesFocus().isMessagesFocused())
}

// TestMessagesPulseBlinksWhatJustLanded covers a message blinking five times as it lands and
// leaving the row as it found it.
func TestMessagesPulseBlinksWhatJustLanded(t *testing.T) {
	restore := lipgloss.ColorProfile()
	t.Cleanup(func() { lipgloss.SetColorProfile(restore) })
	lipgloss.SetColorProfile(termenv.TrueColor)

	lit, _, _ := strings.Cut(flashFieldStyle(paint.Grey).Render("x"), "x")
	require.NotEmpty(t, lit)

	m := detailsModel(&recordingTmux{})
	m.height, m.width = 40, 120
	m.pushMessage(message{
		kind:    messageError,
		summary: "Nothing to review in fix-ci",
		detail:  "no change against the base",
	})

	settled := m.messagesSlot()[0]
	assert.NotContains(t, settled, lit)

	m, cmd := m.pulseMessages(m.messages.landed - 1)
	require.NotNil(t, cmd)

	blinks := 0
	for {
		if strings.Contains(m.messagesSlot()[0], lit) {
			blinks++
		}

		if m.advancePulses() == nil {
			break
		}
	}

	assert.Equal(t, messagesPulseBlinks, blinks)
	assert.Equal(t, settled, m.messagesSlot()[0])
}

// TestMessagesPulseHoldsStillOnAnOlderMessage covers a message landing while the reader is back
// in the stack, where the blink would draw the eye to the wrong summary.
func TestMessagesPulseHoldsStillOnAnOlderMessage(t *testing.T) {
	m := detailsModel(&recordingTmux{})
	m.height, m.width = 40, 120
	m.pushMessage(message{summary: "first", detail: "one"})
	m.pushMessage(message{summary: "second", detail: "two"})

	m, _ = m.pulseMessages(m.messages.landed - 1)
	require.True(t, m.messagesPulseOn())

	m.messages.cursor = 1
	assert.False(t, m.messagesPulseOn())
}

// TestChromeKeepsEveryRowInsideThePane covers every block of chrome the panel draws staying
// inside the pane it is drawn in.
func TestChromeKeepsEveryRowInsideThePane(t *testing.T) {
	const path = "/Users/harrisoncramer/.local/state/weave/worktrees/01a086ae-d59b-769b-a8a0-202d4014bddd/bin/editor"

	// 61 is a width where the wrap misplaces a breakpoint in this path and hands back a row two
	// columns over the column it was given.
	for _, width := range []int{20, 30, 45, 61, 80} {
		m := detailsModel(&recordingTmux{})
		m.height, m.width = 40, width
		m.messages = oneMessage("Your editor could not be opened on that message", "fork/exec "+path+": no such file or directory")
		m.messages.focused = true

		view := m.View()
		for i, row := range strings.Split(view, "\n") {
			assert.LessOrEqual(t, ansi.StringWidth(row), width, "message row %d at width %d: %q", i, width, ansi.Strip(row))
		}

		assert.Equal(t, m.height, strings.Count(view, "\n")+1, "frame rows at width %d", width)

		note := m.reviewNote(dimStyle, "editing this round replaces the edit of an earlier one in "+path+", so press again to go on")
		for i, row := range strings.Split(strings.TrimRight(note, "\n"), "\n") {
			assert.LessOrEqual(t, ansi.StringWidth(row), m.paneWidth(), "review note row %d at width %d: %q", i, width, ansi.Strip(row))
		}

		for i, row := range m.noticeLines(notice{icon: "✗", text: "nothing was written to " + path}, 0) {
			assert.LessOrEqual(t, ansi.StringWidth(row), m.paneWidth(), "notice row %d at width %d: %q", i, width, ansi.Strip(row))
		}

		// A worktree path is nothing but dashes, and a wrap that breaks at one has to keep every
		// column it was handed: a path the reader copies out and cannot find is the worse failure.
		joined := strings.Join(paint.WrapRows("a "+path, width), "")
		assert.Contains(t, strings.ReplaceAll(joined, " ", ""), path, "path survives the wrap at width %d", width)
	}
}

// TestReviewNoteWrapsBeforeTheFirstResize covers the note drawn before the panel has been told how
// wide it is, which reaches the wrap as a column of nothing and would come out one letter a row.
func TestReviewNoteWrapsBeforeTheFirstResize(t *testing.T) {
	m := detailsModel(&recordingTmux{})
	m.height, m.width = 0, 0

	note := strings.TrimRight(m.reviewNote(dimStyle, "editing this round replaces the edit of an earlier one, so press again to go on"), "\n")

	assert.Less(t, strings.Count(note, "\n")+1, 10, "rows before the first resize: %q", ansi.Strip(note))
}
