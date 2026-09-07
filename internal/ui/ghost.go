package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

const (
	// ghostGap is the space left between the slot and the row beside it, a single cell so that
	// the column reads as its own without pushing the rows away from it.
	ghostGap = 1

	// ghostWidth is the slot itself, held open against whatever is drawn in it later.
	ghostWidth = 4
)

// ghostSlot opens a fixed column at the head of a row so that every row starts in the same
// place, dropping the slot when the row would not fit the pane beside it, since nothing
// downstream clips a row and an overrun one wraps onto the row below it.
func ghostSlot(row string, width int) string {
	if lipgloss.Width(row)+ghostWidth+ghostGap > width {
		return row
	}

	// TODO(@harrisoncramer): use this slot?
	return strings.Repeat(" ", ghostWidth+ghostGap) + row
}
