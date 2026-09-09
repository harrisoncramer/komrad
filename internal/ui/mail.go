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
	agentv1 "weave/proto/gen/agent/v1"
	"weave/proto/gen/agent/v1/agentv1connect"
	paginationv1 "weave/proto/gen/pagination/v1"

	"connectrpc.com/connect"
	tea "github.com/charmbracelet/bubbletea"
)

const mailTimeout = 10 * time.Second

// mailPageSize is how far back either half reads, a screenful many times over rather than the
// whole mailbox.
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
}

// mailRow is either the line naming a thread or one message under it.
type mailRow struct {
	// header is the line that names a thread, drawn as furniture the cursor steps over.
	header bool
	// label is who the thread is with, drawn on its header.
	label string
	item  *agentv1.Mail
	sent  bool
}

// key is the row the cursor is put back on as the mailbox changes underneath it, empty for a
// thread line, which holds no message.
func (r mailRow) key() string {
	return r.item.GetId()
}

type mailLoadedMsg struct {
	agentID string
	inbox   []*agentv1.Mail
	outbox  []*agentv1.Mail
	err     error
}

// enterMail opens an agent's mail, with the threads in the sidebar and the message the cursor is
// standing on in the stage.
func (m model) enterMail(agent agentstore.Agent) (tea.Model, tea.Cmd) {
	m.enterScreen(screenMail)
	m.mail = mailState{
		agentID:   agent.ID,
		agentName: agent.Name,
		loading:   true,
	}
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
	if msg.err != nil {
		m.report("Could not read the mailbox", msg.err)

		return
	}

	m.mail.inbox, m.mail.outbox = msg.inbox, msg.outbox
	m.placeMailCursor(hovered)
}

// refreshMailCmd is what the panel's own tick asks for while the mailbox is up, so mail arriving
// while it is open lands on screen.
func (m *model) refreshMailCmd() tea.Cmd {
	if m.screen != screenMail || m.mail.loading || m.mail.polling {
		return nil
	}

	m.mail.polling = true

	return m.loadMailCmd(m.mail.agentID)
}

// mailItems is the whole correspondence in the order it happened, both halves merged.
func (m model) mailItems() []mailRow {
	rows := make([]mailRow, 0, len(m.mail.inbox)+len(m.mail.outbox))
	for _, item := range m.mail.inbox {
		rows = append(rows, mailRow{item: item})
	}
	for _, item := range m.mail.outbox {
		rows = append(rows, mailRow{item: item, sent: true})
	}

	// Sorted by id rather than by the stamp.
	slices.SortFunc(rows, func(a, b mailRow) int {
		return strings.Compare(a.item.GetId(), b.item.GetId())
	})

	return rows
}

func (m model) mailRows() []mailRow {
	return m.threadedMailRows(m.filteredMailRows(m.mailItems()))
}

// threadedMailRows groups the messages into their threads, newest thread first and the newest
// message first inside each.
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

	// A thread stands where its newest message does, which is the id of its last message.
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

// newestMailID is the id a thread stands by, being its last message.
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
// its own.
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

		// Walked with a guard rather than trusted.
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

// mailThreadOf is every message in the thread the given one belongs to, in order.
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

// mailCounterparty is the agent at the other end of a message: who wrote it when it came in, who
// it went to when it went out.
func (m model) mailCounterparty(r mailRow) string {
	if !r.sent {
		return orDash(r.item.GetFromName())
	}

	return m.agentName(r.item.GetToAgentId())
}

// agentName is what an id is called in the listing, falling back to the id itself for an agent
// this panel has never listed.
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
	m.landMailCursor(rows, nextMailRow(rows, placeCursor(mailRowIndexOfKey(rows, hovered), m.mail.cursor, len(rows)), 1))
	m.stageMail()
}

func (m model) hoveredMail() (mailRow, bool) {
	r, ok := rowAt(m.mailRows(), m.mail.cursor)
	if !ok || r.header {
		return mailRow{}, false
	}

	return r, true
}

// mailColumn is the message the cursor is standing on drawn beside the threads.
func mailColumn() columnSpec {
	return columnSpec{floor: columnFloor, doc: model.mailDoc}
}

// mailDoc is the markdown drawn beside the threads: the message the cursor is on, or a notice
// while the mailbox is still being read or has nothing in it.
func (m model) mailDoc() string {
	if m.mail.loading {
		return mailLoadingMarkdown
	}

	r, ok := m.hoveredMail()
	if !ok {
		return mailEmptyMarkdown
	}

	return m.mailMarkdown(r)
}

// landMailCursor puts the cursor on a row, naming the row rather than only its place in the list
// so the message's own offset is kept.
func (m *model) landMailCursor(rows []mailRow, idx int) {
	m.mail.cursor = idx
	m.placeColumn(screenMail, mailRowKeyAt(rows, idx))
}

func (m *model) stageMail() {
	m.stageInfo()
}

// nextMailRow is the nearest message to the given row, looked for the way the cursor was
// travelling and then back the other way, so a move or a listing never leaves it standing on a
// thread line.
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

	// Only an empty list reaches here.
	return 0
}

func (m model) updateMail(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := key.String()

	// The doubled top key is a gesture rather than a binding, so it is settled before the table,
	// which would otherwise let a single press jump.
	if m.takeTopPress(k) {
		return m.jumpMailCursor(0), nil
	}
	if m.topHeld {
		return m, nil
	}

	return mailActions().dispatch(m, k)
}

// leaveMail drops the mailbox on the way out.
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

	m.landMailCursor(m.mailRows(), next)
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

	m.landMailCursor(rows, nextMailRow(rows, next, direction))
	m.stageMail()

	return m
}
