package ui

import (
	"errors"
	"testing"
	"time"

	agentstore "weave/internal/store/agent"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestACommentOpensWhileTheAgentIsStillStopping pins the half of the pass that does not wait: a
// comment writes nothing in the worktree, so making the reader watch an agent come to rest before
// they may type is a wait for nothing.
func TestACommentOpensWhileTheAgentIsStillStopping(t *testing.T) {
	dir := reviewRepo(t)
	agents := &fakeReviewAgents{}
	m, _ := reviewModel(t, agents, dir)
	m = openReview(t, m)

	next, cmd := m.updateReview(keyMsg("c"))
	m, ok := next.(model)
	require.True(t, ok)

	assert.Equal(t, modeReviewing, m.review.mode)
	assert.True(t, m.review.composing, "the composer is up before the agent has stopped")
	assert.True(t, m.review.pausing)
	assert.False(t, m.review.paused)

	require.NotNil(t, cmd)
	m = settlePauseBatch(t, m, cmd)

	require.NotNil(t, agents.held, "and the agent is stopped all the same")
	assert.True(t, m.review.paused)
	assert.True(t, m.review.composing, "with what was being typed left alone")
}

// TestAPassThatCannotStopTheAgentTakesTheComposerWithIt covers a comment left standing over an
// agent free to write past the snapshot it is pinned to.
func TestAPassThatCannotStopTheAgentTakesTheComposerWithIt(t *testing.T) {
	dir := reviewRepo(t)
	agents := &fakeReviewAgents{holdErr: errors.New("the agent did not stop")}
	m, _ := reviewModel(t, agents, dir)
	m = openReview(t, m)

	next, cmd := m.updateReview(keyMsg("c"))
	m, ok := next.(model)
	require.True(t, ok)
	require.True(t, m.review.composing)

	m = settlePauseBatch(t, m, cmd)

	assert.Equal(t, modeWatching, m.review.mode)
	assert.False(t, m.review.composing, "the composer goes with the pass")
	assert.False(t, m.review.paused)
}

// TestLeavingAPassLetsTheAgentGo pins the other end of it, since an agent stopped by a pass that
// nobody ended would sit there until somebody noticed.
func TestLeavingAPassLetsTheAgentGo(t *testing.T) {
	dir := reviewRepo(t)
	agents := &fakeReviewAgents{}
	m, _ := reviewModel(t, agents, dir)
	m = openReview(t, m)

	m.review.cursor = reviewRowWithAnEditTarget(t, m)
	m = pressEdit(t, m)
	require.True(t, m.review.paused)

	next, _ := m.closeReviewEdit()
	m, ok := next.(model)
	require.True(t, ok)

	next, cmd := m.leaveReviewWriting()
	m, ok = next.(model)
	require.True(t, ok)

	assert.Equal(t, modeWatching, m.review.mode)
	assert.False(t, m.review.paused)

	runReviewCmd(t, cmd)

	require.NotNil(t, agents.letGo, "the agent the pass stopped is let go")
	assert.Equal(t, m.review.agentID, agents.letGo.GetId())
	assert.Contains(t, agents.letGo.GetSaid(), "let you go")
	assert.Contains(t, agents.letGo.GetSaid(), "greet.go", "and told where to look")
}

// TestAPauseTheReaderTookThemselvesIsNotThePassToLetGo covers a person who stopped the agent on
// purpose before reading it: ending the pass is not a reason to start it again.
func TestAPauseTheReaderTookThemselvesIsNotThePassToLetGo(t *testing.T) {
	dir := reviewRepo(t)
	agents := &fakeReviewAgents{}
	m, _ := reviewModel(t, agents, dir)
	m = openReview(t, m)

	at := time.Now()
	for i, item := range m.listing.agents {
		if item.agent.ID == m.review.agentID {
			m.listing.agents[i].agent.HeldAt = &at
		}
	}

	m.review.cursor = reviewRowWithAnEditTarget(t, m)
	m = pressEdit(t, m)

	assert.Nil(t, agents.held, "an agent already stopped is not stopped again")
	require.True(t, m.review.paused)
	require.False(t, m.review.ourPause)

	next, cmd := m.leaveReviewWriting()
	m, _ = next.(model)

	assert.Nil(t, cmd, "so the end of the pass asks for nothing")
	assert.Nil(t, agents.letGo, "and the pause the reader took stands")
}

// TestAnEditOnADormantAgentPausesNothing covers the key this screen keeps for an agent nobody can
// reach, which has nothing to stop and nothing to let go.
func TestAnEditOnADormantAgentPausesNothing(t *testing.T) {
	dir := reviewRepo(t)
	agents := &fakeReviewAgents{}
	m, _ := reviewModel(t, agents, dir)
	m = openReview(t, m)

	for i, item := range m.listing.agents {
		if item.agent.ID == m.review.agentID {
			m.listing.agents[i].alive = false
		}
	}

	m.review.cursor = reviewRowWithAnEditTarget(t, m)
	m = pressEdit(t, m)

	assert.Equal(t, modeReviewing, m.review.mode)
	assert.True(t, m.review.atEditor, "the editor opens on the worktree it left behind")
	assert.False(t, m.review.paused)
	assert.Nil(t, agents.held)
}

// TestAnUnreadablePaneIsNotStopped pins the state a pass may not stop its way out of, since the
// pane a pause is granted on is the one weave cannot read.
func TestAnUnreadablePaneIsNotStopped(t *testing.T) {
	dir := reviewRepo(t)
	agents := &fakeReviewAgents{}
	m, _ := reviewModel(t, agents, dir)
	m = openReview(t, m)

	setReviewAgentState(&m, agentstore.StateUncertain)
	m.review.cursor = reviewRowWithAnEditTarget(t, m)

	next, _ := m.updateReview(editorKey())
	m, ok := next.(model)
	require.True(t, ok)

	assert.Equal(t, modeWatching, m.review.mode)
	assert.Nil(t, agents.held, "nothing asks a pane weave cannot read to stop")
	assert.Contains(t, m.review.notice, "look at the pane")
}

// settlePauseBatch runs a batch holding the stop and whatever was opened beside it, and hands the
// panel the answer to the stop.
func settlePauseBatch(t *testing.T, m model, cmd tea.Cmd) model {
	t.Helper()

	for _, msg := range flatten(cmd()) {
		paused, ok := msg.(reviewPausedMsg)
		if !ok {
			continue
		}

		next, _ := m.applyReviewPaused(paused)
		settled, ok := next.(model)
		require.True(t, ok)

		return settled
	}

	t.Fatal("the pass asked for nothing to stop")

	return m
}

// flatten is the messages a command produced, one deep, since a batch answers with a message
// holding the commands it was given.
func flatten(msg tea.Msg) []tea.Msg {
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		return []tea.Msg{msg}
	}

	msgs := make([]tea.Msg, 0, len(batch))
	for _, cmd := range batch {
		if cmd != nil {
			msgs = append(msgs, cmd())
		}
	}

	return msgs
}

// runReviewCmd runs a command for its write, which is what reaches the fake.
func runReviewCmd(t *testing.T, cmd tea.Cmd) {
	t.Helper()

	require.NotNil(t, cmd)
	cmd()
}
