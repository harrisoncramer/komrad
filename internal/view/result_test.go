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
		{Key: "archived", Kind: KindResult, Name: "payouts", Tag: "archived", Dim: true, Process: ProcessNone},
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

// TestRenderTagFollowsTheName pins where a tag is drawn and that it is drawn dim, since it is a
// word about the row rather than part of what the row is called.
func TestRenderTagFollowsTheName(t *testing.T) {
	drawn := drawResults(canonicalPane)
	require.Len(t, drawn, 2)

	assert.Equal(t, "payouts archived", strings.TrimSpace(drawn[1]))
	assert.NotContains(t, drawn[0], "archived", "a row with no tag draws none")

	styled := Render(Prepare(Snapshot{Rows: resultRows(), Dropped: everyColumn(), Pane: canonicalPane}), paint.Panel, 0)
	assert.Contains(t, styled, paint.Panel.Style(paint.ToneMuted).Render("archived"))
}

// TestTagCostsTheNameItsColumns pins that a tag is counted into the width like the marks after
// it, so a name is cut to make room for it rather than the row overrunning its pane.
func TestTagCostsTheNameItsColumns(t *testing.T) {
	const pane = drawnResultNameColumn + 14

	tagged := Prepare(Snapshot{
		Rows:    []Row{{Key: "one", Kind: KindResult, Name: "a-long-agent-name", Tag: "archived"}},
		Dropped: everyColumn(),
		Pane:    pane,
	})
	bare := Prepare(Snapshot{
		Rows:    []Row{{Key: "one", Kind: KindResult, Name: "a-long-agent-name"}},
		Dropped: everyColumn(),
		Pane:    pane,
	})

	require.Len(t, tagged.Lines, 1)
	require.Len(t, bare.Lines, 1)
	assert.Equal(t, "a-lo…", tagged.Lines[0].Name)
	assert.Equal(t, "a-long-agent-…", bare.Lines[0].Name)

	for _, drawn := range strings.Split(ansiEscapes.ReplaceAllString(Render(tagged, paint.Panel, 0), ""), "\n") {
		assert.LessOrEqual(t, drawnColumns(drawn), pane, "row %q", drawn)
	}
}

// TestRenderResultList draws a result list whole.
func TestRenderResultList(t *testing.T) {
	list := Prepare(Snapshot{Rows: resultRows(), Dropped: everyColumn(), Focused: "live", Pane: canonicalPane})

	requireGolden(t, "list-results", Render(list, paint.Panel, canonicalPane))
}
