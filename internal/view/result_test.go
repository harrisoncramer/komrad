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
		{
			Key:  "live",
			Kind: KindResult,
			Name: "billing",
		},
		{
			Key:     "live/3",
			Kind:    KindHit,
			Lead:    "agent #3",
			Name:    "the ledger is reconciled nightly",
			Matches: []Span{{Start: 4, End: 10}},
		},
		{
			Key:      "archived",
			Kind:     KindResult,
			Name:     "payouts",
			Archived: true,
			Process:  ProcessStopped,
		},
		{
			Key:      "archived/1",
			Kind:     KindHit,
			Lead:     "you #1",
			Name:     "ledger",
			Matches:  []Span{{Start: 0, End: 6}},
			Archived: true,
			Process:  ProcessStopped,
		},
	}
}

// drawResults is a result list with its styling taken off.
func drawResults(pane int) []string {
	list := Prepare(Snapshot{
		Rows:    resultRows(),
		Dropped: everyColumn(),
		Focused: "live",
		Pane:    pane,
	})

	return strings.Split(ansiEscapes.ReplaceAllString(render(list, paint.Panel, 0), ""), "\n")
}

// TestPrepareResultsDropEveryColumn pins what a list of results asks for: the three columns are
// gone whatever the pane.
func TestPrepareResultsDropEveryColumn(t *testing.T) {
	wide := Prepare(Snapshot{
		Rows:    resultRows(),
		Dropped: everyColumn(),
		Pane:    200,
	})

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
	list := Prepare(Snapshot{
		Rows:    resultRows(),
		Dropped: everyColumn(),
		Pane:    canonicalPane,
	})
	drawn := drawResults(canonicalPane)
	require.Len(t, drawn, len(list.Lines))

	for i, line := range list.Lines {
		if line.Lead != "" {
			continue
		}

		assert.Equal(t, 0, line.Indent, "row %q", drawn[i])
		assert.Equal(t, WeightRegular, line.Weight, "row %q", drawn[i])
		assert.Equal(t, drawnResultNameColumn, nameColumnOf(drawn[i], line), "row %q", drawn[i])
	}
}

// TestHitRowHangsUnderItsResult pins that a turn is drawn one step in under the agent that
// said it, its lead before the words and the three columns left blank.
func TestHitRowHangsUnderItsResult(t *testing.T) {
	list := Prepare(Snapshot{
		Rows:    resultRows(),
		Dropped: everyColumn(),
		Pane:    canonicalPane,
	})
	drawn := drawResults(canonicalPane)
	require.Len(t, drawn, len(list.Lines))

	hits := 0
	for i, line := range list.Lines {
		if line.Lead == "" {
			continue
		}
		hits++

		assert.Equal(t, indentColumns, line.Indent, "row %q", drawn[i])
		assert.Equal(t, WeightRegular, line.Weight, "row %q", drawn[i])
		assert.False(t, line.Rule, "row %q", drawn[i])
		lead := strings.Index(drawn[i], line.Lead)
		require.GreaterOrEqual(t, lead, 0, "row %q", drawn[i])
		assert.Equal(t, wantNameColumn(KindHit), drawnColumns(drawn[i][:lead]), "row %q", drawn[i])
		assert.Equal(t, wantNameColumn(KindHit)+len(line.Lead)+1, nameColumnOf(drawn[i], line), "row %q", drawn[i])
		for _, cell := range []Cell{line.Repo, line.Review, line.Forge} {
			assert.True(t, cell.IsBlank(), "row %q", drawn[i])
		}
	}

	assert.Equal(t, 2, hits)
}

// TestHitRowGivesItsLeadRoom pins that a narrow pane clips the words rather than the lead.
func TestHitRowGivesItsLeadRoom(t *testing.T) {
	line := prepareOne(t, Row{
		Kind: KindHit,
		Lead: "agent #3",
		Name: "the ledger is reconciled nightly",
	})
	fitted := fitRow(line, 20)

	assert.Equal(t, "agent #3", fitted.Lead)
	assert.Len(t, []rune(fitted.Name), 20-indentColumns-len("agent #3")-1)
}

// TestRenderResultList draws a result list whole.
func TestRenderResultList(t *testing.T) {
	list := Prepare(Snapshot{
		Rows:    resultRows(),
		Dropped: everyColumn(),
		Focused: "live",
		Pane:    canonicalPane,
	})

	requireGolden(t, "list-results", render(list, paint.Panel, canonicalPane))
}
