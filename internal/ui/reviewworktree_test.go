package ui

import (
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestReviewWorktreeSkipsTheHashMidInput pins that no hash starts while the reader is typing.
func TestReviewWorktreeSkipsTheHashMidInput(t *testing.T) {
	tests := []struct {
		name        string
		beginTyping func(*model)
	}{
		{
			name:        "a message is being composed",
			beginTyping: func(m *model) { m.review.threads.composing = true },
		},
		{
			name:        "a search pattern is being typed",
			beginTyping: func(m *model) { m.review.diff.filter.typing = true },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := reviewRepo(t)
			m, _ := reviewModel(t, &fakeReviewAgents{}, dir)
			m = openReview(t, m)

			tt.beginTyping(&m)

			assert.Nil(t, m.hashReviewWorktreeCmd(time.Now()))
			assert.False(t, m.review.threads.reading)
			assert.False(t, m.review.diff.worktree.hashing)
		})
	}
}

// TestReviewWorktreeCatchesUpOnceTheTurnIsOver covers what watching is for: the worktree is
// hashed in the background, and a move an idle agent left behind is read again on its own.
func TestReviewWorktreeCatchesUpOnceTheTurnIsOver(t *testing.T) {
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

	cmd := m.review.hashWorktreeCmd(m.shared())
	hashed, ok := cmd().(reviewWorktreeHashedMsg)
	require.True(t, ok)
	require.NoError(t, hashed.err)

	tmux.sentKeys = nil
	catchUp := m.review.applyWorktreeHashed(m.shared(), hashed)

	require.True(t, m.review.diff.worktree.moved)
	require.NotNil(t, catchUp, "an idle agent's move is read again rather than offered")
	assert.Empty(t, tmux.sentKeys, "and nothing is typed at anybody to do it")

	// The patch is only replaced once the read answers, so what is on screen until then is still
	// the diff the reader was standing in.
	shown := ansi.Strip(m.review.view(m.shared()))
	assert.Contains(t, shown, "catching up")
	assert.NotContains(t, shown, "written by the agent")

	loaded, ok := catchUp().(reviewLoadedMsg)
	require.True(t, ok)
	m.review.applyLoaded(&m, m.shared(), loaded)

	assert.Contains(t, ansi.Strip(m.review.view(m.shared())), "written by the agent", "and then it is drawn")
}

// TestReviewWorktreeHashOfAnUnmovedTreeRaisesNoNotice pins an unchanged worktree showing no notice.
func TestReviewWorktreeHashOfAnUnmovedTreeRaisesNoNotice(t *testing.T) {
	dir := reviewRepo(t)
	m, _ := reviewModel(t, &fakeReviewAgents{}, dir)
	m = openReview(t, m)

	hashed, ok := m.review.hashWorktreeCmd(m.shared())().(reviewWorktreeHashedMsg)
	require.True(t, ok)
	require.NoError(t, hashed.err)

	m.review.applyWorktreeHashed(m.shared(), hashed)

	assert.False(t, m.review.diff.worktree.moved)
	assert.NotContains(t, ansi.Strip(m.review.view(m.shared())), "the agent has written since")
}

// TestReviewWorktreeWaitsBetweenHashes pins at most one hash per interval.
func TestReviewWorktreeWaitsBetweenHashes(t *testing.T) {
	dir := reviewRepo(t)
	m, _ := reviewModel(t, &fakeReviewAgents{}, dir)
	m = openReview(t, m)

	at := time.Now()
	require.NotNil(t, m.hashReviewWorktreeCmd(at))
	require.True(t, m.review.diff.worktree.hashing)

	m.review.applyWorktreeHashed(m.shared(), reviewWorktreeHashedMsg{agentID: "a1"})

	assert.Nil(t, m.hashReviewWorktreeCmd(at.Add(time.Second)), "a tick a second later reads nothing")
	assert.False(t, m.review.diff.worktree.hashing)

	require.NotNil(t, m.hashReviewWorktreeCmd(at.Add(reviewWorktreeHashInterval)))
	assert.True(t, m.review.diff.worktree.hashing)
}

// TestReviewWorktreeHashesOnlyWhileTheReviewIsUp pins that no hash starts once the review is left.
func TestReviewWorktreeHashesOnlyWhileTheReviewIsUp(t *testing.T) {
	dir := reviewRepo(t)
	m, _ := reviewModel(t, &fakeReviewAgents{}, dir)
	m = openReview(t, m)

	m.review.leave(&m, m.shared())

	assert.Nil(t, m.hashReviewWorktreeCmd(time.Now()))
}
