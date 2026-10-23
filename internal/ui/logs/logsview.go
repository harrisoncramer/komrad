package logs

import (
	"fmt"
	"strconv"

	"komrad/internal/errui"
	"komrad/internal/glyph"
	"komrad/internal/logread"
	"komrad/internal/ui/filter"
	"komrad/internal/ui/list"
	"komrad/internal/ui/paint"
	"komrad/internal/ui/screen"
	"komrad/internal/ui/zone"
	"komrad/internal/view"
	"komrad/lib/protoenum"
	"komrad/lib/xslice"
	logv1 "komrad/proto/gen/log/v1"
)

// Zone names the lines of the log, each keyed by its line number.
const Zone zone.Name = "log"

func (l *Screen) emptyLine() string {
	if l.err != nil && !l.listed {
		return "could not list: " + errui.GetReason(l.err)
	}

	return "reading…"
}

func (l *Screen) snapshot(in screen.Inputs, pane list.Pane) view.Snapshot {
	rows := l.rows(in)
	caret := pane.Caret
	if l.IsBlockFocused(in) {
		caret = view.CaretRendered
	}

	return view.Snapshot{
		Rows: xslice.Map(rows, func(row sourceRow) view.Row {
			name := sourceRowName(row)
			var tail view.Cell
			if row.failed {
				tail = view.Cell{Glyph: glyph.FailedScript, Tone: paint.ToneDanger}
			}

			return view.Row{
				Key:     row.Key(),
				Kind:    view.KindResult,
				Name:    name,
				Matches: list.Matches(in.Room.Pattern.Regexp, name),
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

	return fmt.Sprintf("%03d %s %s", source.GetRun(), protoenum.Name(source.GetTrigger()), source.GetScript())
}

// block is the log rendered at the width and height of the block beside the sidebar, its lines
// pinned to the prompt at its foot.
func (l *Screen) block(in screen.Inputs, width int) []string {
	split := in.Room.Split
	if !split.On {
		return nil
	}

	height := max(split.Rows-1, 1)
	rows := l.logRows(in, width, height)
	if l.read && l.viewer.Len() > 0 {
		rows = append(make([]string, max(height-len(rows), 0)), rows...)
	}
	for len(rows) < height {
		rows = append(rows, "")
	}

	search := paint.FitRow(list.Blank+l.logFilterLine(l.deps.Glyphs), width)
	if in.Room.Marks {
		search = zone.MarkKey(zone.Search, width, 0, zone.SearchBlock) + search
	}

	return append(rows, search)
}

// logFilterLine is the prompt at the foot of the log, gray while it does not hold the keys.
func (l *Screen) logFilterLine(glyphs glyph.Set) string {
	if l.logFilter.IsTyping {
		return l.logFilter.Line(glyphs)
	}

	return paint.Panel.Style(paint.ToneMuted).Render(filter.ModeFilter.Glyph(glyphs) + " " + l.logFilter.Query())
}

// logRows is the lines of the log rendered in height rows, or what stands in for them.
func (l *Screen) logRows(in screen.Inputs, width, height int) []string {
	row, hovered := list.At(l.rows(in), l.Row)

	switch {
	case !hovered:
		return nil
	case !l.read && l.err != nil:
		return []string{paint.Panel.Style(paint.ToneMuted).Render("could not read: " + errui.GetReason(l.err))}
	case !l.read:
		return []string{paint.Panel.Style(paint.ToneMuted).Render("reading…")}
	case l.viewer.Len() == 0 && l.pattern != "":
		return []string{paint.Panel.Style(paint.ToneMuted).Render(fmt.Sprintf("Nothing matching in the last %d MB.", logread.ScanBudget>>20))}
	case l.viewer.Len() == 0 && row.source.Run != nil:
		return []string{paint.Panel.Style(paint.ToneMuted).Render("This run wrote nothing.")}
	case l.viewer.Len() == 0:
		return []string{paint.Panel.Style(paint.ToneMuted).Render(fmt.Sprintf("Nothing about this agent in the last %d MB of the log.", logread.ScanBudget>>20))}
	}

	rows, cursor := l.viewer.Rows(width-len(list.Blank), height)
	var lines []int
	if in.Room.Marks {
		lines = l.viewer.Lines(width-len(list.Blank), height)
	}
	focused := l.IsBlockFocused(in)
	for i, row := range rows {
		prefix := list.Blank
		if focused && i == cursor {
			prefix = list.RenderArrow()
		}
		rows[i] = prefix + row
		if i < len(lines) {
			rows[i] = zone.MarkKey(Zone, width, 0, strconv.Itoa(lines[i])) + rows[i]
		}
	}

	return rows
}
