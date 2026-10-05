package ui

import (
	"slices"
	"strings"

	"komrad/internal/errui"
	"komrad/internal/glyph"
	"komrad/internal/ui/paint"
	"komrad/internal/ui/paint/label"
	"komrad/internal/view"
	"komrad/lib/dates"
	"komrad/lib/xslice"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func (c *statusChecksScreen) view(m *model) string {
	return m.columnListView(c.listParams(m))
}

func (c *statusChecksScreen) emptyLine(m *model) string {
	switch {
	case c.err != nil:
		return "could not list: " + errui.GetReason(c.err)
	case m.statusChecksPullRequestID(c.agentID) == "":
		return "no pull request"
	case !c.listed:
		return "reading…"
	case len(c.commit.items) == 0:
		return "no status checks yet"
	}

	switch scope := c.shownScope(); scope {
	case statusCheckScopeNotPassing:
		return "every status check passes"
	case statusCheckScopeActive, statusCheckScopeFailing:
		return "no " + statusCheckScopeLabels[scope] + " status checks"
	case statusCheckScopeAll:
	}

	return "no status checks yet"
}

// statusCheckColumns is the gutter of the status checks list: the icon of where each status check stands.
func statusCheckColumns() view.Columns {
	return view.Columns{
		{Kind: view.ColumnText, Width: glyph.Columns},
	}
}

func (c *statusChecksScreen) snapshot(m *model, pane listPane) view.Snapshot {
	rows := c.rows(m)

	return view.Snapshot{
		Rows: xslice.Map(rows, func(r statusCheckRow) view.Row {
			return view.Row{
				Key:     r.key(),
				Kind:    view.KindResult,
				Name:    r.item.name,
				Matches: matchesIn(m.filter.pattern, r.item.name),
				Process: view.ProcessNone,
				Cells: []view.Cell{
					statusCheckIcon(r.item, m.loading.frame()),
				},
			}
		}),
		Caret:      pane.caret,
		BlinkedOut: pane.blinkedOut,
		Focused:    rowKeyAt(rows, c.cursor),
		Pane:       pane.width,
		Columns:    statusCheckColumns(),
	}
}

// statusCheckIcon is the icon drawn beside a check, in the glyphs and tones the forge column of
// the sidebar draws a pull request's checks with.
func statusCheckIcon(item statusCheckItem, loaderFrame string) view.Cell {
	switch {
	case isStatusCheckUnknown(item):
		return view.Cell{Glyph: glyph.StatusCheckUnknown, Tone: paint.ToneMuted}
	case isStatusCheckInFlight(item):
		return view.Cell{Text: loaderFrame, Tone: paint.ToneWarning}
	case strings.EqualFold(item.conclusion, "success"):
		return view.Cell{Glyph: glyph.StatusCheckPassing, Tone: paint.ToneSuccess}
	case slices.Contains(skippedConclusions, strings.ToLower(item.conclusion)):
		return view.Cell{Glyph: glyph.StatusCheckSkipped, Tone: paint.ToneMuted}
	}

	return view.Cell{Glyph: glyph.StatusCheckFailing, Tone: paint.ToneDanger}
}

func (c *statusChecksScreen) docRows(m *model, width int) []string {
	rows := c.rows(m)
	if c.cursor < 0 || c.cursor >= len(rows) {
		return nil
	}

	item := rows[c.cursor].item
	indent := strings.Repeat(" ", docMargin())
	icon := func(item statusCheckItem) string {
		cell := statusCheckIcon(item, m.loading.frame())
		text := cell.Text
		if text == "" {
			text = m.glyphs.Spell(cell.Glyph)
		}

		return paint.Panel.Style(cell.Tone).Render(text)
	}

	var notice []string
	switch duration := dates.Between(item.startedAt, item.finishedAt); {
	case isStatusCheckUnknown(item):
		notice = append(notice, "unknown")
	case duration != "":
		notice = append(notice, duration)
	}
	if len(item.steps) > 0 {
		notice = append(notice, label.Count(len(item.steps), "step", "steps"))
	}

	out := []string{fitRow(indent+icon(item)+" "+lipgloss.NewStyle().Bold(true).Render(item.name), width)}
	if len(notice) > 0 {
		out = append(out, fitRow(indent+dimStyle().Render(strings.Join(notice, separated)), width))
	}
	out = append(out, "")
	if len(item.steps) == 0 {
		return append(out, fitRow(indent+dimStyle().Render("no steps reported"), width))
	}

	focal := slices.IndexFunc(item.steps, func(step statusCheckItem) bool { return !isStatusCheckClean(step) })
	for i, step := range item.steps {
		lead := indent + icon(step) + " "
		duration := dates.Between(step.startedAt, step.finishedAt)
		name := ansi.Truncate(step.name, max(width-lipgloss.Width(lead)-lipgloss.Width(duration)-docMargin()-1, 0), "…")
		if i == focal {
			name = lipgloss.NewStyle().Bold(true).Render(name)
		}

		left := lead + name
		gap := max(width-lipgloss.Width(left)-lipgloss.Width(duration)-docMargin(), 1)
		out = append(out, fitRow(left+strings.Repeat(" ", gap)+dimStyle().Render(duration), width))
	}

	return out
}
