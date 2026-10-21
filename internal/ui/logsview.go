package ui

import (
	"fmt"
	"strconv"

	"komrad/internal/errui"
	"komrad/internal/glyph"
	"komrad/internal/logread"
	"komrad/internal/ui/paint"
	"komrad/internal/view"
	"komrad/lib/xslice"
	logv1 "komrad/proto/gen/log/v1"
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

func (l *logsScreen) snapshot(m *model, pane listPane) view.Snapshot {
	rows := l.rows(m)
	caret := pane.caret
	if l.isBlockFocused(m) {
		caret = view.CaretRendered
	}

	return view.Snapshot{
		Rows: xslice.Map(rows, func(row logSourceRow) view.Row {
			name := logSourceRowName(row)
			var tail view.Cell
			if row.failed {
				tail = view.Cell{Glyph: glyph.FailedScript, Tone: paint.ToneDanger}
			}

			return view.Row{
				Key:     row.key(),
				Kind:    view.KindResult,
				Name:    name,
				Matches: matchesIn(m.filter.pattern, name),
				Process: view.ProcessNone,
				Tail:    tail,
			}
		}),
		Caret:   caret,
		Focused: rowKeyAt(rows, l.rowCursor.cursor),
		Pane:    pane.width,
		Columns: view.Columns{},
	}
}

// nameSource is what a source is called, led by the agent that ran it when the screen lists a task's runs.
func (l *logsScreen) nameSource(m *model, source *logv1.LogSource) string {
	if l.taskID == "" {
		return sourceLabel(source)
	}

	name := source.GetAgentId()
	if item, ok := m.listing.anyAgent(name); ok {
		name = m.listing.agentName(item.Agent)
	}

	return name + " " + sourceLabel(source)
}

func sourceLabel(source *logv1.LogSource) string {
	if source.Run == nil {
		return "server"
	}

	return fmt.Sprintf("%03d %s %s", source.GetRun(), source.GetTrigger(), source.GetScript())
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

	return append(rows, m.markSearch(stopBlock, width, fitRow(blankCursor+l.logFilterLine(m.glyphs), width)))
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
	var lines []int
	if m.term.markingZones {
		lines = l.viewer.Lines(width-len(blankCursor), height)
	}
	focused := l.isBlockFocused(m)
	for i, row := range rows {
		prefix := blankCursor
		if focused && i == cursor {
			prefix = renderedCaret()
		}
		rows[i] = prefix + row
		if i < len(lines) {
			rows[i] = m.markRow(zoneLog, width, strconv.Itoa(lines[i]), rows[i])
		}
	}

	return rows
}
