package ui

import (
	"weave/internal/errui"
	"weave/internal/ui/paint"

	agentstore "weave/internal/store/agent"

	"github.com/charmbracelet/lipgloss"
)

const (
	unreadMailIcon = "✉"

	nameWidth = 30
)

var dimStyle = errui.Dim

func (m model) rowCursorStyle() lipgloss.Style {
	return paint.Panel.Cursor()
}

// Both switches below carry no default, so a state added later fails the exhaustive check here
// rather than quietly drawing as an idle row.
func stateIcon(state agentstore.State) string {
	switch state {
	case agentstore.StateIdle:
		return "•"
	case agentstore.StateWaiting:
		return "!"
	case agentstore.StateThinking:
		return "…"
	case agentstore.StateUncertain:
		return "?"
	case agentstore.StateSettingUp:
		return "○"
	case agentstore.StateFailedToCreate, agentstore.StateFailedToCleanup:
		return "✗"
	}

	return "•"
}

func stateTone(state agentstore.State) paint.Tone {
	switch state {
	case agentstore.StateIdle, agentstore.StateSettingUp, agentstore.StateUncertain:
		return paint.ToneMuted
	case agentstore.StateWaiting, agentstore.StateFailedToCreate, agentstore.StateFailedToCleanup:
		return paint.ToneDanger
	case agentstore.StateThinking:
		return paint.ToneWarning
	}

	return paint.ToneMuted
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

func (m model) nameStyle(hovered, inactive bool) lipgloss.Style {
	switch {
	case hovered:
		return m.rowCursorStyle()
	case inactive:
		return dimStyle
	default:
		return lipgloss.NewStyle()
	}
}

func clipName(name string) string {
	return paint.Clip(name, nameWidth)
}
