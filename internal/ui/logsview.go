package ui

import (
	"fmt"
	"strconv"

	"komrad/internal/errui"
	"komrad/internal/glyph"
	"komrad/internal/logread"
	"komrad/internal/ui/list"
	"komrad/internal/ui/paint"
	"komrad/internal/ui/screen"
	"komrad/internal/view"
	"komrad/lib/xslice"
	logv1 "komrad/proto/gen/log/v1"
)

func (l *logsScreen) emptyLine() string {
	if l.err != nil && !l.listed {
		return "could not list: " + errui.GetReason(l.err)
	}

	return "reading…"
}

func (l *logsScreen) snapshot(m *model, in screen.Inputs, pane list.Pane) view.Snapshot {
	rows := l.rows(m, in)
	caret := pane.Caret
	if l.isBlockFocused(m, in) {
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
				Key:     row.Key(),
				Kind:    view.KindResult,
				Name:    name,
				Matches: matchesIn(in.Room.Pattern.Regexp, name),
				Process: view.ProcessNone,
				Tail:    tail,
			}
		}),
		Caret:   caret,
		Focused: list.KeyAt(rows, l.Row),
		Pane:    pane.Width,
		Columns: view.Columns{},
	}
}

func sourceLabel(source *logv1.LogSource) string {
	if source.Run == nil {
		return "server"
	}

	return fmt.Sprintf("%03d %s %s", source.GetRun(), source.GetTrigger(), source.GetScript())
}

// block is the log rendered at the width and height of the block beside the sidebar, its lines
// pinned to the prompt at its foot.
func (l *logsScreen) block(m *model, in screen.Inputs, width int) []string {
	split := in.Room.Split
	if !split.On {
		return nil
	}

	height := max(split.Rows-1, 1)
	rows := l.logRows(m, in, width, height)
	if l.read && l.viewer.Len() > 0 {
		rows = append(make([]string, max(height-len(rows), 0)), rows...)
	}
	for len(rows) < height {
		rows = append(rows, "")
	}

	search := fitRow(list.Blank+l.logFilterLine(l.deps.glyphs), width)
	if in.Room.Marks {
		search = searchMark(stopBlock, width, search)
	}

	return append(rows, search)
}

// logFilterLine is the prompt at the foot of the log, gray while it does not hold the keys.
func (l *logsScreen) logFilterLine(glyphs glyph.Set) string {
	if l.logFilter.typing {
		return l.logFilter.line(glyphs)
	}

	return dimStyle().Render(filterModeFilter.glyph(glyphs) + " " + l.logFilter.query())
}

// logRows is the lines of the log rendered in height rows, or what stands in for them.
func (l *logsScreen) logRows(m *model, in screen.Inputs, width, height int) []string {
	row, hovered := list.At(l.rows(m, in), l.Row)

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

	rows, cursor := l.viewer.Rows(width-len(list.Blank), height)
	var lines []int
	if in.Room.Marks {
		lines = l.viewer.Lines(width-len(list.Blank), height)
	}
	focused := l.isBlockFocused(m, in)
	for i, row := range rows {
		prefix := list.Blank
		if focused && i == cursor {
			prefix = list.RenderArrow()
		}
		rows[i] = prefix + row
		if i < len(lines) {
			rows[i] = markKeyedRow(zoneLog, width, strconv.Itoa(lines[i]), rows[i])
		}
	}

	return rows
}
