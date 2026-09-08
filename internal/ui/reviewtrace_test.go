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
		{Id: "r1", Ordinal: 1, SnapshotSha: first},
		{Id: "r2", Ordinal: 2, SnapshotSha: second},
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

// TestTraceThreadsLeavesTheCurrentRoundAlone covers a thread written against the snapshot on
// screen, which answers to its own coordinates and needs no tracing at all.
func TestTraceThreadsLeavesTheCurrentRoundAlone(t *testing.T) {
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
		Rounds: []*agentv1.ReviewRound{{Id: "r2", Ordinal: 2, SnapshotSha: second}},
	})

	assert.Empty(t, traced, "nothing to trace between one snapshot and itself")
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
		Rounds: []*agentv1.ReviewRound{{Id: "r1", Ordinal: 1, SnapshotSha: first}},
	})

	found, ok := traced["t1"]
	require.True(t, ok)
	assert.True(t, found.FileOnly, "the line it was about is gone")
	assert.Equal(t, "greet.go", found.FilePath, "and the file it was in is not")
	assert.Zero(t, found.NewStart)
}
