package diffview

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"komrad/internal/diff"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// paneHeight is the rows the panes under test are drawn in.
const paneHeight = 5

// numberedText is lines "line 1" to "line n", with any line given replaced.
func numberedText(n int, replaced map[int]string) string {
	lines := make([]string, n)
	for i := range lines {
		lines[i] = fmt.Sprint("line ", i+1)
		if text, ok := replaced[i+1]; ok {
			lines[i] = text
		}
	}

	return strings.Join(lines, "\n")
}

// twoChangePane is a headless pane over twenty lines with the first and the last rewritten.
func twoChangePane() Pane {
	view := NewView(patchWith(diff.FromTexts("notes.md",
		numberedText(20, nil),
		numberedText(20, map[int]string{1: "first", 20: "last"}),
	)), false)
	view.SetHeadless(true)

	return NewPane(view)
}

func readCursorText(p *Pane) string {
	return strings.TrimSpace(ansi.Strip(p.View().RenderRow(p.Cursor(), NoCursor, 80, 0)))
}

// TestNewPaneStandsOnTheFirstLine covers where a reader starts.
func TestNewPaneStandsOnTheFirstLine(t *testing.T) {
	pane := twoChangePane()

	assert.Equal(t, pane.View().GetFirstRow(), pane.Cursor())
	assert.Equal(t, 0, pane.Top())
	assert.Equal(t, 0, pane.Pan())
}

// TestPaneMoves covers the cursor walking the rows and reporting whether it went anywhere.
func TestPaneMoves(t *testing.T) {
	tests := []struct {
		name      string
		move      func(p *Pane) bool
		wantMoved bool
		wantText  string
	}{
		{
			name:      "a step down lands on the next row",
			move:      func(p *Pane) bool { return p.Step(1, 1, paneHeight) },
			wantMoved: true,
			wantText:  "1 +  first",
		},
		{
			name:     "a step up from the first row stays there",
			move:     func(p *Pane) bool { return p.Step(-1, 1, paneHeight) },
			wantText: "1      -  line 1",
		},
		{
			name:     "moving to the row already stood on is no move",
			move:     func(p *Pane) bool { return p.MoveTo(p.Cursor(), paneHeight) },
			wantText: "1      -  line 1",
		},
		{
			name:      "several steps are taken at once",
			move:      func(p *Pane) bool { return p.Step(1, 3, paneHeight) },
			wantMoved: true,
			wantText:  "3    3    line 3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pane := twoChangePane()

			assert.Equal(t, tt.wantMoved, tt.move(&pane))
			assert.Equal(t, tt.wantText, readCursorText(&pane))
		})
	}
}

// TestPaneScrollsToKeepTheCursorInSight covers the window following the cursor with a margin.
func TestPaneScrollsToKeepTheCursorInSight(t *testing.T) {
	pane := twoChangePane()
	last := pane.View().GetLastRow()

	pane.MoveTo(last, paneHeight)
	assert.Equal(t, last-paneHeight+1+2, pane.Top(), "a margin of two rows is kept under the cursor")

	pane.MoveTo(pane.View().GetFirstRow(), paneHeight)
	assert.Equal(t, 0, pane.Top())

	pane.MoveTo(4, 0)
	assert.Equal(t, 0, pane.Top(), "a pane not yet measured leaves the window where it is")
}

// TestPaneJumpFilePutsTheFileAtTheTop covers a file jump and the window holding the file's
// header in place under the steps after it.
func TestPaneJumpFilePutsTheFileAtTheTop(t *testing.T) {
	const height = 10

	pane := NewPane(NewView(patchWith(
		diff.FromTexts("a.md", "one", "two"),
		diff.FromTexts("b.md", "three\nfour\nfive", "3\n4\n5"),
	), false))

	require.True(t, pane.JumpFile(1, height))
	file, ok := pane.View().GetDrawnFileAt(pane.Cursor())
	require.True(t, ok)
	assert.Equal(t, "b.md", file.Path())

	header := pane.View().GetFileTopRow(pane.Cursor())
	assert.Equal(t, header, pane.Top())

	pane.Step(1, 2, height)
	pane.Step(-1, 1, height)
	assert.Equal(t, header, pane.Top(), "the margin gives way to the header while the cursor is in its file")

	assert.False(t, pane.JumpFile(1, height), "there is no file past the last")
}

// TestPaneSearch covers a pattern landing the cursor on what it found and walking the matches.
func TestPaneSearch(t *testing.T) {
	pane := twoChangePane()

	_, found := pane.JumpMatch(1, paneHeight)
	assert.False(t, found, "nothing is found before anything is searched for")

	pane.Search(regexp.MustCompile("last|first"), paneHeight)
	assert.Equal(t, "1 +  first", readCursorText(&pane))

	moved, found := pane.JumpMatch(1, paneHeight)
	assert.True(t, found)
	assert.True(t, moved)
	assert.Equal(t, "20 +  last", readCursorText(&pane))
	assert.Positive(t, pane.Top(), "the window follows a match off the bottom")

	pane.JumpMatch(1, paneHeight)
	assert.Equal(t, "1 +  first", readCursorText(&pane), "the matches wrap at the end")
}

// TestPaneToggleExpanded covers drawing a file whole and back, with the cursor kept on its line.
func TestPaneToggleExpanded(t *testing.T) {
	pane := twoChangePane()
	pane.Step(1, 1, paneHeight)
	tight := pane.View().Len()

	assert.Equal(t, ExpandWhole, pane.ToggleExpanded(paneHeight))
	assert.Greater(t, pane.View().Len(), tight)
	assert.Equal(t, "1 +  first", readCursorText(&pane))

	assert.Equal(t, ExpandChanged, pane.ToggleExpanded(paneHeight))
	assert.Equal(t, tight, pane.View().Len())
	assert.Equal(t, "1 +  first", readCursorText(&pane))
}

// TestPaneToggleExpandedRefuses covers the two files the key does nothing to.
func TestPaneToggleExpandedRefuses(t *testing.T) {
	t.Run("a file whose every line is drawn already has nothing more to show", func(t *testing.T) {
		pane := NewPane(NewView(patchWith(diff.FromTexts("a.md", "one\ntwo", "one\n2")), false))

		assert.Equal(t, ExpandNoMore, pane.ToggleExpanded(paneHeight))
	})

	t.Run("the row for the change as a whole is in no file", func(t *testing.T) {
		pane := NewPane(NewView(patchWith(diff.FromTexts("a.md", "one", "two")), false))
		pane.MoveTo(0, paneHeight)

		assert.Equal(t, ExpandNothing, pane.ToggleExpanded(paneHeight))
	})
}

// TestPaneKeepsItsPlace covers the cursor coming back to its line after the rows are rebuilt or
// the view is replaced.
func TestPaneKeepsItsPlace(t *testing.T) {
	t.Run("rows rebuilt under the cursor", func(t *testing.T) {
		pane := twoChangePane()
		pane.Step(1, 1, paneHeight)

		place, ok := pane.View().GetPlace(pane.Cursor())
		pane.View().SetFileExpanded("notes.md", true)
		pane.Keep(place, ok)

		assert.Equal(t, "1 +  first", readCursorText(&pane))
	})

	t.Run("a view built again", func(t *testing.T) {
		pane := twoChangePane()
		pane.PanBy(8)
		pane.MoveTo(pane.View().GetLastRow(), paneHeight)
		place, _ := pane.View().GetPlace(pane.Cursor())

		fresh := twoChangePane()
		pane.Replace(fresh.View(), &place)

		assert.Equal(t, "20 +  last", readCursorText(&pane))
		assert.Equal(t, 8, pane.Pan(), "the pan is kept")
	})

	t.Run("a view built again with nowhere to return to", func(t *testing.T) {
		pane := twoChangePane()
		pane.MoveTo(pane.View().GetLastRow(), paneHeight)

		fresh := twoChangePane()
		pane.Replace(fresh.View(), nil)

		assert.Equal(t, pane.View().GetFirstRow(), pane.Cursor())
	})
}

// TestPanePanStopsAtTheLeftEdge covers sliding the rows sideways.
func TestPanePanStopsAtTheLeftEdge(t *testing.T) {
	pane := twoChangePane()

	pane.PanBy(8)
	assert.Equal(t, 8, pane.Pan())

	pane.PanBy(-20)
	assert.Equal(t, 0, pane.Pan())
}

// TestPaneRender covers the window of rows drawn, the cursor's row marked and the rest blank.
func TestPaneRender(t *testing.T) {
	pane := twoChangePane()
	pane.Step(1, 1, paneHeight)
	height := pane.View().Len() + 2

	rows := pane.Render(40, height, LiveCursor)

	require.Len(t, rows, height)
	assert.Equal(t, pane.View().RenderRow(pane.Cursor(), LiveCursor, 40, 0), rows[pane.Cursor()])
	assert.Equal(t, pane.View().RenderRow(0, NoCursor, 40, 0), rows[0])
	assert.Equal(t, strings.Repeat(" ", 40), rows[height-1])

	pane.MoveTo(pane.View().GetLastRow(), paneHeight)
	window := pane.Render(40, paneHeight, DimCursor)
	require.Len(t, window, paneHeight)
	start := min(pane.Top(), pane.View().Len()-paneHeight)
	assert.Equal(t, pane.View().RenderRow(start, NoCursor, 40, 0), window[0], "the window never runs past the last row")
	assert.Equal(t, pane.View().RenderRow(pane.Cursor(), DimCursor, 40, 0), window[pane.Cursor()-start])
}
