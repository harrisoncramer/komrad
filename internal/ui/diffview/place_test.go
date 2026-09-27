package diffview

import (
	"testing"

	"komrad/internal/diff"
	agentv1 "komrad/proto/gen/agent/v1"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func threadAt(id, path string, oldStart, newStart int32, quote string) *agentv1.ReviewThread {
	return &agentv1.ReviewThread{
		Id:          id,
		BaseSha:     patchBaseSHA,
		FilePath:    path,
		OldStart:    oldStart,
		OldEnd:      oldStart,
		NewStart:    newStart,
		NewEnd:      newStart,
		QuotedLines: []string{quote},
	}
}

// TestPinHoldsWhenLinesShiftBeneathIt covers a line added at the top of the file, which moves
// every new-side number below it without changing the code a thread was written against.
func TestPinHoldsWhenLinesShiftBeneathIt(t *testing.T) {
	view := NewView(patchWith(diff.File{
		OldPath: "greet.go",
		NewPath: "greet.go",
		Hunks: []diff.Hunk{hunkOf(
			diff.Line{
				Kind:      diff.Addition,
				Text:      "// new",
				NewNumber: 1,
			},
			diff.Line{
				Kind:      diff.Context,
				Text:      "package greet",
				OldNumber: 1,
				NewNumber: 2,
			},
		)},
	}), false)

	thread := threadAt("t1", "greet.go", 1, 1, " package greet")
	view.PinThreads([]*agentv1.ReviewThread{thread})

	found, ok := view.GetThreadAt(4)
	require.True(t, ok, "the thread should still be on the line it was written against")
	assert.Equal(t, "t1", found.GetId())
	assert.False(t, view.IsGhosted(thread))
}

// TestPinFollowsCodeThatMoved covers a line the agent moved rather than changed, where neither
// side's numbers match any more and the text reads exactly as it did.
func TestPinFollowsCodeThatMoved(t *testing.T) {
	view := NewView(patchWith(diff.File{
		OldPath: "greet.go",
		NewPath: "greet.go",
		Hunks: []diff.Hunk{hunkOf(
			diff.Line{
				Kind:      diff.Addition,
				Text:      "func Hello() string {",
				NewNumber: 40,
			},
		)},
	}), false)

	thread := threadAt("t1", "greet.go", 0, 12, "+func Hello() string {")
	view.PinThreads([]*agentv1.ReviewThread{thread})

	found, ok := view.GetThreadAt(3)
	require.True(t, ok)
	assert.Equal(t, "t1", found.GetId())
	assert.False(t, view.IsGhosted(thread))
}

// TestPinGhostsWhenTheLineChangedUnderIt covers the old-side anchor landing the thread where
// it belongs on code that no longer says what the thread was about.
func TestPinGhostsWhenTheLineChangedUnderIt(t *testing.T) {
	view := NewView(patchWith(diff.File{
		OldPath: "greet.go",
		NewPath: "greet.go",
		Hunks: []diff.Hunk{hunkOf(
			diff.Line{
				Kind:      diff.Deletion,
				Text:      "return \"hello\"",
				OldNumber: 4,
			},
			diff.Line{
				Kind:      diff.Addition,
				Text:      "return \"hi\"",
				NewNumber: 4,
			},
		)},
	}), false)

	thread := threadAt("t1", "greet.go", 4, 4, " return \"hello\"")
	view.PinThreads([]*agentv1.ReviewThread{thread})

	found, ok := view.GetThreadAt(3)
	require.True(t, ok, "the thread stays on its old-side line")
	assert.Equal(t, "t1", found.GetId())
	assert.True(t, view.IsGhosted(thread))
}

// TestPinRefusesToGuessBetweenIdenticalLines covers two lines that read the same, where a
// guess is worse than saying where the thread was written and letting the reader look.
func TestPinRefusesToGuessBetweenIdenticalLines(t *testing.T) {
	view := NewView(patchWith(diff.File{
		OldPath: "greet.go",
		NewPath: "greet.go",
		Hunks: []diff.Hunk{hunkOf(
			diff.Line{
				Kind:      diff.Addition,
				Text:      "}",
				NewNumber: 8,
			},
			diff.Line{
				Kind:      diff.Addition,
				Text:      "}",
				NewNumber: 9,
			},
		)},
	}), false)

	thread := threadAt("t1", "greet.go", 0, 3, "+}")
	view.PinThreads([]*agentv1.ReviewThread{thread})

	assert.Equal(t, adrift, view.placed[thread.GetId()])
}

// TestAdriftThreadGetsItsOwnRow covers a thread whose file has left the diff keeping a row at
// the foot without moving the rows the patch produced.
func TestAdriftThreadGetsItsOwnRow(t *testing.T) {
	view := NewView(patchWith(diff.File{
		OldPath: "greet.go",
		NewPath: "greet.go",
		Hunks: []diff.Hunk{hunkOf(
			diff.Line{
				Kind:      diff.Context,
				Text:      "package greet",
				OldNumber: 1,
				NewNumber: 1,
			},
		)},
	}), false)

	lines := view.Len()

	thread := threadAt("t1", "gone.go", 0, 12, "+what this used to say")
	view.PinThreads([]*agentv1.ReviewThread{thread})

	require.Equal(t, lines+2, view.Len(), "one row saying what the section is, and one for the thread")

	last := view.GetLastRow()
	found, ok := view.GetThreadAt(last)
	require.True(t, ok)
	assert.Equal(t, "t1", found.GetId())
	assert.Equal(t, adrift, view.placed[thread.GetId()])

	place, ok := view.GetPlace(last)
	require.True(t, ok, "the cursor's place has to survive the diff being read again")
	assert.Equal(t, "t1", place.ThreadID)

	found2, ok := view.Locate(place)
	require.True(t, ok)
	assert.Equal(t, last, found2)
}

// TestPinningTwiceLeavesOneSection covers the rebuild: threads are pinned again on every read
// and every write, and the section at the foot is rebuilt rather than added to.
func TestPinningTwiceLeavesOneSection(t *testing.T) {
	view := NewView(patchWith(diff.File{
		OldPath: "greet.go",
		NewPath: "greet.go",
		Hunks: []diff.Hunk{hunkOf(
			diff.Line{
				Kind:      diff.Context,
				Text:      "package greet",
				OldNumber: 1,
				NewNumber: 1,
			},
		)},
	}), false)

	threads := []*agentv1.ReviewThread{threadAt("t1", "gone.go", 0, 12, "+what this used to say")}

	view.PinThreads(threads)
	once := view.Len()
	view.PinThreads(threads)

	assert.Equal(t, once, view.Len())
}

// TestASecondThreadOnALineGetsItsOwnRow covers the second of two conversations on one line going
// to the foot.
func TestASecondThreadOnALineGetsItsOwnRow(t *testing.T) {
	view := NewView(patchWith(diff.File{
		OldPath: "greet.go",
		NewPath: "greet.go",
		Hunks: []diff.Hunk{hunkOf(
			diff.Line{
				Kind:      diff.Context,
				Text:      "package greet",
				OldNumber: 1,
				NewNumber: 1,
			},
		)},
	}), false)

	first := threadAt("t1", "greet.go", 1, 1, " package greet")
	second := threadAt("t2", "greet.go", 1, 1, " package greet")
	view.PinThreads([]*agentv1.ReviewThread{first, second})

	onTheLine, ok := view.GetThreadAt(3)
	require.True(t, ok)
	assert.Equal(t, "t1", onTheLine.GetId())

	atTheFoot, ok := view.GetThreadAt(view.GetLastRow())
	require.True(t, ok, "the second thread is reachable rather than hidden behind the first")
	assert.Equal(t, "t2", atTheFoot.GetId())
	assert.Equal(t, adrift, view.placed[second.GetId()])
}

// TestAdriftThreadsSortByLineNotByText covers the foot section's order, which put line 10
// before line 9 while it sorted the formatted location.
func TestAdriftThreadsSortByLineNotByText(t *testing.T) {
	view := NewView(patchWith(diff.File{
		OldPath: "greet.go",
		NewPath: "greet.go",
		Hunks: []diff.Hunk{hunkOf(
			diff.Line{
				Kind:      diff.Context,
				Text:      "package greet",
				OldNumber: 1,
				NewNumber: 1,
			},
		)},
	}), false)

	view.PinThreads([]*agentv1.ReviewThread{
		threadAt("t1", "gone.go", 0, 10, "+ten"),
		threadAt("t2", "gone.go", 0, 9, "+nine"),
	})

	last := view.GetLastRow()

	ninth, ok := view.GetThreadAt(last - 1)
	require.True(t, ok)
	assert.Equal(t, "t2", ninth.GetId(), "line 9 comes before line 10")

	tenth, ok := view.GetThreadAt(last)
	require.True(t, ok)
	assert.Equal(t, "t1", tenth.GetId())
}

// TestPinTakesAQuoteStoredWithoutItsMarker covers a thread whose quote is stored as bare code
// rather than as the diff drew it.
func TestPinTakesAQuoteStoredWithoutItsMarker(t *testing.T) {
	view := NewView(patchWith(diff.File{
		OldPath: "greet.go",
		NewPath: "greet.go",
		Hunks: []diff.Hunk{hunkOf(
			diff.Line{
				Kind:      diff.Addition,
				Text:      "\treturn \"hi\"",
				NewNumber: 4,
			},
		)},
	}), false)

	thread := threadAt("t1", "greet.go", 0, 4, "\treturn \"hi\"")
	view.PinThreads([]*agentv1.ReviewThread{thread})

	found, ok := view.GetThreadAt(3)
	require.True(t, ok, "the thread stands on its own line")
	assert.Equal(t, "t1", found.GetId())
	assert.False(t, view.IsGhosted(thread), "the line reads exactly as the thread quoted it")
}

// TestPinFindsAMovedLineFromABareQuote covers the same storage against the text index, which is
// the last thing tried before a thread is given up on.
func TestPinFindsAMovedLineFromABareQuote(t *testing.T) {
	view := NewView(patchWith(diff.File{
		OldPath: "greet.go",
		NewPath: "greet.go",
		Hunks: []diff.Hunk{hunkOf(
			diff.Line{
				Kind:      diff.Addition,
				Text:      "\tname = \"world\"",
				NewNumber: 9,
			},
		)},
	}), false)

	thread := threadAt("t1", "greet.go", 0, 2, "\tname = \"world\"")
	view.PinThreads([]*agentv1.ReviewThread{thread})

	found, ok := view.GetThreadAt(3)
	require.True(t, ok, "the text index carries the thread to where the line went")
	assert.Equal(t, "t1", found.GetId())
}

// TestPinRefusesWhenTwoRowsReadTheSameWithoutMarkers covers the cost of matching without the
// marker: an added line and a context line that read alike are two candidates, not one.
func TestPinRefusesWhenTwoRowsReadTheSameWithoutMarkers(t *testing.T) {
	view := NewView(patchWith(diff.File{
		OldPath: "greet.go",
		NewPath: "greet.go",
		Hunks: []diff.Hunk{hunkOf(
			diff.Line{
				Kind:      diff.Context,
				Text:      "}",
				OldNumber: 7,
				NewNumber: 7,
			},
			diff.Line{
				Kind:      diff.Addition,
				Text:      "}",
				NewNumber: 8,
			},
		)},
	}), false)

	thread := threadAt("t1", "greet.go", 0, 3, "}")
	view.PinThreads([]*agentv1.ReviewThread{thread})

	assert.Equal(t, adrift, view.placed[thread.GetId()])
}

// TestPinKeepsAMarkedQuoteOffACollidingLine covers the hazard of holding both spellings in one
// index: a marked context quote whose text is a space and a brace reads the same as the stripped
// spelling of a row whose code is a space and a brace, and that row is a deletion.
func TestPinKeepsAMarkedQuoteOffACollidingLine(t *testing.T) {
	view := NewView(patchWith(diff.File{
		OldPath: "conf.yaml",
		NewPath: "conf.yaml",
		Hunks: []diff.Hunk{hunkOf(
			diff.Line{
				Kind:      diff.Deletion,
				Text:      " }",
				OldNumber: 5,
			},
		)},
	}), false)

	thread := threadAt("t1", "conf.yaml", 0, 200, " }")
	view.PinThreads([]*agentv1.ReviewThread{thread})

	assert.Equal(t, adrift, view.placed[thread.GetId()],
		"a context line the newer round draws as a deletion is gone")
}

// TestPinSkipsTheOldSideWhenItIsForeign covers a thread whose old side was counted from another
// commit, where its old-side line names an unrelated row of this patch.
func TestPinSkipsTheOldSideWhenItIsForeign(t *testing.T) {
	patch := patchWith(diff.File{
		OldPath: "greet.go",
		NewPath: "greet.go",
		Hunks: []diff.Hunk{hunkOf(
			diff.Line{
				Kind:      diff.Context,
				Text:      "other code",
				OldNumber: 38,
				NewNumber: 38,
			},
			diff.Line{
				Kind:      diff.Addition,
				Text:      "func Hello() {}",
				NewNumber: 60,
			},
		)},
	})

	thread := threadAt("t1", "greet.go", 38, 41, " func Hello() {}")

	measured := NewView(patch, false)
	measured.PinThreads([]*agentv1.ReviewThread{thread})
	require.Equal(t, ghosted, measured.placed[thread.GetId()],
		"the old-side arm lands it on the row that happens to hold line 38")

	elsewhere := threadAt("t2", "greet.go", 38, 41, " func Hello() {}")
	elsewhere.BaseSha = "0000000000000000000000000000000000000000"

	foreign := NewView(patch, false)
	foreign.PinThreads([]*agentv1.ReviewThread{elsewhere})

	assert.Equal(t, adrift, foreign.placed[elsewhere.GetId()],
		"the foot is the honest home for a thread nothing can place")

	for row := range foreign.Len() {
		found, ok := foreign.GetThreadAt(row)
		if !ok || found.GetId() != "t2" {
			continue
		}

		quote, drawn := foreign.quoteAt(row)
		assert.False(t, drawn && quote == " other code",
			"and never the row that happens to hold old line 38")
	}
}

// TestPinHoldsASpaceIndentedQuoteThatNothingTouched covers a bare quote of a line indented with
// spaces, whose first byte is the byte a context marker is also spelled with.
func TestPinHoldsASpaceIndentedQuoteThatNothingTouched(t *testing.T) {
	view := NewView(patchWith(diff.File{
		OldPath: "agent.proto",
		NewPath: "agent.proto",
		Hunks: []diff.Hunk{hunkOf(
			diff.Line{
				Kind:      diff.Addition,
				Text:      "  int32 local_review_rounds = 40;",
				NewNumber: 10,
			},
			diff.Line{
				Kind:      diff.Context,
				Text:      "  bool in_local_review = 41;",
				OldNumber: 10,
				NewNumber: 11,
			},
		)},
	}), false)

	thread := threadAt("t1", "agent.proto", 10, 11, "  bool in_local_review = 41;")
	view.PinThreads([]*agentv1.ReviewThread{thread})

	assert.Equal(t, pinned, view.placed[thread.GetId()],
		"nothing was written over that line, so it should not read as ghosted")
}

// TestPinRefusesASpaceIndentedQuoteThatTwoRowsAnswerTo covers the same quote reading as a bare
// line on one row and as the drawn spelling of its neighbour.
func TestPinRefusesASpaceIndentedQuoteThatTwoRowsAnswerTo(t *testing.T) {
	view := NewView(patchWith(diff.File{
		OldPath: "steps.yml",
		NewPath: "steps.yml",
		Hunks: []diff.Hunk{hunkOf(
			diff.Line{
				Kind:      diff.Addition,
				Text:      " run: check",
				NewNumber: 20,
			},
			diff.Line{
				Kind:      diff.Context,
				Text:      "run: check",
				OldNumber: 4,
				NewNumber: 21,
			},
		)},
	}), false)

	thread := threadAt("t1", "steps.yml", 0, 99, " run: check")
	view.PinThreads([]*agentv1.ReviewThread{thread})

	assert.Equal(t, adrift, view.placed[thread.GetId()],
		"the foot is honest where the neighbour row is a guess")
}

// renamedPatch is a file the round renamed, whose lines the diff draws under the new name.
func renamedPatch() *diff.Patch {
	return patchWith(diff.File{
		OldPath: "greet.go",
		NewPath: "hello.go",
		Hunks: []diff.Hunk{hunkOf(
			diff.Line{
				Kind:      diff.Context,
				Text:      "package greet",
				OldNumber: 1,
				NewNumber: 1,
			},
			diff.Line{
				Kind:      diff.Deletion,
				Text:      "old",
				OldNumber: 2,
			},
			diff.Line{
				Kind:      diff.Addition,
				Text:      "new",
				NewNumber: 2,
			},
		)},
	})
}

// TestPinFindsTheOldSideOfARenamedFile covers a thread whose old-side line is drawn in the patch
// under a name the thread has never heard of.
func TestPinFindsTheOldSideOfARenamedFile(t *testing.T) {
	view := NewView(renamedPatch(), false)

	thread := threadAt("t1", "greet.go", 1, 1, " package greet // gone")
	view.SetTraced(map[string]Traced{"t1": {FilePath: "hello.go", FileOnly: true}})
	view.PinThreads([]*agentv1.ReviewThread{thread})

	require.Equal(t, ghosted, view.placed[thread.GetId()],
		"old line 1 of the shared base is on screen under the new name")

	found, ok := view.GetThreadAt(3)
	require.True(t, ok)
	assert.Equal(t, "t1", found.GetId())
}

// TestPinFindsTheQuoteOfARenamedFile covers the text arm reaching a line the rename moved, where
// trace says the file survived and the lines did not.
func TestPinFindsTheQuoteOfARenamedFile(t *testing.T) {
	view := NewView(renamedPatch(), false)

	thread := threadAt("t1", "greet.go", 0, 9, "+new")
	view.SetTraced(map[string]Traced{"t1": {FilePath: "hello.go", FileOnly: true}})
	view.PinThreads([]*agentv1.ReviewThread{thread})

	assert.Equal(t, pinned, view.placed[thread.GetId()],
		"the line the thread quoted is drawn unchanged under the new name")
}

// TestPinHoldsADrawnQuoteOnARemovedLine covers the spelling a row is actually written in, which
// answers for itself and is not weighed by the side the thread's line was counted on.
func TestPinHoldsADrawnQuoteOnARemovedLine(t *testing.T) {
	view := NewView(patchWith(diff.File{
		OldPath: "greet.go",
		NewPath: "greet.go",
		Hunks: []diff.Hunk{hunkOf(
			diff.Line{
				Kind:      diff.Deletion,
				Text:      "gone",
				OldNumber: 2,
			},
		)},
	}), false)

	thread := threadAt("t1", "greet.go", 2, 2, "-gone")
	thread.BaseSha = "0000000000000000000000000000000000000000"
	view.PinThreads([]*agentv1.ReviewThread{thread})

	found, ok := view.GetThreadAt(3)
	require.True(t, ok, "the row the diff draws that text on is the row")
	assert.Equal(t, "t1", found.GetId())
}

// TestPinStillReadsAnIndentAsUnchanged holds the mislabel reading both spellings leaves behind,
// which only a recorded spelling can take away.
func TestPinStillReadsAnIndentAsUnchanged(t *testing.T) {
	view := NewView(patchWith(diff.File{
		OldPath: "conf.yaml",
		NewPath: "conf.yaml",
		Hunks: []diff.Hunk{hunkOf(
			diff.Line{
				Kind:      diff.Deletion,
				Text:      "x",
				OldNumber: 3,
			},
			diff.Line{
				Kind:      diff.Addition,
				Text:      " x",
				NewNumber: 3,
			},
		)},
	}), false)

	thread := threadAt("t1", "conf.yaml", 3, 3, " x")
	view.SetTraced(map[string]Traced{"t1": {FilePath: "conf.yaml", NewStart: 3}})
	view.PinThreads([]*agentv1.ReviewThread{thread})

	assert.Equal(t, pinned, view.placed[thread.GetId()],
		"the drawn spelling of the line before the indent reads as the bare spelling of it after")
}

// expandableView is a file whose patch draws one changed line in the middle of it, with the whole
// of its new side to hand.
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

// headingAbove is the foot section a thread was sorted into.
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

// TestACommentOnAFileThatHasLeftTheChangeSaysSo covers the whole-change diff, where a file the
// patch does not draw is a file that is gone.
func TestACommentOnAFileThatHasLeftTheChangeSaysSo(t *testing.T) {
	view := expandableView()

	thread := threadAt("t2", "departed.go", 3, 3, "+gone")
	view.PinThreads([]*agentv1.ReviewThread{thread})

	require.Equal(t, adrift, view.placed["t2"], "the patch draws no such file")
	assert.Equal(t, goneNotice, headingAbove(t, view, "t2"))
}

// TestACommentOutsideANarrowedDiffIsNotGone covers the same comment on a diff that is a slice of
// the change, where the file may be in the rounds the reader has stepped past.
func TestACommentOutsideANarrowedDiffIsNotGone(t *testing.T) {
	view := expandableView()
	view.SetNarrowed(true)

	thread := threadAt("t1", "departed.go", 3, 3, "+gone")
	view.PinThreads([]*agentv1.ReviewThread{thread})

	require.Equal(t, adrift, view.placed["t1"])
	assert.Equal(t, outsideNotice, headingAbove(t, view, "t1"))
}

// TestCommentsOnTheChangeWaitAtTheFoot covers several comments on the change as a whole: the row
// for the change stays free to start another, it says how many there are, and the comments wait in
// a section at the foot, none of them counted as moved.
func TestCommentsOnTheChangeWaitAtTheFoot(t *testing.T) {
	view := NewView(twoFilePatch(), false)
	settled := newChangeThread("t1", "2026-09-27T00:00:00Z")
	open := newChangeThread("t2", "")
	onLine := threadAt("t3", "greet.go", 0, 2, "+// greet")

	view.PinThreads([]*agentv1.ReviewThread{settled, open, onLine})

	_, ok := view.GetThreadAt(0)
	assert.False(t, ok, "the row is left to start another comment")
	assert.Contains(t, ansi.Strip(view.RenderRow(0, NoCursor, 60, 0)), "the change as a whole  2 at the end of the diff")

	notice := view.Len() - 3
	assert.Contains(t, ansi.Strip(view.RenderRow(notice, NoCursor, 60, 0)), "on the change as a whole")

	for at, want := range []string{"t1", "t2"} {
		found, ok := view.GetThreadAt(notice + 1 + at)
		require.True(t, ok)
		assert.Equal(t, want, found.GetId())
	}

	assert.False(t, view.HasMoved(settled))
	assert.False(t, view.HasMoved(open))
	assert.False(t, view.HasMoved(onLine))
}

func newChangeThread(id, resolvedAt string) *agentv1.ReviewThread {
	return &agentv1.ReviewThread{
		Id:         id,
		BaseSha:    patchBaseSHA,
		ResolvedAt: resolvedAt,
	}
}
