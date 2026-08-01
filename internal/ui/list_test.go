package ui

import (
	"fmt"
	"strings"
	"testing"

	"weave/internal/appconfig"
	"weave/internal/errui"
	agentstore "weave/internal/store/agent"
	schedulestore "weave/internal/store/schedule"
	workflowstore "weave/internal/store/workflow"

	"github.com/stretchr/testify/assert"
)

// numbered builds a list of rows named for their index, so a window over them can
// be asserted on by name rather than by counting blank strings.
func numbered(n int) []string {
	rows := make([]string, 0, n)
	for i := range n {
		rows = append(rows, fmt.Sprintf("row%d", i))
	}

	return rows
}

// lines splits a rendered list into the lines it drew, dropping the trailing
// newline every row ends with so an empty final element does not count as a line.
func lines(rendered string) []string {
	trimmed := strings.TrimSuffix(rendered, "\n")
	if trimmed == "" {
		return nil
	}

	return strings.Split(trimmed, "\n")
}

// TestRenderListFits covers the lists short enough to draw whole. Nothing is cut
// and no counts are drawn, since a list that fits has nothing to say about what is
// off screen.
func TestRenderListFits(t *testing.T) {
	tests := []struct {
		name  string
		rows  []string
		lines int
	}{
		{
			name:  "shorter than the pane",
			rows:  numbered(3),
			lines: 10,
		},
		{
			name:  "exactly as tall as the pane",
			rows:  numbered(10),
			lines: 10,
		},
		{
			name:  "a height nothing has measured yet",
			rows:  numbered(100),
			lines: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.rows, lines(renderList(tt.rows, 0, tt.lines)))
		})
	}
}

// TestRenderListEmpty covers a list with no rows at all. It draws nothing rather
// than a blank line, so the screen's own empty-state text is what shows.
func TestRenderListEmpty(t *testing.T) {
	assert.Empty(t, renderList(nil, 0, 10))
}

// TestRenderListWindow covers the window a clipped list draws. The row under the
// cursor is always on screen, the list never scrolls past either end, and the
// counts standing in for what was cut come out of the same budget, so the block is
// never taller than the pane it was given.
func TestRenderListWindow(t *testing.T) {
	const total = 20

	tests := []struct {
		name   string
		cursor int
		lines  int
		want   []string
	}{
		{
			name:   "at the top the list is pinned there",
			cursor: 0,
			lines:  5,
			want:   []string{"row0", "row1", "row2", "row3", "  ↓ 16 more"},
		},
		{
			name:   "in the middle the cursor sits centred with both ends counted",
			cursor: 10,
			lines:  5,
			want:   []string{"  ↑ 9 more", "row9", "row10", "row11", "  ↓ 8 more"},
		},
		{
			name:   "at the bottom the list is pinned there",
			cursor: total - 1,
			lines:  5,
			want:   []string{"  ↑ 16 more", "row16", "row17", "row18", "row19"},
		},
		{
			// A pane this short cannot both say what it cut and show the row under the
			// cursor, so the counts are what give way.
			name:   "a pane with room for one row spends it on the hovered row",
			cursor: 7,
			lines:  1,
			want:   []string{"row7"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := lines(renderList(numbered(total), tt.cursor, tt.lines))

			assert.Equal(t, tt.want, got)
		})
	}
}

// TestRenderListKeepsCursorVisible walks the cursor down the whole list at every
// height a pane could give it. The row under the cursor has to be drawn and the
// block has to fit, and both have to hold for every cursor rather than at the
// edges alone, since the window is derived from the cursor and holds no offset of
// its own.
func TestRenderListKeepsCursorVisible(t *testing.T) {
	rows := numbered(50)

	for height := 1; height <= len(rows); height++ {
		for cursor := range rows {
			drawn := lines(renderList(rows, cursor, height))

			assert.Contains(t, drawn, fmt.Sprintf("row%d", cursor))
			assert.LessOrEqual(t, len(drawn), height)
		}
	}
}

// TestScreensFitThePane covers the whole screen rather than the list alone. The
// header, the list, and the legend under it have to fit the pane together, since a
// legend shoved off the bottom is the thing the clipping is there to save.
//
// The pane is taller than the tallest legend on purpose. A list keeps the row under the
// cursor whatever is above it, so a pane too short for its own chrome overflows by
// design, and measuring that says nothing about the clipping this is here for.
func TestScreensFitThePane(t *testing.T) {
	const (
		height = 24
		total  = 40
	)

	m := model{
		keys:   appconfig.DefaultKeybindings(),
		height: height,
		tree:   treeState{workflows: []workflowstore.Workflow{{ID: "w1", Name: "everything"}}},
	}
	for i := range total {
		name := fmt.Sprintf("agent%d", i)
		m.tree.agents = append(m.tree.agents, agentItem{agent: agentstore.Agent{Name: name, WorkflowID: "w1"}})
		m.archive.agents = append(m.archive.agents, agentstore.Agent{Name: name, WorkflowID: "w1"})
		m.schedules.items = append(m.schedules.items, scheduleItem{schedule: schedulestore.Schedule{Name: name}})
	}

	// Every cursor sits on the last row, the one a list that ran off the bottom
	// would have taken off screen with it.
	m.tree.cursor = total
	m.archive.cursor = total
	m.schedules.cursor = total - 1

	tests := []struct {
		name      string
		screen    screen
		wantTitle string
	}{
		{
			name:      "the agents tree",
			screen:    screenAgents,
			wantTitle: "Agents and Workflows",
		},
		{
			name:      "the archive",
			screen:    screenArchive,
			wantTitle: "Archive",
		},
		{
			name:      "the schedules",
			screen:    screenSchedules,
			wantTitle: "Schedules",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			shown := m
			shown.screen = tt.screen
			view := shown.View()

			// The renderer draws what follows the last newline as a line of its own, so
			// a view ending in one is a line taller than it reads and scrolls the pane.
			assert.NotEqual(t, "\n", view[len(view)-1:], "the view ends without a trailing newline")
			assert.LessOrEqual(t, len(lines(view)), height)
			assert.Contains(t, view, "agent39", "the hovered row is on screen")
			assert.Contains(t, view, "back", "the legend is on screen")
			assert.Contains(t, view, tt.wantTitle, "the title is on screen")
		})
	}
}

// TestListLines covers the room a screen's list is left with. The chrome around it
// and the banner over it both come off the pane, and a pane too short to hold even
// that keeps one line for the hovered row.
func TestListLines(t *testing.T) {
	header := "Archive\n\n"
	help := "\nrestore\ndelete\nback\n"

	tests := []struct {
		name   string
		height int
		banner *errui.Banner
		want   int
	}{
		{
			name:   "an unmeasured pane is unbounded",
			height: 0,
			want:   0,
		},
		{
			name:   "a pane shorter than its chrome keeps one row",
			height: 3,
			want:   1,
		},
		{
			name:   "a banner takes its lines too",
			height: 20,
			banner: &errui.Banner{Summary: "nope"},
			want:   10,
		},
		{
			name:   "the pane less the chrome around the list",
			height: 20,
			want:   14,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := model{height: tt.height, banner: tt.banner}

			assert.Equal(t, tt.want, m.listLines(header, help))
		})
	}
}
