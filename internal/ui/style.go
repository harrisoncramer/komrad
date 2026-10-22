package ui

import (
	"komrad/internal/glyph"
	"komrad/internal/ui/paint"
	"komrad/internal/view"
)

const nameWidth = 30

const separated = " " + glyph.Separator + " "

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

func clipName(name string) string {
	return paint.Clip(name, nameWidth)
}
