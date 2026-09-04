package ui

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
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

// mailPageSize is how far back either half reads, which is a screenful many times over rather
// than the whole mailbox, since the screen is for reading what was said recently.
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

// mailRow is either the line naming a thread or one message under it, since the mailbox is read
// a conversation at a time rather than as one run of messages.
type mailRow struct {
	// header is the line that names a thread, drawn as furniture: the cursor steps over it, so
	// nothing has to answer for what a record or a stage would show while standing on one.
	header bool
	// label is who the thread is with, drawn on its header.
	label string
	item  *agentv1.Mail
	sent  bool
}

// key is the row the cursor is put back on as the mailbox changes underneath it, empty for a
// thread line, which holds no message and reads its id off a nil pointer that the generated
// getter answers for.
func (r mailRow) key() string {
	return r.item.GetId()
}

type mailLoadedMsg struct {
	agentID string
	inbox   []*agentv1.Mail
	outbox  []*agentv1.Mail
	err     error
}

// enterMail opens an agent's mail, with the threads in the sidebar and the message the cursor
// is standing on in the stage.
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

		var inbox, outbox []*agentv1.Mail
		var inboxErr, outboxErr error

		var wg sync.WaitGroup
		wg.Go(func() {
			inbox, inboxErr = listMail(ctx, agents, agentID, agentv1.MailDirection_MAIL_DIRECTION_RECEIVED)
		})
		wg.Go(func() {
			outbox, outboxErr = listMail(ctx, agents, agentID, agentv1.MailDirection_MAIL_DIRECTION_SENT)
		})
		wg.Wait()

		if err := errors.Join(inboxErr, outboxErr); err != nil {
			return mailLoadedMsg{agentID: agentID, err: err}
		}

		return mailLoadedMsg{
			agentID: agentID,
			inbox:   inbox,
			outbox:  outbox,
		}
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
	if msg.agentID == "" || msg.agentID != m.mail.agentID {
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

// mailItems is the whole correspondence in the order it happened, both halves merged, since a
// conversation split by direction is the one thing a mailbox must not do.
func (m model) mailItems() []mailRow {
	rows := make([]mailRow, 0, len(m.mail.inbox)+len(m.mail.outbox))
	for _, item := range m.mail.inbox {
		rows = append(rows, mailRow{item: item})
	}
	for _, item := range m.mail.outbox {
		rows = append(rows, mailRow{item: item, sent: true})
	}

	// Sorted by id rather than by the stamp, since the ids are uuidv7 and so already carry the
	// order, and two messages written in the same second would otherwise swap under the reader.
	slices.SortFunc(rows, func(a, b mailRow) int {
		return strings.Compare(a.item.GetId(), b.item.GetId())
	})

	return rows
}

func (m model) mailRows() []mailRow {
	return m.threadedMailRows(m.filteredMailRows(m.mailItems()))
}

// threadedMailRows groups the messages into their threads, newest thread first and the newest
// message first inside each, which is how a mailbox is read: the conversation something just
// happened in, and what just happened in it.
func (m model) threadedMailRows(items []mailRow) []mailRow {
	// The filter has already run, so a thread narrowed away has nothing to build a header from
	// and no header is ever left standing over nothing.
	roots := mailThreadRoots(items)

	threads := make(map[string][]mailRow, len(items))
	order := make([]string, 0, len(items))
	for _, r := range items {
		root := roots[r.item.GetId()]
		if _, seen := threads[root]; !seen {
			order = append(order, root)
		}

		threads[root] = append(threads[root], r)
	}

	// A thread stands where its newest message does, which is the id of its last message, since
	// the items came in ascending and the ids carry the order.
	slices.SortStableFunc(order, func(a, b string) int {
		return strings.Compare(newestMailID(threads[b]), newestMailID(threads[a]))
	})

	rows := make([]mailRow, 0, len(items)+len(order))
	for _, root := range order {
		thread := threads[root]
		newest := thread[len(thread)-1]

		rows = append(rows, mailRow{header: true, label: m.mailCounterparty(newest)})
		for i := len(thread) - 1; i >= 0; i-- {
			rows = append(rows, thread[i])
		}
	}

	return rows
}

// newestMailID is the id a thread stands by, which is its last message, since a thread is only
// ever built around one and the items came in ascending.
func newestMailID(thread []mailRow) string {
	return thread[len(thread)-1].item.GetId()
}

// filteredMailRows narrows the correspondence to what matches the pattern, over the
// correspondent and the body both.
func (m model) filteredMailRows(rows []mailRow) []mailRow {
	if m.filter.pattern == nil {
		return rows
	}

	kept := make([]mailRow, 0, len(rows))
	for _, r := range rows {
		if m.filter.doesMatch(m.mailCounterparty(r)) || m.filter.doesMatch(r.item.GetBody()) {
			kept = append(kept, r)
		}
	}

	return kept
}

// mailThreadRoots resolves every message to the one that opened its thread, following reply_to
// as far as the rows on screen reach, so a reply whose parent is off the page opens a thread of
// its own rather than hanging off nothing.
func mailThreadRoots(rows []mailRow) map[string]string {
	parent := make(map[string]string, len(rows))
	held := make(map[string]bool, len(rows))
	for _, r := range rows {
		held[r.item.GetId()] = true
	}
	for _, r := range rows {
		if up := r.item.GetReplyTo(); up != "" && held[up] {
			parent[r.item.GetId()] = up
		}
	}

	roots := make(map[string]string, len(rows))
	for _, r := range rows {
		root := r.item.GetId()

		// Walked with a guard rather than trusted, since a cycle here would hang the panel and
		// the rows come off the wire.
		seen := map[string]bool{root: true}
		for {
			up, ok := parent[root]
			if !ok || seen[up] {
				break
			}

			seen[up] = true
			root = up
		}

		roots[r.item.GetId()] = root
	}

	return roots
}

// mailThreadOf is every message in the thread the given one belongs to, in order, which is what
// the stage draws and what the record counts.
func (m model) mailThreadOf(id string) []mailRow {
	rows := m.mailItems()
	roots := mailThreadRoots(rows)

	held := make([]mailRow, 0, len(rows))
	for _, r := range rows {
		if roots[r.item.GetId()] == roots[id] {
			held = append(held, r)
		}
	}

	return held
}

// mailCounterparty is the agent at the other end of a message: who wrote it when it came in,
// who it went to when it went out.
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
	m.mail.cursor = nextMailRow(rows, placeCursor(mailRowIndexOfKey(rows, hovered), m.mail.cursor, len(rows)), 1)
	m.stageMail()
}

func (m model) hoveredMail() (mailRow, bool) {
	r, ok := rowAt(m.mailRows(), m.mail.cursor)
	if !ok || r.header {
		return mailRow{}, false
	}

	return r, true
}

// nextMailRow is the nearest message to the given row, looked for the way the cursor was
// travelling and then back the other way, which is what it lands on when a move or a listing
// would otherwise leave it standing on a thread line.
func nextMailRow(rows []mailRow, from, direction int) int {
	for i := from; i >= 0 && i < len(rows); i += direction {
		if !rows[i].header {
			return i
		}
	}

	for i := from; i >= 0 && i < len(rows); i -= direction {
		if !rows[i].header {
			return i
		}
	}

	// Only an empty list reaches here, since a list with any message in it has one either side
	// of wherever the cursor was.
	return 0
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
		return m.leaveMail()
	case isBound(k, m.keys.Nav.Up):
		return m.moveMailCursor(-1), nil
	case isBound(k, m.keys.Nav.Down):
		return m.moveMailCursor(1), nil
	}

	return m, nil
}

// leaveMail drops the mailbox on the way out, since a read in flight would otherwise land on
// the agents list and paint a message over its stage.
func (m model) leaveMail() (tea.Model, tea.Cmd) {
	m.mail = mailState{}

	return m.returnToAgents()
}

func (m model) moveMailCursor(delta int) model {
	rows := m.mailRows()

	next, ok := stepCursor(m.mail.cursor, delta, len(rows))
	if !ok {
		return m
	}

	// Stepped past a thread line rather than onto it, so moving through the mailbox is moving
	// through its messages however much furniture stands between them.
	if next = nextMailRow(rows, next, delta); next == m.mail.cursor {
		return m
	}

	m.mail.cursor = next
	m.stageMail()

	return m
}

func (m model) jumpMailCursor(idx int) model {
	rows := m.mailRows()

	next, ok := landCursor(idx, len(rows))
	if !ok {
		return m
	}

	direction := 1
	if idx > 0 {
		direction = -1
	}

	m.mail.cursor = nextMailRow(rows, next, direction)
	m.stageMail()

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
		recordField{"hops", fmt.Sprintf("%d", item.GetHops())},
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
	count := fmt.Sprintf("%d %s", len(thread), plural(len(thread), "message"))

	switch {
	case !last.sent:
		return count + " · they spoke last"
	case last.item.GetClaimedAt() != "":
		return count + " · read your last"
	case mailIsExpired(last.item):
		return count + " · your last expired unread"
	}

	return count + " · waiting on " + m.mailCounterparty(last)
}

func plural(count int, word string) string {
	if count == 1 {
		return word
	}

	return word + "s"
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
	case mailIsExpired(item):
		return "expired unread"
	}

	return "unread"
}

// mailIsExpired is whether a message ran out of time before anyone took it, reading a stamp it
// cannot parse as time left rather than time gone.
func mailIsExpired(item *agentv1.Mail) bool {
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

func (m *model) stageMail() {
	if r, ok := m.hoveredMail(); ok {
		m.stageDocument(m.mailMarkdown(r))
	} else {
		m.stageDocument(mailEmptyMarkdown)
	}

	m.stageInfo()
}

const mailEmptyMarkdown = "# Mail\n\n_nothing here_\n"

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
	header := m.titleStyle().Render("Mail · "+m.mail.agentName) + "\n" +
		dimStyle.Render(m.mailCountView()) + "\n\n" +
		m.filterView()
	below := m.detailsView() + "\n" + m.renderHelp(m.mailHelp())

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

		if mailIsExpired(r.item) {
			expired++

			continue
		}

		waiting++
	}

	count := fmt.Sprintf("%d %s · %d waiting", len(rows), plural(len(rows), "message"), waiting)
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
	stamps := mailStampWidth(all)

	rows := make([]string, 0, len(all))
	for i, r := range all {
		if r.header {
			rows = append(rows, "  "+blurredHeaderStyle.Render(clip(r.label, max(m.width-mailCursorWidth, 1))))

			continue
		}

		prefix := "  "
		if i == m.mail.cursor {
			prefix = m.rowCursorStyle().Render("› ")
		}

		rows = append(rows, prefix+m.mailRowView(r, stamps, i == m.mail.cursor))
	}

	return rows
}

// mailStampWidth is the room the stamps need, measured over the mail on screen rather than over
// the widest label dates.Clock could draw, so a mailbox that is all from today spends seven
// columns on the time rather than fourteen on a date none of it carries.
func mailStampWidth(rows []mailRow) int {
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
func (m model) mailRowView(r mailRow, stamps int, hovered bool) string {
	// Padded on the right rather than the left, since right-aligning a stamp that is a time on
	// one row and a day and a time on the next leads the short ones in by half the column, which
	// reads as though those rows were nested under something.
	stamp := fmt.Sprintf("%-*s", stamps, mailClock(r.item.GetCreatedAt()))
	head := " " + dimStyle.Render(stamp) + " " +
		dimStyle.Render(mailDirectionMark(r.sent)) + " " +
		mailStateMark(r.item)

	// A pane too narrow even for the stamp gets the stamp alone, clipped while it is still plain
	// text: clipping the styled head would cut through an escape sequence, and a row wider than
	// the pane wraps onto a line the list has not counted.
	if room := m.width - mailCursorWidth; lipgloss.Width(head) > room {
		return dimStyle.Render(clip(strings.TrimSpace(stamp), max(room, 1)))
	}

	body := mailSnippet(r.item.GetBody())

	spare := m.width - mailRowIndent - stamps
	if spare < mailLeastSnippet {
		return head
	}

	// Drawn at full strength whichever way it went: dimming what this agent sent made half the
	// conversation read as though it mattered less, and the arrow already says the direction.
	return head + " " + m.filter.highlight(clip(body, spare), m.nameStyle(hovered, false, false))
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
	case mailIsExpired(item):
		return mailExpiredStyle.Render("!")
	}

	return mailUnreadStyle.Render(unreadMailIcon)
}
