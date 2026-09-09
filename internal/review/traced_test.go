package review

import (
	"testing"

	"weave/internal/diff"
	agentv1 "weave/proto/gen/agent/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTracingPlacesAThreadThatMoved covers the case no string attempt can reach: the thread was
// written on a line the agent had added, so it has no old-side number to fall back to, the code
// around it has been rewritten so its quote no longer appears, and the line itself now sits at a
// different number.
func TestTracingPlacesAThreadThatMoved(t *testing.T) {
	view := NewView(patchWith(diff.File{
		OldPath: "greet.go",
		NewPath: "greet.go",
		Hunks: []diff.Hunk{hunkOf(
			diff.Line{
				Kind:      diff.Addition,
				Text:      "return fmt.Sprintf(\"hello %s\", nameOr(name))",
				NewNumber: 9,
			},
		)},
	}), false)

	// Written at line 4 of an earlier snapshot, on a line that side had just added.
	thread := threadAt("t1", "greet.go", 0, 4, "+\treturn fmt.Sprintf(\"hello %s\", name)")

	view.SetTraced(map[string]Traced{"t1": {FilePath: "greet.go", NewStart: 9}})
	view.PinThreads([]*agentv1.ReviewThread{thread})

	found, ok := view.GetThreadAt(2)
	require.True(t, ok, "the thread is drawn on the line git says it became")
	assert.Equal(t, "t1", found.GetId())
}

// TestTracingFollowsARename covers a file the agent moved, where the thread's own path no longer
// appears in the diff at all.
func TestTracingFollowsARename(t *testing.T) {
	view := NewView(patchWith(diff.File{
		OldPath: "greet.go",
		NewPath: "hello.go",
		Hunks: []diff.Hunk{hunkOf(
			diff.Line{
				Kind:      diff.Context,
				Text:      "return \"hello\"",
				OldNumber: 4,
				NewNumber: 4,
			},
		)},
	}), false)

	thread := threadAt("t1", "greet.go", 4, 4, " return \"hello\"")

	view.SetTraced(map[string]Traced{"t1": {FilePath: "hello.go", NewStart: 4}})
	view.PinThreads([]*agentv1.ReviewThread{thread})

	found, ok := view.GetThreadAt(2)
	require.True(t, ok, "the thread follows its file")
	assert.Equal(t, "t1", found.GetId())
}

// TestTracingLeavesTheLinesToTheFoot covers lines the agent wrote over: the file is still there
// and the lines are not, so the thread keeps its row at the foot rather than claiming a line it
// was never about.
func TestTracingLeavesTheLinesToTheFoot(t *testing.T) {
	view := NewView(patchWith(diff.File{
		OldPath: "greet.go",
		NewPath: "greet.go",
		Hunks: []diff.Hunk{hunkOf(
			diff.Line{
				Kind:      diff.Addition,
				Text:      "return greeting(name)",
				NewNumber: 4,
			},
		)},
	}), false)

	thread := threadAt("t1", "greet.go", 40, 40, " something the agent deleted")

	view.SetTraced(map[string]Traced{"t1": {FilePath: "greet.go", FileOnly: true}})
	view.PinThreads([]*agentv1.ReviewThread{thread})

	assert.Equal(t, adrift, view.placed[thread.GetId()], "a thread with no lines left is not placed on one")
}

// TestTracingLeavesTheOtherAttemptsAlone covers a traced line the patch does not draw, which
// happens whenever the diff is narrowed or a path is excluded: the answer is dropped and the
// attempts that follow it still get their turn.
func TestTracingLeavesTheOtherAttemptsAlone(t *testing.T) {
	view := NewView(patchWith(diff.File{
		OldPath: "greet.go",
		NewPath: "greet.go",
		Hunks: []diff.Hunk{hunkOf(
			diff.Line{
				Kind:      diff.Context,
				Text:      "return \"hello\"",
				OldNumber: 4,
				NewNumber: 4,
			},
		)},
	}), false)

	thread := threadAt("t1", "greet.go", 4, 4, " return \"hello\"")

	view.SetTraced(map[string]Traced{"t1": {FilePath: "greet.go", NewStart: 900}})
	view.PinThreads([]*agentv1.ReviewThread{thread})

	found, ok := view.GetThreadAt(2)
	require.True(t, ok, "a line nobody drew does not cost the thread its own coordinates")
	assert.Equal(t, "t1", found.GetId())
}

// TestAnExactCoordinateKeepsItsRowFromATracedGuess covers the two thread orders meeting one row:
// placement walks the arms across every thread, so a traced answer cannot take a row that a
// thread's own coordinates name, whichever of them the query returned first.
func TestAnExactCoordinateKeepsItsRowFromATracedGuess(t *testing.T) {
	view := NewView(patchWith(diff.File{
		OldPath: "greet.go",
		NewPath: "greet.go",
		Hunks: []diff.Hunk{hunkOf(
			diff.Line{
				Kind:      diff.Addition,
				Text:      "return greeting(nameOr(name))",
				NewNumber: 4,
			},
		)},
	}), false)

	// The older thread has no old side and a quote that has gone, so only tracing can place it,
	// and tracing says the row the newer thread was written on.
	older := threadAt("older", "greet.go", 0, 9, "+return fmt.Sprintf(\"hello\")")
	newer := threadAt("newer", "greet.go", 0, 4, "+return greeting(nameOr(name))")

	view.SetTraced(map[string]Traced{"older": {FilePath: "greet.go", NewStart: 4}})

	// The listing orders by round, so the older thread is offered its row first.
	view.PinThreads([]*agentv1.ReviewThread{older, newer})

	found, ok := view.GetThreadAt(2)
	require.True(t, ok, "the row is drawn")
	assert.Equal(t, "newer", found.GetId(), "and belongs to the thread written against it")
	assert.Equal(t, pinned, view.placed["newer"])
	assert.Equal(t, adrift, view.placed["older"], "the traced guess gives way rather than taking it")
}

// TestAFollowedRenameFoldsWithItsFile covers the fold check reading the thread's own path while
// placement reads the traced one: a thread whose file was renamed and then read is collapsed
// rather than told it is outside a window nobody narrowed.
func TestAFollowedRenameFoldsWithItsFile(t *testing.T) {
	view := NewView(patchWith(diff.File{
		OldPath: "greet.go",
		NewPath: "hello.go",
		Hunks: []diff.Hunk{hunkOf(
			diff.Line{
				Kind:      diff.Context,
				Text:      "return \"hello\"",
				OldNumber: 4,
				NewNumber: 4,
			},
		)},
	}), false)

	thread := threadAt("t1", "greet.go", 4, 4, " return \"hello\"")

	view.SetTraced(map[string]Traced{"t1": {FilePath: "hello.go", NewStart: 4}})
	view.SetViewed(map[string]bool{"hello.go": true})
	view.PinThreads([]*agentv1.ReviewThread{thread})

	assert.Equal(t, collapsed, view.placed["t1"], "the thread folds away with the file it is in")
}

// renamedPatch is a file the agent moved, with a line both sides hold, one it deleted and one it
// added, so every arm below tracing has something to reach for.
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

// TestTheOldSideArmFollowsARename covers a renamed file whose lines tracing could not move,
// written against a line the agent has since rewritten so that nothing but the old side can
// reach it: the old side of a shared base numbers the same lines whatever the file is called
// now, so the arm has a row to give and the thread does not go to the foot.
func TestTheOldSideArmFollowsARename(t *testing.T) {
	view := NewView(renamedPatch(), false)

	thread := threadAt("t1", "greet.go", 2, 0, " what line two used to say")

	view.SetTraced(map[string]Traced{"t1": {FilePath: "hello.go", FileOnly: true}})
	view.PinThreads([]*agentv1.ReviewThread{thread})

	found, ok := view.GetThreadAt(3)
	require.True(t, ok, "the old-side line is drawn under the new path")
	assert.Equal(t, "t1", found.GetId())
	assert.Equal(t, ghosted, view.placed["t1"], "the place is right and the code there is not")
}

// TestTheQuoteArmFollowsARename covers the same file for a thread with no old side, where the
// text it quoted is still drawn and only the path it was stored under has moved.
func TestTheQuoteArmFollowsARename(t *testing.T) {
	view := NewView(renamedPatch(), false)

	thread := threadAt("t1", "greet.go", 0, 9, "+new")

	view.SetTraced(map[string]Traced{"t1": {FilePath: "hello.go", FileOnly: true}})
	view.PinThreads([]*agentv1.ReviewThread{thread})

	found, ok := view.GetThreadAt(4)
	require.True(t, ok, "the quoted text is drawn under the new path")
	assert.Equal(t, "t1", found.GetId())
	assert.Equal(t, pinned, view.placed["t1"])
}

// TestARenameIsFollowedWithoutTracing covers tracing answering for nothing at all, which is what
// a failed or timed out mapping leaves behind: the patch names the rename itself, so the arms
// below tracing still read the file under the path it is drawn as.
func TestARenameIsFollowedWithoutTracing(t *testing.T) {
	view := NewView(renamedPatch(), false)

	thread := threadAt("t1", "greet.go", 1, 1, " package greet")

	view.PinThreads([]*agentv1.ReviewThread{thread})

	found, ok := view.GetThreadAt(2)
	require.True(t, ok, "the rename in the patch is enough to place it")
	assert.Equal(t, "t1", found.GetId())
}

// TestARenamedThreadWithNoLineReadsAsChanged covers a thread in a renamed file that no arm can
// place, which belongs under the code-has-changed heading rather than being called outside a
// window nobody narrowed.
func TestARenamedThreadWithNoLineReadsAsChanged(t *testing.T) {
	view := NewView(renamedPatch(), false)

	thread := threadAt("t1", "greet.go", 40, 40, " something the agent deleted")

	view.PinThreads([]*agentv1.ReviewThread{thread})

	require.Equal(t, adrift, view.placed["t1"])
	assert.Equal(t, changedNotice, view.rows[view.GetLastRow()-1].text)
}
