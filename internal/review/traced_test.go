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
