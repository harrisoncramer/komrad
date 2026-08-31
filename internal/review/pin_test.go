package review

import (
	"testing"

	"weave/internal/diff"
	agentv1 "weave/proto/gen/agent/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func threadAt(id, path string, oldStart, newStart int32, quote string) *agentv1.ReviewThread {
	return &agentv1.ReviewThread{
		Id:          id,
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

	found, ok := view.ThreadAt(3)
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

	found, ok := view.ThreadAt(2)
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

	found, ok := view.ThreadAt(2)
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

	last := view.Last()
	found, ok := view.ThreadAt(last)
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

// TestMarkForSaysWhichThreadIsWhich covers the gutter glyph ranking an unread reply first,
// then settling, then whether the thread still stands on the code it was written against.
func TestMarkForSaysWhichThreadIsWhich(t *testing.T) {
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

	thread := threadAt("t1", "greet.go", 1, 1, " package greet")
	view.PinThreads([]*agentv1.ReviewThread{thread})

	mark, style := view.markFor(thread)
	assert.Equal(t, threadMark, mark)
	assert.Equal(t, threadMarkStyle, style)

	thread.ResolvedAt = "2026-08-27T12:00:00Z"
	mark, style = view.markFor(thread)
	assert.Equal(t, settledMark, mark)
	assert.Equal(t, settledMarkStyle, style, "a settled thread is green rather than yellow")

	thread.UnreadByHuman = true
	mark, _ = view.markFor(thread)
	assert.Equal(t, unreadMark, mark, "an unread reply outranks the settling")
}

// TestASecondThreadOnALineGetsItsOwnRow covers the second of two conversations on one line
// going to the foot, since only one can be the thread the cursor reaches there.
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

	onTheLine, ok := view.ThreadAt(2)
	require.True(t, ok)
	assert.Equal(t, "t1", onTheLine.GetId())

	atTheFoot, ok := view.ThreadAt(view.Last())
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

	last := view.Last()

	ninth, ok := view.ThreadAt(last - 1)
	require.True(t, ok)
	assert.Equal(t, "t2", ninth.GetId(), "line 9 comes before line 10")

	tenth, ok := view.ThreadAt(last)
	require.True(t, ok)
	assert.Equal(t, "t1", tenth.GetId())
}
