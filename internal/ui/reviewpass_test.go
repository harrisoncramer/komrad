package ui

import (
	"errors"
	"testing"
	"time"

	"weave/internal/ui/panel"
	agentv1 "weave/proto/gen/agent/v1"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
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

	assert.Equal(t, modeReviewing, m.review.pass.mode)
	assert.True(t, m.review.threads.composing, "the composer is up before the agent has stopped")
	assert.True(t, m.review.pass.pausing)
	assert.False(t, m.review.pass.paused)

	require.NotNil(t, cmd)
	m = settlePauseBatch(t, m, cmd)

	require.NotNil(t, agents.held, "and the agent is stopped all the same")
	assert.True(t, m.review.pass.paused)
	assert.True(t, m.review.threads.composing, "with what was being typed left alone")
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
	require.True(t, m.review.threads.composing)

	m = settlePauseBatch(t, m, cmd)

	assert.Equal(t, modeWatching, m.review.pass.mode)
	assert.False(t, m.review.threads.composing, "the composer goes with the pass")
	assert.False(t, m.review.pass.paused)
}

// TestLeavingAPassLetsTheAgentGo pins the other end of it.
func TestLeavingAPassLetsTheAgentGo(t *testing.T) {
	dir := reviewRepo(t)
	agents := &fakeReviewAgents{}
	m, _ := reviewModel(t, agents, dir)
	m = openReview(t, m)

	m.review.diff.cursor = reviewRowWithAnEditTarget(t, m)
	m = pressEdit(t, m)
	require.True(t, m.review.pass.paused)

	m.review.closeEdit(&m)

	cmd := m.review.leaveWriting(&m, m.shared())

	assert.Equal(t, modeWatching, m.review.pass.mode)
	assert.False(t, m.review.pass.paused)

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

	m.review.diff.cursor = reviewRowWithAnEditTarget(t, m)
	m = pressEdit(t, m)

	assert.Nil(t, agents.held, "an agent already stopped is not stopped again")
	require.True(t, m.review.pass.paused)
	require.False(t, m.review.pass.ourPause)

	cmd := m.review.leaveWriting(&m, m.shared())

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

	m.review.diff.cursor = reviewRowWithAnEditTarget(t, m)
	m = pressEdit(t, m)

	assert.Equal(t, modeReviewing, m.review.pass.mode)
	assert.True(t, m.review.pass.atEditor, "the editor opens on the worktree it left behind")
	assert.False(t, m.review.pass.paused)
	assert.Nil(t, agents.held)
}

// TestAnUnreadablePaneIsNotStopped pins the state a pass may not stop its way out of.
func TestAnUnreadablePaneIsNotStopped(t *testing.T) {
	dir := reviewRepo(t)
	agents := &fakeReviewAgents{}
	m, _ := reviewModel(t, agents, dir)
	m = openReview(t, m)

	setReviewAgentState(&m, panel.StateUncertain)
	m.review.diff.cursor = reviewRowWithAnEditTarget(t, m)

	next, _ := m.updateReview(editorKey())
	m, ok := next.(model)
	require.True(t, ok)

	assert.Equal(t, modeWatching, m.review.pass.mode)
	assert.Nil(t, agents.held, "nothing asks a pane weave cannot read to stop")
	assert.Contains(t, m.review.notice, "look at the pane")
}

// TestAPassGivenUpBeforeTheAgentStopsLetsItGo pins the gap between the keypress and the answer:
// an agent stopped for a pass nobody is making any more is one nothing else would ever let go.
func TestAPassGivenUpBeforeTheAgentStopsLetsItGo(t *testing.T) {
	dir := reviewRepo(t)
	agents := &fakeReviewAgents{}
	m, _ := reviewModel(t, agents, dir)
	m = openReview(t, m)

	setReviewAgentState(&m, panel.StateThinking)
	m.review.diff.cursor = reviewRowWithAnEditTarget(t, m)

	next, cmd := m.updateReview(editorKey())
	m, ok := next.(model)
	require.True(t, ok)
	require.True(t, m.review.pass.pausing)

	next, _ = m.updateReview(tea.KeyMsg{Type: tea.KeyEsc})
	m, ok = next.(model)
	require.True(t, ok)
	require.Equal(t, modeWatching, m.review.pass.mode, "the reader gave the pass up")
	require.False(t, m.review.pass.pausing)

	paused, ok := cmd().(reviewPausedMsg)
	require.True(t, ok)

	next, letGo := m.Update(paused)
	m, ok = next.(model)
	require.True(t, ok)

	assert.False(t, m.review.pass.atEditor, "nothing opens onto a screen that went back to watching")
	assert.Equal(t, modeWatching, m.review.pass.mode)

	runReviewCmd(t, letGo)

	require.NotNil(t, agents.letGo, "and the agent the pass stopped is let go")
	assert.Equal(t, m.review.agentID, agents.letGo.GetId())
}

// TestASecondEditPressWaitsRatherThanOpening covers the press that arrives while the agent is
// still stopping, which would otherwise walk past the wait the first press is serving.
func TestASecondEditPressWaitsRatherThanOpening(t *testing.T) {
	dir := reviewRepo(t)
	agents := &fakeReviewAgents{}
	m, _ := reviewModel(t, agents, dir)
	m = openReview(t, m)

	setReviewAgentState(&m, panel.StateIdle)
	m.review.diff.cursor = reviewRowWithAnEditTarget(t, m)

	next, cmd := m.updateReview(editorKey())
	m, ok := next.(model)
	require.True(t, ok)
	require.True(t, m.review.pass.pausing)

	next, _ = m.updateReview(editorKey())
	m, ok = next.(model)
	require.True(t, ok)

	assert.False(t, m.review.pass.atEditor, "the second press is the wait, not a second thing to do")
	assert.Contains(t, m.review.notice, "still stopping")

	m = settlePause(t, m, cmd)
	assert.True(t, m.review.pass.atEditor, "and the editor opens once, when the agent has stopped")
}

// TestADiffThatStopsBeingReadableLetsTheAgentGo covers the ways off this screen nobody chose.
func TestADiffThatStopsBeingReadableLetsTheAgentGo(t *testing.T) {
	dir := reviewRepo(t)
	agents := &fakeReviewAgents{}
	m, _ := reviewModel(t, agents, dir)
	m = openReview(t, m)

	m.review.diff.cursor = reviewRowWithAnEditTarget(t, m)
	m = pressEdit(t, m)
	m.review.closeEdit(&m)
	require.True(t, m.review.pass.paused)

	letGo := m.review.abandon(&m, m.shared(), errors.New("the worktree could not be read"))

	assert.Equal(t, screenAgents, m.screen)

	runReviewCmd(t, letGo)

	require.NotNil(t, agents.letGo, "the agent the pass stopped is let go")
	assert.Contains(t, agents.letGo.GetSaid(), "greet.go", "and told where to look")
}

// TestAReadersOwnPauseKeepsTheFilesForWhoeverLetsItGo covers the files under an editor reaching an
// agent whose pause the pass never took.
func TestAReadersOwnPauseKeepsTheFilesForWhoeverLetsItGo(t *testing.T) {
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

	m.review.diff.cursor = reviewRowWithAnEditTarget(t, m)
	m = pressEdit(t, m)
	m.review.closeEdit(&m)

	agentID := m.review.agentID

	m.review.leaveWriting(&m, m.shared())

	held, ok := m.review.agentItem(m.shared())
	require.True(t, ok)

	cmd := pauseAgent(&m, held.agent)
	runReviewCmd(t, cmd)

	require.NotNil(t, agents.letGo, "the pause the reader took is theirs to let go")
	assert.Equal(t, agentID, agents.letGo.GetId())
	assert.Contains(t, agents.letGo.GetSaid(), "greet.go", "and it still says where to look")
}

// TestADroppedPassKeepsTheFilesForAReadersOwnPause covers the exit with nothing to let go of,
// which is the one that has something to keep: the files are the only thing that pass leaves.
func TestADroppedPassKeepsTheFilesForAReadersOwnPause(t *testing.T) {
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

	m.review.diff.cursor = reviewRowWithAnEditTarget(t, m)
	m = pressEdit(t, m)
	m.review.closeEdit(&m)

	agentID := m.review.agentID
	held, ok := m.review.agentItem(m.shared())
	require.True(t, ok)

	letGo := m.review.abandon(&m, m.shared(), errors.New("the worktree could not be read"))
	assert.Nil(t, letGo, "a pause the reader took is not the pass's to let go")

	cmd := pauseAgent(&m, held.agent)
	runReviewCmd(t, cmd)

	require.NotNil(t, agents.letGo)
	assert.Equal(t, agentID, agents.letGo.GetId())
	assert.Contains(t, agents.letGo.GetSaid(), "greet.go", "and the files survived the drop")
}

// TestAPassRedrawsTheReadersOwnEdits redraws the reader's worktree changes.
func TestAPassRedrawsTheReadersOwnEdits(t *testing.T) {
	repo := newFakeRepo(t.TempDir())
	agents := &fakeReviewAgents{}
	m, _ := reviewModelOver(t, agents, repo)
	m = openReview(t, m)

	next, cmd := m.updateReview(keyMsg("c"))
	m, ok := next.(model)
	require.True(t, ok)
	m = settlePauseBatch(t, m, cmd)
	require.True(t, m.review.pass.paused)

	agents.reviews = []*agentv1.Review{{
		Id:         "v1",
		RoundId:    m.review.round.GetId(),
		AuthorKind: agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_HUMAN,
	}}

	m.review.diff.cursor = reviewRowWithAnEditTarget(t, m)
	m = pressEdit(t, m)

	repo.wrote(wroteParams{Tree: "tree-the-reader-edited", Whole: fakePatchWrittenOver})

	cmd = m.review.closeEdit(&m)
	require.NotNil(t, cmd, "closing the editor reads the worktree again")

	loaded, ok := cmd().(reviewLoadedMsg)
	require.True(t, ok)
	m.review.applyLoaded(&m, m.shared(), loaded)

	assert.Equal(t, snapshotOf("tree-the-reader-edited"), m.review.diff.view.GetSnapshotSHA())
	assert.Contains(t, ansi.Strip(m.review.view(m.shared())), "written by the agent")
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

		m.review.applyPaused(&m, m.shared(), paused)

		return m
	}

	t.Fatal("the pass asked for nothing to stop")

	return m
}

// flatten is the messages a command produced, one deep.
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

// runReviewCmd runs a command for its write.
func runReviewCmd(t *testing.T, cmd tea.Cmd) {
	t.Helper()

	require.NotNil(t, cmd)
	cmd()
}

// TestDroppingTheReviewWhileTheAgentIsStillStoppingLetsItGo covers the hold landing after the
// reader has left the review screen altogether.
func TestDroppingTheReviewWhileTheAgentIsStillStoppingLetsItGo(t *testing.T) {
	for _, key := range []tea.KeyMsg{editorKey(), keyMsg("c")} {
		dir := reviewRepo(t)
		agents := &fakeReviewAgents{}
		m, _ := reviewModel(t, agents, dir)
		m = openReview(t, m)

		setReviewAgentState(&m, panel.StateThinking)
		if key.String() == editorKey().String() {
			m.review.diff.cursor = reviewRowWithAnEditTarget(t, m)
		}

		next, cmd := m.updateReview(key)
		m, ok := next.(model)
		require.True(t, ok)
		require.True(t, m.review.pass.pausing)

		agentID := m.review.agentID

		for m.screen == screenReview {
			next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
			m, ok = next.(model)
			require.True(t, ok)
		}

		var paused tea.Msg
		for _, msg := range flatten(cmd()) {
			if _, ok := msg.(reviewPausedMsg); ok {
				paused = msg
			}
		}

		require.NotNil(t, paused)
		require.NotNil(t, agents.held, "the hold landed after the drop")

		next, letGo := m.Update(paused)
		m, ok = next.(model)
		require.True(t, ok)

		runReviewCmd(t, letGo)

		require.NotNil(t, agents.letGo, "the agent the pass stopped is let go")
		assert.Equal(t, agentID, agents.letGo.GetId())
	}
}
