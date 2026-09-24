package ui

import (
	"fmt"
	"testing"

	"weave/internal/diff"
	"weave/internal/gittest"
	"weave/internal/ui/diffview"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestReviewRefreshKeepsThePlace pins that a refresh moves nothing the reader set: where the
// cursor is, where the window is, how far the diff is panned, and what was searched for.
func TestReviewRefreshKeepsThePlace(t *testing.T) {
	dir := reviewRepo(t)
	agents := &fakeReviewAgents{}
	m, _ := reviewModel(t, agents, dir)
	m = openReview(t, m)

	m.review.diff.filter.setQuery("hello")
	m.review.search(m.shared())

	m.review.diff.panBy(12)

	moved, _ := m.routeScreenMessage(keyMsg("]"))
	m = moved.(model)

	cursor, top, pan := m.review.diff.cursor, m.review.diff.top, m.review.diff.pan
	pattern, matches := m.review.diff.filter.query, m.review.diff.view.MatchCount()

	streamThreads(t, &m, reviewReply(t, m))

	assert.Equal(t, cursor, m.review.diff.cursor)
	assert.Equal(t, top, m.review.diff.top)
	assert.Equal(t, pan, m.review.diff.pan)
	assert.Equal(t, pattern, m.review.diff.filter.query)
	assert.Equal(t, matches, m.review.diff.view.MatchCount(), "the search still holds the rows it found")
}

// TestReviewRefreshKeepsAFileNameSearch covers the place a search by file name leaves the
// cursor: the header of a file drawn whole, which nothing else puts it on, and which a reply
// arriving would otherwise drag it off and out of the search.
func TestReviewRefreshKeepsAFileNameSearch(t *testing.T) {
	dir := reviewRepo(t)
	agents := &fakeReviewAgents{}
	m, _ := reviewModel(t, agents, dir)
	m = openReview(t, m)

	m.review.diff.filter.setQuery("greet.go")
	m.review.search(m.shared())
	require.Equal(t, 1, m.review.diff.view.MatchCount())

	header := m.review.diff.cursor

	// A thread has to be pinned to a line, so the reply is anchored where the search is not.
	m.review.diff.cursor = m.review.diff.view.GetFirstRow()
	threads := reviewReply(t, m)
	m.review.diff.cursor = header

	streamThreads(t, &m, threads)

	assert.Equal(t, header, m.review.diff.cursor)
	assert.Contains(t, ansi.Strip(m.review.diff.matchCount()), "1 of 1",
		"the sidebar still says where in the search the reader is")
}

// TestReviewReloadKeepsAFileNameSearch covers the same header held across the reload key, which
// reads the worktree again and builds the rows from nothing.
func TestReviewReloadKeepsAFileNameSearch(t *testing.T) {
	dir := reviewRepo(t)
	m, _ := reviewModel(t, &fakeReviewAgents{}, dir)
	m = openReview(t, m)

	m.review.diff.filter.setQuery("greet.go")
	m.review.search(m.shared())
	header := m.review.diff.cursor

	cmd := m.review.reload(m.shared())
	require.NotNil(t, cmd)
	loaded, ok := cmd().(reviewLoadedMsg)
	require.True(t, ok)
	m.review.applyLoaded(&m, m.shared(), loaded)

	assert.Equal(t, header, m.review.diff.cursor)
	assert.Contains(t, ansi.Strip(m.review.diff.matchCount()), "1 of 1",
		"the sidebar still says where in the search the reader is")
}

// TestReviewReloadKeepsTheSearch pins the whole search surviving the reload key.
func TestReviewReloadKeepsTheSearch(t *testing.T) {
	dir := reviewRepo(t)
	m, _ := reviewModel(t, &fakeReviewAgents{}, dir)
	m = openReview(t, m)

	m.review.diff.filter.setQuery("hello")
	m.review.search(m.shared())
	matches := m.review.diff.view.MatchCount()
	require.Positive(t, matches)

	cmd := m.review.reload(m.shared())
	require.NotNil(t, cmd)
	loaded, ok := cmd().(reviewLoadedMsg)
	require.True(t, ok)
	m.review.applyLoaded(&m, m.shared(), loaded)

	assert.Equal(t, matches, m.review.diff.view.MatchCount())
	assert.Contains(t, ansi.Strip(m.review.diff.matchCount()), fmt.Sprintf("1 of %d", matches))

	_, jumped := m.review.diff.view.JumpMatch(m.review.diff.cursor, 1)
	assert.True(t, jumped, "the search still has rows to step through")
}

// TestReviewBacksOutWhenTheWorktreeWasEmptied pins a reload that finds no changes returning the reader to the listing.
func TestReviewBacksOutWhenTheWorktreeWasEmptied(t *testing.T) {
	repo := gittest.NewRepo(t.TempDir())
	m, _ := reviewModelOver(t, &fakeReviewAgents{}, repo)
	m = openReview(t, m)

	repo.Wrote(gittest.WroteParams{Tree: "tree-of-the-base"})

	cmd := m.review.reload(m.shared())
	require.NotNil(t, cmd)

	loaded, ok := cmd().(reviewLoadedMsg)
	require.True(t, ok)
	require.ErrorIs(t, loaded.err, diff.ErrNoChanges, "the round of an unmoved tree holds nothing")

	m.review.applyLoaded(&m, m.shared(), loaded)

	assert.Equal(t, screenAgents, m.screen, "the reader is put back on the listing")
	assert.Nil(t, m.review.diff.view, "and the review is let go of")
}

// TestReviewReloadKeyOpensTheNextRound pins the opt-in: the reader asks for the next round, and
// lands back where they were standing in the new one.
func TestReviewReloadKeyOpensTheNextRound(t *testing.T) {
	repo := gittest.NewRepo(t.TempDir())
	m, _ := reviewModelOver(t, &fakeReviewAgents{}, repo)
	m = openReview(t, m)

	first := m.review.round.GetOrdinal()

	repo.Wrote(gittest.WroteParams{
		Tree:  "tree-written-again",
		Whole: gittest.PatchWrittenOver,
		Sources: map[string]string{
			gittest.File: "package greet\n\nfunc Hello() string {\n\treturn \"written by the agent\"\n}\n",
		},
	})

	markReviewWorktreeMoved(&m)
	require.True(t, m.review.isWorktreeMoved(m.shared()))

	m.review.diff.cursor = m.review.diff.view.Move(m.review.diff.cursor, 1)
	place, located := m.review.diff.view.GetPlace(m.review.diff.cursor)
	require.True(t, located)

	next, cmd := m.routeScreenMessage(keyMsg("R"))
	m = next.(model)
	require.NotNil(t, cmd, "the next round has to be taken")

	loaded, ok := cmd().(reviewLoadedMsg)
	require.True(t, ok)
	require.NoError(t, loaded.err)
	m.review.applyLoaded(&m, m.shared(), loaded)

	assert.Greater(t, m.review.round.GetOrdinal(), first, "the round moved on")
	assert.False(t, m.review.isWorktreeMoved(m.shared()), "the notice goes with the round it was about")

	found, ok := m.review.diff.view.Locate(place)
	require.True(t, ok)
	assert.Equal(t, found, m.review.diff.cursor, "the reader is back where they were standing")

	var drawn string
	for y := range m.review.diff.view.Len() {
		drawn += m.review.diff.view.RenderRow(y, diffview.NoCursor, 120, 0)
	}
	assert.Contains(t, drawn, "written by the agent")
}

// TestReviewReloadKeyReadsAgainWithNothingMoved pins the key still meaning reload when the
// background probe has found nothing.
func TestReviewReloadKeyReadsAgainWithNothingMoved(t *testing.T) {
	dir := reviewRepo(t)
	m, _ := reviewModel(t, &fakeReviewAgents{}, dir)
	m = openReview(t, m)

	require.False(t, m.review.isWorktreeMoved(m.shared()))

	_, cmd := m.routeScreenMessage(keyMsg("R"))
	require.NotNil(t, cmd)

	loaded, ok := cmd().(reviewLoadedMsg)
	require.True(t, ok)
	require.NoError(t, loaded.err)
}

// TestWhatChangedSinceYourLastReviewSurvivesAKeypress covers a standing fact about the diff
// being kept out of the notice slot, which every key in the review clears.
func TestWhatChangedSinceYourLastReviewSurvivesAKeypress(t *testing.T) {
	dir := reviewRepo(t)
	m, _ := reviewModel(t, &fakeReviewAgents{}, dir)
	m = openReview(t, m)

	m.review.diff.since = "4 files changed since you last reviewed"
	require.Contains(t, reviewProse(m), "4 files changed since you last reviewed")

	next, _ := m.Update(keyMsg("j"))
	scrolled := next.(model)

	assert.Equal(t, "4 files changed since you last reviewed", scrolled.review.diff.since,
		"scrolling is not an answer to it")
	assert.Contains(t, reviewProse(scrolled), "4 files changed since you last reviewed",
		"and it is still on screen")
}

// TestANoticeCoversWhatChangedAndGivesItBack covers the two lines sharing one row: an answer to a
// keypress wins while it stands, and the standing line returns once the next key clears it.
func TestANoticeCoversWhatChangedAndGivesItBack(t *testing.T) {
	dir := reviewRepo(t)
	m, _ := reviewModel(t, &fakeReviewAgents{}, dir)
	m = openReview(t, m)

	m.review.diff.since = "4 files changed since you last reviewed"
	m.review.notice = "no matches"

	shown := ansi.Strip(m.review.view(m.shared()))
	assert.Contains(t, shown, "no matches")
	assert.NotContains(t, shown, "4 files changed", "the answer to the last key wins the row")

	next, _ := m.Update(keyMsg("j"))
	assert.Contains(t, reviewProse(next.(model)), "4 files changed since you last reviewed")
}

// TestReviewSaysWhenEverythingIsExcluded covers the difference the reader has to be told about:
// an agent that has written nothing and one whose every line is in a path the workflow hides both
// leave the screen empty, and only one of them means the agent has not worked.
func TestReviewSaysWhenEverythingIsExcluded(t *testing.T) {
	repo := gittest.NewRepo(t.TempDir())
	repo.Wrote(gittest.WroteParams{
		Tree:   "tree-inside-an-excluded-path",
		Whole:  gittest.PatchInAnExcludedPath,
		Hidden: true,
	})

	m, _ := reviewModelOver(t, &fakeReviewAgents{}, repo)
	m = openReview(t, m)

	loaded, ok := loadReviewCmd(m.shared(), loadReviewCmdParams{
		AgentID: m.review.agentID,
		Dir:     repo.Root,
		Exclude: []string{"proto/gen"},
	})().(reviewLoadedMsg)
	require.True(t, ok)
	require.ErrorIs(t, loaded.err, diff.ErrAllExcluded, "the agent wrote a file, it is only hidden")

	m.review.applyLoaded(&m, m.shared(), loaded)

	assert.Equal(t, screenAgents, m.screen, "there is still nothing to draw, so it backs out")
	require.NotEmpty(t, m.messages.items)
	assert.Contains(t, newestMessage(m).summary, "excluded from the diff",
		"and says which of the two empty screens this is")
	assert.NotContains(t, newestMessage(m).summary, "Nothing to review")
}
