package ui

import (
	"fmt"
	"testing"
	"time"

	"weave/internal/diff"
	"weave/internal/ui/diffview"
	agentv1 "weave/proto/gen/agent/v1"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// reviewReply is a thread carrying one message, pinned to wherever the cursor is standing, which
// is what a refresh finding an answer has to draw.
func reviewReply(t *testing.T, m model) []*agentv1.ReviewThread {
	t.Helper()

	anchor, ok := m.review.diff.view.GetAnchor(m.review.diff.cursor)
	require.True(t, ok)

	return []*agentv1.ReviewThread{{
		Id:          "t1",
		FilePath:    anchor.FilePath,
		OldStart:    int32(anchor.OldStart),
		NewStart:    int32(anchor.NewStart),
		QuotedLines: anchor.QuotedLines,
		UpdatedAt:   "2026-09-02T10:00:00Z",
		Messages: []*agentv1.ReviewMessage{{
			Id:         "m1",
			AuthorKind: agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_AGENT,
			Body:       "fixed it",
			UpdatedAt:  "2026-09-02T10:00:00Z",
		}},
	}}
}

// TestReviewRefreshDrawsAnAnswer covers the whole path a reply takes onto the screen without a
// keypress.
func TestReviewRefreshDrawsAnAnswer(t *testing.T) {
	dir := reviewRepo(t)
	agents := &fakeReviewAgents{}
	m, _ := reviewModel(t, agents, dir)
	m = openReview(t, m)

	agents.threads = reviewReply(t, m)

	refreshed, ok := m.review.refreshThreadsCmd(m.shared())().(reviewRefreshedMsg)
	require.True(t, ok)
	m.review.applyRefreshed(m.shared(), refreshed)

	assert.Contains(t, ansi.Strip(m.review.view(m.shared())), "fixed it")
}

// TestReviewRefreshDoesNothingWhenNothingWasSaid pins the cheap probe: repinning walks every row
// of the patch, so a tick that finds the conversation where it left it must not do it again.
func TestReviewRefreshDoesNothingWhenNothingWasSaid(t *testing.T) {
	dir := reviewRepo(t)
	agents := &fakeReviewAgents{}
	m, _ := reviewModel(t, agents, dir)
	m = openReview(t, m)

	held := reviewReply(t, m)
	m.review.applyThreads(m.shared(), held)

	// The tick answers with an equal conversation built fresh, so what the model is holding
	// afterwards says whether the probe let it through.
	agents.threads = reviewReply(t, m)

	refreshed, ok := m.review.refreshThreadsCmd(m.shared())().(reviewRefreshedMsg)
	require.True(t, ok)
	m.review.applyRefreshed(m.shared(), refreshed)

	assert.Same(t, held[0], m.review.threads.list[0], "the same conversation is not pinned again")
}

// TestReviewRefreshSeesDeliveryMoveWithoutAStamp is the probe's hard case: the agent claiming
// the mail a review was sent under writes to the mail table alone, so delivery moves with every
// stamp and count on the review standing exactly still.
func TestReviewRefreshSeesDeliveryMoveWithoutAStamp(t *testing.T) {
	tests := []struct {
		name string
		then agentv1.ReviewDeliveryState
	}{
		{
			name: "the agent claims the notice",
			then: agentv1.ReviewDeliveryState_REVIEW_DELIVERY_STATE_DELIVERED,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := reviewRepo(t)
			agents := &fakeReviewAgents{}
			m, _ := reviewModel(t, agents, dir)
			m = openReview(t, m)

			sent := reviewComment(t, m, agentv1.ReviewDeliveryState_REVIEW_DELIVERY_STATE_SENT)
			m.review.applyThreads(m.shared(), sent)

			moved := reviewComment(t, m, tt.then)
			require.Equal(t, sent[0].GetUpdatedAt(), moved[0].GetUpdatedAt(), "no row was written")
			require.Len(t, moved[0].GetMessages(), len(sent[0].GetMessages()), "and none was added")

			m.review.applyRefreshed(m.shared(), reviewRefreshedMsg{agentID: "a1", threads: moved})

			assert.Same(t, moved[0], m.review.threads.list[0], "the move is taken")
			assert.Contains(t, ansi.Strip(m.review.view(m.shared())), reviewDeliveryOf(tt.then).word)
		})
	}
}

// reviewComment is a thread carrying one comment of the person's own in a given delivery state,
// with every stamp held still so only the state itself has moved.
func reviewComment(t *testing.T, m model, state agentv1.ReviewDeliveryState) []*agentv1.ReviewThread {
	t.Helper()

	threads := reviewReply(t, m)
	threads[0].Messages = []*agentv1.ReviewMessage{{
		Id:         "m1",
		AuthorKind: agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_HUMAN,
		Body:       "wrong greeting",
		UpdatedAt:  "2026-09-02T10:00:00Z",
		SentAt:     "2026-09-02T10:00:00Z",
		Delivery:   state,
	}}

	return threads
}

// TestReviewRefreshHoldsOneReadAtATime pins the guard: a slow read is not asked for again by
// every tick that passes underneath it.
func TestReviewRefreshHoldsOneReadAtATime(t *testing.T) {
	dir := reviewRepo(t)
	m, _ := reviewModel(t, &fakeReviewAgents{}, dir)
	m = openReview(t, m)

	at := time.Now()
	require.NotNil(t, m.refreshReviewCmd(at))
	require.True(t, m.review.threads.polling)

	assert.Nil(t, m.refreshReviewCmd(at.Add(time.Second)), "nothing is asked for while a read is in flight")

	m.review.applyRefreshed(m.shared(), reviewRefreshedMsg{agentID: "a1"})
	assert.False(t, m.review.threads.polling)
	assert.NotNil(t, m.refreshReviewCmd(at.Add(2*time.Second)), "and the tick after it lands asks again")
}

// TestReviewRefreshStandsDownMidInput pins that the tick does not fire while the reader is
// typing.
func TestReviewRefreshStandsDownMidInput(t *testing.T) {
	tests := []struct {
		name  string
		start func(*model)
	}{
		{
			name:  "a message is being composed",
			start: func(m *model) { m.review.threads.composing = true },
		},
		{
			name:  "a search pattern is being typed",
			start: func(m *model) { m.review.diff.filter.typing = true },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := reviewRepo(t)
			m, _ := reviewModel(t, &fakeReviewAgents{}, dir)
			m = openReview(t, m)

			tt.start(&m)

			assert.Nil(t, m.refreshReviewCmd(time.Now()))
			assert.False(t, m.review.threads.polling)
			assert.False(t, m.review.diff.diffPolling)
		})
	}
}

// TestReviewRefreshLeavesTheComposerAlone covers a read that was already in flight landing while
// the reader has started writing, which must not take the editor, the cursor or the window.
func TestReviewRefreshLeavesTheComposerAlone(t *testing.T) {
	dir := reviewRepo(t)
	agents := &fakeReviewAgents{}
	m, tmux := reviewModel(t, agents, dir)
	m = openReview(t, m)

	threads := reviewReply(t, m)

	m.review.compose(&m, m.shared())
	require.True(t, m.review.threads.composing)

	m = typeReviewMessage(t, m, "half a thought")
	cursor, top := m.review.diff.cursor, m.review.diff.top
	tmux.sentKeys = nil

	m.review.applyRefreshed(m.shared(), reviewRefreshedMsg{agentID: "a1", threads: threads})

	assert.True(t, m.review.threads.composing, "the editor is still up")
	assert.Equal(t, "half a thought", m.review.threads.editor.value(), "with what was typed into it")
	assert.Equal(t, cursor, m.review.diff.cursor)
	assert.Equal(t, top, m.review.diff.top)
	assert.Empty(t, tmux.sentKeys, "and the diff was not repainted under it")

	// The probe is left where it was, so the tick after the composer closes finds this again.
	m.review.threads.composing = false
	m.review.threads.editor = reviewInput{}
	m.review.applyRefreshed(m.shared(), reviewRefreshedMsg{agentID: "a1", threads: threads})
	assert.Contains(t, ansi.Strip(m.review.view(m.shared())), "fixed it")
}

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

	moved, _ := m.routeScreenMessage(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("]")})
	m = moved.(model)

	cursor, top, pan := m.review.diff.cursor, m.review.diff.top, m.review.diff.pan
	pattern, matches := m.review.diff.filter.query, m.review.diff.view.MatchCount()

	m.review.applyRefreshed(m.shared(), reviewRefreshedMsg{
		agentID: "a1",
		threads: reviewReply(t, m),
	})

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

	m.review.applyRefreshed(m.shared(), reviewRefreshedMsg{agentID: "a1", threads: threads})

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

// TestReviewRefreshSaysNothingWhenTheReadFails pins a failed read being swallowed.
func TestReviewRefreshSaysNothingWhenTheReadFails(t *testing.T) {
	dir := reviewRepo(t)
	m, _ := reviewModel(t, &fakeReviewAgents{}, dir)
	m = openReview(t, m)

	m.review.applyRefreshed(m.shared(), reviewRefreshedMsg{agentID: "a1", err: assert.AnError})

	assert.Empty(t, m.messages.items)
	assert.False(t, m.review.threads.polling)
}

// TestReviewDiffPollCatchesUpOnceTheTurnIsOver covers what watching is for: the worktree is
// hashed in the background, and a move an idle agent left behind is read again on its own.
func TestReviewDiffPollCatchesUpOnceTheTurnIsOver(t *testing.T) {
	repo := newFakeRepo(t.TempDir())
	m, tmux := reviewModelOver(t, &fakeReviewAgents{}, repo)
	m = openReview(t, m)

	repo.wrote(wroteParams{
		Tree:  "tree-written-again",
		Whole: fakePatchWrittenOver,
		Sources: map[string]string{
			fakeFile: "package greet\n\nfunc Hello() string {\n\treturn \"written by the agent\"\n}\n",
		},
	})

	cmd := m.review.pollDiffCmd(m.shared())
	polled, ok := cmd().(reviewDiffPolledMsg)
	require.True(t, ok)
	require.NoError(t, polled.err)

	tmux.sentKeys = nil
	caught := m.review.applyDiffPolled(m.shared(), polled)

	require.True(t, m.review.diff.moved)
	require.NotNil(t, caught, "an idle agent's move is read again rather than offered")
	assert.Empty(t, tmux.sentKeys, "and nothing is typed at anybody to do it")

	// The patch is only replaced once the read answers, so what is on screen until then is still
	// the diff the reader was standing in.
	shown := ansi.Strip(m.review.view(m.shared()))
	assert.Contains(t, shown, "catching up")
	assert.NotContains(t, shown, "written by the agent")

	loaded, ok := caught().(reviewLoadedMsg)
	require.True(t, ok)
	m.review.applyLoaded(&m, m.shared(), loaded)

	assert.Contains(t, ansi.Strip(m.review.view(m.shared())), "written by the agent", "and then it is drawn")
}

// TestReviewDiffPollIgnoresAWorktreeThatHasNotMoved pins the probe leaving an unchanged worktree
// alone.
func TestReviewDiffPollIgnoresAWorktreeThatHasNotMoved(t *testing.T) {
	dir := reviewRepo(t)
	m, _ := reviewModel(t, &fakeReviewAgents{}, dir)
	m = openReview(t, m)

	polled, ok := m.review.pollDiffCmd(m.shared())().(reviewDiffPolledMsg)
	require.True(t, ok)
	require.NoError(t, polled.err)

	m.review.applyDiffPolled(m.shared(), polled)

	assert.False(t, m.review.diff.moved)
	assert.NotContains(t, ansi.Strip(m.review.view(m.shared())), "the agent has written since")
}

// TestReviewBacksOutWhenTheWorktreeWasEmptied covers the agent that throws its work away while
// the review is open: catching up finds nothing to show, so the reader is put back on the listing
// rather than left in a diff viewer with no diff in it.
func TestReviewBacksOutWhenTheWorktreeWasEmptied(t *testing.T) {
	repo := newFakeRepo(t.TempDir())
	m, _ := reviewModelOver(t, &fakeReviewAgents{}, repo)
	m = openReview(t, m)

	repo.wrote(wroteParams{Tree: "tree-of-the-base"})

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
	repo := newFakeRepo(t.TempDir())
	m, _ := reviewModelOver(t, &fakeReviewAgents{}, repo)
	m = openReview(t, m)

	first := m.review.round.GetOrdinal()

	repo.wrote(wroteParams{
		Tree:  "tree-written-again",
		Whole: fakePatchWrittenOver,
		Sources: map[string]string{
			fakeFile: "package greet\n\nfunc Hello() string {\n\treturn \"written by the agent\"\n}\n",
		},
	})

	polled, ok := m.review.pollDiffCmd(m.shared())().(reviewDiffPolledMsg)
	require.True(t, ok)
	m.review.applyDiffPolled(m.shared(), polled)
	require.True(t, m.review.diff.moved)

	m.review.diff.cursor = m.review.diff.view.Move(m.review.diff.cursor, 1)
	place, located := m.review.diff.view.GetPlace(m.review.diff.cursor)
	require.True(t, located)

	next, cmd := m.routeScreenMessage(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("R")})
	m = next.(model)
	require.NotNil(t, cmd, "the next round has to be taken")

	loaded, ok := cmd().(reviewLoadedMsg)
	require.True(t, ok)
	require.NoError(t, loaded.err)
	m.review.applyLoaded(&m, m.shared(), loaded)

	assert.Greater(t, m.review.round.GetOrdinal(), first, "the round moved on")
	assert.False(t, m.review.diff.moved, "the notice goes with the round it was about")

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

	require.False(t, m.review.diff.moved)

	_, cmd := m.routeScreenMessage(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("R")})
	require.NotNil(t, cmd)

	loaded, ok := cmd().(reviewLoadedMsg)
	require.True(t, ok)
	require.NoError(t, loaded.err)
}

// TestReviewDiffPollWaitsBetweenReads pins git being run every few seconds rather than every
// tick.
func TestReviewDiffPollWaitsBetweenReads(t *testing.T) {
	dir := reviewRepo(t)
	m, _ := reviewModel(t, &fakeReviewAgents{}, dir)
	m = openReview(t, m)

	at := time.Now()
	require.NotNil(t, m.refreshReviewCmd(at))
	require.True(t, m.review.diff.diffPolling)

	m.review.applyRefreshed(m.shared(), reviewRefreshedMsg{agentID: "a1"})
	m.review.applyDiffPolled(m.shared(), reviewDiffPolledMsg{agentID: "a1"})

	require.NotNil(t, m.refreshReviewCmd(at.Add(time.Second)))
	assert.False(t, m.review.diff.diffPolling, "a tick a second later reads the conversation and not the worktree")

	m.review.applyRefreshed(m.shared(), reviewRefreshedMsg{agentID: "a1"})

	require.NotNil(t, m.refreshReviewCmd(at.Add(reviewDiffPollInterval)))
	assert.True(t, m.review.diff.diffPolling)
}

// TestReviewRefreshIsOnlyForTheScreenThatIsUp pins the tick doing nothing anywhere else.
func TestReviewRefreshIsOnlyForTheScreenThatIsUp(t *testing.T) {
	dir := reviewRepo(t)
	m, _ := reviewModel(t, &fakeReviewAgents{}, dir)
	m = openReview(t, m)

	m.review.leave(&m, m.shared())

	assert.Nil(t, m.refreshReviewCmd(time.Now()))
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
	repo := newFakeRepo(t.TempDir())
	repo.wrote(wroteParams{
		Tree:   "tree-inside-an-excluded-path",
		Whole:  fakePatchInAnExcludedPath,
		Hidden: true,
	})

	m, _ := reviewModelOver(t, &fakeReviewAgents{}, repo)
	m = openReview(t, m)

	loaded, ok := loadReviewCmd(m.shared(), loadReviewCmdParams{
		AgentID: m.review.agentID,
		Dir:     repo.root,
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
