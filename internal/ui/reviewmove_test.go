package ui

import (
	"testing"

	"weave/internal/gittest"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestReviewMoveWaitsWhileTheReaderTypes pins that no catch-up starts while the reader is typing.
func TestReviewMoveWaitsWhileTheReaderTypes(t *testing.T) {
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
			markReviewWorktreeMoved(&m)

			assert.Nil(t, m.review.followMove(m.shared()))
			assert.False(t, m.review.loading)
		})
	}
}

// TestReviewMoveCatchesUpOnceTheTurnIsOver covers what watching is for: a move the watch reports
// of an idle agent is read again on its own.
func TestReviewMoveCatchesUpOnceTheTurnIsOver(t *testing.T) {
	repo := gittest.NewRepo(t.TempDir())
	m, tmux := buildReviewModel(t, &fakeReviewAgents{}, repo)
	m = openReview(t, m)

	repo.Wrote(gittest.WroteParams{
		Tree:  "tree-written-again",
		Whole: gittest.PatchWrittenOver,
		Sources: map[string]string{
			gittest.File: "package greet\n\nfunc Hello() string {\n\treturn \"written by the agent\"\n}\n",
		},
	})
	markReviewWorktreeMoved(&m)

	tmux.sentKeys = nil
	catchUp := m.review.followMove(m.shared())

	require.True(t, m.review.isWorktreeMoved(m.shared()))
	require.NotNil(t, catchUp, "an idle agent's move is read again rather than offered")
	assert.Empty(t, tmux.sentKeys, "and nothing is typed at anybody to do it")

	shown := ansi.Strip(m.review.view(m.shared()))
	assert.Contains(t, shown, "catching up")
	assert.NotContains(t, shown, "written by the agent")

	loaded, ok := catchUp().(reviewLoadedMsg)
	require.True(t, ok)
	m.review.applyLoaded(&m, m.shared(), loaded)

	assert.Contains(t, ansi.Strip(m.review.view(m.shared())), "written by the agent", "and then it is drawn")
	assert.False(t, m.review.isWorktreeMoved(m.shared()), "and the move was against the round it left")
}

// TestReviewMoveFlagsOnlyTheRoundOnScreen pins which moves the notice is raised for.
func TestReviewMoveFlagsOnlyTheRoundOnScreen(t *testing.T) {
	tests := []struct {
		name  string
		moved func(m model) string
		want  bool
	}{
		{
			name:  "the worktree still hashes to its newest round",
			moved: func(model) string { return "" },
			want:  false,
		},
		{
			name:  "the worktree moved from a round other than the one on screen",
			moved: func(model) string { return "snapshot-of-another-round" },
			want:  false,
		},
		{
			name:  "the worktree moved from the round on screen",
			moved: func(m model) string { return m.review.round.GetSnapshotSha() },
			want:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := reviewRepo(t)
			m, _ := reviewModel(t, &fakeReviewAgents{}, dir)
			m = openReview(t, m)

			setReviewMovedSinceRound(&m, tt.moved(m))

			assert.Equal(t, tt.want, m.review.isWorktreeMoved(m.shared()))
			assert.Equal(t, tt.want, m.review.followMove(m.shared()) != nil)
		})
	}
}

// TestReviewMoveFollowsEachMoveOnce pins one catch-up per move the watch reports.
func TestReviewMoveFollowsEachMoveOnce(t *testing.T) {
	dir := reviewRepo(t)
	m, _ := reviewModel(t, &fakeReviewAgents{}, dir)
	m = openReview(t, m)

	markReviewWorktreeMoved(&m)
	require.NotNil(t, m.review.followMove(m.shared()))

	assert.Nil(t, m.review.followMove(m.shared()), "the same move is not read twice")

	setReviewMovedSinceRound(&m, "")
	assert.Nil(t, m.review.followMove(m.shared()))

	markReviewWorktreeMoved(&m)
	assert.NotNil(t, m.review.followMove(m.shared()), "a later move is read again")
}

// TestReviewMoveCatchesUpOnceTheReaderLetsGo pins the catch-up a reader held back running on the
// screen's next message rather than waiting for the watch.
func TestReviewMoveCatchesUpOnceTheReaderLetsGo(t *testing.T) {
	dir := reviewRepo(t)
	m, _ := reviewModel(t, &fakeReviewAgents{}, dir)
	m = openReview(t, m)

	m.review.threads.composing = true
	markReviewWorktreeMoved(&m)
	require.Nil(t, m.review.followMove(m.shared()))

	m.review.threads.composing = false
	m.review.update(&m, m.shared(), reviewWroteMsg{agentID: m.review.agentID})

	assert.Equal(t, m.review.round.GetSnapshotSha(), m.review.diff.followedMove)
}

// markReviewWorktreeMoved has the watch report the worktree moved past the round on screen.
func markReviewWorktreeMoved(m *model) {
	setReviewMovedSinceRound(m, m.review.round.GetSnapshotSha())
}

// setReviewMovedSinceRound puts what the watch says of the reviewed agent's worktree on its row.
func setReviewMovedSinceRound(m *model, snapshotSHA string) {
	for i, item := range m.listing.agents {
		if item.Agent.ID == m.review.agentID {
			m.listing.agents[i].MovedSinceRound = snapshotSHA
		}
	}
}
