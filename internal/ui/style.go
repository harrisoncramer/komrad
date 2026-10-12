package ui

import (
	"komrad/internal/glyph"
	"komrad/internal/ui/paint"
	"komrad/internal/view"

	"charm.land/lipgloss/v2"
)

const nameWidth = 30

const separated = " " + glyph.Separator + " "

func dimStyle() lipgloss.Style {
	return paint.Panel.Style(paint.ToneMuted)
}

func rowCursorStyle() lipgloss.Style {
	return paint.Panel.Cursor()
}

func scheduleIcon(enabled bool) view.Cell {
	if enabled {
		return view.Cell{Glyph: glyph.ScheduleEnabled, Tone: scheduleTone(enabled)}
	}

	return view.Cell{Glyph: glyph.ScheduleDisabled, Tone: scheduleTone(enabled)}
}

func scheduleTone(enabled bool) paint.Tone {
	if enabled {
		return paint.ToneSuccess
	}

	return paint.ToneMuted
}

func nameStyle(hovered, inactive bool) lipgloss.Style {
	switch {
	case hovered:
		return rowCursorStyle()
	case inactive:
		return dimStyle()
	default:
		return lipgloss.NewStyle()
	}
}

func clipName(name string) string {
	return paint.Clip(name, nameWidth)
}
