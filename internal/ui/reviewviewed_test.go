package ui

import (
	"testing"

	agentv1 "weave/proto/gen/agent/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakePatchOfGreetWrittenOverSince is the changed file between the first round and the second.
const fakePatchOfGreetWrittenOverSince = `diff --git a/greet.go b/greet.go
index 23cef252a97433f151c14725c88e0c2dafce63ef..5586caad9270132f074ee5374d4652b83d0acc98 100644
--- a/greet.go
+++ b/greet.go
@@ -1,5 +1,5 @@
 package greet
 
 func Hello() string {
-	return "goodbye"
+	return "written by the agent"
 }
`

// reviewOnASecondRoundOfOneFile is the panel on a second round the agent wrote the same file in,
// with the first round reviewed and released.
func reviewOnASecondRoundOfOneFile(t *testing.T, repo *fakeRepo, agents *fakeReviewAgents) model {
	t.Helper()

	m, _ := reviewModelOver(t, agents, repo)
	m = openReview(t, m)
	require.Len(t, agents.rounds, 1)

	first := agents.rounds[0]
	agents.reviews = []*agentv1.Review{{
		Id:           "v1",
		RoundId:      first.GetId(),
		RoundOrdinal: first.GetOrdinal(),
		AuthorKind:   agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_HUMAN,
		ReleasedAt:   "2026-09-08T10:00:00Z",
	}}

	repo.wrote(wroteParams{
		Tree:  "tree-written-over",
		Whole: fakePatchWrittenOver,
		Since: fakePatchOfGreetWrittenOverSince,
		Sources: map[string]string{
			fakeFile: "package greet\n\nfunc Hello() string {\n\treturn \"written by the agent\"\n}\n",
		},
	})

	m = openReview(t, m)
	require.Len(t, agents.rounds, 2)
	require.NotNil(t, m.review.diff.baseRound)

	return m
}

// pressReviewBaseForward walks the key back out to the whole change the way the panel does.
func pressReviewBaseForward(t *testing.T, m model) model {
	t.Helper()

	next, cmd := m.updateReview(keyMsg(">"))
	m = next.(model)
	require.NotNil(t, cmd, "widening reloads the diff")

	loaded, ok := cmd().(reviewLoadedMsg)
	require.True(t, ok)
	m.applyReviewLoaded(loaded)

	return m
}

// TestAViewedMarkTakenInAChapterDoesNotEatTheWholeChangeMark covers the mark meaning a file has
// been read in the change rather than in one round of it.
func TestAViewedMarkTakenInAChapterDoesNotEatTheWholeChangeMark(t *testing.T) {
	repo := newFakeRepo(t.TempDir())
	agents := &fakeReviewAgents{}

	m := reviewOnASecondRoundOfOneFile(t, repo, agents)

	next, cmd := m.updateReview(keyMsg("v"))
	m = next.(model)
	require.NotNil(t, cmd)
	cmd()

	require.NotNil(t, agents.marked)
	require.Equal(t, fakeFile, agents.marked.GetFilePath())
	whole := agents.marked.GetFingerprint()
	require.NotEmpty(t, whole)

	m = pressReviewBase(t, m)
	require.NotEmpty(t, m.review.diff.base, "the diff narrowed to a round")
	require.False(t, m.review.diff.view.IsViewed(fakeFile), "a chapter is a different diff, so it starts unread")

	next, cmd = m.updateReview(keyMsg("v"))
	m = next.(model)
	require.Nil(t, cmd, "the refusal sends no write")

	assert.Equal(t, whole, agents.marked.GetFingerprint(),
		"the mark taken in a chapter does not overwrite the one taken on the whole change")
	assert.False(t, m.review.diff.view.IsViewed(fakeFile), "and the chapter folds nothing away")
	assert.Contains(t, m.review.notice, "whole change", "the refusal says why")

	m = pressReviewBaseForward(t, m)

	require.Empty(t, m.review.diff.base, "the whole change is back on screen")
	assert.True(t, m.review.diff.view.IsViewed(fakeFile), "and the file is still read")
}
