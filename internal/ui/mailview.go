package ui

import (
	"fmt"
	"strings"
	"time"

	"weave/lib/dates"
	agentv1 "weave/proto/gen/agent/v1"

	"github.com/charmbracelet/lipgloss"
)

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
		{"thread", m.mailThreadWord(m.mailThreadOf(item.GetId()))},
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
		recordField{"mail_id", item.GetId()},
	)
}

// mailThreadWord is how far a conversation has run and who it is standing on, which is the
// answer a stalled handoff is looked for in: the last word went out and nobody has taken it.
func (m model) mailThreadWord(thread []mailRow) string {
	if len(thread) == 0 {
		return "—"
	}

	last := thread[len(thread)-1]
	count := counted(len(thread), "message")

	switch {
	case !last.sent:
		return count + " · they spoke last"
	case last.item.GetClaimedAt() != "":
		return count + " · read your last"
	case isMailExpired(last.item):
		return count + " · your last expired unread"
	}

	return count + " · waiting on " + m.mailCounterparty(last)
}

// counted is how many of something there are, taking -es after an ending a bare s will not sit
// on, since a match is one of those and will not be the last.
func counted(count int, word string) string {
	if count == 1 {
		return "1 " + word
	}

	suffix := "s"
	for _, ending := range []string{"ch", "sh", "s", "x", "z"} {
		if strings.HasSuffix(word, ending) {
			suffix = "es"

			break
		}
	}

	return fmt.Sprintf("%d %s%s", count, word, suffix)
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

// mailDirectionMark is the arrow a row opens with, which is what carries the direction now that
// the list is one conversation rather than two halves.
func mailDirectionMark(sent bool) string {
	if sent {
		return "→"
	}

	return "←"
}

func mailKindWord(kind agentv1.MailKind) string {
	switch kind {
	case agentv1.MailKind_MAIL_KIND_PLAIN:
		return "plain"
	case agentv1.MailKind_MAIL_KIND_REVIEW_ACTIVITY:
		return "review activity"
	case agentv1.MailKind_MAIL_KIND_EXCHANGE_CAPPED:
		return "exchange capped"
	case agentv1.MailKind_MAIL_KIND_UNSPECIFIED:
		return "—"
	}

	return "—"
}

// mailStateWord is what became of a piece of mail: read, still waiting, or never read and
// past its deadline, which is the fact a mailbox is usually opened to settle.
func mailStateWord(item *agentv1.Mail) string {
	switch {
	case item.GetClaimedAt() != "":
		return "read"
	case isMailExpired(item):
		return "expired unread"
	}

	return "unread"
}

// isMailExpired is whether a message ran out of time before anyone took it, reading a stamp it
// cannot parse as time left rather than time gone.
func isMailExpired(item *agentv1.Mail) bool {
	expires, err := time.Parse(time.RFC3339, item.GetExpiresAt())
	if err != nil {
		return false
	}

	return time.Now().After(expires)
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

// mailClock is the stamp a row carries, which is the panel's own short label rather than a
// clock of its own, so a row here and a record anywhere else read the same way.
func mailClock(stamp string) string {
	parsed, err := time.Parse(time.RFC3339, stamp)
	if err != nil {
		return ""
	}

	return dates.Clock(parsed)
}

const mailEmptyMarkdown = "# Mail\n\n_nothing here_\n"

// mailLoadingMarkdown stands in the column while the mailbox is being read, so the column is
// there from the moment the screen is rather than arriving under the reader.
const mailLoadingMarkdown = "_reading the mailbox…_"

// mailMarkdown is the one message the cursor is on, since the rows around it already carry the
// rest of its thread and drawing the conversation again only asked the reader to find their
// place in it twice.
func (m model) mailMarkdown(r mailRow) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s %s\n\n", mailDirectionWord(r.sent), m.mailCounterparty(r))
	fmt.Fprintf(&b, "_%s · %s · %s_\n\n",
		mailStamp(r.item.GetCreatedAt()),
		mailStateWord(r.item),
		mailKindWord(r.item.GetKind()),
	)
	b.WriteString(r.item.GetBody() + "\n")

	return b.String()
}

func (m model) mailView() string {
	return m.columnView(model.mailListView)
}

func (m model) mailListView() string {
	header := m.titleStyle().Render("Mail · "+m.mail.agentName) + "\n" +
		dimStyle.Render(m.mailCountView()) + "\n\n" +
		m.filterView()
	record, help := m.detailsView(), m.renderHelp(m.mailHelp())
	below := record + m.messagesRegion(record, help) + help

	rows := m.mailRowsView()
	if len(rows) == 0 {
		return header + renderList([]string{dimStyle.Render(m.mailNotice())}, 0, m.listLines(header, below)) + below
	}

	return header + renderList(rows, m.mail.cursor, m.listLines(header, below)) + below
}

// mailCountView is what the mailbox holds and what of it is owed an answer, since a message
// nobody has taken is the reason this screen is usually open.
func (m model) mailCountView() string {
	rows := m.mailItems()

	var waiting, expired int
	for _, r := range rows {
		if r.item.GetClaimedAt() != "" {
			continue
		}

		if isMailExpired(r.item) {
			expired++

			continue
		}

		waiting++
	}

	count := fmt.Sprintf("%s · %d waiting", counted(len(rows), "message"), waiting)
	if expired > 0 {
		count += fmt.Sprintf(" · %d expired", expired)
	}

	return count
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
	if m.mail.loading || m.mail.err != nil {
		return nil
	}

	all := m.mailRows()
	clocks := mailClockWidth(all)

	rows := make([]string, 0, len(all))
	for i, r := range all {
		if r.header {
			rows = append(rows, "  "+blurredHeaderStyle.Render(clip(r.label, max(m.width-mailCursorWidth, 1))))

			continue
		}

		prefix := "  "
		if i == m.mail.cursor {
			prefix = m.rowCursorPrefix()
		}

		rows = append(rows, prefix+m.mailRowView(r, clocks, i == m.mail.cursor))
	}

	return rows
}

// mailClockWidth is the room the clocks need, measured over the mail on screen rather than over
// the widest label dates.Clock could draw, so a mailbox that is all from today spends seven
// columns on the time rather than fourteen on a date none of it carries.
func mailClockWidth(rows []mailRow) int {
	width := 0
	for _, r := range rows {
		if r.header {
			continue
		}

		width = max(width, lipgloss.Width(mailClock(r.item.GetCreatedAt())))
	}

	return width
}

const (
	// mailCursorWidth is the cursor drawn before every row, which belongs to the list rather
	// than to the row.
	mailCursorWidth = 2
	// mailRowIndent is that cursor, the arrow, the mark, and the four single spaces around them,
	// which is what the snippet does not get.
	mailRowIndent = mailCursorWidth + 6
	// mailLeastSnippet is the least of a message worth drawing, under which the row is when it
	// was written, which way it went, and what became of it.
	mailLeastSnippet = 8
)

// mailRowView draws one message on one line: when it was written, which way it went and what
// became of it, then as much of the message as the sidebar has room for, since who it was with
// is on the thread line above rather than repeated down the block.
func (m model) mailRowView(r mailRow, clocks int, hovered bool) string {
	// Padded on the right rather than the left, since right-aligning a clock that is a time on
	// one row and a day and a time on the next leads the short ones in by half the column, which
	// reads as though those rows were nested under something.
	clock := fmt.Sprintf("%-*s", clocks, mailClock(r.item.GetCreatedAt()))
	head := " " + dimStyle.Render(clock) + " " +
		dimStyle.Render(mailDirectionMark(r.sent)) + " " +
		mailStateMark(r.item)

	// A pane too narrow even for the clock gets the clock alone, clipped while it is still plain
	// text: clipping the styled head would cut through an escape sequence, and a row wider than
	// the pane wraps onto a line the list has not counted.
	if room := m.width - mailCursorWidth; lipgloss.Width(head) > room {
		return dimStyle.Render(clip(strings.TrimSpace(clock), max(room, 1)))
	}

	body := mailSnippet(r.item.GetBody())

	spare := m.width - mailRowIndent - clocks
	if spare < mailLeastSnippet {
		return head
	}

	// Drawn at full strength whichever way it went: dimming what this agent sent made half the
	// conversation read as though it mattered less, and the arrow already says the direction.
	return head + " " + m.filter.highlight(clip(body, spare), m.nameStyle(hovered, false))
}

// mailSnippet is the opening of a message on one line, which is what makes a row worth
// reading before the stage has drawn the thread itself.
func mailSnippet(body string) string {
	return strings.Join(strings.Fields(body), " ")
}

var (
	mailUnreadStyle  = lipgloss.NewStyle().Foreground(colorAccent)
	mailExpiredStyle = lipgloss.NewStyle().Foreground(colorDanger).Bold(true)
	mailReadStyle    = lipgloss.NewStyle().Foreground(colorSuccess)
)

// mailStateMark is the glyph in the gutter of a row: waiting, never read, or taken.
func mailStateMark(item *agentv1.Mail) string {
	switch {
	case item.GetClaimedAt() != "":
		return mailReadStyle.Render("✓")
	case isMailExpired(item):
		return mailExpiredStyle.Render("!")
	}

	return mailUnreadStyle.Render(unreadMailIcon)
}
