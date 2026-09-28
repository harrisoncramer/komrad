package ui

import (
	"slices"
	"strings"

	"komrad/config"
	"komrad/internal/ui/paint"
	"komrad/internal/ui/reader"
	"komrad/internal/view"

	tea "charm.land/bubbletea/v2"
)

// documentReading is where the reader stands in the document drawn beside the list, and what
// they searched it for.
type documentReading struct {
	reader.Reader
	// filter is the pattern being searched for in the document.
	filter filterState
	// origin is the row the cursor was on when the search opened.
	origin int
	// said is what the last key on the document said.
	said string
}

// isReadDrawn reports whether the document under the cursor is drawn as it stands beside the
// list.
func (d *documentsScreen) isReadDrawn(s panelShared) bool {
	if _, ok := s.draw.screenColumnSplit(); !ok || d.editing != nil {
		return false
	}

	_, hovered := d.hovered(s)

	return hovered && !d.isSteppingHovered(s)
}

// isReadFocused reports whether the document as it stands holds the keys.
func (d *documentsScreen) isReadFocused(s panelShared) bool {
	return d.focus && d.history == nil && d.isReadDrawn(s) && s.draw.isListFocused()
}

// readRows is the document as it stands, rendered a cursor's width in from the edge of the block.
func (d *documentsScreen) readRows(s panelShared, width int) []string {
	return paint.DocStyledRows(d.doc(s), max(width-len(blankCursor), 1), columnStyle())
}

// readHead is the rows over the document: the search and what the last key said.
func (d *documentsScreen) readHead(s panelShared, rows []string, width int) []string {
	r := d.reading

	var head []string
	if r.filter.typing || r.filter.isStanding() {
		line := blankCursor + r.filter.line(paint.Panel, s.glyphs)
		if r.filter.isStanding() {
			line += dimStyle.Render("  " + reader.Count(reader.Matches(r.filter.pattern, rows), r.Line))
		}
		head = append(head, fitRow(line, width))
	}

	if r.said != "" {
		head = append(head, fitRow(blankCursor+dimStyle.Render(r.said), width))
	}

	if len(head) > 0 {
		head = append(head, "")
	}

	return head
}

// readBlock is the document as it stands at the width of the block beside the list, under the
// search over it, from where the reader has moved to.
func (d *documentsScreen) readBlock(s panelShared, width int, caret view.Caret) []string {
	rows, height, ok := d.readLayout(s)
	if !ok {
		rows, height = d.readRows(s, width), 1
	}

	r := d.reading
	focused := d.isReadFocused(s)
	drawn := make([]string, len(rows))
	for i, row := range rows {
		prefix := blankCursor
		if focused && i == r.Line {
			prefix = caretGlyph(caret)
		}

		drawn[i] = prefix + paint.Panel.HighlightStyled(row, reader.Spans(r.filter.pattern, row))
	}

	window := docWindow{rows: drawn, top: r.Top, markers: true}

	return append(d.readHead(s, rows, width), strings.Split(window.view(height), "\n")...)
}

// readLayout is the document as it stands rendered for the block, and the rows of the window
// under the search.
func (d *documentsScreen) readLayout(s panelShared) ([]string, int, bool) {
	split, ok := s.draw.screenColumnSplit()
	if !ok {
		return nil, 0, false
	}

	rows := d.readRows(s, split.column)

	return rows, max(split.rows-len(d.readHead(s, rows, split.column)), 1), true
}

// moveReadTo puts the cursor on a row of the document and brings the window round to it.
func (d *documentsScreen) moveReadTo(s panelShared, line int) {
	if rows, height, ok := d.readLayout(s); ok {
		d.reading.MoveTo(line, len(rows), height)
	}
}

// focusRead hands the keys to the document, keeping the cursor on a row the window draws.
func (d *documentsScreen) focusRead(s panelShared) {
	d.focus = true
	if rows, height, ok := d.readLayout(s); ok {
		d.reading.Settle(len(rows), height)
	}
}

// scrollRead moves the cursor through the document while it holds the keys, and the window over
// it otherwise.
func (d *documentsScreen) scrollRead(s panelShared, delta int) tea.Cmd {
	if d.isReadFocused(s) {
		d.moveReadTo(s, d.reading.Line+delta)

		return nil
	}

	if rows, height, ok := d.readLayout(s); ok {
		d.reading.Scroll(delta, len(rows), height)
	}

	return nil
}

// updateReadFilter is the search prompt over the document taking keys, moving to the first match
// from where it opened as the pattern grows.
func (d *documentsScreen) updateReadFilter(h host, s panelShared, key tea.KeyPressMsg) tea.Cmd {
	if key.String() == "ctrl+c" {
		return h.hide()
	}

	r := &d.reading
	if !r.filter.takeKey(key) {
		return nil
	}

	if !r.filter.isStanding() {
		d.moveReadTo(s, r.origin)

		return nil
	}

	rows, _, ok := d.readLayout(s)
	if !ok {
		return nil
	}

	if line, found := reader.From(reader.Matches(r.filter.pattern, rows), r.origin); found {
		d.moveReadTo(s, line)
	}

	return nil
}

// jumpReadMatch steps to the next or previous row the search found, wrapping at the ends.
func (d *documentsScreen) jumpReadMatch(s panelShared, direction int) tea.Cmd {
	r := &d.reading
	rows, _, ok := d.readLayout(s)
	if !ok {
		return nil
	}

	line, found := reader.Jump(reader.Matches(r.filter.pattern, rows), r.Line, direction)
	if !found {
		r.said = "nothing to search for yet"
		if r.filter.isStanding() {
			r.said = "no matches"
		}

		return nil
	}

	d.moveReadTo(s, line)

	return nil
}

// readActions is the keys on the document as it stands once the cycle key has handed them to it.
func (d *documentsScreen) readActions() screenActionSpec {
	step := d.stepActions()

	return screenActionSpec{
		table: slices.Concat([]screenAction{
			{
				id:   actionMove,
				keys: func(b config.Keybindings) []string { return slices.Concat(b.Nav.Up, b.Nav.Down) },
				run: func(_ host, s panelShared, pressed string) tea.Cmd {
					d.moveReadTo(s, d.reading.Line+directionOf(pressed, s.keys.Nav.Up))

					return nil
				},
			},
			{
				id:   actionNavPage,
				keys: func(b config.Keybindings) []string { return slices.Concat(b.Nav.PageUp, b.Nav.PageDown) },
				run: func(_ host, s panelShared, pressed string) tea.Cmd {
					d.moveReadTo(s, d.reading.Line+directionOf(pressed, s.keys.Nav.PageUp)*navPageStep)

					return nil
				},
			},
			{
				id:   actionColumnScroll,
				keys: func(b config.Keybindings) []string { return slices.Concat(b.Column.ScrollDown, b.Column.ScrollUp) },
				run: func(_ host, s panelShared, pressed string) tea.Cmd {
					return d.scrollRead(s, directionOf(pressed, s.keys.Column.ScrollUp)*columnWheelStep)
				},
			},
			{
				id:   actionJump,
				keys: func(b config.Keybindings) []string { return slices.Concat(b.Nav.Top, b.Nav.Bottom) },
				run: func(_ host, s panelShared, pressed string) tea.Cmd {
					line := 0
					if rows, _, ok := d.readLayout(s); ok && isBound(pressed, s.keys.Nav.Bottom) {
						line = len(rows) - 1
					}
					d.moveReadTo(s, line)

					return nil
				},
			},
			{
				id:   actionFilterOpen,
				keys: func(config.Keybindings) []string { return []string{filterOpenKey} },
				run: func(host, panelShared, string) tea.Cmd {
					d.reading.filter.typing = true
					d.reading.filter.setQuery("")
					d.reading.origin = d.reading.Line

					return nil
				},
			},
			{
				id:   actionFilterMatch,
				keys: func(b config.Keybindings) []string { return slices.Concat(b.Filter.NextMatch, b.Filter.PrevMatch) },
				run: func(_ host, s panelShared, pressed string) tea.Cmd {
					return d.jumpReadMatch(s, directionOf(pressed, s.keys.Filter.PrevMatch))
				},
			},
		}, step.table),
		legend: slices.Concat([]screenLegendRow{
			diffMoveRow(),
			diffPageRow(),
			diffSearchRow(),
			{
				of:   []actionID{actionFilterMatch},
				desc: func(panelShared) string { return "next/previous match" },
				label: func(k config.Keybindings) string {
					return headLabel(k.Filter.NextMatch) + "/" + headLabel(k.Filter.PrevMatch)
				},
			},
			diffJumpRow(),
		}, step.legend),
	}
}
