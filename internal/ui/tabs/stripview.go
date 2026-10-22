package tabs

import (
	"strconv"
	"strings"

	"komrad/internal/glyph"
	"komrad/internal/ui/paint"

	"github.com/charmbracelet/x/ansi"
)

func (s Stops) parts() []string {
	muted := paint.Panel.Style(paint.ToneMuted)
	lit := paint.Panel.Style(paint.TonePrimary)

	parts := make([]string, len(s.Labels))
	for i := range s.Labels {
		style := muted
		switch i {
		case s.Focus:
			style = paint.Panel.Cursor()
		case s.Lit:
			style = lit
		}
		parts[i] = style.Render(s.Labels[i])
		if s.Mark != nil {
			parts[i] = s.Mark(i, parts[i])
		}
	}

	return parts
}

// View is the row of stops on one line.
func (s Stops) View() string {
	return strings.Join(s.parts(), "  ")
}

func (s Strip) groupLabel(state State) string {
	if state.Flat {
		return FlatLabel
	}

	return s.Grouped
}

// View is the strip on one line, cut to the width where one is given.
func (s Strip) View(state State, width int, mark func(int, string) string) string {
	muted := paint.Panel.Style(paint.ToneMuted)
	chosen := s.IndexOf(state)

	labels := make([]string, len(s.Options), len(s.Options)+1)
	for i, option := range s.Options {
		labels[i] = option.Label
		if i == chosen {
			labels[i] = state.Direction.glyph() + " " + option.Label
		}
	}
	if s.Grouped != "" {
		labels = append(labels, s.groupLabel(state))
	}

	row := Stops{
		Labels: labels,
		Focus:  NoStop,
		Lit:    NoStop,
		Mark:   mark,
	}
	if state.Focused {
		row.Focus = s.focusIndex(state)
	}
	parts := row.parts()

	line := muted.Render(s.Title) + "   " + strings.Join(parts[:len(s.Options)], "  ")
	if s.Grouped != "" {
		line += "  " + muted.Render(glyph.Separator) + "  " + parts[len(s.Options)]
	}
	if width > 0 {
		line = ansi.Truncate(line, width, "…")
	}

	return line
}

// StopKeys is the zone key of each stop, its place on the strip.
func (s Strip) StopKeys() []string {
	keys := make([]string, s.CountStops())
	for i := range keys {
		keys[i] = strconv.Itoa(i)
	}

	return keys
}
