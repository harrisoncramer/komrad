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

// TestMailRows covers the shape of the mailbox, where both sections keep their header however
// little is under them, since an empty section is what says the agent never wrote or was written to.
func TestMailRows(t *testing.T) {
	tests := []struct {
		name     string
		inbox    []*agentv1.Mail
		outbox   []*agentv1.Mail
		filter   string
		wantRows []string
	}{
		{
			name:     "an empty mailbox is two headers",
			wantRows: []string{"Inbox (0)", "Outbox (0)"},
		},
		{
			name:     "mail is drawn under the section it belongs to",
			inbox:    []*agentv1.Mail{received("m1", "neovim-fan", "vim wins")},
			outbox:   []*agentv1.Mail{sent("m2", "a2", "emacs wins")},
			wantRows: []string{"Inbox (1)", "neovim-fan", "Outbox (1)", "emacs-fan"},
		},
		{
			name:     "a filter narrows on the body as well as the correspondent",
			inbox:    []*agentv1.Mail{received("m1", "neovim-fan", "vim wins"), received("m3", "weave", "review waiting")},
			outbox:   []*agentv1.Mail{sent("m2", "a2", "emacs wins")},
			filter:   "wins",
			wantRows: []string{"Inbox (1/2)", "neovim-fan", "Outbox (1/1)", "emacs-fan"},
		},
		{
			name:     "a filter matching nothing leaves the headers standing",
			inbox:    []*agentv1.Mail{received("m1", "neovim-fan", "vim wins")},
			filter:   "kakoune",
			wantRows: []string{"Inbox (0/1)", "Outbox (0/0)"},
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
					got = append(got, r.section+" "+mailSectionCount(r, m.filter.isStanding()))

					continue
				}

				got = append(got, m.mailCounterparty(r))
			}

			assert.Equal(t, tt.wantRows, got)
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
			cursor:     1,
			wantScreen: screenAgents,
		},
		{
			name:       "down moves onto the next piece of mail",
			key:        "j",
			cursor:     1,
			wantScreen: screenMail,
			wantCursor: 2,
		},
		{
			name:       "bottom jumps to the last row",
			key:        "G",
			cursor:     0,
			wantScreen: screenMail,
			wantCursor: 4,
		},
		{
			name:       "thread jumps from a reply to the mail it answers",
			key:        "t",
			cursor:     4,
			wantScreen: screenMail,
			wantCursor: 1,
		},
		{
			name:       "thread jumps from a mail to the reply it drew",
			key:        "t",
			cursor:     1,
			wantScreen: screenMail,
			wantCursor: 4,
		},
		{
			name:       "thread on a mail nothing answers stays put",
			key:        "t",
			cursor:     2,
			wantScreen: screenMail,
			wantCursor: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := mailModel(keys)
			m.mail.inbox = []*agentv1.Mail{
				received("m1", "neovim-fan", "vim wins"),
				received("m2", "weave", "review waiting"),
			}
			m.mail.outbox = []*agentv1.Mail{replyTo("m3", "a2", "emacs wins", "m1")}
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
	m.mail.outbox = []*agentv1.Mail{sent("m1", "a2", "emacs wins")}
	m.mail.cursor = 2

	held := m.mailDetails()

	assert.Equal(t, "m1", held.id)
	assert.Equal(t, "to emacs-fan", held.title)
	assert.Contains(t, fieldValue(t, held, "to"), "emacs-fan (a2)")
	assert.Equal(t, "plain", fieldValue(t, held, "kind"))
	assert.Equal(t, "unread", fieldValue(t, held, "state"))
}

// TestMailViewDrawsBothSections covers the sidebar as a reader sees it, since the two sections
// and the cursor between them are the whole point of the screen.
func TestMailViewDrawsBothSections(t *testing.T) {
	m := mailModel(config.DefaultKeybindings())
	m.height, m.width = 40, 60
	m.mail.inbox = []*agentv1.Mail{received("m1", "neovim-fan", "vim wins")}
	m.mail.outbox = []*agentv1.Mail{sent("m2", "a2", "emacs wins")}
	m.mail.cursor = 1

	drawn := ansi.Strip(m.mailView())

	assert.Contains(t, drawn, "Mail · reviewer")
	assert.Contains(t, drawn, "Inbox (1)")
	assert.Contains(t, drawn, "Outbox (1)")
	assert.Contains(t, drawn, "neovim-fan")
	assert.Contains(t, drawn, "emacs-fan")
	assert.Contains(t, drawn, "vim wins")
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

			drawn := ansi.Strip(m.mailRowView(m.mailRows()[1], false))

			assert.Equal(t, tt.wantSnippet, strings.Contains(drawn, "vim wins"))
			assert.LessOrEqual(t, lipgloss.Width(drawn), tt.width-mailRowIndent)
		})
	}
}

// TestMailSectionStage covers what the stage draws while the cursor is on a section head,
// since a head is a row like any other and leaving the stage blank there reads as a bug.
func TestMailSectionStage(t *testing.T) {
	m := mailModel(config.DefaultKeybindings())
	m.mail.inbox = []*agentv1.Mail{
		received("m1", "neovim-fan", "vim wins"),
		stale(received("m2", "emacs-fan", "org-mode wins")),
	}

	rows := m.mailRows()
	inbox := m.mailSectionMarkdown(rows[0])
	outbox := m.mailSectionMarkdown(rows[3])

	assert.Contains(t, inbox, "2 held · 2 unread · 1 expired unread")
	assert.Contains(t, outbox, "written to nobody")
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

// TestMailLoadedAfterLeavingIsDropped covers a read still in flight when the reader pressed
// back, which would otherwise paint a message over the agents list's own stage.
func TestMailLoadedAfterLeavingIsDropped(t *testing.T) {
	m := mailModel(config.DefaultKeybindings())
	m.mail.inbox = []*agentv1.Mail{received("m1", "neovim-fan", "vim wins")}
	m.mail.cursor = 1

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

// TestMailSectionStageCountsThePastFilter covers the summary while a pattern stands, where
// counting the unread over the narrowed rows would say mail had been read that had not.
func TestMailSectionStageCountsThePastFilter(t *testing.T) {
	m := mailModel(config.DefaultKeybindings())
	m.mail.inbox = []*agentv1.Mail{
		received("m1", "neovim-fan", "vim wins"),
		received("m2", "emacs-fan", "org-mode rules"),
	}
	m.filter.setQuery("wins")

	assert.Contains(t, m.mailSectionMarkdown(m.mailRows()[0]), "2 held · 2 unread · 0 expired unread")
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

func sent(id, to, body string) *agentv1.Mail {
	sender := "a1"

	return &agentv1.Mail{
		Id:          id,
		FromAgentId: &sender,
		FromName:    "reviewer",
		ToAgentId:   to,
		Body:        body,
		Kind:        agentv1.MailKind_MAIL_KIND_PLAIN,
		Hops:        1,
		CreatedAt:   time.Now().Add(-2 * time.Minute).Format(time.RFC3339),
		ExpiresAt:   time.Now().Add(time.Hour).Format(time.RFC3339),
	}
}

func replyTo(id, to, body, parent string) *agentv1.Mail {
	item := sent(id, to, body)
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
