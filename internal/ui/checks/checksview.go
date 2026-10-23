package checks

import (
	"slices"
	"strings"

	"komrad/internal/errui"
	"komrad/internal/glyph"
	"komrad/internal/ui/list"
	"komrad/internal/ui/paint"
	"komrad/internal/ui/paint/label"
	"komrad/internal/ui/screen"
	"komrad/internal/view"
	"komrad/lib/dates"
	"komrad/lib/xslice"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func (c *Screen) emptyLine(in screen.Inputs) string {
	switch {
	case c.err != nil:
		return "could not list: " + errui.GetReason(c.err)
	case pullRequestOf(in.Listing, c.agentID) == "":
		return "no pull request"
	case !c.listed:
		return "reading…"
	case len(c.commit.items) == 0:
		return "no status checks yet"
	}

	switch shown := c.shownScope(); shown {
	case scopeNotPassing:
		return "every status check passes"
	case scopeActive, scopeFailing:
		return "no " + scopeLabels[shown] + " status checks"
	case scopeAll:
	}

	return "no status checks yet"
}

// columns is the gutter of the status checks list: the icon of where each status check stands.
func columns() view.Columns {
	return view.Columns{
		{Kind: view.ColumnText, Width: glyph.Columns},
	}
}

func (c *Screen) snapshot(in screen.Inputs, pane list.Pane) view.Snapshot {
	rows := c.rows(in)

	return view.Snapshot{
		Rows: xslice.Map(rows, func(r checkRow) view.Row {
			return view.Row{
				Key:     r.Key(),
				Kind:    view.KindResult,
				Name:    r.item.name,
				Matches: list.Matches(in.Room.Pattern.Regexp, r.item.name),
				Process: view.ProcessNone,
				Cells: []view.Cell{
					checkIcon(r.item, in.Room.SpinnerFrame),
				},
			}
		}),
		Caret:   pane.Caret,
		Focused: list.KeyAt(rows, c.Row),
		Pane:    pane.Width,
		Columns: columns(),
	}
}

// checkIcon is the icon rendered beside a check, in the glyphs and tones the forge column of
// the sidebar renders a pull request's checks with.
func checkIcon(item check, loaderFrame string) view.Cell {
	switch {
	case isUnknown(item):
		return view.Cell{Glyph: glyph.StatusCheckUnknown, Tone: paint.ToneMuted}
	case isInFlight(item):
		return view.Cell{Text: loaderFrame, Tone: paint.ToneWarning}
	case item.passed && strings.EqualFold(item.conclusion, "success"):
		return view.Cell{Glyph: glyph.StatusCheckPassing, Tone: paint.ToneSuccess}
	case item.passed:
		return view.Cell{Glyph: glyph.StatusCheckSkipped, Tone: paint.ToneMuted}
	}

	return view.Cell{Glyph: glyph.StatusCheckFailing, Tone: paint.ToneDanger}
}

func (c *Screen) docRows(in screen.Inputs, width int) []string {
	rows := c.rows(in)
	if c.Row < 0 || c.Row >= len(rows) {
		return nil
	}

	item := rows[c.Row].item
	icon := func(item check) string {
		cell := checkIcon(item, in.Room.SpinnerFrame)
		text := cell.Text
		if text == "" {
			text = c.deps.Glyphs.Spell(cell.Glyph)
		}

		return paint.Panel.Style(cell.Tone).Render(text)
	}

	var notice []string
	switch duration := dates.Between(item.startedAt, item.finishedAt); {
	case isUnknown(item):
		notice = append(notice, "unknown")
	case duration != "":
		notice = append(notice, duration)
	}
	if len(item.steps) > 0 {
		notice = append(notice, label.Count(len(item.steps), "step", "steps"))
	}

	out := []string{paint.FitRow(icon(item)+" "+lipgloss.NewStyle().Bold(true).Render(item.name), width)}
	if len(notice) > 0 {
		out = append(out, paint.FitRow(paint.Panel.Style(paint.ToneMuted).Render(strings.Join(notice, separated)), width))
	}
	out = append(out, "")
	if len(item.steps) == 0 {
		return append(out, paint.FitRow(paint.Panel.Style(paint.ToneMuted).Render("no steps reported"), width))
	}

	focal := slices.IndexFunc(item.steps, func(step check) bool { return !step.passed })
	for i, step := range item.steps {
		lead := icon(step) + " "
		duration := dates.Between(step.startedAt, step.finishedAt)
		name := ansi.Truncate(step.name, max(width-lipgloss.Width(lead)-lipgloss.Width(duration)-1, 0), "…")
		if i == focal {
			name = lipgloss.NewStyle().Bold(true).Render(name)
		}

		left := lead + name
		gap := max(width-lipgloss.Width(left)-lipgloss.Width(duration), 1)
		out = append(out, paint.FitRow(left+strings.Repeat(" ", gap)+paint.Panel.Style(paint.ToneMuted).Render(duration), width))
	}

	return out
}
