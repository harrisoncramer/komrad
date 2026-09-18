package view

import (
	"fmt"
	"strings"
	"testing"

	"weave/internal/ui/paint"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// windowedRows is a list of three workflows with four agents apiece, so a window taken out of
// the middle of it has a header above the fold and the counts have to say rows to be right.
func windowedRows() []Row {
	rows := make([]Row, 0, 15)
	for _, workflow := range []string{"alpha", "beta", "gamma"} {
		rows = append(rows, Row{Key: workflow, Kind: KindWorkflow, Name: workflow, Dormant: true})
		for i := range 4 {
			name := fmt.Sprintf("%s-%d", workflow, i)
			rows = append(rows, Row{Key: name, Kind: KindAgent, Name: name})
		}
	}

	return rows
}

// windowed draws a window over the list above, focused on the row named, and hands back its
// lines with the styling taken off.
func windowed(t *testing.T, focused string, lines int) []string {
	t.Helper()

	list := Prepare(Snapshot{Rows: windowedRows(), Focused: focused, NameWidth: canonicalNameWidth})
	block := Window(WindowParams{List: list, Palette: paint.Colored, Width: 40, Lines: lines})
	require.True(t, strings.HasSuffix(block, "\n"), "a window closes its last row")

	return strings.Split(ansiEscapes.ReplaceAllString(strings.TrimSuffix(block, "\n"), ""), "\n")
}

// TestWindowFitsThePane pins where the list is scrolled to and what it says about the rows it is
// not showing, which is the whole of what windowing decides.
func TestWindowFitsThePane(t *testing.T) {
	tests := []struct {
		name    string
		focused string
		lines   int
		want    []string
	}{
		{
			name:    "a list shorter than the pane is drawn whole and says nothing",
			focused: "alpha",
			lines:   20,
			want: []string{
				"alpha", "alpha-0", "alpha-1", "alpha-2", "alpha-3",
				"beta", "beta-0", "beta-1", "beta-2", "beta-3",
				"gamma", "gamma-0", "gamma-1", "gamma-2", "gamma-3",
			},
		},
		{
			name:    "the top of a long list spends one row saying what is below it",
			focused: "alpha",
			lines:   6,
			want:    []string{"alpha", "alpha-0", "alpha-1", "alpha-2", "alpha-3", "  ↓ 10 more"},
		},
		{
			name:    "the bottom of one spends its row saying what is above",
			focused: "gamma-3",
			lines:   6,
			want:    []string{"  ↑ 10 more", "gamma", "gamma-0", "gamma-1", "gamma-2", "gamma-3"},
		},
		{
			name:    "the middle of one spends a row at each end",
			focused: "beta-1",
			lines:   6,
			want:    []string{"  ↑ 5 more", "beta", "beta-0", "beta-1", "beta-2", "  ↓ 6 more"},
		},
		{
			name:    "a pane too short for either count draws rows alone",
			focused: "beta-1",
			lines:   2,
			want:    []string{"beta-0", "beta-1"},
		},
		{
			name:    "a key naming no row leaves the list at the top",
			focused: "no-such-agent",
			lines:   6,
			want:    []string{"alpha", "alpha-0", "alpha-1", "alpha-2", "alpha-3", "  ↓ 10 more"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			drawn := windowed(t, tt.focused, tt.lines)
			require.Len(t, drawn, len(tt.want))

			for i, want := range tt.want {
				assert.True(t, strings.HasSuffix(drawn[i], want), "row %d is %q, wanted it to end in %q", i, drawn[i], want)
			}
		})
	}
}

// TestWindowCountsRowsRatherThanAgents pins the number a marker carries against a fold with a
// workflow header above it, where the rows hidden outnumber the agents hidden.
func TestWindowCountsRowsRatherThanAgents(t *testing.T) {
	drawn := windowed(t, "beta-2", 6)

	assert.Equal(t, "  ↑ 6 more", drawn[0], "the alpha header, its four agents, and the beta header")
	assert.True(t, strings.HasSuffix(drawn[1], "beta-0"))
}

// TestWindowNeverOverrunsThePane pins the one thing a caller cannot get wrong: whatever the list
// and the pane, the block it draws is no taller than the room it was given.
func TestWindowNeverOverrunsThePane(t *testing.T) {
	list := Prepare(Snapshot{Rows: windowedRows(), Focused: "beta-1", NameWidth: canonicalNameWidth})

	for lines := 1; lines <= 20; lines++ {
		block := Window(WindowParams{List: list, Palette: paint.Colored, Width: 40, Lines: lines})
		assert.Equal(t, min(lines, len(list.Lines)), paint.CountLines(block), "a pane of %d lines", lines)
	}
}

// TestWindowKeepsASelectedRowTheWidthOfThePane pins that the bar under a selected row still
// reaches the edge once the row is inside a window, in the columns the font draws rather than
// the ones lipgloss counts.
func TestWindowKeepsASelectedRowTheWidthOfThePane(t *testing.T) {
	const pane = 40

	rows := windowedRows()
	rows[7].Selected = true

	list := Prepare(Snapshot{Rows: rows, Focused: "beta-1", NameWidth: NameColumns(pane)})
	block := Window(WindowParams{List: list, Palette: paint.Colored, Width: pane, Lines: 6})

	painted, ok := rowNamed(strings.Split(block, "\n"), "beta-1")
	require.True(t, ok)
	assert.Equal(t, pane, drawnColumns(ansiEscapes.ReplaceAllString(painted, "")))
}

// rowNamed is the drawn row holding a name, once its styling is off.
func rowNamed(rows []string, name string) (string, bool) {
	for _, row := range rows {
		if strings.Contains(ansiEscapes.ReplaceAllString(row, ""), name) {
			return row, true
		}
	}

	return "", false
}

// TestWindowOverTheLivePanel draws a window over the listing the panel actually holds, at a pane
// short enough to need one, which the agents tab is not today.
func TestWindowOverTheLivePanel(t *testing.T) {
	list := Prepare(Snapshot{Rows: livePanelRows(), Focused: "strip-demo", NameWidth: canonicalNameWidth})
	block := Window(WindowParams{List: list, Palette: paint.Colored, Width: 60, Lines: 12})

	requireGolden(t, "window-live-panel", Header(paint.Colored)+"\n"+block)
}
