package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	agentstore "weave/internal/store/agent"
	"weave/internal/ui/layout"
	"weave/lib/dates"
	agentv1 "weave/proto/gen/agent/v1"
	"weave/proto/gen/agent/v1/agentv1connect"
	paginationv1 "weave/proto/gen/pagination/v1"

	"connectrpc.com/connect"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const mailTimeout = 10 * time.Second

// mailPageSize is how far back either section reads, which is a screenful many times over
// rather than the whole mailbox, since the screen is for reading what was said recently.
const mailPageSize = 200

// mailState is the mailbox of one agent, while that screen is up.
type mailState struct {
	agentID   string
	agentName string
	inbox     []*agentv1.Mail
	outbox    []*agentv1.Mail
	cursor    int
	loading   bool
	polling   bool
	err       error
}

// mailRow is one line of the sidebar, either a section head or a piece of mail under it.
type mailRow struct {
	header  bool
	section string
	count   int
	item    *agentv1.Mail
	sent    bool
}

// key is what a cursor is put back on as the mailbox changes underneath it.
func (r mailRow) key() string {
	if r.header {
		return "section:" + r.section
	}

	return r.item.GetId()
}

type mailLoadedMsg struct {
	agentID string
	inbox   []*agentv1.Mail
	outbox  []*agentv1.Mail
	err     error
}

// enterMail opens an agent's mailbox, with the two sections in the sidebar and the message
// under the cursor in the stage.
func (m model) enterMail(agent agentstore.Agent) (tea.Model, tea.Cmd) {
	m.enterScreen(screenMail)
	m.mail = mailState{
		agentID:   agent.ID,
		agentName: agent.Name,
		loading:   true,
	}
	m.frame.Show(layout.RegionStage, newTextContent("_reading the mailbox…_"))

	return m, m.loadMailCmd(agent.ID)
}

func (m model) loadMailCmd(agentID string) tea.Cmd {
	agents := m.api.agents

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), mailTimeout)
		defer cancel()

		inbox, err := listMail(ctx, agents, agentID, agentv1.MailDirection_MAIL_DIRECTION_RECEIVED)
		if err != nil {
			return mailLoadedMsg{agentID: agentID, err: err}
		}

		outbox, err := listMail(ctx, agents, agentID, agentv1.MailDirection_MAIL_DIRECTION_SENT)
		if err != nil {
			return mailLoadedMsg{agentID: agentID, err: err}
		}

		return mailLoadedMsg{agentID: agentID, inbox: inbox, outbox: outbox}
	}
}

func listMail(
	ctx context.Context,
	agents agentv1connect.AgentServiceClient,
	agentID string,
	direction agentv1.MailDirection,
) ([]*agentv1.Mail, error) {
	limit := int32(mailPageSize)

	resp, err := agents.ListMail(ctx, connect.NewRequest(&agentv1.ListMailRequest{
		Id:         &agentID,
		Direction:  direction,
		Pagination: &paginationv1.Pagination{Limit: &limit},
	}))
	if err != nil {
		return nil, fmt.Errorf("failed to read the mailbox of %q: %w", agentID, err)
	}

	return resp.Msg.GetMail(), nil
}

func (m *model) applyMailLoaded(msg mailLoadedMsg) {
	if msg.agentID != m.mail.agentID {
		return
	}

	hovered := mailRowKeyAt(m.mailRows(), m.mail.cursor)

	m.mail.loading, m.mail.polling = false, false
	m.mail.err = msg.err
	if msg.err != nil {
		return
	}

	m.mail.inbox, m.mail.outbox = msg.inbox, msg.outbox
	m.placeMailCursor(hovered)
}

// refreshMailCmd is what the panel's own tick asks for while the mailbox is up, so mail
// arriving while it is open lands on screen rather than waiting on the key that reopens it.
func (m *model) refreshMailCmd() tea.Cmd {
	if m.screen != screenMail || m.mail.loading || m.mail.polling {
		return nil
	}

	m.mail.polling = true

	return m.loadMailCmd(m.mail.agentID)
}

func (m model) mailRows() []mailRow {
	return m.filteredMailRows(m.allMailRows())
}

func (m model) allMailRows() []mailRow {
	rows := make([]mailRow, 0, len(m.mail.inbox)+len(m.mail.outbox)+2)
	rows = appendMailSection(rows, "Inbox", m.mail.inbox, false)

	return appendMailSection(rows, "Outbox", m.mail.outbox, true)
}

func appendMailSection(rows []mailRow, section string, mail []*agentv1.Mail, sent bool) []mailRow {
	rows = append(rows, mailRow{header: true, section: section, count: len(mail)})
	for _, item := range mail {
		rows = append(rows, mailRow{section: section, item: item, sent: sent})
	}

	return rows
}

// filteredMailRows narrows the mail to what matches the pattern, keeping a section head
// whether or not anything under it survived, since an emptied section is itself the answer.
func (m model) filteredMailRows(rows []mailRow) []mailRow {
	if m.filter.pattern == nil {
		return rows
	}

	kept := make([]mailRow, 0, len(rows))
	for _, r := range rows {
		if r.header || m.filter.doesMatch(m.mailCounterparty(r)) || m.filter.doesMatch(r.item.GetBody()) {
			kept = append(kept, r)
		}
	}

	return kept
}

// mailCounterparty is the agent at the other end of a piece of mail: who wrote it in the
// inbox, who it went to in the outbox.
func (m model) mailCounterparty(r mailRow) string {
	if !r.sent {
		return orDash(r.item.GetFromName())
	}

	return m.agentName(r.item.GetToAgentId())
}

// agentName is what an id is called in the listing, falling back to the id itself for an
// agent this panel has never listed, which a deleted one is.
func (m model) agentName(id string) string {
	for _, item := range m.listing.agents {
		if item.agent.ID == id {
			return item.agent.Name
		}
	}

	for _, agent := range m.archive.agents {
		if agent.ID == id {
			return agent.Name
		}
	}

	return orDash(id)
}

func mailRowKeyAt(rows []mailRow, cursor int) string {
	r, ok := rowAt(rows, cursor)
	if !ok {
		return ""
	}

	return r.key()
}

func mailRowIndexOfKey(rows []mailRow, key string) int {
	if key == "" {
		return -1
	}

	for i, r := range rows {
		if r.key() == key {
			return i
		}
	}

	return -1
}

// hoveredMailKey is the row the cursor is on, read before a filter narrows the list under it.
func (m model) hoveredMailKey() string {
	return mailRowKeyAt(m.mailRows(), m.mail.cursor)
}

func (m *model) placeMailCursor(hovered string) {
	rows := m.mailRows()
	m.mail.cursor = placeCursor(mailRowIndexOfKey(rows, hovered), m.mail.cursor, len(rows))
	m.stageMail()
}

func (m model) hoveredMail() (mailRow, bool) {
	r, ok := rowAt(m.mailRows(), m.mail.cursor)
	if !ok || r.header {
		return mailRow{}, false
	}

	return r, true
}

func (m model) updateMail(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := key.String()

	if m.takeTopPress(k) {
		return m.jumpMailCursor(0), nil
	}
	if m.topHeld {
		return m, nil
	}

	switch {
	case isBound(k, m.keys.Nav.Bottom):
		return m.jumpMailCursor(len(m.mailRows()) - 1), nil
	case isBound(k, m.keys.Filter.Open):
		return m.openFilter()
	case isBound(k, m.keys.Mail.Quit):
		return m, m.hide()
	case m.filter.isStanding() && isBound(k, m.keys.Mail.Back):
		return m.openFilter()
	case isBound(k, m.keys.Mail.Back):
		return m.returnToAgents()
	case isBound(k, m.keys.Nav.Up):
		return m.moveMailCursor(-1), nil
	case isBound(k, m.keys.Nav.Down):
		return m.moveMailCursor(1), nil
	case isBound(k, m.keys.Mail.Thread):
		return m.jumpMailThread(), nil
	}

	return m, nil
}

func (m model) moveMailCursor(delta int) model {
	next, ok := stepCursor(m.mail.cursor, delta, len(m.mailRows()))
	if !ok {
		return m
	}

	m.mail.cursor = next
	m.stageMail()

	return m
}

func (m model) jumpMailCursor(idx int) model {
	next, ok := landCursor(idx, len(m.mailRows()))
	if !ok {
		return m
	}

	m.mail.cursor = next
	m.stageMail()

	return m
}

// jumpMailThread moves to the other end of a reply: the mail this one answers, or the first
// answer to it, which is what turns two sections back into one conversation.
func (m model) jumpMailThread() model {
	held, ok := m.hoveredMail()
	if !ok {
		return m
	}

	rows := m.mailRows()
	if parent := held.item.GetReplyTo(); parent != "" {
		if idx := mailRowIndexOfKey(rows, parent); idx >= 0 {
			return m.jumpMailCursor(idx)
		}
	}

	for i, r := range rows {
		if !r.header && r.item.GetReplyTo() == held.item.GetId() {
			return m.jumpMailCursor(i)
		}
	}

	return m
}

func (m model) mailDetails() details {
	r, ok := m.hoveredMail()
	if !ok {
		return details{}
	}

	return details{
		id:     r.item.GetId(),
		kind:   "Mail",
		title:  mailDirectionWord(r.sent) + " " + m.mailCounterparty(r),
		fields: m.mailFields(r),
	}
}

func (m model) mailFields(r mailRow) []recordField {
	item := r.item

	fields := []recordField{
		{"from", mailSender(item)},
		{"to", m.agentName(item.GetToAgentId()) + " (" + item.GetToAgentId() + ")"},
		{"kind", mailKindWord(item.GetKind())},
		{"state", mailStateWord(item)},
		{"sent", orDash(mailStamp(item.GetCreatedAt()))},
	}

	if claimed := item.GetClaimedAt(); claimed != "" {
		fields = append(fields, recordField{"read", mailStamp(claimed)})
	} else {
		fields = append(fields, recordField{"expires", mailStamp(item.GetExpiresAt())})
	}

	if parent := item.GetReplyTo(); parent != "" {
		fields = append(fields, recordField{"reply_to", parent})
	}

	return append(fields,
		recordField{"hops", fmt.Sprintf("%d", item.GetHops())},
		recordField{"mail_id", item.GetId()},
	)
}

// mailSender is who wrote a piece of mail, which is a name with no id behind it when weave
// raised it itself rather than an agent sending one.
func mailSender(item *agentv1.Mail) string {
	name := orDash(item.GetFromName())
	if from := item.GetFromAgentId(); from != "" {
		return name + " (" + from + ")"
	}

	return name + " (weave)"
}

func mailDirectionWord(sent bool) string {
	if sent {
		return "to"
	}

	return "from"
}

func mailKindWord(kind agentv1.MailKind) string {
	switch kind {
	case agentv1.MailKind_MAIL_KIND_PLAIN:
		return "plain"
	case agentv1.MailKind_MAIL_KIND_REVIEW_ACTIVITY:
		return "review activity"
	case agentv1.MailKind_MAIL_KIND_UNSPECIFIED:
		return "—"
	}

	return "—"
}

// mailStateWord is what became of a piece of mail: read, still waiting, or never read and
// past its deadline, which is the fact a mailbox is usually opened to settle.
func mailStateWord(item *agentv1.Mail) string {
	if item.GetClaimedAt() != "" {
		return "read"
	}

	if expired, ok := isMailExpired(item); ok && expired {
		return "expired unread"
	}

	return "unread"
}

func isMailExpired(item *agentv1.Mail) (bool, bool) {
	expires, err := time.Parse(time.RFC3339, item.GetExpiresAt())
	if err != nil {
		return false, false
	}

	return time.Now().After(expires), true
}

// mailStamp reads a wire timestamp back as the panel's own friendly one, leaving anything it
// cannot parse as it came so a bad stamp is visible rather than blank.
func mailStamp(stamp string) string {
	parsed, err := time.Parse(time.RFC3339, stamp)
	if err != nil {
		return stamp
	}

	return dates.Friendly(parsed)
}

func mailClock(stamp string) string {
	parsed, err := time.Parse(time.RFC3339, stamp)
	if err != nil {
		return ""
	}

	return parsed.Local().Format("15:04")
}

func (m *model) stageMail() {
	r, ok := m.hoveredMail()
	if !ok {
		m.stageDocument(mailEmptyMarkdown)

		return
	}

	m.stageDocument(m.mailMarkdown(r))
	m.stageInfo()
}

const mailEmptyMarkdown = "# Mail\n\n_nothing here_\n"

func (m model) mailMarkdown(r mailRow) string {
	item := r.item

	var b strings.Builder
	fmt.Fprintf(&b, "# %s %s\n\n", mailDirectionWord(r.sent), m.mailCounterparty(r))
	fmt.Fprintf(&b, "%s · %s · %s\n\n", mailStamp(item.GetCreatedAt()), mailStateWord(item), mailKindWord(item.GetKind()))
	b.WriteString("---\n\n")
	b.WriteString(item.GetBody() + "\n")

	return b.String()
}

func (m model) mailView() string {
	header := m.titleStyle().Render("Mail · "+m.mail.agentName) + "\n\n" + m.filterView()
	below := m.detailsView() + "\n" + m.renderHelp(m.mailHelp())

	rows := m.mailRowsView()
	if len(rows) == 0 {
		return header + renderList([]string{dimStyle.Render(m.mailNotice())}, 0, m.listLines(header, below)) + below
	}

	return header + renderList(rows, m.mail.cursor, m.listLines(header, below)) + below
}

func (m model) mailNotice() string {
	switch {
	case m.mail.err != nil:
		return m.mail.err.Error()
	case m.mail.loading:
		return "reading the mailbox…"
	}

	return "no mail"
}

func (m model) mailRowsView() []string {
	all := m.mailRows()
	if m.mail.loading || m.mail.err != nil {
		return nil
	}

	rows := make([]string, 0, len(all))
	for i, r := range all {
		hovered := i == m.mail.cursor
		prefix := "  "
		if hovered {
			prefix = m.rowCursorStyle().Render("› ")
		}

		if r.header {
			rows = append(rows, prefix+blurredHeaderStyle.Render(fmt.Sprintf("%s (%d)", r.section, r.count)))

			continue
		}

		rows = append(rows, prefix+"  "+m.mailRowView(r, hovered))
	}

	return rows
}

func (m model) mailRowView(r mailRow, hovered bool) string {
	label := clip(m.mailCounterparty(r), mailNameWidth)
	name := m.filter.highlight(label, m.nameStyle(hovered, false, r.sent))

	// Padded off the unstyled label, since the styled one carries escapes a width count would
	// take for characters.
	pad := strings.Repeat(" ", max(mailNameWidth-lipgloss.Width(label), 0))

	return name + pad + " " +
		dimStyle.Render(mailClock(r.item.GetCreatedAt())) + " " +
		mailStateMark(r.item) + " " +
		dimStyle.Render(mailSnippet(r.item.GetBody()))
}

const mailNameWidth = 16

const mailSnippetWidth = 32

// mailSnippet is the opening of a message on one line, which is what makes a row worth
// reading before the stage has drawn the message itself.
func mailSnippet(body string) string {
	return clip(strings.Join(strings.Fields(body), " "), mailSnippetWidth)
}

var (
	mailUnreadStyle  = lipgloss.NewStyle().Foreground(colorAccent)
	mailExpiredStyle = lipgloss.NewStyle().Foreground(colorDanger).Bold(true)
	mailReadStyle    = lipgloss.NewStyle().Foreground(colorSuccess)
)

// mailStateMark is the glyph in the gutter of a row: waiting, never read, or taken.
func mailStateMark(item *agentv1.Mail) string {
	if item.GetClaimedAt() != "" {
		return mailReadStyle.Render("✓")
	}

	if expired, ok := isMailExpired(item); ok && expired {
		return mailExpiredStyle.Render("!")
	}

	return mailUnreadStyle.Render(unreadMailIcon)
}
