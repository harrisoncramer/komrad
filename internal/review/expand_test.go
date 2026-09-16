package review

import (
	"testing"

	"weave/internal/diff"
	agentv1 "weave/proto/gen/agent/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// expandableView is a file whose patch draws one changed line in the middle of it, with the whole
// of its new side to hand, which is what the expand key opens out.
func expandableView() *View {
	return NewView(patchWith(diff.File{
		OldPath:   "tall.go",
		NewPath:   "tall.go",
		NewSource: "one\ntwo\nthree\nfour\nfive\nsix\nseven\neight\n",
		Hunks: []diff.Hunk{{
			Header:   "@@ -4,2 +4,2 @@",
			NewStart: 4,
			Lines: []diff.Line{
				{
					Kind:      diff.Context,
					Text:      "four",
					OldNumber: 4,
					NewNumber: 4,
				},
				{
					Kind:      diff.Addition,
					Text:      "five",
					NewNumber: 5,
				},
			},
		}},
	}), false)
}

// headingAbove is the foot section a thread was sorted into, which is the notice drawn over it.
func headingAbove(t *testing.T, view *View, id string) string {
	t.Helper()

	heading := ""
	for _, current := range view.rows {
		if current.kind == metadataRow {
			heading = current.text
		}

		if current.kind == threadRow && current.thread.GetId() == id {
			return heading
		}
	}

	require.FailNow(t, "that thread has no row")

	return ""
}

// TestAnAdriftCommentOnADrawnLineReadsAsChanged covers the agent rewriting the code under a
// comment after reading the review, where the line is on screen and calling it off screen would
// be false.
func TestAnAdriftCommentOnADrawnLineReadsAsChanged(t *testing.T) {
	view := expandableView()

	thread := threadAt("t1", "tall.go", 0, 5, "+something else entirely")
	view.PinThreads([]*agentv1.ReviewThread{thread})

	require.Equal(t, adrift, view.placed["t1"], "the quote matches nothing, so it has no line")
	assert.Equal(t, changedNotice, headingAbove(t, view, "t1"))
}

// TestAnAdriftCommentOnAnUndrawnLineReadsAsOffScreen covers the line the reader themselves took
// off the screen by drawing the file tight again, which the agent has not touched.
func TestAnAdriftCommentOnAnUndrawnLineReadsAsOffScreen(t *testing.T) {
	view := expandableView()

	thread := threadAt("t1", "tall.go", 8, 8, "+eight")
	view.PinThreads([]*agentv1.ReviewThread{thread})

	require.Equal(t, adrift, view.placed["t1"], "the patch draws no line eight")
	assert.Equal(t, outsideNotice, headingAbove(t, view, "t1"))
}
