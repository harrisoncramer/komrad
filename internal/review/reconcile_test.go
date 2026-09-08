package review_test

import (
	"testing"

	"weave/internal/review"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const reconciledFile = "a.go"

func run(start, end int, lines ...string) review.Run {
	return review.Run{
		FilePath: reconciledFile,
		NewStart: start,
		NewEnd:   end,
		Lines:    lines,
	}
}

func pinned(id, path string, start, end int, lines ...string) review.Pinned {
	return review.Pinned{
		ID:       id,
		FilePath: path,
		NewStart: start,
		NewEnd:   end,
		Lines:    lines,
	}
}

// TestAHarvestOfNothingWritesNothing pins the answer that runs on every screen exit.
func TestAHarvestOfNothingWritesNothing(t *testing.T) {
	t.Parallel()

	plan := review.Reconcile(nil, nil, nil)

	assert.True(t, plan.IsEmpty())
}

// TestRunningTwiceOverTheSameEditWritesNothing is the property that lets harvest fire from
// several places without opening a thread each time.
func TestRunningTwiceOverTheSameEditWritesNothing(t *testing.T) {
	t.Parallel()

	runs := []review.Run{run(10, 12, "one", "two")}
	standing := []review.Pinned{pinned("t1", reconciledFile, 10, 12, "one", "two")}

	plan := review.Reconcile(runs, standing, nil)

	assert.True(t, plan.IsEmpty(), "an unchanged run should neither move nor reopen its thread")
}

// TestANewRunOpensAThread is the ordinary first harvest.
func TestANewRunOpensAThread(t *testing.T) {
	t.Parallel()

	runs := []review.Run{run(4, 4, "fixed")}

	plan := review.Reconcile(runs, nil, nil)

	require.Len(t, plan.Open, 1)
	assert.Equal(t, 4, plan.Open[0].NewStart)
	assert.Empty(t, plan.Delete)
}

// TestEditingTheSameRunAgainMovesItRatherThanOpeningASecond is what keeps a reviewer who keeps
// tweaking one line to one comment.
func TestEditingTheSameRunAgainMovesItRatherThanOpeningASecond(t *testing.T) {
	t.Parallel()

	runs := []review.Run{run(10, 12, "one", "changed")}
	standing := []review.Pinned{pinned("t1", reconciledFile, 10, 12, "one", "two")}

	plan := review.Reconcile(runs, standing, nil)

	require.Len(t, plan.Move, 1)
	assert.Equal(t, "t1", plan.Move[0].ID)
	assert.Empty(t, plan.Open)
	assert.Empty(t, plan.Delete)
}

// TestUndoingAnEditDeletesItsThread is how a comment is retracted, which is the whole reason
// reconciliation exists rather than appending.
func TestUndoingAnEditDeletesItsThread(t *testing.T) {
	t.Parallel()

	standing := []review.Pinned{pinned("t1", reconciledFile, 10, 12, "one")}

	plan := review.Reconcile(nil, standing, nil)

	assert.Equal(t, []string{"t1"}, plan.Delete)
}

// TestMergingTwoRunsKeepsTheLowerThread covers the reviewer editing the unchanged line between
// two runs, which git then reports as one.
func TestMergingTwoRunsKeepsTheLowerThread(t *testing.T) {
	t.Parallel()

	runs := []review.Run{run(10, 22, "whole")}
	standing := []review.Pinned{
		pinned("low", reconciledFile, 10, 12, "one"),
		pinned("high", reconciledFile, 20, 22, "two"),
	}

	plan := review.Reconcile(runs, standing, nil)

	require.Len(t, plan.Move, 1)
	assert.Equal(t, "low", plan.Move[0].ID)
	assert.Equal(t, []string{"high"}, plan.Delete)
	assert.Empty(t, plan.Open)
}

// TestSplittingARunLeavesTheLargerHalfWithTheThread is the reverse of merging.
func TestSplittingARunLeavesTheLargerHalfWithTheThread(t *testing.T) {
	t.Parallel()

	runs := []review.Run{
		run(10, 11, "small"),
		run(15, 22, "large"),
	}
	standing := []review.Pinned{pinned("t1", reconciledFile, 10, 22, "whole")}

	plan := review.Reconcile(runs, standing, nil)

	require.Len(t, plan.Move, 1)
	assert.Equal(t, "t1", plan.Move[0].ID)
	assert.Equal(t, 15, plan.Move[0].Run.NewStart, "the thread should follow the larger half")
	require.Len(t, plan.Open, 1)
	assert.Equal(t, 10, plan.Open[0].NewStart)
}

// TestARunOverAReleasedThreadIsLeftAlone is the rule that stops weave rewriting a comment the
// agent has already been sent.
func TestARunOverAReleasedThreadIsLeftAlone(t *testing.T) {
	t.Parallel()

	runs := []review.Run{run(5, 5, "mine")}
	frozen := []review.Frozen{{
		FilePath: reconciledFile,
		NewStart: 5,
		NewEnd:   5,
	}}

	plan := review.Reconcile(runs, nil, frozen)

	assert.Empty(t, plan.Open)
	require.Len(t, plan.Skipped, 1)
	assert.Equal(t, 5, plan.Skipped[0].NewStart)
}

// TestASkippedRunProtectsTheHarvestedThreadBesideIt is the finding that extending an edit onto a
// released comment's line would otherwise orphan and delete the comment next to it.
func TestASkippedRunProtectsTheHarvestedThreadBesideIt(t *testing.T) {
	t.Parallel()

	runs := []review.Run{run(10, 11, "widened")}
	standing := []review.Pinned{pinned("mine", reconciledFile, 10, 10, "was")}
	frozen := []review.Frozen{{
		FilePath: reconciledFile,
		NewStart: 11,
		NewEnd:   11,
	}}

	plan := review.Reconcile(runs, standing, frozen)

	assert.Empty(t, plan.Delete, "the harvested thread under a skipped run must survive")
	assert.Empty(t, plan.Move)
	assert.Empty(t, plan.Open)
	assert.Len(t, plan.Skipped, 1)
}

// TestThreadsInOtherFilesAreNotMatched pins that the file is part of the identity.
func TestThreadsInOtherFilesAreNotMatched(t *testing.T) {
	t.Parallel()

	runs := []review.Run{run(10, 10, "here")}
	standing := []review.Pinned{pinned("elsewhere", "b.go", 10, 10, "there")}

	plan := review.Reconcile(runs, standing, nil)

	require.Len(t, plan.Open, 1)
	assert.Equal(t, []string{"elsewhere"}, plan.Delete)
	assert.Empty(t, plan.Move)
}

// TestATieIsSettledTheSameWayTwice matters because an unstable answer would move a comment
// between two threads on every harvest.
func TestATieIsSettledTheSameWayTwice(t *testing.T) {
	t.Parallel()

	runs := []review.Run{run(10, 22, "whole")}
	standing := []review.Pinned{
		pinned("high", reconciledFile, 20, 22, "two"),
		pinned("low", reconciledFile, 10, 12, "one"),
	}

	first := review.Reconcile(runs, standing, nil)
	second := review.Reconcile(runs, standing, nil)

	assert.Equal(t, first, second)
	require.Len(t, first.Move, 1)
	assert.Equal(t, "low", first.Move[0].ID, "the lower thread should win an equal overlap")
}

// TestARunAtTheTopOfTheFileIsOrdinary guards the boundary the insertion widening reaches for.
func TestARunAtTheTopOfTheFileIsOrdinary(t *testing.T) {
	t.Parallel()

	runs := []review.Run{run(1, 1, "first")}
	standing := []review.Pinned{pinned("t1", reconciledFile, 1, 1, "was")}

	plan := review.Reconcile(runs, standing, nil)

	require.Len(t, plan.Move, 1)
	assert.Equal(t, "t1", plan.Move[0].ID)
}
