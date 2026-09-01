package ui

import (
	"fmt"
	"strings"
	"time"

	"weave/lib/dates"

	"github.com/charmbracelet/lipgloss"
)

const (
	// ghostGap is the space left between the slot and the row beside it, a single cell so that
	// the column reads as its own without pushing the rows away from it.
	ghostGap = 1

	// ghostWidth is the slot itself, wide enough for the longest span Elapsed writes.
	ghostWidth = 4

	// freshText stands in for a state entered within the minute, which every agent is in for a
	// while after a restart brings them all back at once.
	freshText = "<1m"
)

// ghostSlot opens a fixed column at the head of a row and right-aligns the text in it, holding
// the column open when there is nothing to say so that every row starts in the same place, and
// dropping the slot when the row would not fit the pane beside it, since nothing downstream
// clips a row and an overrun one wraps onto the row below it.
func ghostSlot(row, text string, width int) string {
	if lipgloss.Width(row)+ghostWidth+ghostGap > width {
		return row
	}

	slot := strings.Repeat(" ", ghostWidth)
	if text != "" {
		slot = dimStyle.Render(fmt.Sprintf("%*s", ghostWidth, text))
	}

	return slot + strings.Repeat(" ", ghostGap) + row
}

// ghostText is what the slot says about an agent, which for now is how long it has been in the
// state it is in, reading as freshText within the minute rather than as a count of seconds that
// would redraw on every tick or a blank that reads as a panel with nothing to say.
func ghostText(item agentItem) string {
	if item.agent.StateChangedAt.IsZero() {
		return ""
	}

	held := time.Since(item.agent.StateChangedAt)
	if held < time.Minute {
		return freshText
	}

	return dates.Elapsed(held)
}
