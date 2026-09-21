package ui

import (
	"context"
	"sync"
	"testing"

	"weave/internal/git"
	"weave/internal/position"
	agentv1 "weave/proto/gen/agent/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

// mappingGit answers a pair of snapshots with what git reported between them.
type mappingGit struct {
	fakeGit
	mu      sync.Mutex
	asked   []git.MapRangesParams
	answers map[string][]position.Mapping
}

// newMappingGit is a git that answers for the snapshots a test names and nothing else.
func newMappingGit(answers map[string][]position.Mapping) *mappingGit {
	return &mappingGit{answers: answers}
}

func (g *mappingGit) MapRanges(_ context.Context, params git.MapRangesParams) ([]position.Mapping, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.asked = append(g.asked, params)

	mappings, held := g.answers[pairOf(params.FromSHA, params.ToSHA)]
	if !held {
		return nil, errNoGit
	}

	return mappings, nil
}

// pairsAsked is every pair of snapshots the tracing asked git about.
func (g *mappingGit) pairsAsked() []git.MapRangesParams {
	g.mu.Lock()
	defer g.mu.Unlock()

	return append([]git.MapRangesParams(nil), g.asked...)
}

// TestTraceThreadsFollowsALineThroughGit covers the whole tracing path: a comment on line four of
// the first snapshot is line eight of the second, which only the mapping between the two can say.
func TestTraceThreadsFollowsALineThroughGit(t *testing.T) {
	repo := newMappingGit(map[string][]position.Mapping{pairOf(firstSHA, secondSHA): insertedAHelper})

	rounds := []*agentv1.ReviewRound{
		{
			Id:          "r1",
			Ordinal:     1,
			SnapshotSha: firstSHA,
		},
		{
			Id:          "r2",
			Ordinal:     2,
			SnapshotSha: secondSHA,
		},
	}
	threads := []*agentv1.ReviewThread{{
		Id:       "t1",
		RoundId:  "r1",
		FilePath: tracedFile,
		NewStart: 4,
		NewEnd:   4,
	}}

	traced := traceThreads(t.Context(), traceThreadsParams{
		Git:     repo,
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
	}}, repo.pairsAsked(),
		"the pair read is the snapshot it was written against and the one on screen")
}

// TestTraceThreadsMarksTheRoundOnScreenAsExact covers a thread written against the snapshot on
// screen, whose numbers are counted in that tree and so need no git call to be trusted.
func TestTraceThreadsMarksTheRoundOnScreenAsExact(t *testing.T) {
	repo := newMappingGit(nil)

	traced := traceThreads(t.Context(), traceThreadsParams{
		Git:   repo,
		Dir:   traceDir,
		ToSHA: secondSHA,
		Threads: []*agentv1.ReviewThread{{
			Id:       "t1",
			RoundId:  "r2",
			FilePath: tracedFile,
			NewStart: 8,
			NewEnd:   8,
		}},
		Rounds: []*agentv1.ReviewRound{{
			Id:          "r2",
			Ordinal:     2,
			SnapshotSha: secondSHA,
		}},
	})

	found, ok := traced["t1"]
	require.True(t, ok, "the thread is placed rather than left to another attempt")
	assert.Equal(t, tracedFile, found.FilePath)
	assert.Equal(t, int32(8), found.NewStart, "the number it was written at is the number it holds")
	assert.True(t, found.Exact, "and it is exact rather than traced")
	assert.Empty(t, repo.pairsAsked(), "nothing was read to place it")
}

// TestTraceThreadsKeepsTheFileWhenTheLinesAreGone covers code the agent wrote over: the file is
// still there and the lines are not.
func TestTraceThreadsKeepsTheFileWhenTheLinesAreGone(t *testing.T) {
	repo := newMappingGit(map[string][]position.Mapping{pairOf(firstSHA, thirdSHA): rewroteTheLine})

	traced := traceThreads(t.Context(), traceThreadsParams{
		Git:   repo,
		Dir:   traceDir,
		ToSHA: thirdSHA,
		Threads: []*agentv1.ReviewThread{{
			Id:       "t1",
			RoundId:  "r1",
			FilePath: tracedFile,
			NewStart: 4,
			NewEnd:   4,
		}},
		Rounds: []*agentv1.ReviewRound{{
			Id:          "r1",
			Ordinal:     1,
			SnapshotSha: firstSHA,
		}},
	})

	found, ok := traced["t1"]
	require.True(t, ok)
	assert.True(t, found.FileOnly, "the line it was about is gone")
	assert.Equal(t, tracedFile, found.FilePath, "and the file it was in is not")
	assert.Zero(t, found.NewStart)
}

// TestTraceThreadsSkipsRoundsTheScreenWillNotDraw covers a chapter step: the diff ends at an
// earlier snapshot, so a thread from a later round is not drawn there and tracing it would run a
// backwards diff of the whole tree for an answer nothing reads.
func TestTraceThreadsSkipsRoundsTheScreenWillNotDraw(t *testing.T) {
	repo := newMappingGit(map[string][]position.Mapping{pairOf(secondSHA, firstSHA): insertedAHelper})

	rounds := []*agentv1.ReviewRound{
		{
			Id:          "r1",
			Ordinal:     1,
			SnapshotSha: firstSHA,
		},
		{
			Id:          "r2",
			Ordinal:     2,
			SnapshotSha: secondSHA,
		},
	}
	threads := []*agentv1.ReviewThread{
		{
			Id:       "later",
			RoundId:  "r2",
			FilePath: tracedFile,
			NewStart: 8,
			NewEnd:   8,
		},
	}

	// The reader has stepped back to the first round.
	traced := traceThreads(t.Context(), traceThreadsParams{
		Git:     repo,
		Dir:     traceDir,
		ToSHA:   firstSHA,
		Threads: threads,
		Rounds:  rounds,
	})

	assert.Empty(t, traced, "a later round is not traced backwards into an earlier one")
	assert.Empty(t, repo.pairsAsked(), "and the diff it would have taken was never run")
}

// TestTraceThreadsGivesUpWholeRatherThanInPart covers one pair failing while another answers:
// keeping the answers that arrived would make placement depend on which pairs could be read, so
// two loads of the same screen would draw the same comments in different places.
func TestTraceThreadsGivesUpWholeRatherThanInPart(t *testing.T) {
	repo := newMappingGit(map[string][]position.Mapping{pairOf(firstSHA, secondSHA): insertedAHelper})

	rounds := []*agentv1.ReviewRound{
		{
			Id:          "r1",
			Ordinal:     1,
			SnapshotSha: firstSHA,
		},
		{
			Id:          "gone",
			Ordinal:     2,
			SnapshotSha: unreadableSHA,
		},
		{
			Id:          "r3",
			Ordinal:     3,
			SnapshotSha: secondSHA,
		},
	}
	threads := []*agentv1.ReviewThread{
		{
			Id:       "readable",
			RoundId:  "r1",
			FilePath: tracedFile,
			NewStart: 4,
			NewEnd:   4,
		},
		{
			Id:       "unreadable",
			RoundId:  "gone",
			FilePath: tracedFile,
			NewStart: 4,
			NewEnd:   4,
		},
		{
			Id:       "onscreen",
			RoundId:  "r3",
			FilePath: tracedFile,
			NewStart: 8,
			NewEnd:   8,
		},
	}

	traced := traceThreads(t.Context(), traceThreadsParams{
		Git:     repo,
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
	repo := newMappingGit(nil)

	traced := traceThreads(t.Context(), traceThreadsParams{
		Git:   repo,
		Dir:   traceDir,
		ToSHA: secondSHA,
		Threads: []*agentv1.ReviewThread{{
			Id:       "t1",
			RoundId:  "r1",
			FilePath: tracedFile,
			NewStart: 8,
			NewEnd:   8,
		}},
		Rounds: []*agentv1.ReviewRound{
			{
				Id:          "r1",
				Ordinal:     1,
				SnapshotSha: secondSHA,
			},
			{
				Id:          "r2",
				Ordinal:     2,
				SnapshotSha: firstSHA,
			},
			{
				Id:          "r3",
				Ordinal:     3,
				SnapshotSha: secondSHA,
			},
		},
	})

	found, ok := traced["t1"]
	require.True(t, ok, "the earlier round holding this snapshot is on screen too")
	assert.True(t, found.Exact)
	assert.Equal(t, int32(8), found.NewStart)
	assert.Empty(t, repo.pairsAsked(), "the round it was written against is the one on screen")
}
