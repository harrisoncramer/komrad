package ui

import (
	"fmt"

	"komrad/internal/errui"
	"komrad/internal/glyph"
	"komrad/internal/logread"
	"komrad/internal/ui/paint"
	"komrad/internal/view"
	"komrad/lib/xslice"
)

func (l *logsScreen) view(m *model) string {
	return m.columnListView(l.listParams(m))
}

func (l *logsScreen) emptyLine() string {
	if l.err != nil && !l.listed {
		return "could not list: " + errui.GetReason(l.err)
	}

	return "reading…"
}

// lines is the list as rendered: each source's name behind the cursor, the failed run marked after it.
func (l *logsScreen) lines(m *model, pane listPane) []string {
	rows := l.rows(m)
	if len(rows) == 0 {
		return []string{dimStyle().Render("  " + emptyLineOf(m.filter, l.emptyLine()))}
	}

	caret := pane.caret
	if l.isBlockFocused(m) {
		caret = view.CaretRendered
	}

	return xslice.Map(rows, func(row logSourceRow) string {
		hovered := row.key() == rowKeyAt(rows, l.rowCursor.cursor)
		prefix := "  "
		if hovered {
			prefix = caretGlyph(caret)
		}

		line := prefix + " " + m.filter.highlight(paint.Panel, clipName(logSourceRowName(row)), nameStyle(hovered && caret != view.CaretAbsent, false))
		if row.failed {
			line += " " + paint.Panel.Style(paint.ToneDanger).Render(m.glyphs.Spell(glyph.FailedScript))
		}

		return line
	})
}

// block is the log rendered at the width and height of the block beside the sidebar, its lines
// pinned to the prompt at its foot.
func (l *logsScreen) block(m *model, width int) []string {
	split, ok := m.screenColumnSplit()
	if !ok {
		return nil
	}

	height := max(split.rows-1, 1)
	rows := l.logRows(m, width, height)
	if l.read && l.viewer.Len() > 0 {
		rows = append(make([]string, max(height-len(rows), 0)), rows...)
	}
	for len(rows) < height {
		rows = append(rows, "")
	}

	return append(rows, fitRow(blankCursor+l.logFilterLine(m.glyphs), width))
}

// logFilterLine is the prompt at the foot of the log, gray while it does not hold the keys.
func (l *logsScreen) logFilterLine(glyphs glyph.Set) string {
	if l.logFilter.typing {
		return l.logFilter.line(glyphs)
	}

	return dimStyle().Render(filterModeFilter.glyph(glyphs) + " " + l.logFilter.query())
}

// logRows is the lines of the log rendered in height rows, or what stands in for them.
func (l *logsScreen) logRows(m *model, width, height int) []string {
	row, hovered := rowAt(l.rows(m), l.rowCursor.cursor)

	switch {
	case !hovered:
		return nil
	case !l.read && l.err != nil:
		return []string{dimStyle().Render("could not read: " + errui.GetReason(l.err))}
	case !l.read:
		return []string{dimStyle().Render("reading…")}
	case l.viewer.Len() == 0 && l.pattern != "":
		return []string{dimStyle().Render(fmt.Sprintf("Nothing matching in the last %d MB.", logread.ScanBudget>>20))}
	case l.viewer.Len() == 0 && row.source.Run != nil:
		return []string{dimStyle().Render("This run wrote nothing.")}
	case l.viewer.Len() == 0:
		return []string{dimStyle().Render(fmt.Sprintf("Nothing about this agent in the last %d MB of the log.", logread.ScanBudget>>20))}
	}

	rows, cursor := l.viewer.Rows(width-len(blankCursor), height)
	focused := l.isBlockFocused(m)
	for i, row := range rows {
		prefix := blankCursor
		if focused && i == cursor {
			prefix = renderedCaret()
		}
		rows[i] = prefix + row
	}

	return rows
}
