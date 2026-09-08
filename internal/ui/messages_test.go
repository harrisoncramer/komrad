package ui

import (
	"strings"
	"testing"

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
// focus taken on the agents list does not follow the reader onto it.
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

	focused.screen = screenReview

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

	blurred := m.messagesTally()
	assert.Empty(t, ansi.Strip(blurred), "one message on its own counts nothing while it is unread")

	focused := m.toggleMessagesFocus().messagesTally()

	assert.Contains(t, ansi.Strip(focused), "1 of 1")
	assert.Contains(t, focused, "\x1b[1;34m", "the accent says the region holds the keys")
}

// TestMessagesDocIsColouredByKind covers the block beside the list drawing a message in the
// colour its row carries.
func TestMessagesDocIsColouredByKind(t *testing.T) {
	restore := lipgloss.ColorProfile()
	t.Cleanup(func() { lipgloss.SetColorProfile(restore) })
	lipgloss.SetColorProfile(termenv.TrueColor)

	tests := []struct {
		name string
		kind messageKind
		want string
	}{
		{name: "an error is the scheme's red", kind: messageError, want: "\x1b[91"},
		{name: "a warning is its yellow", kind: messageWarning, want: "\x1b[93"},
		{name: "a success is its green", kind: messageSuccess, want: "\x1b[92"},
		{name: "an info is its muted grey", kind: messageInfo, want: "\x1b[90"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := detailsModel(&recordingTmux{})
			m.height, m.width = 40, 120
			m.pushMessage(message{kind: tt.kind, summary: "Nothing to review in fix-ci", detail: "no change against the base"})

			focused := m.toggleMessagesFocus()
			split, ok := focused.columnSplit()
			require.True(t, ok)

			head := ""
			for _, row := range focused.messagesBlock(split).rows {
				if strings.Contains(ansi.Strip(row), "Nothing to review in fix-ci") {
					head = row
				}
			}

			require.NotEmpty(t, head, "the block draws what happened")
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
