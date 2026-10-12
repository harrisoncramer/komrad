package ui

import (
	"strings"

	"komrad/internal/glyph"
	"komrad/internal/ui/paint"

	"github.com/charmbracelet/x/ansi"
)

func (s stops) parts() []string {
	muted := paint.Panel.Style(paint.ToneMuted)
	lit := paint.Panel.Style(paint.TonePrimary)

	parts := make([]string, len(s.labels))
	for i := range s.labels {
		style := muted
		switch i {
		case s.focus:
			style = paint.Panel.Cursor()
		case s.lit:
			style = lit
		}
		parts[i] = style.Render(s.labels[i])
		if s.mark != nil {
			parts[i] = s.mark(i, parts[i])
		}
	}

	return parts
}

func (s stops) view() string {
	return strings.Join(s.parts(), "  ")
}

func (s tabStrip) groupLabel(state tabsState) string {
	if state.flat {
		return flatLabel
	}

	return s.grouped
}

func (s tabStrip) view(state tabsState, width int) string {
	muted := paint.Panel.Style(paint.ToneMuted)
	chosen := s.indexOf(state)

	labels := make([]string, len(s.options), len(s.options)+1)
	for i, option := range s.options {
		labels[i] = option.label
		if i == chosen {
			labels[i] = state.direction.glyph() + " " + option.label
		}
	}
	if s.grouped != "" {
		labels = append(labels, s.groupLabel(state))
	}

	row := stops{
		labels: labels,
		focus:  noStop,
		lit:    noStop,
	}
	if state.focused {
		row.focus = s.focusIndex(state)
	}
	parts := row.parts()

	line := muted.Render(s.title) + "   " + strings.Join(parts[:len(s.options)], "  ")
	if s.grouped != "" {
		line += "  " + muted.Render(glyph.Separator) + "  " + parts[len(s.options)]
	}
	if width > 0 {
		line = ansi.Truncate(line, width, "…")
	}

	return line
}

func (m *model) tabbedHeader(title, tally string) string {
	var strips string
	if m.isTabsRendered() {
		strips += m.tabsLine()
	}
	if m.isKindsRendered() {
		strips += m.kindsLine()
	}
	if strips == "" {
		return m.talliedHeader(title, tally)
	}

	return m.headerLines(title, tally, strips)
}

func (m *model) tabsLine() string {
	t, _ := m.activeTabs()

	return t.tabs().view(*t.tabsState(), m.paneWidth()) + "\n"
}
