package history

import (
	"testing"

	"komrad/internal/git"
	"komrad/internal/gittest"
	"komrad/internal/position"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTraceThreadsFollowsALineThroughGit covers the whole tracing path: a comment on line four of
// the first snapshot is line eight of the second, which only the mapping between the two can say.
func TestTraceThreadsFollowsALineThroughGit(t *testing.T) {
	repo := &gittest.Repo{Mappings: map[string][]position.Mapping{gittest.NamePair(firstSHA, secondSHA): insertedAHelper}}

	rounds := []Round{
		{
			ID:          "r1",
			Ordinal:     1,
			SnapshotSHA: firstSHA,
		},
		{
			ID:          "r2",
			Ordinal:     2,
			SnapshotSHA: secondSHA,
		},
	}
	threads := []Thread{{
		ID:       "t1",
		RoundID:  "r1",
		FilePath: tracedFile,
		NewStart: 4,
		NewEnd:   4,
	}}

	traced := traceThreads(t.Context(), traceThreadsParams{
		Tracer:  NewTracer(repo),
		Dir:     traceDir,
		ToSHA:   secondSHA,
		Threads: threads,
		Rounds:  rounds,
	})

	found, ok := traced["t1"]
	require.True(t, ok, "the thread was traced")
	assert.Equal(t, tracedFile, found.FilePath)
	assert.Equal(t, int32(8), found.NewStart, "four lines arrived above it")
	assert.False(t, found.FileOnly)

	assert.Equal(t, []git.MapRangesParams{{
		Dir:     traceDir,
		FromSHA: firstSHA,
		ToSHA:   secondSHA,
		Paths:   []string{tracedFile},
	}}, repo.MappingsAsked(),
		"the pair read is the snapshot it was written against and the one on screen")
}

// TestTraceThreadsMarksTheRoundOnScreenAsExact covers a thread written against the snapshot on
// screen, whose numbers are counted in that tree and so need no git call to be trusted.
func TestTraceThreadsMarksTheRoundOnScreenAsExact(t *testing.T) {
	repo := &gittest.Repo{}

	traced := traceThreads(t.Context(), traceThreadsParams{
		Tracer: NewTracer(repo),
		Dir:    traceDir,
		ToSHA:  secondSHA,
		Threads: []Thread{{
			ID:       "t1",
			RoundID:  "r2",
			FilePath: tracedFile,
			NewStart: 8,
			NewEnd:   8,
		}},
		Rounds: []Round{{
			ID:          "r2",
			Ordinal:     2,
			SnapshotSHA: secondSHA,
		}},
	})

	found, ok := traced["t1"]
	require.True(t, ok, "the thread is placed rather than left to another attempt")
	assert.Equal(t, tracedFile, found.FilePath)
	assert.Equal(t, int32(8), found.NewStart, "the number it was written at is the number it holds")
	assert.True(t, found.Exact, "and it is exact rather than traced")
	assert.Empty(t, repo.MappingsAsked(), "nothing was read to place it")
}

// TestTraceThreadsKeepsTheFileWhenTheLinesAreGone covers code the agent wrote over: the file is
// still there and the lines are not.
func TestTraceThreadsKeepsTheFileWhenTheLinesAreGone(t *testing.T) {
	repo := &gittest.Repo{Mappings: map[string][]position.Mapping{gittest.NamePair(firstSHA, thirdSHA): rewroteTheLine}}

	traced := traceThreads(t.Context(), traceThreadsParams{
		Tracer: NewTracer(repo),
		Dir:    traceDir,
		ToSHA:  thirdSHA,
		Threads: []Thread{{
			ID:       "t1",
			RoundID:  "r1",
			FilePath: tracedFile,
			NewStart: 4,
			NewEnd:   4,
		}},
		Rounds: []Round{{
			ID:          "r1",
			Ordinal:     1,
			SnapshotSHA: firstSHA,
		}},
	})

	found, ok := traced["t1"]
	require.True(t, ok)
	assert.True(t, found.FileOnly, "the line it was about is gone")
	assert.Equal(t, tracedFile, found.FilePath, "and the file it was in is not")
	assert.Zero(t, found.NewStart)
}

// TestTraceThreadsSkipsRoundsTheScreenWillNotRender covers a chapter step: the diff ends at an
// earlier snapshot, so a thread from a later round is not rendered there and tracing it would run a
// backwards diff of the whole tree for an answer nothing reads.
func TestTraceThreadsSkipsRoundsTheScreenWillNotRender(t *testing.T) {
	repo := &gittest.Repo{Mappings: map[string][]position.Mapping{gittest.NamePair(secondSHA, firstSHA): insertedAHelper}}

	rounds := []Round{
		{
			ID:          "r1",
			Ordinal:     1,
			SnapshotSHA: firstSHA,
		},
		{
			ID:          "r2",
			Ordinal:     2,
			SnapshotSHA: secondSHA,
		},
	}
	threads := []Thread{
		{
			ID:       "later",
			RoundID:  "r2",
			FilePath: tracedFile,
			NewStart: 8,
			NewEnd:   8,
		},
	}

	// The reader has stepped back to the first round.
	traced := traceThreads(t.Context(), traceThreadsParams{
		Tracer:  NewTracer(repo),
		Dir:     traceDir,
		ToSHA:   firstSHA,
		Threads: threads,
		Rounds:  rounds,
	})

	assert.Empty(t, traced, "a later round is not traced backwards into an earlier one")
	assert.Empty(t, repo.MappingsAsked(), "and the diff it would have taken was never run")
}

// TestTraceThreadsGivesUpWholeRatherThanInPart covers one pair failing while another answers:
// keeping the answers that arrived would make placement depend on which pairs could be read, so
// two loads of the same screen would render the same comments in different places.
func TestTraceThreadsGivesUpWholeRatherThanInPart(t *testing.T) {
	repo := &gittest.Repo{Mappings: map[string][]position.Mapping{gittest.NamePair(firstSHA, secondSHA): insertedAHelper}}

	rounds := []Round{
		{
			ID:          "r1",
			Ordinal:     1,
			SnapshotSHA: firstSHA,
		},
		{
			ID:          "gone",
			Ordinal:     2,
			SnapshotSHA: unreadableSHA,
		},
		{
			ID:          "r3",
			Ordinal:     3,
			SnapshotSHA: secondSHA,
		},
	}
	threads := []Thread{
		{
			ID:       "readable",
			RoundID:  "r1",
			FilePath: tracedFile,
			NewStart: 4,
			NewEnd:   4,
		},
		{
			ID:       "unreadable",
			RoundID:  "gone",
			FilePath: tracedFile,
			NewStart: 4,
			NewEnd:   4,
		},
		{
			ID:       "onscreen",
			RoundID:  "r3",
			FilePath: tracedFile,
			NewStart: 8,
			NewEnd:   8,
		},
	}

	traced := traceThreads(t.Context(), traceThreadsParams{
		Tracer:  NewTracer(repo),
		Dir:     traceDir,
		ToSHA:   secondSHA,
		Threads: threads,
		Rounds:  rounds,
	})

	assert.NotContains(t, traced, "readable", "a pair nobody could read means no tracing rather than some of it")
	assert.NotContains(t, traced, "unreadable")

	found, ok := traced["onscreen"]
	require.True(t, ok, "the round on screen needs no git and so cannot be lost with it")
	assert.True(t, found.Exact)
}

// TestTraceThreadsMarksEveryRoundHoldingTheSnapshotOnScreen covers an agent that edited a file
// and then put it back, which takes a second round carrying a snapshot an earlier round already
// held.
func TestTraceThreadsMarksEveryRoundHoldingTheSnapshotOnScreen(t *testing.T) {
	repo := &gittest.Repo{}

	traced := traceThreads(t.Context(), traceThreadsParams{
		Tracer: NewTracer(repo),
		Dir:    traceDir,
		ToSHA:  secondSHA,
		Threads: []Thread{{
			ID:       "t1",
			RoundID:  "r1",
			FilePath: tracedFile,
			NewStart: 8,
			NewEnd:   8,
		}},
		Rounds: []Round{
			{
				ID:          "r1",
				Ordinal:     1,
				SnapshotSHA: secondSHA,
			},
			{
				ID:          "r2",
				Ordinal:     2,
				SnapshotSHA: firstSHA,
			},
			{
				ID:          "r3",
				Ordinal:     3,
				SnapshotSHA: secondSHA,
			},
		},
	})

	found, ok := traced["t1"]
	require.True(t, ok, "the earlier round holding this snapshot is on screen too")
	assert.True(t, found.Exact)
	assert.Equal(t, int32(8), found.NewStart)
	assert.Empty(t, repo.MappingsAsked(), "the round it was written against is the one on screen")
}

const (
	traceDir      = "/checkout"
	firstSHA      = "1111111111111111111111111111111111111111"
	secondSHA     = "2222222222222222222222222222222222222222"
	thirdSHA      = "3333333333333333333333333333333333333333"
	unreadableSHA = "0000000000000000000000000000000000000000"
	tracedFile    = "greet.go"
)

// insertedAHelper is what git reports between two snapshots where the second put a four line
// helper above the commented line, taken from a real repository.
var insertedAHelper = []position.Mapping{{
	File:   position.FileMapping{OldPath: tracedFile, NewPath: tracedFile},
	Ranges: []position.RangeMapping{{Old: position.Range{Start: 3, End: 3}, New: position.Range{Start: 3, End: 7}}},
}}

// rewroteTheLine is what git reports where the commented line itself was written over, which
// leaves the file in place and the line nowhere.
var rewroteTheLine = []position.Mapping{{
	File:   position.FileMapping{OldPath: tracedFile, NewPath: tracedFile},
	Ranges: []position.RangeMapping{{Old: position.Range{Start: 4, End: 5}, New: position.Range{Start: 4, End: 5}}},
}}
