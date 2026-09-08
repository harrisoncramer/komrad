package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"weave/config"
	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"
	"weave/internal/ui/layout"
	agentv1 "weave/proto/gen/agent/v1"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMailRows covers the shape of the list: threads newest first, the newest message first
// inside each, and a line naming who each thread is with.
func TestMailRows(t *testing.T) {
	tests := []struct {
		name     string
		inbox    []*agentv1.Mail
		outbox   []*agentv1.Mail
		filter   string
		wantRows []string
	}{
		{
			name:     "an empty mailbox draws nothing",
			wantRows: []string{},
		},
		{
			name:   "a thread holds its messages, newest first, under a line naming it",
			inbox:  []*agentv1.Mail{received("m1", "neovim-fan", "vim wins")},
			outbox: []*agentv1.Mail{replyToFirst("m2")},
			wantRows: []string{
				"# emacs-fan",
				"→ emacs wins",
				"← vim wins",
			},
		},
		{
			name: "the thread something last happened in stands at the top",
			inbox: []*agentv1.Mail{
				received("m1", "neovim-fan", "vim wins"),
				received("m3", "weave", "review waiting"),
			},
			outbox: []*agentv1.Mail{replyToFirst("m2")},
			wantRows: []string{
				"# weave",
				"← review waiting",
				"# emacs-fan",
				"→ emacs wins",
				"← vim wins",
			},
		},
		{
			name: "a reply pulls its own thread back to the top over a quieter one",
			inbox: []*agentv1.Mail{
				received("m1", "neovim-fan", "vim wins"),
				received("m2", "weave", "review waiting"),
			},
			outbox: []*agentv1.Mail{replyToFirst("m3")},
			wantRows: []string{
				"# emacs-fan",
				"→ emacs wins",
				"← vim wins",
				"# weave",
				"← review waiting",
			},
		},
		{
			name: "a thread narrowed away takes its line with it",
			inbox: []*agentv1.Mail{
				received("m1", "neovim-fan", "vim wins"),
				received("m2", "weave", "review waiting"),
			},
			filter: "vim",
			wantRows: []string{
				"# neovim-fan",
				"← vim wins",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := mailModel(config.DefaultKeybindings())
			m.mail.inbox, m.mail.outbox = tt.inbox, tt.outbox
			m.filter.setQuery(tt.filter)

			got := make([]string, 0, len(m.mailRows()))
			for _, r := range m.mailRows() {
				if r.header {
					got = append(got, "# "+r.label)

					continue
				}

				got = append(got, mailDirectionMark(r.sent)+" "+r.item.GetBody())
			}

			assert.Equal(t, tt.wantRows, got)
		})
	}
}

// TestMailCursorStepsOverThreadLines covers the cursor, which moves through messages however
// much furniture stands between them, since a thread line answers for no record and no stage.
func TestMailCursorStepsOverThreadLines(t *testing.T) {
	keys := config.DefaultKeybindings()

	tests := []struct {
		name       string
		key        string
		cursor     int
		wantCursor int
	}{
		{
			name:       "down out of a thread lands on the next thread's newest",
			key:        "j",
			cursor:     1,
			wantCursor: 3,
		},
		{
			name:       "up back into it lands on the thread above",
			key:        "k",
			cursor:     3,
			wantCursor: 1,
		},
		{
			name:       "bottom lands on the last message rather than a line",
			key:        "G",
			cursor:     1,
			wantCursor: 4,
		},
		{
			name:       "down at the last message stays put",
			key:        "j",
			cursor:     4,
			wantCursor: 4,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := mailModel(keys)
			m.mail.inbox = []*agentv1.Mail{
				received("m1", "neovim-fan", "vim wins"),
				received("m3", "weave", "review waiting"),
			}
			m.mail.outbox = []*agentv1.Mail{replyToFirst("m2")}
			m.mail.cursor = tt.cursor

			updated, _ := m.updateMail(keyMsg(tt.key))
			got, ok := updated.(model)
			require.True(t, ok)

			assert.Equal(t, tt.wantCursor, got.mail.cursor)
			held, on := got.hoveredMail()
			require.True(t, on, "the cursor is never left on a thread line")
			assert.False(t, held.header)
		})
	}
}

// TestMailThreadWord covers what the record says a thread is standing on, since a last word
// that went out and was never taken is the stalled handoff this screen is opened to find.
func TestMailThreadWord(t *testing.T) {
	tests := []struct {
		name   string
		inbox  []*agentv1.Mail
		outbox []*agentv1.Mail
		want   string
	}{
		{
			name:   "an unread reply of ours is waiting on the other agent",
			inbox:  []*agentv1.Mail{received("m1", "neovim-fan", "vim wins")},
			outbox: []*agentv1.Mail{replyToFirst("m2")},
			want:   "2 messages · waiting on emacs-fan",
		},
		{
			name:   "a reply they took reads as read",
			inbox:  []*agentv1.Mail{received("m1", "neovim-fan", "vim wins")},
			outbox: []*agentv1.Mail{claimed(replyToFirst("m2"))},
			want:   "2 messages · read your last",
		},
		{
			name:   "a reply of ours nobody took in time says so",
			inbox:  []*agentv1.Mail{received("m1", "neovim-fan", "vim wins")},
			outbox: []*agentv1.Mail{stale(replyToFirst("m2"))},
			want:   "2 messages · your last expired unread",
		},
		{
			name:  "a message of theirs nobody answered is on us",
			inbox: []*agentv1.Mail{received("m1", "neovim-fan", "vim wins")},
			want:  "1 message · they spoke last",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := mailModel(config.DefaultKeybindings())
			m.mail.inbox, m.mail.outbox = tt.inbox, tt.outbox

			assert.Equal(t, tt.want, m.mailThreadWord(m.mailThreadOf("m1")))
		})
	}
}

// TestMailRowClock covers the stamp on a row, which says the time for something that arrived
// today and the day for anything older, since a bare clock on old mail reads as though it had
// just come in.
func TestMailRowClock(t *testing.T) {
	old := time.Now().AddDate(0, 0, -3)

	m := mailModel(config.DefaultKeybindings())
	m.height, m.width = 40, 90
	// The older message takes the lower id, since the ids are uuidv7 in the real thing and
	// carry the same order the stamps do.
	m.mail.inbox = []*agentv1.Mail{
		agedAt(received("m1", "emacs-fan", "org-mode wins"), old),
		received("m2", "neovim-fan", "vim wins"),
	}

	rows := m.mailRows()

	today, err := time.Parse(time.RFC3339, rows[1].item.GetCreatedAt())
	require.NoError(t, err)

	assert.Equal(t, today.Local().Format("3:04pm"), mailClock(rows[1].item.GetCreatedAt()))
	assert.Equal(t, old.Local().Format("Jan 2 3:04pm"), mailClock(rows[3].item.GetCreatedAt()))
	assert.Contains(t, ansi.Strip(m.mailRowView(rows[3], mailClockWidth(rows), false)), old.Local().Format("Jan 2 3:04pm"))
}

// TestMailKeys covers what each key does on the mailbox, since every one of them but back
// moves the cursor rather than writing anything.
func TestMailKeys(t *testing.T) {
	keys := config.DefaultKeybindings()

	tests := []struct {
		name       string
		key        string
		cursor     int
		wantScreen screen
		wantCursor int
	}{
		{
			name:       "back returns to the agents tree",
			key:        "esc",
			cursor:     1,
			wantScreen: screenAgents,
		},
		{
			name:       "down moves onto the next message of the same thread",
			key:        "j",
			cursor:     1,
			wantScreen: screenMail,
			wantCursor: 2,
		},
		{
			name:       "bottom jumps to the last row, which is the quietest thread",
			key:        "G",
			cursor:     1,
			wantScreen: screenMail,
			wantCursor: 4,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := mailModel(keys)
			m.mail.inbox = []*agentv1.Mail{
				received("m1", "neovim-fan", "vim wins"),
				received("m2", "weave", "review waiting"),
			}
			m.mail.outbox = []*agentv1.Mail{replyToFirst("m3")}
			m.mail.cursor = tt.cursor

			updated, _ := m.updateMail(keyMsg(tt.key))
			got, ok := updated.(model)
			require.True(t, ok)

			assert.Equal(t, tt.wantScreen, got.screen)
			if tt.wantScreen == screenMail {
				assert.Equal(t, tt.wantCursor, got.mail.cursor)
			}
		})
	}
}

// TestMailState covers the three things that become of a piece of mail, since which one it is
// is the fact a mailbox is usually opened to settle.
func TestMailState(t *testing.T) {
	tests := []struct {
		name string
		item *agentv1.Mail
		want string
	}{
		{
			name: "mail the recipient took reads as read",
			item: claimed(received("m1", "neovim-fan", "vim wins")),
			want: "read",
		},
		{
			name: "mail still inside its deadline reads as unread",
			item: received("m1", "neovim-fan", "vim wins"),
			want: "unread",
		},
		{
			name: "mail nobody took before the deadline reads as expired",
			item: stale(received("m1", "neovim-fan", "vim wins")),
			want: "expired unread",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, mailStateWord(tt.item))
		})
	}
}

// TestMailDetailsNameTheCorrespondents covers the record under the list, which is where the
// ids are, since the rows have room for a name alone.
func TestMailDetailsNameTheCorrespondents(t *testing.T) {
	m := mailModel(config.DefaultKeybindings())
	m.mail.outbox = []*agentv1.Mail{sent("m1", "emacs wins")}
	m.mail.cursor = 1

	held := m.mailDetails()

	assert.Equal(t, "m1", held.id)
	assert.Equal(t, "to emacs-fan", held.title)
	assert.Contains(t, fieldValue(t, held, "to"), "emacs-fan (a2)")
	assert.Equal(t, "plain", fieldValue(t, held, "kind"))
	assert.Equal(t, "unread", fieldValue(t, held, "state"))
}

// TestMailViewDrawsThreads covers the sidebar as a reader sees it: a line naming each thread,
// its messages under it, and no trace of the inbox and outbox the mailbox used to be split into.
func TestMailViewDrawsThreads(t *testing.T) {
	m := mailModel(config.DefaultKeybindings())
	m.height, m.width = 40, 70
	m.mail.inbox = []*agentv1.Mail{received("m1", "neovim-fan", "vim wins")}
	m.mail.outbox = []*agentv1.Mail{replyToFirst("m2")}
	m.mail.cursor = 1

	drawn := ansi.Strip(m.mailView())

	assert.Contains(t, drawn, "Mail · reviewer")
	assert.Contains(t, drawn, "2 messages · 2 waiting")
	assert.Contains(t, drawn, "emacs-fan")
	assert.Contains(t, drawn, "→ ")
	assert.Contains(t, drawn, "← ")
	assert.Contains(t, drawn, "emacs wins")
	assert.Contains(t, drawn, "vim wins")
	assert.NotContains(t, drawn, "Inbox")
	assert.NotContains(t, drawn, "Outbox")
}

// TestMailRowsFitTheSidebar covers a pane too narrow for a snippet, where a row that wrapped
// would take a line the list has not counted and push the rest of the mailbox off the bottom.
func TestMailRowsFitTheSidebar(t *testing.T) {
	tests := []struct {
		name        string
		width       int
		wantSnippet bool
	}{
		{
			name:        "a wide sidebar carries the opening of the message",
			width:       80,
			wantSnippet: true,
		},
		{
			name:        "a narrow one drops it rather than wrapping",
			width:       20,
			wantSnippet: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := mailModel(config.DefaultKeybindings())
			m.height, m.width = 40, tt.width
			m.mail.inbox = []*agentv1.Mail{received("m1", "neovim-fan", "vim wins")}

			rows := m.mailRows()
			drawn := ansi.Strip(m.mailRowView(rows[1], mailClockWidth(rows), false))

			assert.Equal(t, tt.wantSnippet, strings.Contains(drawn, "vim wins"))
			assert.LessOrEqual(t, lipgloss.Width(drawn)+2, tt.width, "the row and the cursor before it fit the pane")
		})
	}
}

// TestMailRowsFillTheSidebar covers a message long enough to run to the edge, which is what
// holds the chrome count honest: a row that stops short is a column of snippet given away, and
// nothing else in the suite would notice.
func TestMailRowsFillTheSidebar(t *testing.T) {
	for _, width := range []int{40, 60, 80} {
		t.Run(fmt.Sprintf("a row fills a pane of %d", width), func(t *testing.T) {
			m := mailModel(config.DefaultKeybindings())
			m.height, m.width = 40, width
			m.mail.inbox = []*agentv1.Mail{received("m1", "neovim-fan", strings.Repeat("wide ", 40))}

			rows := m.mailRows()
			drawn := ansi.Strip(m.mailRowView(rows[1], mailClockWidth(rows), false))

			assert.Equal(t, width-mailCursorWidth, lipgloss.Width(drawn), "the row and the cursor before it are the pane")
		})
	}
}

// TestMailCountView covers the line under the title, which is what the screen is usually
// opened to read, and which counts the whole mailbox rather than what a filter left of it.
func TestMailCountView(t *testing.T) {
	m := mailModel(config.DefaultKeybindings())
	m.mail.inbox = []*agentv1.Mail{
		received("m1", "neovim-fan", "vim wins"),
		stale(received("m2", "emacs-fan", "org-mode wins")),
		claimed(received("m3", "weave", "review waiting")),
	}

	assert.Equal(t, "3 messages · 1 waiting · 1 expired", m.mailCountView())

	m.filter.setQuery("vim")
	assert.Equal(t, "3 messages · 1 waiting · 1 expired", m.mailCountView(), "a pattern narrows the rows, not the mailbox")
}

// TestMailStage covers what the stage draws, which is the one message under the cursor: the
// rows around it already carry the rest of its thread.
func TestMailStage(t *testing.T) {
	m := mailModel(config.DefaultKeybindings())
	m.mail.inbox = []*agentv1.Mail{
		received("m1", "neovim-fan", "vim wins"),
		received("m3", "weave", "review waiting"),
	}
	m.mail.outbox = []*agentv1.Mail{replyToFirst("m2")}
	m.mail.cursor = 3

	drawn := m.mailMarkdown(m.mailRows()[3])

	assert.Contains(t, drawn, "# to emacs-fan")
	assert.Contains(t, drawn, "emacs wins", "the message the cursor is on")
	assert.NotContains(t, drawn, "vim wins", "the message it answers stays in the rows")
	assert.NotContains(t, drawn, "review waiting")
	assert.NotContains(t, drawn, "›", "nothing to mark, since there is only one message")
}

// TestMailLoadedKeepsTheCursorOnItsMail covers a poll landing under the reader, where mail
// arriving above the cursor would otherwise slide the message being read out from under it.
func TestMailLoadedKeepsTheCursorOnItsMail(t *testing.T) {
	m := mailModel(config.DefaultKeybindings())
	m.mail.inbox = []*agentv1.Mail{received("m1", "neovim-fan", "vim wins")}
	m.mail.cursor = 1

	m.applyMailLoaded(mailLoadedMsg{
		agentID: m.mail.agentID,
		inbox: []*agentv1.Mail{
			received("m2", "weave", "review waiting"),
			received("m1", "neovim-fan", "vim wins"),
		},
	})

	require.Len(t, m.mailRows(), 4)
	held, ok := m.hoveredMail()
	require.True(t, ok)
	assert.Equal(t, "m1", held.item.GetId())
}

// TestMailOpensOnTheNewestMessage covers where the cursor sits when a mailbox opens, which is
// the newest message of the thread something last happened in, since that is what the ordering
// puts at the top and the cursor steps over the line above it.
func TestMailOpensOnTheNewestMessage(t *testing.T) {
	arrivals := []*agentv1.Mail{
		received("m1", "neovim-fan", "vim wins"),
		received("m2", "emacs-fan", "org-mode wins"),
		received("m3", "weave", "review waiting"),
	}

	t.Run("a first read opens on it", func(t *testing.T) {
		m := mailModel(config.DefaultKeybindings())

		m.applyMailLoaded(mailLoadedMsg{agentID: "a1", inbox: arrivals})

		held, ok := m.hoveredMail()
		require.True(t, ok)
		assert.Equal(t, "m3", held.item.GetId())
	})

	t.Run("a mailbox that was quiet at the time opens on what arrives next", func(t *testing.T) {
		m := mailModel(config.DefaultKeybindings())

		m.applyMailLoaded(mailLoadedMsg{agentID: "a1"})
		m.applyMailLoaded(mailLoadedMsg{agentID: "a1", inbox: arrivals})

		held, ok := m.hoveredMail()
		require.True(t, ok)
		assert.Equal(t, "m3", held.item.GetId())
	})
}

// TestMailLoadedAfterLeavingIsDropped covers a read still in flight when the reader pressed
// back, which would otherwise paint a message over the agents list's own stage.
func TestMailLoadedAfterLeavingIsDropped(t *testing.T) {
	m := mailModel(config.DefaultKeybindings())
	m.mail.inbox = []*agentv1.Mail{received("m1", "neovim-fan", "vim wins")}

	left, _ := m.updateMail(keyMsg("esc"))
	gone, ok := left.(model)
	require.True(t, ok)
	require.Equal(t, screenAgents, gone.screen)

	updated, _, handled := gone.routeAsyncMessage(mailLoadedMsg{
		agentID: "a1",
		inbox:   []*agentv1.Mail{received("m2", "weave", "review waiting")},
	})
	require.True(t, handled)

	after, ok := updated.(model)
	require.True(t, ok)
	assert.Equal(t, screenAgents, after.screen)
	assert.Empty(t, after.mail.inbox, "the mailbox went with the screen")
}

// TestMailLoadedIgnoresAnotherAgentsMailbox covers a read in flight when the reader left, which
// would otherwise fill this mailbox with somebody else's mail.
func TestMailLoadedIgnoresAnotherAgentsMailbox(t *testing.T) {
	m := mailModel(config.DefaultKeybindings())

	m.applyMailLoaded(mailLoadedMsg{
		agentID: "a9",
		inbox:   []*agentv1.Mail{received("m1", "neovim-fan", "vim wins")},
	})

	assert.Empty(t, m.mail.inbox)
}

// TestMailScrollSurvivesARefresh covers a reader partway down a long message when the panel's
// own tick lands, which placed the cursor of every list it read and took the block back to its
// top under them.
func TestMailScrollSurvivesARefresh(t *testing.T) {
	m := mailModel(config.DefaultKeybindings())
	m.width, m.height = 120, 40

	inbox := []*agentv1.Mail{received("m1", "neovim-fan", strings.Repeat("vim wins. ", 200))}
	m.applyMailLoaded(mailLoadedMsg{agentID: "a1", inbox: inbox})
	m.column.top = 5

	m.applyRefresh(refreshResult{})
	assert.Equal(t, 5, m.column.top, "the agents listing moved the mailbox's own offset")

	m.applyMailLoaded(mailLoadedMsg{agentID: "a1", inbox: inbox})
	assert.Equal(t, 5, m.column.top, "reading the mailbox again moved the offset")
}

func mailModel(keys config.Keybindings) model {
	tmux := &recordingTmux{}

	return model{
		api: api{
			agents:    emptyLists{},
			workflows: emptyLists{},
			schedules: emptyLists{},
		},
		tmux:   tmux,
		popup:  NewPopupWindow(tmux, "test-session"),
		frame:  layout.New(tmux, config.DefaultLayout()),
		keys:   keys,
		screen: screenMail,
		listing: listing{
			agents: []agentItem{
				{agent: agentstore.Agent{
					ID:         "a1",
					Name:       "reviewer",
					WorkflowID: "w1",
				}},
				{agent: agentstore.Agent{
					ID:         "a2",
					Name:       "emacs-fan",
					WorkflowID: "w1",
				}},
			},
			workflows: []workflowstore.Workflow{{ID: "w1", Name: "backend"}},
		},
		mail: mailState{agentID: "a1", agentName: "reviewer"},
	}
}

func received(id, from, body string) *agentv1.Mail {
	sender := "a" + id

	return &agentv1.Mail{
		Id:          id,
		FromAgentId: &sender,
		FromName:    from,
		ToAgentId:   "a1",
		Body:        body,
		Kind:        agentv1.MailKind_MAIL_KIND_PLAIN,
		CreatedAt:   time.Now().Add(-time.Minute).Format(time.RFC3339),
		ExpiresAt:   time.Now().Add(time.Hour).Format(time.RFC3339),
	}
}

func sent(id, body string) *agentv1.Mail {
	sender := "a1"

	return &agentv1.Mail{
		Id:          id,
		FromAgentId: &sender,
		FromName:    "reviewer",
		ToAgentId:   "a2",
		Body:        body,
		Kind:        agentv1.MailKind_MAIL_KIND_PLAIN,
		CreatedAt:   time.Now().Add(-2 * time.Minute).Format(time.RFC3339),
		ExpiresAt:   time.Now().Add(time.Hour).Format(time.RFC3339),
	}
}

// replyToFirst is a message of this agent's own, carrying the id it is given, sent in answer to
// m1, which is what every thread fixture in this file is built out of.
func replyToFirst(id string) *agentv1.Mail {
	parent := "m1"

	item := sent(id, "emacs wins")
	item.ReplyTo = &parent

	return item
}

func agedAt(item *agentv1.Mail, at time.Time) *agentv1.Mail {
	item.CreatedAt = at.Format(time.RFC3339)

	return item
}

func claimed(item *agentv1.Mail) *agentv1.Mail {
	at := time.Now().Add(-30 * time.Second).Format(time.RFC3339)
	item.ClaimedAt = &at

	return item
}

func stale(item *agentv1.Mail) *agentv1.Mail {
	item.ExpiresAt = time.Now().Add(-time.Minute).Format(time.RFC3339)

	return item
}
