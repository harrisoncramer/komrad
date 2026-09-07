package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

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
	return messagesState{items: []message{{kind: messageError, summary: summary, detail: detail}}}
}

// TestMessagesRegionKeepsItsHeight covers the whole point of the region: the rows it takes are
// the same whether it is holding a failure or saying there is nothing to say.
func TestMessagesRegionKeepsItsHeight(t *testing.T) {
	m := detailsModel(&recordingTmux{})
	m.height, m.width = 40, 60

	record, help := m.detailsView(), m.renderHelp(m.agentHelp())

	idle := m.messagesRegion(record, help)
	require.NotEmpty(t, idle)
	assert.Contains(t, ansi.Strip(idle), "No messages")

	m.pushMessage(message{kind: messageError, summary: "Nothing to review in fix-ci", detail: "the worktree holds no change against the base"})
	filled := m.messagesRegion(record, help)

	assert.Equal(t, strings.Count(idle, "\n"), strings.Count(filled, "\n"))
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
	m.pushMessage(message{kind: messageError, summary: "Nothing to review in fix-ci", detail: "the worktree holds no change against the base", trace: "7c1f2a94"})

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
// focus taken on the agents list does not follow the reader onto it.
func TestMessagesFocusGoesDormantOffItsScreen(t *testing.T) {
	m := detailsModel(&recordingTmux{})
	m.height, m.width = 40, 120
	m.pushMessage(message{kind: messageError, summary: "Nothing to review in fix-ci", detail: "no change"})

	focused := m.toggleMessagesFocus()
	require.True(t, focused.isMessagesFocused())

	focused.screen = screenReview

	assert.False(t, focused.isMessagesFocused())
	assert.True(t, focused.isListFocused())
}

// TestMessagesTallySaysWhereTheKeysAre covers the region's own focus indicator, since it draws no
// title of its own: the tally is dim while the list has the keys and the accent once it does not.
func TestMessagesTallySaysWhereTheKeysAre(t *testing.T) {
	restore := lipgloss.ColorProfile()
	t.Cleanup(func() { lipgloss.SetColorProfile(restore) })
	lipgloss.SetColorProfile(termenv.TrueColor)

	m := detailsModel(&recordingTmux{})
	m.height, m.width = 40, 60
	m.pushMessage(message{kind: messageError, summary: "Nothing to review in fix-ci", detail: "no change"})

	assert.Empty(t, m.messagesTally(), "one message on its own counts nothing while it is unread")

	focused := m.toggleMessagesFocus().messagesSlot()[0]

	assert.Contains(t, ansi.Strip(focused), "1 of 1")
	assert.Contains(t, focused, "\x1b[1;34m", "the accent says the region holds the keys")
}

// TestMessagesRefuseFocusWithNothingToRead covers the key being refused rather than opening a
// region whose block would say there is nothing in it.
func TestMessagesRefuseFocusWithNothingToRead(t *testing.T) {
	m := detailsModel(&recordingTmux{})
	m.height, m.width = 40, 60

	assert.False(t, m.toggleMessagesFocus().isMessagesFocused())
}

// TestMessagesRegionStaysInsideThePane covers the region's own width: no row it draws is ever
// wider than the pane, whatever the tally has grown to and wherever the cursor is standing, since
// a row a single column over wraps and the region stops being the three rows it promises.
func TestMessagesRegionStaysInsideThePane(t *testing.T) {
	summaries := []string{"Could not copy to your clipboard", "x", strings.Repeat("a long reason ", 12)}

	for _, width := range []int{4, 8, 12, 20, 30, 40, 60, 80, 120} {
		for _, focused := range []bool{false, true} {
			for _, count := range []int{0, 1, 2, 12, messagesCap} {
				m := detailsModel(&recordingTmux{})
				m.height, m.width = 40, width
				for i := range count {
					m.report(summaries[i%len(summaries)], errNoWorkflow)
				}
				m.messages.focused = focused

				for cursor := range max(count, 1) {
					m.messages.cursor = cursor
					region := m.messagesRegion(m.detailsView(), m.renderHelp(m.agentHelp()))

					for _, line := range strings.Split(region, "\n") {
						assert.LessOrEqual(t, ansi.StringWidth(line), m.lineWidth(),
							"width %d, focused %v, %d messages, cursor %d: %q", width, focused, count, cursor, ansi.Strip(line))
					}
				}
			}
		}
	}
}

// TestMessagesDropTheKeysWhenThePaneShrinks covers the region losing its rows under a reader who
// is holding them: a pane too short to draw the region hands the keys back to the list, since a
// list whose arrows have stopped moving with no message on screen says nothing about why.
func TestMessagesDropTheKeysWhenThePaneShrinks(t *testing.T) {
	m := detailsModel(&recordingTmux{})
	m.height, m.width = 40, 60
	m.pushMessage(message{kind: messageError, summary: "Could not archive agent", detail: "every agent belongs to a workflow"})

	reading := m.toggleMessagesFocus()
	require.True(t, reading.isMessagesFocused())

	small := reading.resizeTo(tea.WindowSizeMsg{Width: 60, Height: 12})
	require.Empty(t, small.messagesRegion(small.detailsView(), small.renderHelp(small.agentHelp())))

	assert.False(t, small.isMessagesFocused(), "the region cannot hold the keys off a pane it is not drawn on")
	assert.True(t, small.isListFocused(), "the list has the keys back")

	// The reader asked for the region once, on a pane that could draw it, and asking again is
	// theirs to do rather than the panel's to do for them.
	grown := small.resizeTo(tea.WindowSizeMsg{Width: 60, Height: 40})
	assert.False(t, grown.isMessagesFocused())
}

// TestMessagesRefuseFocusOnAShortPane covers the key pressed on a pane that was already too short
// for the region, which is the same dead end reached without a resize.
func TestMessagesRefuseFocusOnAShortPane(t *testing.T) {
	m := detailsModel(&recordingTmux{})
	m.height, m.width = 12, 60
	m.pushMessage(message{kind: messageError, summary: "Could not archive agent", detail: "every agent belongs to a workflow"})
	require.Empty(t, m.messagesRegion(m.detailsView(), m.renderHelp(m.agentHelp())))

	assert.False(t, m.toggleMessagesFocus().isMessagesFocused())
}
