package ui

import (
	"fmt"
	"strings"
	"time"

	"komrad/internal/errui"
	"komrad/internal/ui/diffview"
	"komrad/internal/ui/list"
	"komrad/internal/ui/paint"
	"komrad/internal/ui/paint/label"
	"komrad/internal/ui/record"
	"komrad/internal/ui/screen"
	"komrad/internal/view"
	"komrad/lib/dates"
	"komrad/lib/xslice"
	agentv1 "komrad/proto/gen/agent/v1"
)

func (d *documentsScreen) emptyLine() string {
	switch {
	case d.err != nil:
		return "could not list: " + errui.GetReason(d.err)
	case !d.listed:
		return "reading…"
	}

	return "no documents"
}

// documentColumns is the gutter of the documents list: how long ago each was last written.
func documentColumns() view.Columns {
	return view.Columns{{Kind: view.ColumnText, Width: ageColumns}}
}

func (d *documentsScreen) snapshot(m *model, in screen.Inputs, pane list.Pane) view.Snapshot {
	rows := d.rows(m, in)
	picked := list.Picked(in.Room.Pattern, rows, d.documentRowFits(in))
	now := in.Room.Now
	caret := pane.Caret
	if d.isDiffFocused(in) {
		caret = view.CaretRendered
	}

	return view.Snapshot{
		Rows: xslice.Map(rows, func(r documentRow) view.Row {
			return d.renamingField().RenderedOver(view.Row{
				Key:     r.Key(),
				Kind:    view.KindResult,
				Name:    r.item.name,
				Matches: pickedMatches(in.Room.Pattern.Regexp, picked, r.Key(), r.item.name),
				Process: view.ProcessNone,
				Cells:   []view.Cell{mutedCell(label.AgeAt(r.item.updatedAt, now))},
			})
		}),
		Caret:   caret,
		Focused: list.KeyAt(rows, d.Row),
		Pane:    pane.Width,
		Columns: documentColumns(),
	}
}

func (d *documentsScreen) details(m *model, in screen.Inputs) record.Details {
	item, ok := d.hovered(m, in)
	if !ok {
		return record.Details{}
	}

	return record.Details{
		ID: item.id,
		Fields: []record.Field{
			{Label: "type", Value: label.OrDash(item.typeName)},
			{Label: "updated", Value: label.OrDash(label.Friendly(item.updatedAt))},
			{Label: "agent", Value: findAgentName(in.Listing, item.agentID)},
		},
	}
}

func (d *documentsScreen) documentTypeSnapshot(m *model, in screen.Inputs, pane list.Pane) view.Snapshot {
	rows := d.documentTypeRows(m, in)
	now := in.Room.Now

	return view.Snapshot{
		Rows: xslice.Map(rows, func(r documentTypeRow) view.Row {
			return view.Row{
				Key:     r.Key(),
				Kind:    view.KindResult,
				Name:    r.documentType.GetName(),
				Matches: matchesIn(in.Room.Pattern.Regexp, r.documentType.GetName()),
				Process: view.ProcessNone,
				Cells:   []view.Cell{mutedCell(label.AgeAt(dates.Parse(r.documentType.GetUpdatedAt()), now))},
			}
		}),
		Caret:   pane.Caret,
		Focused: list.KeyAt(rows, d.documentTypes.Row),
		Pane:    pane.Width,
		Columns: documentColumns(),
	}
}

// doc is the markdown rendered beside the list for the document the cursor is on.
func (d *documentsScreen) doc(m *model, in screen.Inputs) string {
	item, ok := d.hovered(m, in)
	if !ok {
		return renderDocument("Documents", "_"+d.emptyLine()+"_", "", "")
	}

	read, asked := d.bodies[item.id]
	notice := fmt.Sprintf("_%s, written %s ago_", item.typeName, label.AgeAt(item.updatedAt, in.Room.Now))
	switch {
	case !asked || (read.pending && read.body == ""):
		return renderDocument(item.name, notice, "_reading…_", "")
	case read.err != nil:
		return renderDocument(item.name, notice, "could not read: "+errui.GetReason(read.err), "")
	}

	return renderDocument(item.name, notice, documentProse(item.name, read.body), "")
}

// documentProse is a document's text without the heading that opens it when that heading only
// repeats the name.
func documentProse(name, body string) string {
	body = strings.TrimSpace(body)
	first, rest, _ := strings.Cut(body, "\n")
	heading, isHeading := strings.CutPrefix(first, "# ")
	if isHeading && strings.TrimSpace(heading) == strings.TrimSpace(name) {
		return strings.TrimSpace(rest)
	}

	return body
}

// docRows is the document rendered beside the list at a width: the write stepped to and what it
// changed, or the document as it stands.
func (d *documentsScreen) docRows(m *model, in screen.Inputs, width int) []string {
	if d.kind == documentKindTypes {
		return paint.DocStyledRows(documentTypeFrom(d.hoveredDocumentType(m, in), in.Room.Now), max(width, 1), columnStyle())
	}

	item, ok := d.hovered(m, in)
	if !ok {
		return paint.DocStyledRows(d.doc(m, in), max(width, 1), columnStyle())
	}
	if !d.isSteppingHovered(m, in) {
		return in.Room.Pattern.MarkSaid(item.id, paint.DocStyledRows(d.doc(m, in), max(width, 1), columnStyle()))
	}

	h := d.history
	head := h.head(m, in, item, width)
	if h.err != nil || h.versions == nil {
		return head
	}

	if !h.isRendered(in.Room.Dark) {
		return append(head, h.changeRows(width)...)
	}

	held := diffview.DimCursor
	if d.isDiffFocused(in) {
		held = diffview.LiveCursor
	}

	window := d.diffWindow(m, in)
	window.Width = width

	lines := h.pane.Render(window, held)
	if in.Room.Marks {
		lines = markDiff(&h.pane, window, lines)
	}

	return append(head, lines...)
}

// head is the rows over the change: which write it is and the search.
func (h *documentHistory) head(m *model, in screen.Inputs, item documentItem, width int) []string {
	notice := "_reading its versions…_"
	switch {
	case h.err != nil:
		notice = "could not list its versions: " + errui.GetReason(h.err)
	case h.versions != nil:
		notice = h.notice(in.Room.Now)
	}

	rows := paint.DocStyledRows(renderDocument(item.name, notice, "", ""), width, columnStyle())
	if h.err != nil || h.versions == nil {
		return rows
	}

	count := strings.TrimRight(diffMatchCount(&h.pane, h.filter), "\n")

	return append(rows, h.filter.head(m, count, "", width)...)
}

// notice names the write stepped to, who made it and how long ago.
func (h *documentHistory) notice(now time.Time) string {
	current := h.versions[h.at]
	written := "written"
	if current.by != "" {
		written += " by " + current.by
	}

	notice := fmt.Sprintf("version %d of %d, %s %s ago",
		current.version, h.versions[0].version, written, label.AgeAt(dates.Parse(current.createdAt), now))
	if renamed := h.renamedFrom(); renamed != "" {
		notice += fmt.Sprintf(", renamed from %q", renamed)
	}
	if h.isOldestKeptLate() {
		notice += ", and the writes before it were not kept"
	}

	return "_" + notice + "_"
}

// renamedFrom is the name the write before the one stood on had, where the write stood on renamed
// it, and empty otherwise.
func (h *documentHistory) renamedFrom() string {
	if h.at+1 >= len(h.versions) {
		return ""
	}

	before, current := h.versions[h.at+1].name, h.versions[h.at].name
	if before == "" || current == "" || before == current {
		return ""
	}

	return before
}

// changeRows is what stands in place of the diff while the change is not rendered as one.
func (h *documentHistory) changeRows(width int) []string {
	indent := strings.Repeat(" ", docMargin())

	before, after, ok := h.readChange()
	switch {
	case !ok:
		return []string{fitRow(indent+dimStyle().Render("reading…"), width)}
	case after.err != nil:
		return []string{fitRow(indent+"could not read: "+errui.GetReason(after.err), width)}
	case before.err != nil:
		return []string{fitRow(indent+"could not read: "+errui.GetReason(before.err), width)}
	case h.isOldestKeptLate():
		return paint.DocStyledRows(after.body+"\n", width, columnStyle())
	}

	return []string{fitRow(indent+dimStyle().Render("no change to the text"), width)}
}

// documentTypeFrom is a type's context as the panel renders it beside the list.
func documentTypeFrom(documentType *agentv1.DocumentType, now time.Time) string {
	if documentType == nil {
		return renderDocument("Document types", "_no document types yet_", "", "")
	}

	notice := fmt.Sprintf("_version %d, written %s ago_", documentType.GetVersion(), label.AgeAt(dates.Parse(documentType.GetUpdatedAt()), now))

	return renderDocument(documentType.GetName(), notice, documentProse(documentType.GetName(), documentType.GetContext()), "")
}
