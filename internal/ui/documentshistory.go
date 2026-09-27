package ui

import (
	"fmt"
	"strings"

	"komrad/internal/errui"
	"komrad/internal/ui/paint"
	"komrad/internal/ui/paint/label"
	"komrad/lib/dates"
	agentv1 "komrad/proto/gen/agent/v1"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/pmezard/go-difflib/difflib"
)

// diffContext is how many unchanged lines are drawn either side of a change.
const diffContext = 3

// documentHistory is one document's writes stepped through, drawing what the one stood on changed.
type documentHistory struct {
	id string
	// versions is every write kept, newest first, and at the one stood on.
	versions []*agentv1.DocumentVersion
	at       int
	listing  bool
	err      error
	reads    map[int32]versionRead
}

// versionRead is one write's text as it was read, or why it could not be.
type versionRead struct {
	body    string
	pending bool
	err     error
}

// stepOlder steps back to the write before the one drawn, starting from the newest.
func (d *documentsScreen) stepOlder(h host, s panelShared) tea.Cmd {
	item, ok := d.hovered(s)
	if !ok || d.editing != nil {
		return nil
	}

	if d.history == nil || d.history.id != item.id {
		d.history = &documentHistory{id: item.id}

		return h.waitToStage()
	}

	if d.history.at+1 < len(d.history.versions) {
		d.history.at++
	}

	return h.waitToStage()
}

// stepNewer steps forward to the write after the one drawn, and past the newest back to the
// document as it stands.
func (d *documentsScreen) stepNewer(h host, s panelShared) tea.Cmd {
	if !d.isSteppingHovered(s) {
		return nil
	}

	if d.history.at == 0 {
		d.history = nil

		return nil
	}

	d.history.at--

	return h.waitToStage()
}

// isSteppingHovered reports whether the document under the cursor is being stepped through.
func (d *documentsScreen) isSteppingHovered(s panelShared) bool {
	item, ok := d.hovered(s)

	return ok && d.history != nil && d.history.id == item.id
}

// docRows is the document drawn beside the list at a width: the write stepped to and what it
// changed, or the document as it stands.
func (d *documentsScreen) docRows(s panelShared, width int) []string {
	item, ok := d.hovered(s)
	if !ok || !d.isSteppingHovered(s) {
		return paint.DocStyledRows(d.doc(s), width, columnStyle())
	}

	return d.history.rows(item, width)
}

func (h *documentHistory) rows(item documentItem, width int) []string {
	notice := "_reading its versions…_"
	switch {
	case h.err != nil:
		notice = "could not list its versions: " + errui.GetReason(h.err)
	case h.versions != nil:
		notice = h.notice()
	}

	rows := paint.DocStyledRows(renderDocument(item.name, notice, "", ""), width, columnStyle())
	if h.err != nil || h.versions == nil {
		return rows
	}

	return append(append(rows, ""), h.changeRows(width)...)
}

// notice names the write stepped to, who made it and how long ago.
func (h *documentHistory) notice() string {
	current := h.versions[h.at]
	by := "the agent"
	if current.GetByPerson() {
		by = "you"
	}

	return fmt.Sprintf("_version %d of %d, written by %s %s ago_",
		current.GetVersion(), h.versions[0].GetVersion(), by, label.Age(dates.Parse(current.GetCreatedAt())))
}

// changeRows is what the write stepped to changed from the one kept before it.
func (h *documentHistory) changeRows(width int) []string {
	indent := strings.Repeat(" ", docMargin())

	after, ok := h.reads[h.versions[h.at].GetVersion()]
	before := versionRead{}
	if h.at+1 < len(h.versions) {
		var held bool
		before, held = h.reads[h.versions[h.at+1].GetVersion()]
		ok = ok && held
	}

	switch {
	case !ok || after.pending || before.pending:
		return []string{fitRow(indent+dimStyle.Render("reading…"), width)}
	case after.err != nil:
		return []string{fitRow(indent+"could not read: "+errui.GetReason(after.err), width)}
	case before.err != nil:
		return []string{fitRow(indent+"could not read: "+errui.GetReason(before.err), width)}
	case before.body == after.body:
		return []string{fitRow(indent+dimStyle.Render("no change to the text"), width)}
	}

	return diffRows(before.body, after.body, indent, width)
}

// diffRows is the lines one text changed in another, with a few unchanged either side, wrapped
// to a width.
func diffRows(before, after, indent string, width int) []string {
	a, b := diffLines(before), diffLines(after)
	added := paint.Panel.Style(reviewAddedTone)
	removed := paint.Panel.Style(reviewRemovedTone)
	unchanged := paint.Panel.Style(paint.ToneMuted)
	room := max(width-len(indent)-2, 1)

	var rows []string
	for i, group := range difflib.NewMatcher(a, b).GetGroupedOpCodes(diffContext) {
		if i > 0 {
			rows = append(rows, indent+dimStyle.Render("⋯"))
		}

		for _, op := range group {
			if op.Tag == 'e' {
				rows = append(rows, wrappedDiffRows(a[op.I1:op.I2], diffMark{" ", unchanged}, indent, room)...)

				continue
			}
			if op.Tag == 'r' || op.Tag == 'd' {
				rows = append(rows, wrappedDiffRows(a[op.I1:op.I2], diffMark{"-", removed}, indent, room)...)
			}
			if op.Tag == 'r' || op.Tag == 'i' {
				rows = append(rows, wrappedDiffRows(b[op.J1:op.J2], diffMark{"+", added}, indent, room)...)
			}
		}
	}

	return rows
}

// diffMark is how a changed line is drawn: the sign before it and its colour.
type diffMark struct {
	sign  string
	style lipgloss.Style
}

func wrappedDiffRows(lines []string, mark diffMark, indent string, room int) []string {
	var rows []string
	for _, line := range lines {
		for j, part := range strings.Split(ansi.Wrap(line, room, ""), "\n") {
			sign := mark.sign
			if j > 0 {
				sign = " "
			}

			rows = append(rows, indent+mark.style.Render(sign+" "+part))
		}
	}

	return rows
}

func diffLines(body string) []string {
	body = strings.TrimRight(body, "\n")
	if body == "" {
		return nil
	}

	return strings.Split(body, "\n")
}

// documentVersionsListedMsg is the writes kept of a document come back.
type documentVersionsListedMsg struct {
	id       string
	versions []*agentv1.DocumentVersion
	err      error
}

// documentVersionReadMsg is one write's text come back.
type documentVersionReadMsg struct {
	id      string
	version int32
	read    versionRead
}

// readHistoryCmd lists the writes of the document being stepped through, then reads the one
// stood on and the one before it.
func (m *model) readHistoryCmd() tea.Cmd {
	d := &m.documents
	if !d.isSteppingHovered(m.shared()) {
		return nil
	}

	h, clients := d.history, m.api
	if h.versions == nil {
		if h.listing || h.err != nil {
			return nil
		}

		h.listing = true
		id := h.id

		return func() tea.Msg {
			versions, err := clients.listDocumentVersions(id)

			return documentVersionsListedMsg{id: id, versions: versions, err: err}
		}
	}

	var cmds []tea.Cmd
	for _, at := range []int{h.at, h.at + 1} {
		if at < len(h.versions) {
			cmds = append(cmds, m.readVersionCmd(h.versions[at].GetVersion()))
		}
	}

	return tea.Batch(cmds...)
}

// readVersionCmd reads one write of the document being stepped through, once.
func (m *model) readVersionCmd(version int32) tea.Cmd {
	h, clients := m.documents.history, m.api
	if _, asked := h.reads[version]; asked {
		return nil
	}

	if h.reads == nil {
		h.reads = map[int32]versionRead{}
	}
	h.reads[version] = versionRead{pending: true}
	id := h.id

	return func() tea.Msg {
		document, err := clients.getDocumentVersion(id, version)
		if err != nil {
			return documentVersionReadMsg{id: id, version: version, read: versionRead{err: err}}
		}

		return documentVersionReadMsg{id: id, version: version, read: versionRead{body: document.GetBody()}}
	}
}

func (m *model) applyDocumentVersionsListed(msg documentVersionsListedMsg) tea.Cmd {
	h := m.documents.history
	if h == nil || h.id != msg.id {
		return nil
	}

	h.listing = false
	if msg.err != nil {
		h.err = msg.err

		return nil
	}

	h.versions, h.at = msg.versions, 0

	return m.readHistoryCmd()
}

func (m *model) applyDocumentVersionRead(msg documentVersionReadMsg) {
	h := m.documents.history
	if h == nil || h.id != msg.id || h.reads == nil {
		return
	}

	h.reads[msg.version] = msg.read
}
