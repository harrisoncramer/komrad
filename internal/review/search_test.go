package review

import (
	"regexp"
	"testing"

	"weave/internal/diff"
	agentv1 "weave/proto/gen/agent/v1"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func twoHunkView() *View {
	return NewView(patchWith(diff.File{
		OldPath: "greet.go",
		NewPath: "greet.go",
		Hunks: []diff.Hunk{
			hunkOf(
				diff.Line{
					Kind:      diff.Context,
					Text:      "package greet",
					OldNumber: 1,
					NewNumber: 1,
				},
				diff.Line{
					Kind:      diff.Deletion,
					Text:      "return \"hello\"",
					OldNumber: 4,
				},
				diff.Line{
					Kind:      diff.Addition,
					Text:      "return \"Hello\"",
					NewNumber: 4,
				},
			),
			hunkOf(
				diff.Line{
					Kind:      diff.Context,
					Text:      "func Other() {",
					OldNumber: 20,
					NewNumber: 20,
				},
				diff.Line{
					Kind:      diff.Addition,
					Text:      "\treturn nil",
					NewNumber: 21,
				},
			),
		},
	}), false)
}

// TestSearchMatchesTheLineAsDrawn covers what a pattern is matched against.
func TestSearchMatchesTheLineAsDrawn(t *testing.T) {
	view := twoHunkView()

	assert.Equal(t, 2, view.SetPattern(regexp.MustCompile(`^\+`)), "the marker is part of what is searched")
}

// TestSearchWrapsAtTheEnds covers stepping through matches: forwards past the last one comes back
// to the first.
func TestSearchWrapsAtTheEnds(t *testing.T) {
	view := twoHunkView()

	require.Equal(t, 3, view.SetPattern(regexp.MustCompile("return")))

	first, ok := view.JumpMatch(-1, 1)
	require.True(t, ok)

	at, ok := view.MatchNumber(first)
	require.True(t, ok)
	assert.Equal(t, 1, at)

	second, ok := view.JumpMatch(first, 1)
	require.True(t, ok)
	third, ok := view.JumpMatch(second, 1)
	require.True(t, ok)

	wrapped, ok := view.JumpMatch(third, 1)
	require.True(t, ok)
	assert.Equal(t, first, wrapped, "past the last match is the first again")

	back, ok := view.JumpMatch(first, -1)
	require.True(t, ok)
	assert.Equal(t, third, back, "and before the first is the last")
}

// TestSearchingForNothingClearsIt covers the way a search is put away: no pattern at all.
func TestSearchingForNothingClearsIt(t *testing.T) {
	view := twoHunkView()

	view.SetPattern(regexp.MustCompile("return"))

	assert.Zero(t, view.SetPattern(nil))

	_, ok := view.JumpMatch(0, 1)
	assert.False(t, ok, "nothing to step through once it is cleared")
}

// TestSearchReachesTheThreadsAtTheFoot covers the adrift threads, which the cursor can rest on
// and a search that skipped them would be lying about what it looked at.
func TestSearchReachesTheThreadsAtTheFoot(t *testing.T) {
	view := twoHunkView()

	view.PinThreads([]*agentv1.ReviewThread{{
		Id:          "t1",
		FilePath:    "gone.go",
		NewStart:    12,
		QuotedLines: []string{"+what this used to say"},
		Messages:    []*agentv1.ReviewMessage{{Id: "m1", Body: "why did this go?"}},
	}})

	require.Equal(t, 1, view.SetPattern(regexp.MustCompile("gone.go")))

	at, ok := view.JumpMatch(-1, 1)
	require.True(t, ok)
	assert.Equal(t, view.GetLastRow(), at, "the match is the thread's own row")
}

// TestSearchIsRunAgainWhenTheRowsMove covers pinning rebuilding the foot section, which leaves
// old match rows pointing at the wrong thread or past the end of the list.
func TestSearchIsRunAgainWhenTheRowsMove(t *testing.T) {
	view := twoHunkView()

	adriftThread := &agentv1.ReviewThread{
		Id: "t1", FilePath: "gone.go", NewStart: 12,
		QuotedLines: []string{"+gone"},
		Messages:    []*agentv1.ReviewMessage{{Id: "m1", Body: "why did this go?"}},
	}

	view.PinThreads([]*agentv1.ReviewThread{adriftThread})

	require.Equal(t, 1, view.SetPattern(regexp.MustCompile("gone.go")))

	view.PinThreads(nil)

	assert.Zero(t, view.MatchCount(), "the row it matched is not there any more")

	at, ok := view.JumpMatch(-1, 1)
	assert.False(t, ok)
	assert.LessOrEqual(t, at, view.GetLastRow())
}

// TestSearchFindsFileNames covers the file headers, which are rows the cursor rests on and are
// how somebody reaches a file in a diff too long to scroll through.
func TestSearchFindsFileNames(t *testing.T) {
	view := twoHunkView()

	require.Equal(t, 1, view.SetPattern(regexp.MustCompile(`greet\.go`)))

	at, ok := view.JumpMatch(-1, 1)
	require.True(t, ok)
	assert.Equal(t, 0, at, "the match is the file's own header")
}

// TestSearchFindsAViewedFileName covers a file that has been read, whose header is the only row
// it has left and so the only way back into it.
func TestSearchFindsAViewedFileName(t *testing.T) {
	view := twoHunkView()
	view.SetFileViewed("greet.go", true)

	require.Equal(t, 1, view.SetPattern(regexp.MustCompile(`greet\.go`)))

	at, ok := view.JumpMatch(-1, 1)
	require.True(t, ok)
	assert.Equal(t, 0, at)
}

// TestFileNameMatchIsPainted covers the header a search found being drawn with the match on it.
func TestFileNameMatchIsPainted(t *testing.T) {
	restore := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(restore) })

	view := twoHunkView()
	require.Equal(t, 1, view.SetPattern(regexp.MustCompile(`eet\.g`)))

	drawn := view.RenderRow(0, NoCursor, 40, 0)
	assert.Equal(t, "greet.go", ansi.Strip(drawn))
	assert.Contains(t, drawn, fileStyle.Background(matchBackground).Render("eet.g"))

	onCursor := view.RenderRow(0, LiveCursor, 40, 0)
	assert.Equal(t, 40, lipgloss.Width(onCursor), "the cursor's row reaches the edge of the pane")
}
