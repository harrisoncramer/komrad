package ui

import (
	"strings"

	"komrad/internal/glyph"
	"komrad/internal/ui/paint"
	"komrad/internal/ui/screen"

	"github.com/charmbracelet/x/ansi"
)

const (
	// sideMarkBlank holds the cursor's mark's place at the start of each row over the file tree.
	sideMarkBlank = "  "
	// descriptionIndent sets a description's lines in under its title.
	descriptionIndent = "  "
)

// sideView is the rows over the file tree cut to lines rows, windowed on the row the cursor is on.
func (r reviewScreen) sideView(m *model, in screen.Inputs, lines int) string {
	sideRows := r.sideRows(m, in)
	if len(sideRows) == 0 || lines == 0 {
		return ""
	}

	width := lineWidthOf(in.Room.Width)
	focused := r.isBlockFocused(m, in)
	rows := make([]string, 0, len(sideRows))
	keys := make([]string, 0, len(sideRows))
	cursor := 0
	for at, row := range sideRows {
		current := r.side != nil && *r.side == row.sideCursor && row.kind.isStop()
		if current {
			cursor = at
		}

		rows = append(rows, ansi.Truncate(r.sideRowView(row, current && focused), max(width, 0), "…"))
		keys = append(keys, row.key())
	}

	if mark := m.spanMarker(zoneSide, keys); mark != nil {
		for at, row := range rows {
			if sideRows[at].kind.isStop() {
				rows[at] = mark(at, row)
			}
		}
	}

	if lines < 0 || lines >= len(rows) {
		return strings.Join(rows, "\n") + "\n"
	}

	return paint.RenderList(rows, cursor, lines, dimStyle())
}

// sideRowView is one row over the file tree, lit while the cursor is on it.
func (r reviewScreen) sideRowView(row sideRow, lit bool) string {
	text := row.text
	switch row.kind {
	case sideBlank:
		return ""
	case sideHeading:
		return paint.Panel.Title(text)
	case sideTask, sideThisTask:
		if row.depth > 0 {
			text = strings.Repeat("  ", row.depth-1) + glyph.Child + " " + text
		}
		if row.kind == sideThisTask {
			text = paint.Panel.Style(reviewScopeOnTone).Render(text)
		}
	case sideWaiting:
		text = dimStyle().Italic(true).Render(text)
	case sideDescription:
		mark := glyph.FolderClosed
		if r.openDescription {
			mark = glyph.FolderOpen
		}
		text = r.deps.glyphs.Spell(mark) + " " + text
	case sideDescriptionLine:
		text = dimStyle().Render(descriptionIndent + text)
	case sideForge, sideNeed:
	}

	if row.note != "" {
		text += "  " + dimStyle().Render(row.note)
	}

	prefix := sideMarkBlank
	if lit {
		prefix = paint.Panel.Style(paint.TonePrimary).Bold(true).Render(glyph.Focus) + " "
	}

	return prefix + text
}
