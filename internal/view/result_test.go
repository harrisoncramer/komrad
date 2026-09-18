package view

import (
	"strings"
	"testing"

	"weave/internal/ui/paint"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// drawnResultNameColumn is the column a result list opens its name at, counted off the drawing
// by hand rather than from the package's own arithmetic.
const drawnResultNameColumn = 6

// resultRows are the rows a list of search hits is made of.
func resultRows() []Row {
	return []Row{
		{Key: "live", Kind: KindResult, Name: "billing"},
		{Key: "archived", Kind: KindResult, Name: "payouts", Dim: true, Process: ProcessStopped},
	}
}

// drawResults is a result list with its styling taken off.
func drawResults(pane int) []string {
	list := Prepare(Snapshot{Rows: resultRows(), Dropped: everyColumn(), Focused: "live", Pane: pane})

	return strings.Split(ansiEscapes.ReplaceAllString(Render(list, paint.Panel, 0), ""), "\n")
}

// TestPrepareResultsDropEveryColumn pins what a list of results asks for: the three columns are
// gone whatever the pane, rather than narrowed away.
func TestPrepareResultsDropEveryColumn(t *testing.T) {
	wide := Prepare(Snapshot{Rows: resultRows(), Dropped: everyColumn(), Pane: 200})

	assert.Equal(t, Columns{}, wide.Columns)
	assert.Equal(t, everyColumn(), Prepare(Snapshot{Rows: resultRows(), Pane: 200}).Columns,
		"the same pane draws all three when none is dropped")
}

// TestDroppedColumnsGiveTheirRoomToTheName pins what the columns are given up for: a name in a
// result list has the room the three columns would have taken.
func TestDroppedColumnsGiveTheirRoomToTheName(t *testing.T) {
	assert.Equal(t, drawnResultNameColumn, GutterColumns(Columns{}))
	assert.Greater(t, NameColumns(canonicalPane, Columns{}), NameColumns(canonicalPane, everyColumn()))
}

// TestResultRowOpensAtTheGutter pins that a result is not a tree node: nothing is above it to be
// indented under, so its name opens where a workflow's would and it is lettered as an ordinary
// row.
func TestResultRowOpensAtTheGutter(t *testing.T) {
	list := Prepare(Snapshot{Rows: resultRows(), Dropped: everyColumn(), Pane: canonicalPane})
	drawn := drawResults(canonicalPane)
	require.Len(t, drawn, len(list.Lines))

	for i, line := range list.Lines {
		assert.Equal(t, 0, line.Indent, "row %q", drawn[i])
		assert.Equal(t, WeightRegular, line.Weight, "row %q", drawn[i])
		assert.Equal(t, drawnResultNameColumn, nameColumnOf(drawn[i], line), "row %q", drawn[i])
	}
}

// TestRenderResultList draws a result list whole.
func TestRenderResultList(t *testing.T) {
	list := Prepare(Snapshot{Rows: resultRows(), Dropped: everyColumn(), Focused: "live", Pane: canonicalPane})

	requireGolden(t, "list-results", Render(list, paint.Panel, canonicalPane))
}
