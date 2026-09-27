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
func numberedText(count int, replaced map[int]string) string {
	lines := make([]string, count)
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
	view := NewView(patchWith(diff.CompareTexts("notes.md",
		numberedText(20, nil),
		numberedText(20, map[int]string{1: "first", 20: "last"}),
	)), false)
	view.SetHeadless(true)

	return NewPane(view)
}

func readCursorText(pane *Pane) string {
	return strings.TrimSpace(ansi.Strip(pane.GetView().RenderRow(pane.GetCursor(), NoCursor, 80, 0)))
}

// TestNewPaneStandsOnTheFirstLine covers where a reader starts.
func TestNewPaneStandsOnTheFirstLine(t *testing.T) {
	pane := twoChangePane()

	assert.Equal(t, pane.GetView().GetFirstRow(), pane.GetCursor())
	assert.Equal(t, 0, pane.GetTop())
	assert.Equal(t, 0, pane.GetPan())
}

// TestPaneMoves covers the cursor walking the rows and reporting whether it went anywhere.
func TestPaneMoves(t *testing.T) {
	tests := []struct {
		name      string
		move      func(pane *Pane) bool
		wantMoved bool
		wantText  string
	}{
		{
			name:     "a step up from the first row stays there",
			move:     func(pane *Pane) bool { return pane.Step(-1, 1, paneHeight) },
			wantText: "1      -  line 1",
		},
		{
			name:     "moving to the row already stood on is no move",
			move:     func(pane *Pane) bool { return pane.MoveTo(pane.GetCursor(), paneHeight) },
			wantText: "1      -  line 1",
		},
		{
			name:      "several steps are taken at once",
			move:      func(pane *Pane) bool { return pane.Step(1, 3, paneHeight) },
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
	last := pane.GetView().GetLastRow()

	pane.MoveTo(last, paneHeight)
	assert.Equal(t, last-paneHeight+1+2, pane.GetTop(), "a margin of two rows is kept under the cursor")

	pane.MoveTo(pane.GetView().GetFirstRow(), paneHeight)
	assert.Equal(t, 0, pane.GetTop())

	pane.MoveTo(4, 0)
	assert.Equal(t, 0, pane.GetTop(), "a pane not yet measured leaves the window where it is")
}

// TestPaneJumpFilePutsTheFileAtTheTop covers a file jump and the window holding the file's
// header in place under the steps after it.
func TestPaneJumpFilePutsTheFileAtTheTop(t *testing.T) {
	const height = 10

	pane := NewPane(NewView(patchWith(
		diff.CompareTexts("a.md", "one", "two"),
		diff.CompareTexts("b.md", "three\nfour\nfive", "3\n4\n5"),
	), false))

	require.True(t, pane.JumpFile(1, height))
	file, ok := pane.GetView().GetDrawnFileAt(pane.GetCursor())
	require.True(t, ok)
	assert.Equal(t, "b.md", file.Path())

	header := pane.GetView().GetFileTopRow(pane.GetCursor())
	assert.Equal(t, header, pane.GetTop())

	pane.Step(1, 2, height)
	pane.Step(-1, 1, height)
	assert.Equal(t, header, pane.GetTop(), "the margin gives way to the header while the cursor is in its file")

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
	assert.Positive(t, pane.GetTop(), "the window follows a match off the bottom")

	pane.JumpMatch(1, paneHeight)
	assert.Equal(t, "1 +  first", readCursorText(&pane), "the matches wrap at the end")
}

// TestPaneToggleExpanded covers drawing a file whole and back, with the cursor kept on its line.
func TestPaneToggleExpanded(t *testing.T) {
	pane := twoChangePane()
	pane.Step(1, 1, paneHeight)
	tight := pane.GetView().Len()

	assert.Equal(t, ExpandWhole, pane.ToggleExpanded(paneHeight))
	assert.Greater(t, pane.GetView().Len(), tight)
	assert.Equal(t, "1 +  first", readCursorText(&pane))

	assert.Equal(t, ExpandChanged, pane.ToggleExpanded(paneHeight))
	assert.Equal(t, tight, pane.GetView().Len())
	assert.Equal(t, "1 +  first", readCursorText(&pane))
}

// TestPaneToggleExpandedRefuses covers the panes the key does nothing to.
func TestPaneToggleExpandedRefuses(t *testing.T) {
	tests := []struct {
		name  string
		build func() Pane
		want  Expansion
	}{
		{
			name:  "a pane with no view",
			build: func() Pane { return Pane{} },
			want:  ExpandNothing,
		},
		{
			name: "the row for the change as a whole is in no file",
			build: func() Pane {
				pane := NewPane(NewView(patchWith(diff.CompareTexts("a.md", "one", "two")), false))
				pane.MoveTo(0, paneHeight)

				return pane
			},
			want: ExpandNothing,
		},
		{
			name: "a file whose every line is drawn already has nothing more to show",
			build: func() Pane {
				return NewPane(NewView(patchWith(diff.CompareTexts("a.md", "one\ntwo", "one\n2")), false))
			},
			want: ExpandNoMore,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pane := tt.build()

			assert.Equal(t, tt.want, pane.ToggleExpanded(paneHeight))
		})
	}
}

// TestPaneKeepsItsPlace covers the cursor coming back to its line after the rows are rebuilt or
// the view is replaced.
func TestPaneKeepsItsPlace(t *testing.T) {
	tests := []struct {
		name     string
		act      func(pane *Pane)
		wantText string
		wantPan  int
	}{
		{
			name: "a view built again with nowhere to return to",
			act: func(pane *Pane) {
				pane.MoveTo(pane.GetView().GetLastRow(), paneHeight)

				fresh := twoChangePane()
				pane.Replace(fresh.GetView(), nil)
			},
			wantText: "1      -  line 1",
		},
		{
			name: "rows rebuilt under the cursor",
			act: func(pane *Pane) {
				pane.Step(1, 1, paneHeight)

				place, ok := pane.GetView().GetPlace(pane.GetCursor())
				pane.GetView().SetFileExpanded("notes.md", true)
				pane.Keep(place, ok)
			},
			wantText: "1 +  first",
		},
		{
			name: "a view built again keeps the line and the pan",
			act: func(pane *Pane) {
				pane.PanBy(8)
				pane.MoveTo(pane.GetView().GetLastRow(), paneHeight)
				place, _ := pane.GetView().GetPlace(pane.GetCursor())

				fresh := twoChangePane()
				pane.Replace(fresh.GetView(), &place)
			},
			wantText: "20 +  last",
			wantPan:  8,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pane := twoChangePane()

			tt.act(&pane)

			assert.Equal(t, tt.wantText, readCursorText(&pane))
			assert.Equal(t, tt.wantPan, pane.GetPan())
		})
	}
}

// TestPaneWithNoView covers a pane whose view has been dropped taking every key without moving.
func TestPaneWithNoView(t *testing.T) {
	pane := twoChangePane()
	pane.Drop()

	assert.False(t, pane.Step(1, 1, paneHeight))
	assert.False(t, pane.JumpFile(1, paneHeight))

	moved, found := pane.JumpMatch(1, paneHeight)
	assert.False(t, moved)
	assert.False(t, found)

	pane.Search(regexp.MustCompile("first"), paneHeight)
	pane.Keep(Place{}, false)
	assert.Equal(t, strings.Repeat(" ", 40), pane.Render(40, paneHeight, LiveCursor)[0])
}

// TestPanePanStopsAtTheLeftEdge covers sliding the rows sideways.
func TestPanePanStopsAtTheLeftEdge(t *testing.T) {
	pane := twoChangePane()

	pane.PanBy(8)
	assert.Equal(t, 8, pane.GetPan())

	pane.PanBy(-20)
	assert.Equal(t, 0, pane.GetPan())
}

// TestPaneRender covers the window of rows drawn, the cursor's row marked and the rest blank.
func TestPaneRender(t *testing.T) {
	pane := twoChangePane()
	pane.Step(1, 1, paneHeight)
	height := pane.GetView().Len() + 2

	rows := pane.Render(40, height, LiveCursor)

	require.Len(t, rows, height)
	assert.Equal(t, pane.GetView().RenderRow(pane.GetCursor(), LiveCursor, 40, 0), rows[pane.GetCursor()])
	assert.Equal(t, pane.GetView().RenderRow(0, NoCursor, 40, 0), rows[0])
	assert.Equal(t, strings.Repeat(" ", 40), rows[height-1])

	pane.MoveTo(pane.GetView().GetLastRow(), paneHeight)
	window := pane.Render(40, paneHeight, DimCursor)
	require.Len(t, window, paneHeight)
	start := min(pane.GetTop(), pane.GetView().Len()-paneHeight)
	assert.Equal(t, pane.GetView().RenderRow(start, NoCursor, 40, 0), window[0], "the window never runs past the last row")
	assert.Equal(t, pane.GetView().RenderRow(pane.GetCursor(), DimCursor, 40, 0), window[pane.GetCursor()-start])
}
