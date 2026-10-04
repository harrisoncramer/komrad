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

// paneWindow is the window the panes under test are drawn in.
var paneWindow = Window{Width: 80, Height: paneHeight}

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
	return strings.TrimSpace(ansi.Strip(p.View().renderRow(p.Cursor(), noCursor, 80, 0)))
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
			move:      func(p *Pane) bool { return p.Step(1, 1, paneWindow) },
			wantMoved: true,
			wantText:  "1 +  first",
		},
		{
			name:     "a step up from the first row stays there",
			move:     func(p *Pane) bool { return p.Step(-1, 1, paneWindow) },
			wantText: "1      -  line 1",
		},
		{
			name:     "moving to the row already stood on is no move",
			move:     func(p *Pane) bool { return p.MoveTo(p.Cursor(), paneWindow) },
			wantText: "1      -  line 1",
		},
		{
			name:      "several steps are taken at once",
			move:      func(p *Pane) bool { return p.Step(1, 3, paneWindow) },
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

	pane.MoveTo(last, paneWindow)
	assert.Equal(t, last-paneHeight+1+2, pane.Top(), "a margin of two rows is kept under the cursor")

	pane.MoveTo(pane.View().GetFirstRow(), paneWindow)
	assert.Equal(t, 0, pane.Top())

	pane.MoveTo(4, Window{})
	assert.Equal(t, 0, pane.Top(), "a pane not yet measured leaves the window where it is")
}

// TestPaneJumpFilePutsTheFileAtTheTop covers a file jump and the window holding the file's
// header in place under the steps after it.
func TestPaneJumpFilePutsTheFileAtTheTop(t *testing.T) {
	window := Window{Width: 80, Height: 10}

	pane := NewPane(NewView(patchWith(
		diff.FromTexts("a.md", "one", "two"),
		diff.FromTexts("b.md", "three\nfour\nfive", "3\n4\n5"),
	), false))

	require.True(t, pane.JumpFile(1, window))
	file, ok := pane.View().GetDrawnFileAt(pane.Cursor())
	require.True(t, ok)
	assert.Equal(t, "b.md", file.Path())

	header := pane.View().getFileTopRow(pane.Cursor())
	assert.Equal(t, header, pane.Top())

	pane.Step(1, 2, window)
	pane.Step(-1, 1, window)
	assert.Equal(t, header, pane.Top(), "the margin gives way to the header while the cursor is in its file")

	assert.False(t, pane.JumpFile(1, window), "there is no file past the last")
}

// TestPaneSearch covers a pattern landing the cursor on what it found and walking the matches.
func TestPaneSearch(t *testing.T) {
	pane := twoChangePane()

	_, found := pane.JumpMatch(1, paneWindow)
	assert.False(t, found, "nothing is found before anything is searched for")

	pane.Search(regexp.MustCompile("last|first"), paneWindow)
	assert.Equal(t, "1 +  first", readCursorText(&pane))

	moved, found := pane.JumpMatch(1, paneWindow)
	assert.True(t, found)
	assert.True(t, moved)
	assert.Equal(t, "20 +  last", readCursorText(&pane))
	assert.Positive(t, pane.Top(), "the window follows a match off the bottom")

	pane.JumpMatch(1, paneWindow)
	assert.Equal(t, "1 +  first", readCursorText(&pane), "the matches wrap at the end")
}

// TestPaneToggleExpanded covers drawing a file whole and back, with the cursor kept on its line.
func TestPaneToggleExpanded(t *testing.T) {
	pane := twoChangePane()
	pane.Step(1, 1, paneWindow)
	tight := pane.View().Len()

	assert.Equal(t, ExpandWhole, pane.ToggleExpanded(paneWindow))
	assert.Greater(t, pane.View().Len(), tight)
	assert.Equal(t, "1 +  first", readCursorText(&pane))

	assert.Equal(t, ExpandChanged, pane.ToggleExpanded(paneWindow))
	assert.Equal(t, tight, pane.View().Len())
	assert.Equal(t, "1 +  first", readCursorText(&pane))
}

// TestPaneToggleExpandedRefuses covers the two files the key does nothing to.
func TestPaneToggleExpandedRefuses(t *testing.T) {
	t.Run("a file whose every line is drawn already has nothing more to show", func(t *testing.T) {
		pane := NewPane(NewView(patchWith(diff.FromTexts("a.md", "one\ntwo", "one\n2")), false))

		assert.Equal(t, ExpandNoMore, pane.ToggleExpanded(paneWindow))
	})
}

// TestPaneKeepsItsPlace covers the cursor coming back to its line after the rows are rebuilt or
// the view is replaced.
func TestPaneKeepsItsPlace(t *testing.T) {
	t.Run("rows rebuilt under the cursor", func(t *testing.T) {
		pane := twoChangePane()
		pane.Step(1, 1, paneWindow)

		place, ok := pane.View().GetPlace(pane.Cursor())
		pane.View().setFileExpanded("notes.md", true)
		pane.Keep(place, ok)

		assert.Equal(t, "1 +  first", readCursorText(&pane))
	})

	t.Run("a view built again", func(t *testing.T) {
		pane := twoChangePane()
		pane.PanBy(8)
		pane.MoveTo(pane.View().GetLastRow(), paneWindow)
		place, _ := pane.View().GetPlace(pane.Cursor())

		fresh := twoChangePane()
		pane.Replace(fresh.View(), &place)

		assert.Equal(t, "20 +  last", readCursorText(&pane))
		assert.Equal(t, 8, pane.Pan(), "the pan is kept")
	})

	t.Run("a view built again with nowhere to return to", func(t *testing.T) {
		pane := twoChangePane()
		pane.MoveTo(pane.View().GetLastRow(), paneWindow)

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
	pane.Step(1, 1, paneWindow)
	height := pane.View().Len() + 2

	rows := pane.Render(Window{Width: 40, Height: height}, LiveCursor)

	require.Len(t, rows, height)
	assert.Equal(t, pane.View().renderRow(pane.Cursor(), LiveCursor, 40, 0), rows[pane.Cursor()])
	assert.Equal(t, pane.View().renderRow(0, noCursor, 40, 0), rows[0])
	assert.Equal(t, strings.Repeat(" ", 40), rows[height-1])

	pane.MoveTo(pane.View().GetLastRow(), paneWindow)
	window := pane.Render(Window{Width: 40, Height: paneHeight}, DimCursor)
	require.Len(t, window, paneHeight)
	start := min(pane.Top(), pane.View().Len()-paneHeight)
	assert.Equal(t, pane.View().renderRow(start, noCursor, 40, 0), window[0], "the window never runs past the last row")
	assert.Equal(t, pane.View().renderRow(pane.Cursor(), DimCursor, 40, 0), window[pane.Cursor()-start])
}

// wrappedWindow is a pane narrow enough that every line of longText wraps.
var wrappedWindow = Window{
	Width:  GutterWidth + 10,
	Height: 6,
	Wrap:   true,
}

// longText is ten lines each long enough to wrap in wrappedWindow.
func longText(word string) string {
	lines := make([]string, 10)
	for i := range lines {
		lines[i] = fmt.Sprint(word, " ", i+1, " runs on past the edge")
	}

	return strings.Join(lines, "\n")
}

// assertCursorInSight checks the cursor's row is drawn whole in the window, or from its top when
// it is taller than the window.
func assertCursorInSight(t *testing.T, p *Pane, window Window) {
	t.Helper()

	first, height := p.GetCursorLines(window)
	assert.GreaterOrEqual(t, first, 0, "the cursor's row starts inside the window")
	if height <= window.Height {
		assert.LessOrEqual(t, first+height, window.Height, "the cursor's row ends inside the window")
	}
}

// TestPaneScrollsWrappedRows covers the window following the cursor by lines when rows wrap.
func TestPaneScrollsWrappedRows(t *testing.T) {
	pane := NewPane(NewView(patchWith(diff.FromTexts("notes.md", longText("old"), longText("new"))), false))
	view := pane.View()
	require.Greater(t, view.measureRow(view.GetLastRow(), wrappedWindow), 1)

	for pane.Step(1, 1, wrappedWindow) {
		assertCursorInSight(t, &pane, wrappedWindow)
	}
	assert.Equal(t, view.GetLastRow(), pane.Cursor())

	for pane.Step(-1, 1, wrappedWindow) {
		assertCursorInSight(t, &pane, wrappedWindow)
	}
	assert.Equal(t, 0, pane.Top())
}

// TestPaneFileJumpGivesWayToWrappedRows covers a file jump's pin letting go once the cursor
// would fall out of the window.
func TestPaneFileJumpGivesWayToWrappedRows(t *testing.T) {
	pane := NewPane(NewView(patchWith(
		diff.FromTexts("a.md", "one", "two"),
		diff.FromTexts("b.md", longText("old"), longText("new")),
	), false))

	require.True(t, pane.JumpFile(1, wrappedWindow))
	header := pane.Top()

	for range 6 {
		pane.Step(1, 1, wrappedWindow)
		assertCursorInSight(t, &pane, wrappedWindow)
	}
	assert.Greater(t, pane.Top(), header, "the window leaves the header behind to follow the cursor")
}

// TestPaneShowsTheTopOfARowTallerThanTheWindow covers a cursor row that cannot fit whole.
func TestPaneShowsTheTopOfARowTallerThanTheWindow(t *testing.T) {
	window := Window{
		Width:  wrappedWindow.Width,
		Height: 2,
		Wrap:   true,
	}
	pane := NewPane(NewView(patchWith(diff.FromTexts("notes.md", longText("old"), longText("new"))), false))

	pane.MoveTo(pane.View().GetLastRow(), window)

	first, height := pane.GetCursorLines(window)
	assert.Equal(t, 0, first)
	assert.Greater(t, height, window.Height)
	assert.Len(t, pane.Render(window, LiveCursor), window.Height)
}

// TestPaneKeepsTheCursorInSightWhenLinesStartWrapping covers a window scrolled with lines cut off
// and drawn again wrapped, before anything scrolls it.
func TestPaneKeepsTheCursorInSightWhenLinesStartWrapping(t *testing.T) {
	pane := NewPane(NewView(patchWith(diff.FromTexts("notes.md", longText("old"), longText("new"))), false))
	cutOff := Window{Width: wrappedWindow.Width, Height: wrappedWindow.Height}

	pane.MoveTo(pane.View().GetLastRow()-4, cutOff)

	assertCursorInSight(t, &pane, wrappedWindow)
}

// TestPaneKeepsTheFileNameOnTop covers the header of the file under the window standing over its
// first line once the header has scrolled away.
func TestPaneKeepsTheFileNameOnTop(t *testing.T) {
	oldNumbered := numberedText(30, nil)
	newNumbered := numberedText(30, map[int]string{1: "first", 30: "last"})
	tests := []struct {
		name     string
		oldText  string
		newText  string
		headless bool
		window   Window
		wantTop  []string
	}{
		{
			name:    "a header scrolled away is drawn over the first line",
			oldText: oldNumbered,
			newText: newNumbered,
			window:  paneWindow,
			wantTop: []string{"notes.md"},
		},
		{
			name:     "a headless view has no header to draw",
			oldText:  oldNumbered,
			newText:  newNumbered,
			headless: true,
			window:   paneWindow,
			wantTop:  []string{"27   27    line 27"},
		},
		{
			name:    "the cursor on the first line is not covered",
			oldText: oldNumbered,
			newText: newNumbered,
			window:  Window{Width: 80, Height: 1},
			wantTop: []string{"30 +  last"},
		},
		{
			name:    "the rest of a wrapped first line is blanked under the header",
			oldText: longText("old"),
			newText: longText("new"),
			window:  wrappedWindow,
			wantTop: []string{"notes.md", ""},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			view := NewView(patchWith(diff.FromTexts("notes.md", tt.oldText, tt.newText)), false)
			view.SetHeadless(tt.headless)
			pane := NewPane(view)

			pane.MoveTo(view.GetLastRow(), tt.window)

			rows := pane.Render(tt.window, noCursor)
			for i, want := range tt.wantTop {
				assert.Equal(t, want, strings.TrimSpace(ansi.Strip(rows[i])))
			}
		})
	}
}
