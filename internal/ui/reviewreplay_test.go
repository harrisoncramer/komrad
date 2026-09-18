package ui

import (
	"testing"

	"weave/internal/git"
	agentv1 "weave/proto/gen/agent/v1"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	// olderBase is the commit the first round was measured from before the base moved on.
	olderBase = "1111111111111111111111111111111111111111"
	// replayedTree is the first round's snapshot as it would stand on the moved base.
	replayedTree = "replayed-round-one"
	// secondTree is the worktree after the base moved and the agent wrote again.
	secondTree = "tree-with-a-second-file"
)

// reviewAcrossAMovedBase is two reviewed rounds either side of the base moving: the first was
// measured from an older base, and git says what carrying it onto the newer one gives, or
// cannot say when nothing is stated.
func reviewAcrossAMovedBase(
	t *testing.T,
	repo *fakeRepo,
	agents *fakeReviewAgents,
	replayed *git.Replayed,
) model {
	t.Helper()

	m, _ := reviewModelOver(t, agents, repo)
	m = openReview(t, m)
	require.Len(t, agents.rounds, 1)

	first := agents.rounds[0]
	first.BaseSha = olderBase
	agents.reviews = []*agentv1.Review{{
		Id:           "v1",
		RoundId:      first.GetId(),
		RoundOrdinal: first.GetOrdinal(),
		AuthorKind:   agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_HUMAN,
		ReleasedAt:   "2026-09-08T10:00:00Z",
	}}

	// The two snapshots stand on different bases, so their own diff carries the base's move
	// through the first file as though the agent had written it.
	repo.wrote(wroteParams{
		Tree:  secondTree,
		Whole: fakePatchOfBothFiles,
		Since: fakePatchOfBothFiles,
		Sources: map[string]string{
			fakeFile:    fakeHeadSource,
			"second.go": "package greet\n\nfunc Second() {}\n",
		},
	})

	if replayed != nil {
		repo.replays = map[string]*git.Replayed{
			replayOf(olderBase, fakeBaseSHA, fakeSnapshotSHA): replayed,
		}
	}

	repo.patches[pairOf(replayedTree, snapshotOf(secondTree))] = fakePatchOfTheSecondFile

	m = openReview(t, m)
	require.Len(t, agents.rounds, 2)

	return m
}

// TestARoundAcrossAMovedBaseIsReplayedOntoIt is the case this exists for: the chapter between
// two reviews either side of a rebase compares as the agent's own work.
func TestARoundAcrossAMovedBaseIsReplayedOntoIt(t *testing.T) {
	agents := &fakeReviewAgents{}
	m := reviewAcrossAMovedBase(t, newFakeRepo(t.TempDir()), agents, &git.Replayed{Tree: replayedTree})

	m = pressReviewBase(t, m)

	assert.Equal(t, replayedTree, m.review.base, "the old side is the first round carried onto the moved base")
	assert.Equal(t, snapshotOf(secondTree), m.review.head)

	files := m.review.view.Stat().Files
	assert.Equal(t, 1, files, "only what the agent wrote between the two reviews is drawn")

	shown := ansi.Strip(m.reviewView())
	assert.Contains(t, shown, "round 2 of 2, not reviewed yet, the base moved, replayed")
	assert.NotContains(t, shown, "not comparable")
}

// TestAChapterTheBaseRewroteUnderTheAgentStaysRefused covers the one case no honest comparison
// exists for, where the band still refuses and now names the paths.
func TestAChapterTheBaseRewroteUnderTheAgentStaysRefused(t *testing.T) {
	agents := &fakeReviewAgents{}
	m := reviewAcrossAMovedBase(t, newFakeRepo(t.TempDir()), agents, &git.Replayed{
		Tree:      "a-tree-with-markers-in-it",
		Conflicts: []string{fakeFile, "second.go"},
	})

	m = pressReviewBase(t, m)

	assert.Equal(t, fakeSnapshotSHA, m.review.base, "the diff is still the two snapshots")

	shown := ansi.Strip(m.reviewView())
	assert.Contains(t, shown, "round 2 of 2, not comparable, the base moved, conflicts in greet.go, second.go")
}

// TestAGitThatCannotReplayLeavesTheRefusalStanding covers a git older than the replay needs,
// which is refused the way it was before rather than failing the load.
func TestAGitThatCannotReplayLeavesTheRefusalStanding(t *testing.T) {
	agents := &fakeReviewAgents{}
	m := reviewAcrossAMovedBase(t, newFakeRepo(t.TempDir()), agents, nil)

	m = pressReviewBase(t, m)

	assert.Equal(t, fakeSnapshotSHA, m.review.base)

	shown := ansi.Strip(m.reviewView())
	assert.Contains(t, shown, "round 2 of 2, not comparable, the base moved")
	assert.NotContains(t, shown, "rebased")
}

// TestARewrittenHistoryIsSaidToBeOne covers a base the branch no longer descends from, which is
// a different thing from the base moving on and is said so.
func TestARewrittenHistoryIsSaidToBeOne(t *testing.T) {
	agents := &fakeReviewAgents{}
	repo := newFakeRepo(t.TempDir())
	repo.rewritten = map[string]bool{pairOf(olderBase, fakeBaseSHA): true}
	m := reviewAcrossAMovedBase(t, repo, agents, &git.Replayed{Tree: replayedTree})

	m = pressReviewBase(t, m)

	shown := ansi.Strip(m.reviewView())
	assert.Contains(t, shown, "round 2 of 2, not reviewed yet, history was rewritten, replayed")
}

// TestTheWalkHasTheSameStopsWhetherOrNotItReplays pins that replaying changes what a chapter
// draws and never how many there are, so a key moves the same distance either way.
func TestTheWalkHasTheSameStopsWhetherOrNotItReplays(t *testing.T) {
	agents := &fakeReviewAgents{}
	replayed := reviewAcrossAMovedBase(t, newFakeRepo(t.TempDir()), agents, &git.Replayed{Tree: replayedTree})

	refused := reviewAcrossAMovedBase(t, newFakeRepo(t.TempDir()), &fakeReviewAgents{}, &git.Replayed{
		Tree:      "a-tree-with-markers-in-it",
		Conflicts: []string{fakeFile},
	})

	require.Len(t, replayed.review.chapters, 2)
	require.Len(t, refused.review.chapters, 2)

	for i := range replayed.review.chapters {
		assert.Equal(t, refused.review.chapters[i].ToSHA, replayed.review.chapters[i].ToSHA)
		assert.Equal(t, refused.review.chapters[i].Ordinal, replayed.review.chapters[i].Ordinal)
	}
}

// TestAnOldSideCountedInAReplayedTreePinsOnlyThere covers a comment written while standing on
// a replayed chapter: its old-side line is a line of that tree, so it is trusted against that
// tree and refused against the round's own base, where the same number names another line.
func TestAnOldSideCountedInAReplayedTreePinsOnlyThere(t *testing.T) {
	agents := &fakeReviewAgents{}
	repo := newFakeRepo(t.TempDir())
	m := reviewAcrossAMovedBase(t, repo, agents, &git.Replayed{Tree: replayedTree})

	// The chapter draws the agent's own edit of the first file, whose old side is the replayed
	// tree.
	repo.patches[pairOf(replayedTree, snapshotOf(secondTree))] = fakePatch

	agents.threads = []*agentv1.ReviewThread{{
		Id:          "t1",
		RoundId:     agents.rounds[1].GetId(),
		FilePath:    fakeFile,
		BaseSha:     replayedTree,
		OldStart:    4,
		OldEnd:      4,
		QuotedLines: []string{"-\treturn \"nowhere\""},
		Messages: []*agentv1.ReviewMessage{{
			Id:         "m1",
			AuthorKind: agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_HUMAN,
			Body:       "written on the replayed chapter",
		}},
	}}

	m = pressReviewBase(t, m)
	require.Equal(t, replayedTree, m.review.base)

	row := reviewRowOfThread(t, m, "t1")
	anchor, ok := m.review.view.GetAnchor(row)
	require.True(t, ok, "on the replayed chapter the thread stands on a line of the diff")
	assert.Equal(t, 4, anchor.OldStart, "the line its old side was counted in")

	m = pressReviewBaseForward(t, m)
	require.Empty(t, m.review.base, "the whole change is measured from the round's own base")

	row = reviewRowOfThread(t, m, "t1")
	_, ok = m.review.view.GetAnchor(row)
	assert.False(t, ok, "and there the same number is refused, so the thread stands at the foot")
}

// reviewRowOfThread is the row a thread is drawn on.
func reviewRowOfThread(t *testing.T, m model, id string) int {
	t.Helper()

	for y := range m.review.view.Len() {
		if thread, ok := m.review.view.GetThreadAt(y); ok && thread.GetId() == id {
			return y
		}
	}

	t.Fatalf("thread %q is drawn on no row", id)

	return -1
}

// TestTheFirstChapterIsMeasuredFromItsOwnBase covers the oldest round after a rebase: it was
// measured from the base the branch left main at then, and is drawn from that base rather
// than from the one the round on screen stands on.
func TestTheFirstChapterIsMeasuredFromItsOwnBase(t *testing.T) {
	agents := &fakeReviewAgents{}
	repo := newFakeRepo(t.TempDir())
	m := reviewAcrossAMovedBase(t, repo, agents, &git.Replayed{Tree: replayedTree})

	// What git reports between the moved base and round one carries main's own commits
	// reversed, so it is two files where the agent wrote one.
	repo.patches[pairOf(olderBase, fakeSnapshotSHA)] = fakePatch
	repo.patches[pairOf(fakeBaseSHA, fakeSnapshotSHA)] = fakePatchOfBothFiles

	m = pressReviewBase(t, m)
	m = pressReviewBase(t, m)

	require.Equal(t, 0, m.review.chapter, "two presses reach the oldest round")
	require.NotNil(t, m.review.view)
	assert.Equal(t, olderBase, m.review.view.GetBaseSHA(), "the diff is measured from round one's own base")
	assert.Equal(t, fakeSnapshotSHA, m.review.view.GetSnapshotSHA())
	assert.Equal(t, 1, m.review.view.Stat().Files)

	shown := ansi.Strip(m.reviewView())
	assert.Contains(t, shown, "round 1 of 2, reviewed at "+releasedClock("2026-09-08T10:00:00Z")+", on an earlier base")
}
