package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"weave/internal/diff"
	"weave/internal/review"
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

	anchor, ok := m.review.view.GetAnchor(m.review.cursor)
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
// keypress, which is what the refresh is for.
func TestReviewRefreshDrawsAnAnswer(t *testing.T) {
	dir := reviewRepo(t)
	agents := &fakeReviewAgents{}
	m, _ := reviewModel(t, agents, dir)
	m = openReview(t, m)

	agents.threads = reviewReply(t, m)

	refreshed, ok := m.refreshReviewThreadsCmd()().(reviewRefreshedMsg)
	require.True(t, ok)
	m.applyReviewRefreshed(refreshed)

	assert.Contains(t, ansi.Strip(m.reviewView()), "fixed it")
}

// TestReviewRefreshDoesNothingWhenNothingWasSaid pins the cheap probe: repinning walks every row
// of the patch, so a tick that finds the conversation where it left it must not do it again.
func TestReviewRefreshDoesNothingWhenNothingWasSaid(t *testing.T) {
	dir := reviewRepo(t)
	agents := &fakeReviewAgents{}
	m, _ := reviewModel(t, agents, dir)
	m = openReview(t, m)

	held := reviewReply(t, m)
	m.applyReviewThreads(held)

	// The tick answers with an equal conversation built fresh, so what the model is holding
	// afterwards says whether the probe let it through.
	agents.threads = reviewReply(t, m)

	refreshed, ok := m.refreshReviewThreadsCmd()().(reviewRefreshedMsg)
	require.True(t, ok)
	m.applyReviewRefreshed(refreshed)

	assert.Same(t, held[0], m.review.threads[0], "the same conversation is not pinned again")
}

// TestReviewRefreshSeesDeliveryMoveWithoutAStamp is the probe's hard case: the agent claiming
// the mail a review was sent under writes to the mail table alone, so delivery moves with every
// stamp and count on the review standing exactly still.
func TestReviewRefreshSeesDeliveryMoveWithoutAStamp(t *testing.T) {
	tests := []struct {
		name string
		then agentv1.ReviewDeliveryState
	}{
		{name: "the agent claims the notice", then: agentv1.ReviewDeliveryState_REVIEW_DELIVERY_STATE_DELIVERED},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := reviewRepo(t)
			agents := &fakeReviewAgents{}
			m, _ := reviewModel(t, agents, dir)
			m = openReview(t, m)

			sent := reviewComment(t, m, agentv1.ReviewDeliveryState_REVIEW_DELIVERY_STATE_SENT)
			m.applyReviewThreads(sent)

			moved := reviewComment(t, m, tt.then)
			require.Equal(t, sent[0].GetUpdatedAt(), moved[0].GetUpdatedAt(), "no row was written")
			require.Len(t, moved[0].GetMessages(), len(sent[0].GetMessages()), "and none was added")

			m.applyReviewRefreshed(reviewRefreshedMsg{agentID: "a1", threads: moved})

			assert.Same(t, moved[0], m.review.threads[0], "the move is taken")
			assert.Contains(t, ansi.Strip(m.reviewView()), reviewDeliveryOf(tt.then).word)
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
	require.True(t, m.review.polling)

	assert.Nil(t, m.refreshReviewCmd(at.Add(time.Second)), "nothing is asked for while a read is in flight")

	m.applyReviewRefreshed(reviewRefreshedMsg{agentID: "a1"})
	assert.False(t, m.review.polling)
	assert.NotNil(t, m.refreshReviewCmd(at.Add(2*time.Second)), "and the tick after it lands asks again")
}

// TestReviewRefreshStandsDownMidInput pins that the tick does not fire while the reader is
// typing, since the cursor, the window and the composer are theirs until they are done.
func TestReviewRefreshStandsDownMidInput(t *testing.T) {
	tests := []struct {
		name  string
		start func(*model)
	}{
		{
			name:  "a message is being composed",
			start: func(m *model) { m.review.composing = true },
		},
		{
			name:  "a search pattern is being typed",
			start: func(m *model) { m.review.filter.typing = true },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := reviewRepo(t)
			m, _ := reviewModel(t, &fakeReviewAgents{}, dir)
			m = openReview(t, m)

			tt.start(&m)

			assert.Nil(t, m.refreshReviewCmd(time.Now()))
			assert.False(t, m.review.polling)
			assert.False(t, m.review.diffPolling)
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

	next, _ := m.composeReview()
	m = next.(model)
	require.True(t, m.review.composing)

	m.review.editor.SetValue("half a thought")
	cursor, top := m.review.cursor, m.review.top
	tmux.sentKeys = nil

	m.applyReviewRefreshed(reviewRefreshedMsg{agentID: "a1", threads: threads})

	assert.True(t, m.review.composing, "the editor is still up")
	assert.Equal(t, "half a thought", m.review.editor.Value(), "with what was typed into it")
	assert.Equal(t, cursor, m.review.cursor)
	assert.Equal(t, top, m.review.top)
	assert.Empty(t, tmux.sentKeys, "and the diff was not repainted under it")

	// The probe is left where it was, so the tick after the composer closes finds this again.
	m.review.composing = false
	m.review.editor.Blur()
	m.applyReviewRefreshed(reviewRefreshedMsg{agentID: "a1", threads: threads})
	assert.Contains(t, ansi.Strip(m.reviewView()), "fixed it")
}

// TestReviewRefreshKeepsThePlace pins that a refresh moves nothing the reader set: where the
// cursor is, where the window is, how far the diff is panned, and what was searched for.
func TestReviewRefreshKeepsThePlace(t *testing.T) {
	dir := reviewRepo(t)
	agents := &fakeReviewAgents{}
	m, _ := reviewModel(t, agents, dir)
	m = openReview(t, m)

	m.review.filter.setQuery("hello")
	m.searchReview()

	next, _ := m.panReview(12)
	m = next.(model)

	moved, _ := m.updateReview(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("]")})
	m = moved.(model)

	cursor, top, pan := m.review.cursor, m.review.top, m.review.pan
	pattern, matches := m.review.filter.query, m.review.view.MatchCount()

	m.applyReviewRefreshed(reviewRefreshedMsg{
		agentID: "a1",
		threads: reviewReply(t, m),
	})

	assert.Equal(t, cursor, m.review.cursor)
	assert.Equal(t, top, m.review.top)
	assert.Equal(t, pan, m.review.pan)
	assert.Equal(t, pattern, m.review.filter.query)
	assert.Equal(t, matches, m.review.view.MatchCount(), "the search still holds the rows it found")
}

// TestReviewRefreshKeepsAFileNameSearch covers the place a search by file name leaves the
// cursor: the header of a file drawn whole, which nothing else puts it on, and which a reply
// arriving would otherwise drag it off and out of the search.
func TestReviewRefreshKeepsAFileNameSearch(t *testing.T) {
	dir := reviewRepo(t)
	agents := &fakeReviewAgents{}
	m, _ := reviewModel(t, agents, dir)
	m = openReview(t, m)

	m.review.filter.setQuery("greet.go")
	m.searchReview()
	require.Equal(t, 1, m.review.view.MatchCount())

	header := m.review.cursor

	// A thread has to be pinned to a line, so the reply is anchored where the search is not.
	m.review.cursor = m.review.view.GetFirstRow()
	threads := reviewReply(t, m)
	m.review.cursor = header

	m.applyReviewRefreshed(reviewRefreshedMsg{agentID: "a1", threads: threads})

	assert.Equal(t, header, m.review.cursor)
	assert.Contains(t, ansi.Strip(m.reviewMatchCount()), "1 of 1",
		"the sidebar still says where in the search the reader is")
}

// TestReviewReloadKeepsAFileNameSearch covers the same header held across the reload key, which
// reads the worktree again and builds the rows from nothing.
func TestReviewReloadKeepsAFileNameSearch(t *testing.T) {
	dir := reviewRepo(t)
	m, _ := reviewModel(t, &fakeReviewAgents{}, dir)
	m = openReview(t, m)

	m.review.filter.setQuery("greet.go")
	m.searchReview()
	header := m.review.cursor

	next, cmd := m.reloadReview()
	require.NotNil(t, cmd)
	loaded, ok := cmd().(reviewLoadedMsg)
	require.True(t, ok)
	next.applyReviewLoaded(loaded)

	assert.Equal(t, header, next.review.cursor)
	assert.Contains(t, ansi.Strip(next.reviewMatchCount()), "1 of 1",
		"the sidebar still says where in the search the reader is")
}

// TestReviewReloadKeepsTheSearch pins the whole search surviving the reload key, since the
// prompt goes on showing the pattern either way and a count of nothing under it reads as the
// lines having left the diff.
func TestReviewReloadKeepsTheSearch(t *testing.T) {
	dir := reviewRepo(t)
	m, _ := reviewModel(t, &fakeReviewAgents{}, dir)
	m = openReview(t, m)

	m.review.filter.setQuery("hello")
	m.searchReview()
	matches := m.review.view.MatchCount()
	require.Positive(t, matches)

	next, cmd := m.reloadReview()
	require.NotNil(t, cmd)
	loaded, ok := cmd().(reviewLoadedMsg)
	require.True(t, ok)
	next.applyReviewLoaded(loaded)

	assert.Equal(t, matches, next.review.view.MatchCount())
	assert.Contains(t, ansi.Strip(next.reviewMatchCount()), fmt.Sprintf("1 of %d", matches))

	_, jumped := next.review.view.JumpMatch(next.review.cursor, 1)
	assert.True(t, jumped, "the search still has rows to step through")
}

// TestReviewRefreshSaysNothingWhenTheReadFails pins a failed read being swallowed, since the
// reader is mid-review and the next tick asks again.
func TestReviewRefreshSaysNothingWhenTheReadFails(t *testing.T) {
	dir := reviewRepo(t)
	m, _ := reviewModel(t, &fakeReviewAgents{}, dir)
	m = openReview(t, m)

	m.applyReviewRefreshed(reviewRefreshedMsg{agentID: "a1", err: assert.AnError})

	require.NoError(t, m.review.err)
	assert.False(t, m.review.polling)
}

// TestReviewDiffPollSaysTheWorktreeHasMovedOn covers the round never moving on its own: the
// worktree is hashed in the background, and finding it has moved says so without touching what
// the reader is standing in.
func TestReviewDiffPollSaysTheWorktreeHasMovedOn(t *testing.T) {
	dir := reviewRepo(t)
	m, tmux := reviewModel(t, &fakeReviewAgents{}, dir)
	m = openReview(t, m)

	require.NoError(t, os.WriteFile(filepath.Join(dir, "greet.go"),
		[]byte("package greet\n\nfunc Hello() string {\n\treturn \"written by the agent\"\n}\n"), 0o644))

	cmd := m.pollReviewDiffCmd()
	polled, ok := cmd().(reviewDiffPolledMsg)
	require.True(t, ok)
	require.NoError(t, polled.err)

	tmux.sentKeys = nil
	m.applyReviewDiffPolled(polled)

	require.True(t, m.review.moved)
	assert.Empty(t, tmux.sentKeys, "the diff on screen is left where the reader had it")

	shown := ansi.Strip(m.reviewView())
	assert.Contains(t, shown, "THE WORKTREE HAS MOVED ON")
	assert.NotContains(t, shown, "written by the agent")
}

// TestReviewDiffPollIgnoresAWorktreeThatHasNotMoved pins the probe doing its job, since a notice
// standing over an unchanged worktree asks for a round nobody needs.
func TestReviewDiffPollIgnoresAWorktreeThatHasNotMoved(t *testing.T) {
	dir := reviewRepo(t)
	m, _ := reviewModel(t, &fakeReviewAgents{}, dir)
	m = openReview(t, m)

	polled, ok := m.pollReviewDiffCmd()().(reviewDiffPolledMsg)
	require.True(t, ok)
	require.NoError(t, polled.err)

	m.applyReviewDiffPolled(polled)

	assert.False(t, m.review.moved)
	assert.NotContains(t, ansi.Strip(m.reviewView()), "THE WORKTREE HAS MOVED ON")
}

// TestReviewBacksOutWhenTheWorktreeWasEmptied covers the agent that throws its work away while
// the review is open: catching up finds nothing to show, so the reader is put back on the listing
// rather than left in a diff viewer with no diff in it.
func TestReviewBacksOutWhenTheWorktreeWasEmptied(t *testing.T) {
	dir := reviewRepo(t)
	m, _ := reviewModel(t, &fakeReviewAgents{}, dir)
	m = openReview(t, m)

	gitIn(t, dir, "checkout", "--", ".")

	next, cmd := m.reloadReview()
	require.NotNil(t, cmd)

	loaded, ok := cmd().(reviewLoadedMsg)
	require.True(t, ok)
	require.ErrorIs(t, loaded.err, diff.ErrNoChanges, "the round of an unmoved tree holds nothing")

	next.applyReviewLoaded(loaded)

	assert.Equal(t, screenAgents, next.screen, "the reader is put back on the listing")
	assert.Nil(t, next.review.view, "and the review is let go of")
}

// TestReviewReloadKeyOpensTheNextRound pins the opt-in: the reader asks for the next round, and
// lands back where they were standing in the new one.
func TestReviewReloadKeyOpensTheNextRound(t *testing.T) {
	dir := reviewRepo(t)
	m, _ := reviewModel(t, &fakeReviewAgents{}, dir)
	m = openReview(t, m)

	first := m.review.round.GetOrdinal()

	require.NoError(t, os.WriteFile(filepath.Join(dir, "greet.go"),
		[]byte("package greet\n\nfunc Hello() string {\n\treturn \"written by the agent\"\n}\n"), 0o644))

	polled, ok := m.pollReviewDiffCmd()().(reviewDiffPolledMsg)
	require.True(t, ok)
	m.applyReviewDiffPolled(polled)
	require.True(t, m.review.moved)

	m.review.cursor = m.review.view.Move(m.review.cursor, 1)
	place, located := m.review.view.GetPlace(m.review.cursor)
	require.True(t, located)

	next, cmd := m.updateReview(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("R")})
	m = next.(model)
	require.NotNil(t, cmd, "the next round has to be taken")

	loaded, ok := cmd().(reviewLoadedMsg)
	require.True(t, ok)
	require.NoError(t, loaded.err)
	m.applyReviewLoaded(loaded)

	assert.Greater(t, m.review.round.GetOrdinal(), first, "the round moved on")
	assert.False(t, m.review.moved, "the notice goes with the round it was about")

	found, ok := m.review.view.Locate(place)
	require.True(t, ok)
	assert.Equal(t, found, m.review.cursor, "the reader is back where they were standing")

	var drawn string
	for y := range m.review.view.Len() {
		drawn += m.review.view.RenderRow(y, review.NoCursor, 120, 0)
	}
	assert.Contains(t, drawn, "written by the agent")
}

// TestReviewReloadKeyReadsAgainWithNothingMoved pins the key still meaning reload when the
// background probe has found nothing, since an unmoved tree gives back the round already on
// screen rather than a new one.
func TestReviewReloadKeyReadsAgainWithNothingMoved(t *testing.T) {
	dir := reviewRepo(t)
	m, _ := reviewModel(t, &fakeReviewAgents{}, dir)
	m = openReview(t, m)

	require.False(t, m.review.moved)

	_, cmd := m.updateReview(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("R")})
	require.NotNil(t, cmd)

	loaded, ok := cmd().(reviewLoadedMsg)
	require.True(t, ok)
	assert.NoError(t, loaded.err)
}

// TestReviewDiffPollWaitsBetweenReads pins git being run every few seconds rather than every
// tick, since the conversation is cheap to read and the worktree is not.
func TestReviewDiffPollWaitsBetweenReads(t *testing.T) {
	dir := reviewRepo(t)
	m, _ := reviewModel(t, &fakeReviewAgents{}, dir)
	m = openReview(t, m)

	at := time.Now()
	require.NotNil(t, m.refreshReviewCmd(at))
	require.True(t, m.review.diffPolling)

	m.applyReviewRefreshed(reviewRefreshedMsg{agentID: "a1"})
	m.applyReviewDiffPolled(reviewDiffPolledMsg{agentID: "a1"})

	require.NotNil(t, m.refreshReviewCmd(at.Add(time.Second)))
	assert.False(t, m.review.diffPolling, "a tick a second later reads the conversation and not the worktree")

	m.applyReviewRefreshed(reviewRefreshedMsg{agentID: "a1"})

	require.NotNil(t, m.refreshReviewCmd(at.Add(reviewDiffPollInterval)))
	assert.True(t, m.review.diffPolling)
}

// TestReviewRefreshIsOnlyForTheScreenThatIsUp pins the tick doing nothing anywhere else, since
// the review state is empty the moment the screen is left.
func TestReviewRefreshIsOnlyForTheScreenThatIsUp(t *testing.T) {
	dir := reviewRepo(t)
	m, _ := reviewModel(t, &fakeReviewAgents{}, dir)
	m = openReview(t, m)

	left, _ := m.leaveReview()
	m = left.(model)

	assert.Nil(t, m.refreshReviewCmd(time.Now()))
}

// TestWhatChangedSinceYourLastReviewSurvivesAKeypress covers a standing fact about the diff being
// kept out of the notice slot, since every key in the review clears a notice and the reader would
// otherwise lose the line the moment they scrolled.
func TestWhatChangedSinceYourLastReviewSurvivesAKeypress(t *testing.T) {
	dir := reviewRepo(t)
	m, _ := reviewModel(t, &fakeReviewAgents{}, dir)
	m = openReview(t, m)

	m.review.since = "4 files changed since you last reviewed"
	require.Contains(t, reviewProse(m), "4 files changed since you last reviewed")

	next, _ := m.Update(keyMsg("j"))
	scrolled := next.(model)

	assert.Equal(t, "4 files changed since you last reviewed", scrolled.review.since,
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

	m.review.since = "4 files changed since you last reviewed"
	m.review.notice = "no matches"

	shown := ansi.Strip(m.reviewView())
	assert.Contains(t, shown, "no matches")
	assert.NotContains(t, shown, "4 files changed", "the answer to the last key wins the row")

	next, _ := m.Update(keyMsg("j"))
	assert.Contains(t, reviewProse(next.(model)), "4 files changed since you last reviewed")
}

// TestReviewSaysWhenEverythingIsExcluded covers the difference the reader has to be told about:
// an agent that has written nothing and one whose every line is in a path the workflow hides both
// leave the screen empty, and only one of them means the agent has not worked.
func TestReviewSaysWhenEverythingIsExcluded(t *testing.T) {
	dir := reviewRepo(t)
	gitIn(t, dir, "checkout", "--", ".")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "proto", "gen"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "proto", "gen", "api.pb.go"),
		[]byte("package gen\n"), 0o644))

	m, _ := reviewModel(t, &fakeReviewAgents{}, dir)
	m = openReview(t, m)

	loaded, ok := m.loadReviewCmd(m.review.agentID, dir, []string{"proto/gen"})().(reviewLoadedMsg)
	require.True(t, ok)
	require.ErrorIs(t, loaded.err, diff.ErrAllExcluded, "the agent wrote a file, it is only hidden")

	m.applyReviewLoaded(loaded)

	assert.Equal(t, screenAgents, m.screen, "there is still nothing to draw, so it backs out")
	require.NotEmpty(t, m.messages.items)
	assert.Contains(t, newestMessage(m).summary, "excluded from the diff",
		"and says which of the two empty screens this is")
	assert.NotContains(t, newestMessage(m).summary, "Nothing to review")
}
