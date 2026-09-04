package ui

import (
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

// TestMailRows covers the order and the rail, since a conversation split by direction or drawn
// out of order is the thing this screen exists not to do.
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
			name:     "both halves are merged into one list in the order they happened",
			inbox:    []*agentv1.Mail{received("m1", "neovim-fan", "vim wins"), received("m3", "weave", "review waiting")},
			outbox:   []*agentv1.Mail{sent("m2", "emacs wins")},
			wantRows: []string{"← neovim-fan", "→ emacs-fan", "← weave"},
		},
		{
			name:     "a filter narrows on the body as well as the correspondent",
			inbox:    []*agentv1.Mail{received("m1", "neovim-fan", "vim wins"), received("m3", "weave", "review waiting")},
			outbox:   []*agentv1.Mail{sent("m2", "emacs wins")},
			filter:   "wins",
			wantRows: []string{"← neovim-fan", "→ emacs-fan"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := mailModel(config.DefaultKeybindings())
			m.mail.inbox, m.mail.outbox = tt.inbox, tt.outbox
			m.filter.setQuery(tt.filter)

			got := make([]string, 0, len(m.mailRows()))
			for _, r := range m.mailRows() {
				got = append(got, mailDirectionMark(r.sent)+" "+m.mailCounterparty(r))
			}

			assert.Equal(t, tt.wantRows, got)
		})
	}
}

// TestMailRails covers the gutter, which is the whole of how a thread is told from the messages
// that landed while it was running.
func TestMailRails(t *testing.T) {
	tests := []struct {
		name        string
		inbox       []*agentv1.Mail
		outbox      []*agentv1.Mail
		wantGutters []string
	}{
		{
			name:        "a message nothing answers gets no rail",
			inbox:       []*agentv1.Mail{received("m1", "neovim-fan", "vim wins")},
			wantGutters: []string{""},
		},
		{
			name:        "a reply opens the rail at the message it answers and closes it on itself",
			inbox:       []*agentv1.Mail{received("m1", "neovim-fan", "vim wins")},
			outbox:      []*agentv1.Mail{replyTo("m2", "emacs wins", "m1")},
			wantGutters: []string{"┌", "└"},
		},
		{
			name:  "the rail runs past whatever landed in the middle of the thread",
			inbox: []*agentv1.Mail{received("m1", "neovim-fan", "vim wins"), received("m3", "weave", "review waiting")},
			outbox: []*agentv1.Mail{
				replyTo("m2", "emacs wins", "m1"),
				replyTo("m4", "one more thing", "m2"),
			},
			wantGutters: []string{"┌", "├", "│", "└"},
		},
		{
			name: "a thread opening after another closed takes the lane it gave back",
			inbox: []*agentv1.Mail{
				received("m1", "neovim-fan", "vim wins"),
				received("m2", "emacs-fan", "org-mode wins"),
				received("m4", "planner", "what is the plan"),
			},
			outbox: []*agentv1.Mail{
				replyTo("m3", "answering the first", "m1"),
				replyTo("m5", "answering the planner", "m4"),
				replyTo("m6", "answering the second at last", "m2"),
			},
			wantGutters: []string{"┌ ", "│┌", "└│", "┌│", "└│", " └"},
		},
		{
			name: "two threads running at once take a lane each",
			inbox: []*agentv1.Mail{
				received("m1", "neovim-fan", "vim wins"),
				received("m2", "emacs-fan", "org-mode wins"),
			},
			outbox: []*agentv1.Mail{
				replyTo("m3", "answering the first", "m1"),
				replyTo("m4", "answering the second", "m2"),
			},
			wantGutters: []string{"┌ ", "│┌", "└│", " └"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := mailModel(config.DefaultKeybindings())
			m.mail.inbox, m.mail.outbox = tt.inbox, tt.outbox

			got := make([]string, 0, len(m.mailRows()))
			for _, r := range m.mailRows() {
				got = append(got, r.gutter)
			}

			assert.Equal(t, tt.wantGutters, got)
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
			outbox: []*agentv1.Mail{replyTo("m2", "emacs wins", "m1")},
			want:   "2 messages · waiting on emacs-fan",
		},
		{
			name:   "a reply they took reads as read",
			inbox:  []*agentv1.Mail{received("m1", "neovim-fan", "vim wins")},
			outbox: []*agentv1.Mail{claimed(replyTo("m2", "emacs wins", "m1"))},
			want:   "2 messages · read your last",
		},
		{
			name:   "a reply of ours nobody took in time says so",
			inbox:  []*agentv1.Mail{received("m1", "neovim-fan", "vim wins")},
			outbox: []*agentv1.Mail{stale(replyTo("m2", "emacs wins", "m1"))},
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
			wantScreen: screenAgents,
		},
		{
			name:       "down moves onto the next message",
			key:        "j",
			cursor:     0,
			wantScreen: screenMail,
			wantCursor: 1,
		},
		{
			name:       "bottom jumps to the newest message",
			key:        "G",
			cursor:     0,
			wantScreen: screenMail,
			wantCursor: 2,
		},
		{
			name:       "next in thread steps over the message that landed in between",
			key:        "t",
			cursor:     0,
			wantScreen: screenMail,
			wantCursor: 2,
		},
		{
			name:       "previous in thread steps back over it",
			key:        "T",
			cursor:     2,
			wantScreen: screenMail,
			wantCursor: 0,
		},
		{
			name:       "next in thread on a message nothing answers stays put",
			key:        "t",
			cursor:     1,
			wantScreen: screenMail,
			wantCursor: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := mailModel(keys)
			m.mail.inbox = []*agentv1.Mail{
				received("m1", "neovim-fan", "vim wins"),
				received("m2", "weave", "review waiting"),
			}
			m.mail.outbox = []*agentv1.Mail{replyTo("m3", "emacs wins", "m1")}
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
	m.mail.cursor = 0

	held := m.mailDetails()

	assert.Equal(t, "m1", held.id)
	assert.Equal(t, "to emacs-fan", held.title)
	assert.Contains(t, fieldValue(t, held, "to"), "emacs-fan (a2)")
	assert.Equal(t, "plain", fieldValue(t, held, "kind"))
	assert.Equal(t, "unread", fieldValue(t, held, "state"))
}

// TestMailViewDrawsOneConversation covers the sidebar as a reader sees it, where both halves
// of the correspondence stand in one list under a count of what is owed an answer.
func TestMailViewDrawsOneConversation(t *testing.T) {
	m := mailModel(config.DefaultKeybindings())
	m.height, m.width = 40, 70
	m.mail.inbox = []*agentv1.Mail{received("m1", "neovim-fan", "vim wins")}
	m.mail.outbox = []*agentv1.Mail{replyTo("m2", "emacs wins", "m1")}

	drawn := ansi.Strip(m.mailView())

	assert.Contains(t, drawn, "Mail · reviewer")
	assert.Contains(t, drawn, "2 messages · 2 waiting")
	assert.Contains(t, drawn, "← neovim-fan")
	assert.Contains(t, drawn, "→ emacs-fan")
	assert.Contains(t, drawn, "┌")
	assert.Contains(t, drawn, "└")
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
		{name: "a wide sidebar carries the opening of the message", width: 80, wantSnippet: true},
		{name: "a narrow one drops it rather than wrapping", width: 30, wantSnippet: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := mailModel(config.DefaultKeybindings())
			m.height, m.width = 40, tt.width
			m.mail.inbox = []*agentv1.Mail{received("m1", "neovim-fan", "vim wins")}

			drawn := ansi.Strip(m.mailRowView(m.mailRows()[0], false))

			assert.Equal(t, tt.wantSnippet, strings.Contains(drawn, "vim wins"))
			assert.LessOrEqual(t, lipgloss.Width(drawn)+2, tt.width, "the row and the cursor before it fit the pane")
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

// TestMailThreadStage covers the stage, which draws the whole conversation rather than the one
// message under the cursor, since a reply read alone says half of what happened.
func TestMailThreadStage(t *testing.T) {
	m := mailModel(config.DefaultKeybindings())
	m.mail.inbox = []*agentv1.Mail{
		received("m1", "neovim-fan", "vim wins"),
		received("m3", "weave", "review waiting"),
	}
	m.mail.outbox = []*agentv1.Mail{replyTo("m2", "emacs wins", "m1")}
	m.mail.cursor = 1

	drawn := m.mailThreadMarkdown(m.mailRows()[1])

	assert.Contains(t, drawn, "vim wins", "the message the reply answers")
	assert.Contains(t, drawn, "emacs wins", "the reply itself")
	assert.NotContains(t, drawn, "review waiting", "a message of another thread stays out")
	assert.Contains(t, drawn, "› **→ emacs-fan**", "the hovered message is marked")
}

// TestMailLoadedKeepsTheCursorOnItsMail covers a poll landing under the reader, where mail
// arriving above the cursor would otherwise slide the message being read out from under it.
func TestMailLoadedKeepsTheCursorOnItsMail(t *testing.T) {
	m := mailModel(config.DefaultKeybindings())
	m.mail.inbox = []*agentv1.Mail{received("m1", "neovim-fan", "vim wins")}
	m.mail.landed = true
	m.mail.cursor = 0

	m.applyMailLoaded(mailLoadedMsg{
		agentID: m.mail.agentID,
		inbox: []*agentv1.Mail{
			received("m2", "weave", "review waiting"),
			received("m1", "neovim-fan", "vim wins"),
		},
	})

	require.Len(t, m.mailRows(), 2)
	held, ok := m.hoveredMail()
	require.True(t, ok)
	assert.Equal(t, "m1", held.item.GetId())
}

// TestMailLandsOnTheNewestMessage covers where the cursor sits when a mailbox opens, including
// one that was quiet at the time, which must not spend its landing on nothing and leave the
// reader on the oldest arrival.
func TestMailLandsOnTheNewestMessage(t *testing.T) {
	arrivals := []*agentv1.Mail{
		received("m1", "neovim-fan", "vim wins"),
		received("m2", "emacs-fan", "org-mode wins"),
		received("m3", "weave", "review waiting"),
	}

	t.Run("a first read lands on the newest", func(t *testing.T) {
		m := mailModel(config.DefaultKeybindings())

		m.applyMailLoaded(mailLoadedMsg{agentID: "a1", inbox: arrivals})

		assert.Equal(t, 2, m.mail.cursor)
	})

	t.Run("an empty first read leaves the landing to the read that brings something back", func(t *testing.T) {
		m := mailModel(config.DefaultKeybindings())

		m.applyMailLoaded(mailLoadedMsg{agentID: "a1"})
		require.False(t, m.mail.landed, "nothing came back, so nothing was landed on")

		m.applyMailLoaded(mailLoadedMsg{agentID: "a1", inbox: arrivals})

		assert.Equal(t, 2, m.mail.cursor)
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

func mailModel(keys config.Keybindings) model {
	tmux := &recordingTmux{}

	return model{
		api:    api{agents: emptyLists{}, workflows: emptyLists{}, schedules: emptyLists{}},
		tmux:   tmux,
		popup:  NewPopupWindow(tmux, "test-session"),
		frame:  layout.New(tmux, config.DefaultLayout()),
		keys:   keys,
		screen: screenMail,
		listing: listing{
			agents: []agentItem{
				{agent: agentstore.Agent{ID: "a1", Name: "reviewer", WorkflowID: "w1"}},
				{agent: agentstore.Agent{ID: "a2", Name: "emacs-fan", WorkflowID: "w1"}},
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
		Hops:        1,
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
		Hops:        1,
		CreatedAt:   time.Now().Add(-2 * time.Minute).Format(time.RFC3339),
		ExpiresAt:   time.Now().Add(time.Hour).Format(time.RFC3339),
	}
}

func replyTo(id, body, parent string) *agentv1.Mail {
	item := sent(id, body)
	item.ReplyTo = &parent

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
