package ui

import (
	"fmt"
	"strings"
	"time"

	"weave/lib/dates"

	"github.com/charmbracelet/lipgloss"
)

const (
	// rowChrome is every cell an agent row spends outside its name and its icons: the selection
	// gutter, the cursor, the two spaces behind the state icon, that icon, and the spaces
	// bracketing the name.
	rowChrome = 9

	// maxRowIcons is how many icons follow a name: the removed worktree, the schedule, the
	// hidden mark, the tag, and unread mail.
	maxRowIcons = 5

	// ghostGap is the space left between a row and its slot.
	ghostGap = 2

	// ghostColumn is where the ghost slot opens, which is the widest row an agent can draw plus
	// the gap, so that every slot lines up whatever the row beside it happens to wear.
	ghostColumn = rowChrome + nameWidth + 2*maxRowIcons + ghostGap

	// ghostWidth is the slot itself, wide enough for the longest span Elapsed writes.
	ghostWidth = 4
)

// ghostSlot pads a row out to the ghost column and right-aligns the text in it, drawing nothing
// when this row and a slot would not fit the pane, since nothing downstream clips a row and an
// overrun one wraps onto the row below it.
func ghostSlot(row, text string, width int) string {
	drawn := lipgloss.Width(row)
	if text == "" || drawn+ghostGap+ghostWidth > width {
		return row
	}

	pad := max(ghostColumn-drawn, ghostGap)

	return row + strings.Repeat(" ", pad) + dimStyle.Render(fmt.Sprintf("%*s", ghostWidth, text))
}

// ghostText is what the slot says about an agent, which for now is how long it has been in the
// state it is in.
func ghostText(item agentItem) string {
	if item.agent.StateChangedAt.IsZero() {
		return ""
	}

	return dates.Elapsed(time.Since(item.agent.StateChangedAt))
}
