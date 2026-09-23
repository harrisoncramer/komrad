package ui

import (
	"testing"
	"time"

	"weave/internal/diff"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestReviewTreeStandsDownMidInput pins that the tick does not fire while the reader is
// typing.
func TestReviewTreeStandsDownMidInput(t *testing.T) {
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

			assert.Nil(t, m.checkReviewTreeCmd(time.Now()))
			assert.False(t, m.review.threads.reading)
			assert.False(t, m.review.diff.tree.checking)
		})
	}
}

// TestReviewTreeCatchesUpOnceTheTurnIsOver covers what watching is for: the worktree is
// hashed in the background, and a move an idle agent left behind is read again on its own.
func TestReviewTreeCatchesUpOnceTheTurnIsOver(t *testing.T) {
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

	cmd := m.review.checkTreeCmd(m.shared())
	polled, ok := cmd().(reviewTreeCheckedMsg)
	require.True(t, ok)
	require.NoError(t, polled.err)

	tmux.sentKeys = nil
	caught := m.review.applyTreeChecked(m.shared(), polled)

	require.True(t, m.review.diff.tree.moved)
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

// TestReviewTreeIgnoresAWorktreeThatHasNotMoved pins the probe leaving an unchanged worktree
// alone.
func TestReviewTreeIgnoresAWorktreeThatHasNotMoved(t *testing.T) {
	dir := reviewRepo(t)
	m, _ := reviewModel(t, &fakeReviewAgents{}, dir)
	m = openReview(t, m)

	polled, ok := m.review.checkTreeCmd(m.shared())().(reviewTreeCheckedMsg)
	require.True(t, ok)
	require.NoError(t, polled.err)

	m.review.applyTreeChecked(m.shared(), polled)

	assert.False(t, m.review.diff.tree.moved)
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

// TestReviewTreeWaitsBetweenChecks pins git being run every few seconds rather than every
// tick.
func TestReviewTreeWaitsBetweenChecks(t *testing.T) {
	dir := reviewRepo(t)
	m, _ := reviewModel(t, &fakeReviewAgents{}, dir)
	m = openReview(t, m)

	at := time.Now()
	require.NotNil(t, m.checkReviewTreeCmd(at))
	require.True(t, m.review.diff.tree.checking)

	m.review.applyTreeChecked(m.shared(), reviewTreeCheckedMsg{agentID: "a1"})

	assert.Nil(t, m.checkReviewTreeCmd(at.Add(time.Second)), "a tick a second later reads nothing")
	assert.False(t, m.review.diff.tree.checking)

	require.NotNil(t, m.checkReviewTreeCmd(at.Add(reviewTreeInterval)))
	assert.True(t, m.review.diff.tree.checking)
}

// TestReviewTreeIsOnlyForTheScreenThatIsUp pins the tick doing nothing anywhere else.
func TestReviewTreeIsOnlyForTheScreenThatIsUp(t *testing.T) {
	dir := reviewRepo(t)
	m, _ := reviewModel(t, &fakeReviewAgents{}, dir)
	m = openReview(t, m)

	m.review.leave(&m, m.shared())

	assert.Nil(t, m.checkReviewTreeCmd(time.Now()))
}
