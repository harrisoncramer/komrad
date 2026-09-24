package ui

import (
	"testing"

	"weave/internal/gittest"
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

// buildReviewOnASecondRound is the panel on a second round the agent wrote the same file in,
// with the first round reviewed and released.
func buildReviewOnASecondRound(t *testing.T, repo *gittest.Repo, agents *fakeReviewAgents) model {
	t.Helper()

	m, _ := buildReviewModel(t, agents, repo)
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

	repo.Wrote(gittest.WroteParams{
		Tree:  "tree-written-over",
		Whole: gittest.PatchWrittenOver,
		Since: fakePatchOfGreetWrittenOverSince,
		Sources: map[string]string{
			gittest.File: "package greet\n\nfunc Hello() string {\n\treturn \"written by the agent\"\n}\n",
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

	holdServedThreads(&m)
	next, cmd := m.routeScreenMessage(keyMsg(">"))
	m = next.(model)
	require.NotNil(t, cmd, "widening reloads the diff")

	loaded, ok := cmd().(reviewLoadedMsg)
	require.True(t, ok)
	m.review.applyLoaded(&m, m.shared(), loaded)

	return m
}

// TestAViewedMarkTakenInAChapterDoesNotEatTheWholeChangeMark covers the mark meaning a file has
// been read in the change rather than in one round of it.
func TestAViewedMarkTakenInAChapterDoesNotEatTheWholeChangeMark(t *testing.T) {
	repo := gittest.NewRepo(t.TempDir())
	agents := &fakeReviewAgents{}

	m := buildReviewOnASecondRound(t, repo, agents)

	next, cmd := m.routeScreenMessage(keyMsg("v"))
	m = next.(model)
	require.NotNil(t, cmd)
	cmd()

	require.NotNil(t, agents.marked)
	require.Equal(t, gittest.File, agents.marked.GetFilePath())
	whole := agents.marked.GetFingerprint()
	require.NotEmpty(t, whole)

	m = pressReviewBase(t, m)
	require.NotEmpty(t, m.review.diff.base, "the diff narrowed to a round")
	require.False(t, m.review.diff.view.IsViewed(gittest.File), "a chapter is a different diff, so it starts unread")

	next, cmd = m.routeScreenMessage(keyMsg("v"))
	m = next.(model)
	require.Nil(t, cmd, "the refusal sends no write")

	assert.Equal(t, whole, agents.marked.GetFingerprint(),
		"the mark taken in a chapter does not overwrite the one taken on the whole change")
	assert.False(t, m.review.diff.view.IsViewed(gittest.File), "and the chapter folds nothing away")
	assert.Contains(t, m.review.notice, "whole change", "the refusal says why")

	m = pressReviewBaseForward(t, m)

	require.Empty(t, m.review.diff.base, "the whole change is back on screen")
	assert.True(t, m.review.diff.view.IsViewed(gittest.File), "and the file is still read")
}
