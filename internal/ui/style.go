package ui

import (
	"weave/internal/errui"
	"weave/internal/ui/paint"

	"charm.land/lipgloss/v2"
)

const nameWidth = 30

var dimStyle = errui.Dim

func rowCursorStyle() lipgloss.Style {
	return paint.Panel.Cursor()
}

func scheduleIcon(enabled bool) string {
	if enabled {
		return "◆"
	}

	return "◇"
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
