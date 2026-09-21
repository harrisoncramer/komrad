package view

import (
	"strings"
	"testing"

	"weave/internal/ui/paint"

	"github.com/charmbracelet/lipgloss"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// figureColumns is a list shaped like the worktrees screen: a size then an age, both figures.
func figureColumns() Columns {
	return Columns{
		{Kind: ColumnText, Width: 4},
		{Kind: ColumnText, Width: 4},
	}
}

func muted(text string) Cell {
	return Cell{Text: text, Tone: paint.ToneMuted}
}

// TestRenderTextColumnsSitBesideTheBar draws a list declaring two figure columns: each figure
// set against the right edge of its column, a header carrying only the first, and a child
// carrying none.
func TestRenderTextColumnsSitBesideTheBar(t *testing.T) {
	rows := []Row{
		{
			Kind:  KindWorkflow,
			Name:  "~/weave",
			Cells: []Cell{muted("24G")},
		},
		{
			Kind:  KindAgent,
			Name:  "worktree-view",
			Cells: []Cell{muted("49M"), muted("4d")},
		},
		{
			Kind:  KindAgent,
			Name:  "issue-607-panel-key-binding",
			Cells: []Cell{muted("9.2M"), muted("12h")},
		},
		{Kind: KindChild, Name: "review-607"},
		{
			Kind:    KindAgent,
			Name:    "gone",
			Process: ProcessStopped,
			Cells:   []Cell{muted("—"), muted("104w")},
		},
	}
	list := Prepare(Snapshot{
		Rows:    rows,
		Pane:    30,
		Columns: figureColumns(),
	})

	drawn := render(list, paint.Panel, 30)
	requireGolden(t, "list-figures", drawn)

	lines := strings.Split(ansiEscapes.ReplaceAllString(drawn, ""), "\n")
	require.Len(t, lines, len(rows))

	for i, line := range lines {
		assert.LessOrEqualf(t, lipgloss.Width(line), 30, "row %d draws past the pane", i)
		want := GutterColumns(figureColumns()) + wantNameColumn(rows[i].Kind) - drawnNameColumn
		assert.Equalf(t, want, nameColumnOf(line, list.Lines[i]), "row %d opens its name past the gutter", i)
	}

	assert.Equal(t, "     24G      │ ~/weave", lines[0])
	assert.Equal(t, "    9.2M  12h │   issue-607-p…", lines[2])
	assert.Equal(t, "              │   └ review-607", lines[3])
}

// TestRenderTextCellIsClippedToItsColumn pins a figure wider than its column being cut rather
// than pushing the name along.
func TestRenderTextCellIsClippedToItsColumn(t *testing.T) {
	list := Prepare(Snapshot{
		Rows: []Row{{
			Kind:  KindWorkflow,
			Name:  "weave",
			Cells: []Cell{muted("12345")},
		}},
		Columns: Columns{{Kind: ColumnText, Width: 4}},
	})

	drawn := ansiEscapes.ReplaceAllString(render(list, paint.Panel, 0), "")

	assert.Equal(t, "    123… │ weave", drawn)
}
