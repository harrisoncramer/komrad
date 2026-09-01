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
	// gutter, the cursor, the two spaces behind the state icon, that icon, the unread mark
	// beside it, and the spaces bracketing the name.
	rowChrome = 10

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

// ghostSlot pads a row out to the ghost column, or to the pane's right edge in a sidebar too
// narrow to reach that column, and right-aligns the text in the slot that opens there, drawing
// nothing when even that will not fit, since nothing downstream clips a row and an overrun one
// wraps onto the row below it.
func ghostSlot(row, text string, width int) string {
	drawn := lipgloss.Width(row)
	span := max(ghostWidth, lipgloss.Width(text))
	if text == "" || drawn+ghostGap+span > width {
		return row
	}

	pad := max(min(ghostColumn, width-span)-drawn, ghostGap)

	return row + strings.Repeat(" ", pad) + dimStyle.Render(fmt.Sprintf("%*s", ghostWidth, text))
}

// ghostText is what the slot says about an agent, which for now is how long it has been in the
// state it is in, blank under a minute since a count of seconds redraws on every tick and says
// nothing a row's own state does not already.
func ghostText(item agentItem) string {
	if item.agent.StateChangedAt.IsZero() {
		return ""
	}

	held := time.Since(item.agent.StateChangedAt)
	if held < time.Minute {
		return ""
	}

	return dates.Elapsed(held)
}
