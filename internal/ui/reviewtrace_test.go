package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	agentv1 "weave/proto/gen/agent/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// traceRepo is two snapshots of one file, where the second inserts a helper above the line the
// first commented on and renames nothing, so a line that did not change is at a new number.
func traceRepo(t *testing.T) (string, string, string) {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "greet.go")

	gitIn(t, dir, "init", "-b", "main")
	require.NoError(t, os.WriteFile(path,
		[]byte("package greet\n\nfunc Hello(name string) string {\n\treturn \"hello \" + name\n}\n"), 0o644))
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-m", "first")

	first := gitOut(t, dir, "rev-parse", "HEAD")

	require.NoError(t, os.WriteFile(path,
		[]byte("package greet\n\nfunc nameOr(name string) string {\n\treturn name\n}\n\n"+
			"func Hello(name string) string {\n\treturn \"hello \" + name\n}\n"), 0o644))
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-m", "second")

	return dir, first, gitOut(t, dir, "rev-parse", "HEAD")
}

func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()

	cmd := gitCmd(t, dir, args...)

	out, err := cmd.Output()
	require.NoError(t, err, strings.Join(args, " "))

	return strings.TrimSpace(string(out))
}

// TestTraceThreadsFollowsALineThroughGit covers the whole tracing path against a real repository:
// a comment on line four of the first snapshot is line eight of the second, and nothing but git
// can say so.
func TestTraceThreadsFollowsALineThroughGit(t *testing.T) {
	dir, first, second := traceRepo(t)

	rounds := []*agentv1.ReviewRound{
		{
			Id:          "r1",
			Ordinal:     1,
			SnapshotSha: first,
		},
		{
			Id:          "r2",
			Ordinal:     2,
			SnapshotSha: second,
		},
	}
	threads := []*agentv1.ReviewThread{{
		Id:       "t1",
		RoundId:  "r1",
		FilePath: "greet.go",
		NewStart: 4,
		NewEnd:   4,
	}}

	traced := traceThreads(t.Context(), traceThreadsParams{
		Dir:     dir,
		ToSHA:   second,
		Threads: threads,
		Rounds:  rounds,
	})

	found, ok := traced["t1"]
	require.True(t, ok, "the thread was traced")
	assert.Equal(t, "greet.go", found.FilePath)
	assert.Equal(t, int32(8), found.NewStart, "four lines arrived above it")
	assert.False(t, found.FileOnly)
}

// TestTraceThreadsMarksTheRoundOnScreenAsExact covers a thread written against the snapshot on
// screen, whose numbers are counted in that tree and so need no git call to be trusted.
func TestTraceThreadsMarksTheRoundOnScreenAsExact(t *testing.T) {
	dir, _, second := traceRepo(t)

	traced := traceThreads(t.Context(), traceThreadsParams{
		Dir:   dir,
		ToSHA: second,
		Threads: []*agentv1.ReviewThread{{
			Id:       "t1",
			RoundId:  "r2",
			FilePath: "greet.go",
			NewStart: 8,
			NewEnd:   8,
		}},
		Rounds: []*agentv1.ReviewRound{{
			Id:          "r2",
			Ordinal:     2,
			SnapshotSha: second,
		}},
	})

	found, ok := traced["t1"]
	require.True(t, ok, "the thread is placed rather than left to another attempt")
	assert.Equal(t, "greet.go", found.FilePath)
	assert.Equal(t, int32(8), found.NewStart, "the number it was written at is the number it holds")
	assert.True(t, found.Exact, "and it is exact rather than traced")
}

// TestTraceThreadsKeepsTheFileWhenTheLinesAreGone covers code the agent wrote over: the file is
// still there and the lines are not, which is a file-level answer rather than no answer.
func TestTraceThreadsKeepsTheFileWhenTheLinesAreGone(t *testing.T) {
	dir, first, _ := traceRepo(t)

	path := filepath.Join(dir, "greet.go")
	require.NoError(t, os.WriteFile(path,
		[]byte("package greet\n\nfunc Hello(name string) string {\n\treturn greeting(name)\n}\n"), 0o644))
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-m", "third")

	third := gitOut(t, dir, "rev-parse", "HEAD")

	traced := traceThreads(t.Context(), traceThreadsParams{
		Dir:   dir,
		ToSHA: third,
		Threads: []*agentv1.ReviewThread{{
			Id:       "t1",
			RoundId:  "r1",
			FilePath: "greet.go",
			NewStart: 4,
			NewEnd:   4,
		}},
		Rounds: []*agentv1.ReviewRound{{
			Id:          "r1",
			Ordinal:     1,
			SnapshotSha: first,
		}},
	})

	found, ok := traced["t1"]
	require.True(t, ok)
	assert.True(t, found.FileOnly, "the line it was about is gone")
	assert.Equal(t, "greet.go", found.FilePath, "and the file it was in is not")
	assert.Zero(t, found.NewStart)
}

// TestTraceThreadsSkipsRoundsTheScreenWillNotDraw covers a chapter step: the diff ends at an
// earlier snapshot, so a thread from a later round is not drawn there and tracing it would run a
// backwards diff of the whole tree for an answer nothing reads.
func TestTraceThreadsSkipsRoundsTheScreenWillNotDraw(t *testing.T) {
	dir, first, second := traceRepo(t)

	rounds := []*agentv1.ReviewRound{
		{
			Id:          "r1",
			Ordinal:     1,
			SnapshotSha: first,
		},
		{
			Id:          "r2",
			Ordinal:     2,
			SnapshotSha: second,
		},
	}
	threads := []*agentv1.ReviewThread{
		{
			Id:       "later",
			RoundId:  "r2",
			FilePath: "greet.go",
			NewStart: 8,
			NewEnd:   8,
		},
	}

	// The reader has stepped back to the first round, so that snapshot is what is drawn.
	traced := traceThreads(t.Context(), traceThreadsParams{
		Dir:     dir,
		ToSHA:   first,
		Threads: threads,
		Rounds:  rounds,
	})

	assert.Empty(t, traced, "a later round is not traced backwards into an earlier one")
}

// TestTraceThreadsGivesUpWholeRatherThanInPart covers one pair failing while another answers:
// keeping the answers that arrived would make placement depend on which pairs could be read, so
// two loads of the same screen would draw the same comments in different places.
func TestTraceThreadsGivesUpWholeRatherThanInPart(t *testing.T) {
	dir, first, second := traceRepo(t)

	rounds := []*agentv1.ReviewRound{
		{
			Id:          "r1",
			Ordinal:     1,
			SnapshotSha: first,
		},
		{
			Id:          "gone",
			Ordinal:     2,
			SnapshotSha: "0000000000000000000000000000000000000000",
		},
		{
			Id:          "r3",
			Ordinal:     3,
			SnapshotSha: second,
		},
	}
	threads := []*agentv1.ReviewThread{
		{
			Id:       "readable",
			RoundId:  "r1",
			FilePath: "greet.go",
			NewStart: 4,
			NewEnd:   4,
		},
		{
			Id:       "unreadable",
			RoundId:  "gone",
			FilePath: "greet.go",
			NewStart: 4,
			NewEnd:   4,
		},
		{
			Id:       "onscreen",
			RoundId:  "r3",
			FilePath: "greet.go",
			NewStart: 8,
			NewEnd:   8,
		},
	}

	traced := traceThreads(t.Context(), traceThreadsParams{
		Dir:     dir,
		ToSHA:   second,
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
	dir, first, second := traceRepo(t)

	traced := traceThreads(t.Context(), traceThreadsParams{
		Dir:   dir,
		ToSHA: second,
		Threads: []*agentv1.ReviewThread{{
			Id:       "t1",
			RoundId:  "r1",
			FilePath: "greet.go",
			NewStart: 8,
			NewEnd:   8,
		}},
		Rounds: []*agentv1.ReviewRound{
			{
				Id:          "r1",
				Ordinal:     1,
				SnapshotSha: second,
			},
			{
				Id:          "r2",
				Ordinal:     2,
				SnapshotSha: first,
			},
			{
				Id:          "r3",
				Ordinal:     3,
				SnapshotSha: second,
			},
		},
	})

	found, ok := traced["t1"]
	require.True(t, ok, "the earlier round holding this snapshot is on screen too")
	assert.True(t, found.Exact)
	assert.Equal(t, int32(8), found.NewStart)
}
