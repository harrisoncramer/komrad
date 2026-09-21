package ui

import (
	"testing"

	agentv1 "weave/proto/gen/agent/v1"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// gatedReview is a released review holding one open comment on the line under the cursor.
func gatedReview(t *testing.T, m model, agents *fakeReviewAgents) model {
	t.Helper()

	threads := pinnedThread(t, m, deliveredComment())
	threads[0].ReviewId = "r1"

	agents.threads = threads
	agents.reviews = []*agentv1.Review{{
		Id:          "r1",
		AuthorKind:  agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_HUMAN,
		ReleasedAt:  "2026-09-13T10:00:00Z",
		OpenThreads: 1,
	}}

	return refreshReview(t, m)
}

// refreshReview runs the tick the panel runs on its own.
func refreshReview(t *testing.T, m model) model {
	t.Helper()

	refreshed, ok := m.review.refreshThreadsCmd(&m)().(reviewRefreshedMsg)
	require.True(t, ok)
	m.review.applyRefreshed(m.shared(), refreshed)

	return m
}

// pressReview presses one key and runs whatever it asked for.
func pressReview(t *testing.T, m model, key tea.KeyMsg) model {
	t.Helper()

	next, cmd := m.updateReview(key)
	m, ok := next.(model)
	require.True(t, ok)

	if cmd == nil {
		return m
	}

	wrote, ok := cmd().(reviewWroteMsg)
	if !ok {
		return m
	}

	m.review.applyWrote(&m, m.shared(), wrote)

	return m
}

// TestSettlingTheLastCommentLetsTheNextReviewGo is the way out of the wait that nothing named:
// the reader settles their own comment and the keys that write come back.
func TestSettlingTheLastCommentLetsTheNextReviewGo(t *testing.T) {
	dir := reviewRepo(t)
	agents := &fakeReviewAgents{}
	m, _ := reviewModel(t, agents, dir)
	m = openReview(t, m)
	m = gatedReview(t, m, agents)

	require.True(t, m.review.threads.isOwedAnAnswer(), "the review starts owed an answer")

	m = pressReview(t, m, keyMsg("r"))
	require.Equal(t, "t1", agents.settled.GetId())
	require.True(t, agents.settled.GetResolved())

	m = refreshReview(t, m)

	assert.False(t, m.review.threads.isOwedAnAnswer(), "settling the last comment ends the wait")

	m = pressReview(t, m, tea.KeyMsg{Type: tea.KeyCtrlS})

	assert.True(t, m.review.threads.releasing, "and the verdict panel opens again")
}

// TestReplyingLetsTheNextReviewGo is the other way out: an answer in a comment the review already
// holds is an unsent comment, and an unsent comment is what the send key asks for.
func TestReplyingLetsTheNextReviewGo(t *testing.T) {
	dir := reviewRepo(t)
	agents := &fakeReviewAgents{}
	m, _ := reviewModel(t, agents, dir)
	m = openReview(t, m)
	m = gatedReview(t, m, agents)

	require.Zero(t, m.review.threads.unsentCount(), "nothing is being held yet")

	m = pressReview(t, m, keyMsg("c"))
	require.True(t, m.review.threads.composing, "the composer opened on the thread")
	require.Equal(t, "t1", m.review.threads.editing)

	m = typeReviewMessage(t, m, "still reads like a changelog")
	m = pressReview(t, m, tea.KeyMsg{Type: tea.KeyCtrlS})

	require.Equal(t, "still reads like a changelog", agents.replied.GetBody())
	assert.Equal(t, 1, m.review.threads.unsentCount(), "which the panel is now holding")
	assert.True(t, m.review.threads.isOwedAnAnswer(), "the comment is still open, so the wait stands")

	m = pressReview(t, m, tea.KeyMsg{Type: tea.KeyCtrlS})

	assert.True(t, m.review.threads.releasing, "and what has been written can go")
}

// TestTheFlagAndTheThreadsDisagreeUntilTheNextTick pins the window where the conversation is
// fresh and the flag is not.
func TestTheFlagAndTheThreadsDisagreeUntilTheNextTick(t *testing.T) {
	dir := reviewRepo(t)
	agents := &fakeReviewAgents{}
	m, _ := reviewModel(t, agents, dir)
	m = openReview(t, m)
	m = gatedReview(t, m, agents)

	m = pressReview(t, m, keyMsg("r"))

	assert.Zero(t, m.review.diff.view.CountOpenThreads(), "the conversation came back settled")
	assert.True(t, m.review.threads.isOwedAnAnswer(), "and the flag has not been asked again yet")

	m = refreshReview(t, m)

	assert.False(t, m.review.threads.isOwedAnAnswer(), "the tick is what agrees them")
}
