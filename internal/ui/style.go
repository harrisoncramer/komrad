package ui

import (
	"komrad/internal/errui"
	"komrad/internal/glyph"
	"komrad/internal/ui/paint"

	"charm.land/lipgloss/v2"
)

const nameWidth = 30

const separated = " " + glyph.Separator + " "

var dimStyle = errui.Dim

func rowCursorStyle() lipgloss.Style {
	return paint.Panel.Cursor()
}

func scheduleIcon(glyphs glyph.Set, enabled bool) string {
	if enabled {
		return glyphs.Spell(glyph.ScheduleEnabled)
	}

	return glyphs.Spell(glyph.ScheduleDisabled)
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
		return dimStyle
	default:
		return lipgloss.NewStyle()
	}
}

func clipName(name string) string {
	return paint.Clip(name, nameWidth)
}
