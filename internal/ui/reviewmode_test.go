package ui

import (
	"errors"
	"fmt"
	"testing"

	"weave/internal/diff"
	"weave/internal/ui/diffview"
	"weave/internal/ui/panel"
	agentv1 "weave/proto/gen/agent/v1"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// chapterRounds is the shape the chapter tests share: rounds one to three, all off one base.
func chapterRounds() []*agentv1.ReviewRound {
	return []*agentv1.ReviewRound{
		{
			Id:          "r1",
			Ordinal:     1,
			BaseSha:     "b",
			SnapshotSha: "s1",
			CreatedAt:   "2026-09-08T13:00:00-04:00",
		},
		{
			Id:          "r2",
			Ordinal:     2,
			BaseSha:     "b",
			SnapshotSha: "s2",
			CreatedAt:   "2026-09-08T14:00:00-04:00",
		},
		{
			Id:          "r3",
			Ordinal:     3,
			BaseSha:     "b",
			SnapshotSha: "s3",
			CreatedAt:   "2026-09-08T15:00:00-04:00",
		},
	}
}

func releasedAgainst(ordinal int32, roundID, at string) *agentv1.Review {
	return &agentv1.Review{
		Id:           "v" + roundID,
		AuthorKind:   agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_HUMAN,
		RoundId:      roundID,
		RoundOrdinal: ordinal,
		ReleasedAt:   at,
	}
}

// TestTheNewestChapterHidesNothing covers a diff pinned to a round the agent has already moved
// past: the chapter that ends where the reader is looking must not be closed by a round they
// cannot see, or their own newest comments are cut out of the only view that draws them.
func TestTheNewestChapterHidesNothing(t *testing.T) {
	rounds := chapterRounds()
	reviews := []*agentv1.Review{releasedAgainst(1, "r1", "2026-09-08T13:30:00-04:00")}

	chapters := cutChapters(cutChaptersParams{
		Rounds:   rounds,
		Reviews:  reviews,
		OnScreen: rounds[1],
	})

	require.Len(t, chapters, 2)
	assert.Empty(t, chapters[1].Until, "the chapter the reader is standing on closes at nothing")
}

// TestAMessageWithAnUnreadableStampIsKept covers a stamp that will not parse: hiding the comment
// leaves a thread drawn with no text and no way to find out why, so an unreadable stamp has to
// mean show it.
func TestAMessageWithAnUnreadableStampIsKept(t *testing.T) {
	chapters := []reviewChapter{{
		ToSHA:   "s1",
		Ordinal: 1,
		Until:   "2026-09-08T14:00:00-04:00",
	}}

	thread := &agentv1.ReviewThread{
		Id:           "t1",
		RoundOrdinal: 1,
		Messages: []*agentv1.ReviewMessage{
			{Id: "m1", CreatedAt: "2026-09-08T13:10:00-04:00"},
			{Id: "m2", CreatedAt: "not a stamp"},
			{Id: "m3", CreatedAt: ""},
		},
	}

	drawn := threadsByChapter(threadsByChapterParams{
		Threads:  []*agentv1.ReviewThread{thread},
		Chapters: chapters,
		At:       0,
	})

	require.Len(t, drawn, 1)
	assert.Len(t, drawn[0].GetMessages(), 3, "a stamp nobody can read is not a reason to hide a comment")
}

// TestIsReviewHistoryReadsTheDiffOnScreen covers the chapter list being recut under a stationary
// index: what decides whether a comment can be written is the snapshot the diff was drawn to, not
// what the chapter at that index happens to say now.
func TestIsReviewHistoryReadsTheDiffOnScreen(t *testing.T) {
	m := model{}
	m.review.round = &agentv1.ReviewRound{Id: "r3", SnapshotSha: "s3"}
	m.review.diff.chapters = []reviewChapter{{ToSHA: "s3", Ordinal: 3}}
	m.review.diff.chapter = 0
	m.review.diff.head = "s2"

	said, history := m.review.isHistory()
	assert.True(t, history, "the diff was drawn to an older snapshot")
	assert.NotEmpty(t, said, "and the refusal has something to name")
}

// TestReloadWithNoChaptersLetsTheReaderOut covers the round listing failing under a reader
// standing on a round: the walk goes with it, and the diff that arrived was measured for the
// round they were on, so drawing it under the whole change's label reads as current work.
func TestReloadWithNoChaptersLetsTheReaderOut(t *testing.T) {
	agents := &fakeReviewAgents{}
	m := reviewWithAnEarlierReview(t, newFakeRepo(t.TempDir()), agents)
	m = pressReviewBase(t, m)
	require.NotEmpty(t, m.review.diff.base)
	require.GreaterOrEqual(t, m.review.diff.chapter, 0)

	narrowed := m.review.diff.view

	reloading := m
	reload := reloading.review.reload(reloading.shared())
	require.NotNil(t, reload)

	loaded, ok := reload().(reviewLoadedMsg)
	require.True(t, ok)
	loaded.chapters = nil

	again := m.review.applyLoaded(&m, m.shared(), loaded)

	assert.Negative(t, m.review.diff.chapter, "the index does not outlive the list")
	assert.Empty(t, m.review.diff.base, "and the diff is back to the whole change")
	assert.Empty(t, m.review.diff.head)
	assert.Nil(t, m.review.diff.view, "the diff measured for the old round is not drawn")
	assert.NotSame(t, narrowed, m.review.diff.view)
	assert.True(t, m.review.loading, "and the screen says so until the whole change lands")

	require.NotNil(t, again, "the whole change is asked for rather than assumed")

	answer, ok := again().(reviewLoadedMsg)
	require.True(t, ok)
	assert.Equal(t, m.review.round.GetSnapshotSha(), answer.patch.SnapshotSHA,
		"and what comes back is the work as it stands")
}

// TestIsReviewHistoryReadsTheSnapshotDrawn covers the base and head being cleared under a diff
// that is still an older round: what a comment would anchor to is the code on screen, so the
// refusal follows the patch rather than what was asked for.
func TestIsReviewHistoryReadsTheSnapshotDrawn(t *testing.T) {
	m := model{}
	m.review.round = &agentv1.ReviewRound{Id: "r3", SnapshotSha: "s3"}
	m.review.diff.view = diffview.NewView(&diff.Patch{BaseSHA: "s1", SnapshotSHA: "s2"}, false)

	said, history := m.review.isHistory()
	assert.True(t, history, "the diff on screen was drawn to an older snapshot")
	assert.NotEmpty(t, said, "and the refusal has something to name")
}

// TestThePinHoldsWhenTheReviewsCannotBeRead covers the call the pin rests on failing: taking a
// fresh snapshot would move the code out from under comments already written, so the load fails
// rather than guessing that nothing is open.
func TestThePinHoldsWhenTheReviewsCannotBeRead(t *testing.T) {
	dir := reviewRepo(t)
	agents := &fakeReviewAgents{reviewsErr: errors.New("the server went away")}
	m, _ := reviewModel(t, agents, dir)

	loaded := loadReviewMsg(t, m)

	require.Error(t, loaded.err, "an unreadable answer is not the same as nothing open")
	assert.Nil(t, loaded.patch, "so no fresh snapshot is drawn over comments already written")
	assert.Nil(t, loaded.round)
	assert.False(t, loaded.pinned)
}

// TestTheRoundsGiveUpOnlyWhereThePinNeedsThem covers the load's own asymmetry: the rounds are
// what a held pin is found in, so failing to read them is fatal only while a batch is open, and
// every other answer they feed is a courtesy that goes quiet instead.
func TestTheRoundsGiveUpOnlyWhereThePinNeedsThem(t *testing.T) {
	openBatch := []*agentv1.Review{{
		Id:         "v1",
		AuthorKind: agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_HUMAN,
		RoundId:    "r1",
	}}

	tests := []struct {
		name      string
		reviews   []*agentv1.Review
		wantError bool
	}{
		{
			name:      "a batch is open, so the pin cannot be placed",
			reviews:   openBatch,
			wantError: true,
		},
		{
			name:      "nothing is open, so there is no pin to place",
			reviews:   nil,
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := reviewRepo(t)
			agents := &fakeReviewAgents{
				reviews:   tt.reviews,
				roundsErr: errors.New("the server went away"),
			}
			m, _ := reviewModel(t, agents, dir)

			loaded := loadReviewMsg(t, m)

			if tt.wantError {
				require.Error(t, loaded.err)
				assert.Nil(t, loaded.patch, "and nothing is drawn")

				return
			}

			require.NoError(t, loaded.err)
			assert.NotNil(t, loaded.patch, "the diff is drawn from the round opened for it")
			assert.Empty(t, loaded.chapters, "with the walk the rounds would have cut left empty")
		})
	}
}

// TestSendingAReviewLeavesWritingBehind covers the mode outliving the batch it existed for: with
// the review gone the compose, edit and round keys all withdraw, so a reader left in writing is
// parked in a mode whose own keys are missing and whose banner says nothing was written.
func TestSendingAReviewLeavesWritingBehind(t *testing.T) {
	dir := reviewRepo(t)
	agents := &fakeReviewAgents{}
	m, _ := reviewModel(t, agents, dir)
	m = openReview(t, m)

	next, _ := m.routeScreenMessage(keyMsg("c"))
	m = next.(model)
	require.Equal(t, modeReviewing, m.review.pass.mode, "the compose key opens the mode")

	// The composer holds the keyboard while it is open, so it is put away before the verdict key
	// is pressed.
	next, _ = m.routeScreenMessage(keyMsg("esc"))
	m = next.(model)
	require.False(t, m.review.threads.composing)
	require.Equal(t, modeReviewing, m.review.pass.mode, "and the mode outlives one abandoned comment")

	next, _ = m.routeScreenMessage(tea.KeyMsg{Type: tea.KeyCtrlS})
	m = next.(model)
	require.True(t, m.review.threads.releasing, "the verdict panel is open")

	next, cmd := m.routeScreenMessage(tea.KeyMsg{Type: tea.KeyCtrlS})
	m = next.(model)
	require.NotNil(t, cmd, "and the same key sends it")

	assert.Equal(t, modeReviewing, m.review.pass.mode, "the pass stands until the comments have landed")

	letGo := m.review.applyWrote(&m, m.shared(), reviewWroteMsg{
		agentID: m.review.agentID,
		notice:  "changes requested",
		sent:    true,
	})

	assert.Equal(t, modeWatching, m.review.pass.mode, "and then goes with the review it was for")
	assert.Nil(t, letGo, "with nothing to let go of, since this pass stopped nothing")
}

// TestASendThatFailsKeepsThePass covers the reader being put back to watching with neither their
// comments nor the agent they stopped, over a send that never landed.
func TestASendThatFailsKeepsThePass(t *testing.T) {
	dir := reviewRepo(t)
	agents := &fakeReviewAgents{}
	m, _ := reviewModel(t, agents, dir)
	m = openReview(t, m)

	m.review.diff.cursor = reviewRowWithAnEditTarget(t, m)
	m = pressEdit(t, m)
	m.review.closeEdit(&m, m.shared())
	require.True(t, m.review.pass.paused)

	cmd := m.review.send(m.shared(), false)
	require.NotNil(t, cmd)

	letGo := m.review.applyWrote(&m, m.shared(), reviewWroteMsg{
		agentID: m.review.agentID,
		err:     errors.New("the review could not be released"),
		sent:    true,
	})

	assert.Nil(t, letGo, "the agent is not let go over comments it never got")
	assert.Nil(t, agents.letGo)
	assert.Equal(t, modeReviewing, m.review.pass.mode, "and the reader is left where they were")
	assert.True(t, m.review.pass.paused)
}

// TestASendThatLandedIsNotUndoneByAnUnreadableReview covers the two failures the answer used to
// carry as one: the comments are in the agent's hands, so a review that will not read back is a
// stale screen rather than a send to try again.
func TestASendThatLandedIsNotUndoneByAnUnreadableReview(t *testing.T) {
	dir := reviewRepo(t)
	agents := &fakeReviewAgents{}
	m, _ := reviewModel(t, agents, dir)
	m = openReview(t, m)

	m.review.diff.cursor = reviewRowWithAnEditTarget(t, m)
	m = pressEdit(t, m)
	m.review.closeEdit(&m, m.shared())
	require.True(t, m.review.pass.paused)

	cmd := m.review.send(m.shared(), false)
	require.NotNil(t, cmd)

	letGo := m.review.applyWrote(&m, m.shared(), reviewWroteMsg{
		agentID: m.review.agentID,
		notice:  "changes requested",
		readErr: errors.New("the review could not be read back"),
		sent:    true,
	})

	assert.Equal(t, modeWatching, m.review.pass.mode, "the review went, so the pass is over")
	assert.False(t, m.review.pass.paused)

	runReviewCmd(t, letGo)

	require.NotNil(t, agents.letGo, "and the agent is let go, told what it was sent")
	assert.Contains(t, agents.letGo.GetSaid(), "My comments are pinned")
}

// TestAnotherWriteLandingMidSendIsNotTheSend covers every write on this screen coming back the
// same way: a comment answered while a review is still in flight would otherwise end the pass and
// let the agent go before the review had landed at all.
func TestAnotherWriteLandingMidSendIsNotTheSend(t *testing.T) {
	dir := reviewRepo(t)
	agents := &fakeReviewAgents{}
	m, _ := reviewModel(t, agents, dir)
	m = openReview(t, m)

	m.review.diff.cursor = reviewRowWithAnEditTarget(t, m)
	m = pressEdit(t, m)
	m.review.closeEdit(&m, m.shared())
	require.True(t, m.review.pass.paused)

	cmd := m.review.send(m.shared(), false)
	require.NotNil(t, cmd, "the review is on its way")

	letGo := m.review.applyWrote(&m, m.shared(), reviewWroteMsg{agentID: m.review.agentID, notice: "comment added"})

	assert.Nil(t, letGo, "an ordinary write is not the send")
	assert.Nil(t, agents.letGo)
	assert.Equal(t, modeReviewing, m.review.pass.mode)
	assert.True(t, m.review.pass.paused, "so the agent is still stopped when the review lands")
}

// TestTheVerdictIsRefusedOnAFinishedRound covers rule three reaching the third write key: the
// release panel opened on a round the agent had moved past, and the review went, while the line
// above it said nothing could be written there.
func TestTheVerdictIsRefusedOnAFinishedRound(t *testing.T) {
	dir := reviewRepo(t)
	agents := &fakeReviewAgents{}
	m, _ := reviewModel(t, agents, dir)
	m = openReview(t, m)

	m = standOnAFinishedRound(m)

	next, _ := m.routeScreenMessage(tea.KeyMsg{Type: tea.KeyCtrlS})
	m = next.(model)

	assert.False(t, m.review.threads.releasing, "the verdict panel stays shut")
	assert.Contains(t, m.review.notice, "round 1 of 2 is finished")
	assert.Nil(t, agents.released, "and nothing went to the agent")
}

// TestWritingIsRefusedWhileAReviewIsOut covers a review already sent and unanswered: the keys
// that write were still answered, so comments piled into a batch the service refuses to release.
func TestWritingIsRefusedWhileAReviewIsOut(t *testing.T) {
	dir := reviewRepo(t)
	agents := &fakeReviewAgents{}
	m, _ := reviewModel(t, agents, dir)
	m = openReview(t, m)

	m.review.threads.owed = "r1"

	next, _ := m.routeScreenMessage(keyMsg("c"))
	m = next.(model)

	assert.Equal(t, modeWatching, m.review.pass.mode, "the mode did not open")
	assert.False(t, m.review.threads.composing, "and no editor did either")
	assert.Contains(t, m.review.notice, "still owed an answer")
	assert.Nil(t, agents.created, "and nothing was written")
}

// TestReplyingIsAllowedWhileAReviewIsOut covers the other side of that gate: an answer in a
// comment the review already holds is not the second review the wait is there to hold off.
func TestReplyingIsAllowedWhileAReviewIsOut(t *testing.T) {
	dir := reviewRepo(t)
	agents := &fakeReviewAgents{}
	m, _ := reviewModel(t, agents, dir)
	m = openReview(t, m)

	m.review.applyThreads(m.shared(), pinnedThread(t, m, deliveredComment()))
	m.review.threads.owed = "r1"

	next, _ := m.routeScreenMessage(keyMsg("c"))
	m = next.(model)

	assert.True(t, m.review.threads.composing, "the editor opened on the thread")
	assert.Equal(t, "t1", m.review.threads.editing, "and what it writes goes into that thread")
	assert.Empty(t, m.review.notice)
}

// TestSendingIsRefusedWhileAReviewIsOutWithNothingWritten covers the verdict key, which with
// nothing drafted opens a review of its own rather than answering the one already out.
func TestSendingIsRefusedWhileAReviewIsOutWithNothingWritten(t *testing.T) {
	dir := reviewRepo(t)
	agents := &fakeReviewAgents{}
	m, _ := reviewModel(t, agents, dir)
	m = openReview(t, m)

	m.review.threads.owed = "r1"

	next, _ := m.routeScreenMessage(tea.KeyMsg{Type: tea.KeyCtrlS})
	m = next.(model)

	assert.False(t, m.review.threads.releasing, "the verdict panel stays shut")
	assert.Contains(t, m.review.notice, "still owed an answer")
	assert.Nil(t, agents.released)
}

// TestSendingAReplyIsAllowedWhileAReviewIsOut is the other half.
func TestSendingAReplyIsAllowedWhileAReviewIsOut(t *testing.T) {
	dir := reviewRepo(t)
	agents := &fakeReviewAgents{}
	m, _ := reviewModel(t, agents, dir)
	m = openReview(t, m)

	m.review.threads.owed = "r1"
	m.review.threads.activity.delivery[deliverySlot(agentv1.ReviewDeliveryState_REVIEW_DELIVERY_STATE_UNSENT)] = 1

	next, _ := m.routeScreenMessage(tea.KeyMsg{Type: tea.KeyCtrlS})
	m = next.(model)

	assert.True(t, m.review.threads.releasing, "what has been written can go")
	assert.Empty(t, m.review.notice)
}

// reviewRepoWithADeletion is a worktree whose round both changes a file and removes one.
func reviewRepoWithADeletion(t *testing.T) *fakeRepo {
	t.Helper()

	repo := newFakeRepo(t.TempDir())
	repo.sources[sourceOf(fakeBaseSHA, "gone.go")] = "package greet\n\nfunc Gone() {}\n"
	repo.wrote(wroteParams{
		Tree:    "tree-with-a-deletion",
		Whole:   fakePatchWithADeletion,
		Sources: map[string]string{fakeFile: fakeHeadSource},
	})

	return repo
}

// reviewRowInDeletedFile is a row of the file the round removed.
func reviewRowInDeletedFile(t *testing.T, m model) int {
	t.Helper()

	for row := range m.review.diff.view.Len() {
		if _, _, ok := m.review.diff.view.GetEditTarget(row); ok {
			continue
		}

		if file, ok := m.review.diff.view.GetDrawnFileAt(row); ok && file.OldPath == "gone.go" {
			return row
		}
	}

	t.Fatal("the diff draws no row of the removed file")

	return -1
}

// editorKey is the key the edit action is bound to by default.
func editorKey() tea.KeyMsg {
	return keyMsg("e")
}

// pressEdit presses the edit key and requires that something opened.
func pressEdit(t *testing.T, m model) model {
	t.Helper()

	next, cmd := m.routeScreenMessage(editorKey())
	written, ok := next.(model)
	require.True(t, ok)
	require.Empty(t, written.review.notice, "the press opened the editor")

	// The first write in a pass stops the agent first, so the editor opens on the answer to that
	// rather than on the keypress.
	if written.review.pass.pausing {
		written = settlePause(t, written, cmd)
	}

	return written
}

// settlePause runs the stop the pass asked for and hands the panel what came back.
func settlePause(t *testing.T, m model, cmd tea.Cmd) model {
	t.Helper()

	require.NotNil(t, cmd, "the pass asked for the agent to stop")

	paused, ok := cmd().(reviewPausedMsg)
	require.True(t, ok, "a pass waits on the agent stopping")

	m.review.applyPaused(&m, m.shared(), paused)

	return m
}

// reviewRowWithAnEditTarget is a row the editor key has something to open.
func reviewRowWithAnEditTarget(t *testing.T, m model) int {
	t.Helper()

	for row := range m.review.diff.view.Len() {
		if _, _, ok := m.review.diff.view.GetEditTarget(row); ok {
			return row
		}
	}

	t.Fatal("the diff draws no row the editor can open")

	return -1
}

// TestAnEditRefusedOnADeletedFileOpensNoMode covers a press on a row with no file behind it,
// which used to switch the screen's mode on the strength of an editor it never opened.
func TestAnEditRefusedOnADeletedFileOpensNoMode(t *testing.T) {
	agents := &fakeReviewAgents{}
	m, _ := reviewModelOver(t, agents, reviewRepoWithADeletion(t))
	m = openReview(t, m)

	m.review.diff.cursor = reviewRowInDeletedFile(t, m)

	next, _ := m.routeScreenMessage(editorKey())
	m = next.(model)

	assert.Equal(t, modeWatching, m.review.pass.mode, "a press that opened nothing does not open a mode")
	assert.Contains(t, m.review.notice, "deleted")
}

// TestAnEditThatTakesOpensTheEditingMode pins that editing is its own mode rather than a
// review being written.
func TestAnEditThatTakesOpensTheEditingMode(t *testing.T) {
	dir := reviewRepo(t)
	agents := &fakeReviewAgents{}
	m, _ := reviewModel(t, agents, dir)
	m = openReview(t, m)

	m.review.diff.cursor = reviewRowWithAnEditTarget(t, m)

	m = pressEdit(t, m)

	assert.Equal(t, modeReviewing, m.review.pass.mode)
	assert.True(t, m.review.pass.atEditor, "the editor holds the stage")
	assert.NotEmpty(t, m.review.pass.editingPath, "and the band can name what it opened")
}

// TestAnEditPausesAnAgentMidTurn pins what a pass does about two writers in one checkout: it stops
// the one that is not you.
func TestAnEditPausesAnAgentMidTurn(t *testing.T) {
	dir := reviewRepo(t)
	agents := &fakeReviewAgents{}
	m, _ := reviewModel(t, agents, dir)
	m = openReview(t, m)

	setReviewAgentState(&m, panel.StateThinking)
	m.review.diff.cursor = reviewRowWithAnEditTarget(t, m)

	next, cmd := m.routeScreenMessage(editorKey())
	m = next.(model)

	require.True(t, m.review.pass.pausing, "the editor waits on the agent stopping")
	assert.False(t, m.review.pass.atEditor, "so nothing is open over a writer yet")

	m = settlePause(t, m, cmd)

	require.NotNil(t, agents.held, "the agent was asked to stop")
	assert.Equal(t, m.review.agentID, agents.held.GetId())
	assert.True(t, m.review.pass.paused)
	assert.True(t, m.review.pass.atEditor, "and the editor opens once it has")
}

// TestAPassThatCannotStopTheAgentOpensNothing pins the case the whole wait is for: an interrupt
// that never lands must not put a second writer in the tree.
func TestAPassThatCannotStopTheAgentOpensNothing(t *testing.T) {
	dir := reviewRepo(t)
	agents := &fakeReviewAgents{holdErr: errors.New("the agent did not stop")}
	m, _ := reviewModel(t, agents, dir)
	m = openReview(t, m)

	// An idle agent is the case with nothing else standing in the way, so what keeps the editor
	// shut here is the stop not having landed rather than the row saying the agent is writing.
	setReviewAgentState(&m, panel.StateIdle)
	m.review.diff.cursor = reviewRowWithAnEditTarget(t, m)

	next, cmd := m.routeScreenMessage(editorKey())
	m = next.(model)
	require.False(t, m.review.pass.atEditor, "nothing opens while the agent may still be writing")

	m = settlePause(t, m, cmd)

	assert.Equal(t, modeWatching, m.review.pass.mode, "the pass is given up rather than opened")
	assert.False(t, m.review.pass.paused)
	assert.False(t, m.review.pass.atEditor, "and nothing is open over an agent that may be writing")
}

// TestTheEditorClosingGivesTheStageBack covers the screen noticing an editor that was quit
// from inside.
func TestTheEditorClosingGivesTheStageBack(t *testing.T) {
	dir := reviewRepo(t)
	agents := &fakeReviewAgents{}
	m, tmux := reviewModel(t, agents, dir)
	m = openReview(t, m)

	m.review.diff.cursor = reviewRowWithAnEditTarget(t, m)
	m = pressEdit(t, m)
	require.Equal(t, modeReviewing, m.review.pass.mode)
	require.NotEmpty(t, m.review.pass.editorPane, "the editor was given a pane")

	tmux.deadPanes = map[string]bool{m.review.pass.editorPane: true}

	cmd := m.reconcileEditor()

	assert.Equal(t, modeReviewing, m.review.pass.mode, "closing an editor is not the end of the pass")
	assert.False(t, m.review.pass.atEditor)
	assert.NotNil(t, cmd, "and the diff is read again, since the editor may have written to it")
}

// TestLeavingTheReviewClosesTheEditorFirst pins the back key against the editor outliving the
// screen it was opened from.
func TestLeavingTheReviewClosesTheEditorFirst(t *testing.T) {
	dir := reviewRepo(t)
	agents := &fakeReviewAgents{}
	m, _ := reviewModel(t, agents, dir)
	m = openReview(t, m)

	m.review.diff.cursor = reviewRowWithAnEditTarget(t, m)
	m = pressEdit(t, m)
	require.Equal(t, modeReviewing, m.review.pass.mode)

	m.review.leave(&m, m.shared())

	assert.Equal(t, screenReview, m.screen, "the first press gives up the editor rather than the screen")
	assert.Equal(t, modeReviewing, m.review.pass.mode, "and stays in the pass the editor was opened in")
	assert.False(t, m.review.pass.atEditor)
}

// TestWatchingHoldsTheDiffWhileTheAgentWrites is what keeps watching from costing history: a
// catch-up reads a round, so following every save would leave one behind every few seconds.
func TestWatchingHoldsTheDiffWhileTheAgentWrites(t *testing.T) {
	dir := reviewRepo(t)
	agents := &fakeReviewAgents{}
	m, _ := reviewModel(t, agents, dir)
	m = openReview(t, m)

	setReviewAgentState(&m, panel.StateThinking)
	assert.False(t, m.review.doesFollowLive(m.shared()), "a diff is not redrawn under a writer")

	cmd := m.review.applyDiffPolled(m.shared(), reviewDiffPolledMsg{agentID: m.review.agentID, moved: true})
	assert.Nil(t, cmd, "so nothing is read again while it writes")
	assert.Contains(t, m.review.staleSaid(m.shared()), "catches up when it stops")

	setReviewAgentState(&m, panel.StateIdle)
	require.True(t, m.review.doesFollowLive(m.shared()), "the end of a turn is where it catches up")

	cmd = m.review.applyDiffPolled(m.shared(), reviewDiffPolledMsg{agentID: m.review.agentID, moved: true})
	assert.NotNil(t, cmd, "and that is one read rather than one every poll")
}

// TestAWaitingAgentDoesNotHoldTheEditKey pins the gate against the half of an unfinished turn
// that writes nothing: an agent at a prompt is stopped until somebody answers it.
func TestAWaitingAgentDoesNotHoldTheEditKey(t *testing.T) {
	dir := reviewRepo(t)
	agents := &fakeReviewAgents{}
	m, _ := reviewModel(t, agents, dir)
	m = openReview(t, m)

	setReviewAgentState(&m, panel.StateWaiting)
	m.review.diff.cursor = reviewRowWithAnEditTarget(t, m)

	m = pressEdit(t, m)

	assert.Equal(t, modeReviewing, m.review.pass.mode)
}

// TestAnUnreadablePaneRefusesTheEditKey pins the state that says weave could not read the pane
// rather than that the agent is idle.
func TestAnUnreadablePaneRefusesTheEditKey(t *testing.T) {
	dir := reviewRepo(t)
	agents := &fakeReviewAgents{}
	m, _ := reviewModel(t, agents, dir)
	m = openReview(t, m)

	setReviewAgentState(&m, panel.StateUncertain)
	m.review.diff.cursor = reviewRowWithAnEditTarget(t, m)

	next, _ := m.routeScreenMessage(editorKey())
	m = next.(model)

	assert.Equal(t, modeWatching, m.review.pass.mode, "nothing opened")
	assert.Contains(t, m.review.notice, "cannot read this agent's pane")

	// The catch-up is the other reader of the agent's state, and an unreadable pane is not a
	// reason to hold the diff forever.
	assert.True(t, m.review.doesFollowLive(m.shared()), "the diff still catches up")
}

// TestTheGateReadsTheReviewedAgentRatherThanTheFirstRow covers the listing holding more than one
// agent, which the fixture does not: a gate that stops at the first row answers for whoever
// happens to be at the top of the panel.
func TestTheGateReadsTheReviewedAgentRatherThanTheFirstRow(t *testing.T) {
	dir := reviewRepo(t)
	agents := &fakeReviewAgents{}
	m, _ := reviewModel(t, agents, dir)
	m = openReview(t, m)

	idle := agentItem{agent: panel.Agent{
		ID:    "someone-else",
		Name:  "idle",
		State: panel.StateIdle,
	}, alive: true}
	m.listing.agents = append([]agentItem{idle}, m.listing.agents...)
	setReviewAgentState(&m, panel.StateThinking)

	m.review.diff.cursor = reviewRowWithAnEditTarget(t, m)

	next, cmd := m.routeScreenMessage(editorKey())
	m = next.(model)
	m = settlePause(t, m, cmd)

	require.NotNil(t, agents.held, "the agent being reviewed is the one that decides")
	assert.Equal(t, m.review.agentID, agents.held.GetId(), "rather than whoever is at the top")
	assert.False(t, m.review.doesFollowLive(m.shared()), "and the catch-up reads the same row")
}

// TestEditingGivesTheWindowToTheEditor pins the panel letting go of the window, which it has to
// do or it zooms its own sidebar over the editor it just opened.
func TestEditingGivesTheWindowToTheEditor(t *testing.T) {
	dir := reviewRepo(t)
	agents := &fakeReviewAgents{}
	m, _ := reviewModel(t, agents, dir)
	m = openReview(t, m)

	spec, ok := reviewColumn(), true
	require.True(t, ok)
	require.NotNil(t, spec.holds, "the review says whether it is holding the window")
	require.True(t, spec.holds(m), "and it is, while it is drawing the diff")

	m.review.diff.cursor = reviewRowWithAnEditTarget(t, m)
	m = pressEdit(t, m)

	assert.False(t, spec.holds(m), "the editor has the window now")
}

// setReviewAgentState puts the agent whose diff is on screen into one state.
func setReviewAgentState(m *model, state panel.State) {
	for i, item := range m.listing.agents {
		if item.agent.ID == m.review.agentID {
			m.listing.agents[i].agent.State = state
		}
	}
}

// TestTheForwardRoundKeyIsOfferedOnlyWhereItAnswers covers the two halves of one walk disagreeing:
// the key was drawn in the writing mode, where its own action refuses to run.
func TestTheForwardRoundKeyIsOfferedOnlyWhereItAnswers(t *testing.T) {
	m := model{}
	m.review.diff.chapters = []reviewChapter{{ToSHA: "s1", Ordinal: 1}, {ToSHA: "s2", Ordinal: 2}}
	m.review.diff.chapter = 0
	m.review.pass.mode = modeReviewing

	row := reviewLegendRowFor(t, &m.review, actionReviewBaseForward)
	assert.False(t, row.show(m.shared()), "the forward key is not offered where it answers nothing")
}

// reviewRowWithAnAnchor is a row of the diff a comment can hang on.
func reviewRowWithAnAnchor(t *testing.T, m model) int {
	t.Helper()

	for row := range m.review.diff.view.Len() {
		if _, ok := m.review.diff.view.GetAnchor(row); ok {
			return row
		}
	}

	t.Fatal("the diff draws no line a comment can hang on")

	return -1
}

// reviewAnchorInTheDiff is a line of the diff a comment can hang on.
func reviewAnchorInTheDiff(t *testing.T, m model) diffview.Anchor {
	t.Helper()

	anchor, ok := m.review.diff.view.GetAnchor(reviewRowWithAnAnchor(t, m))
	require.True(t, ok)

	return *anchor
}

// TestAComposerDoesNotOutliveTheDiffItWasWrittenOn covers a comment being typed while the load
// that answers with a short walk lands: the diff it was anchored to is taken away, and the submit
// key read it anyway.
func TestAComposerDoesNotOutliveTheDiffItWasWrittenOn(t *testing.T) {
	agents := &fakeReviewAgents{}
	m := reviewWithAnEarlierReview(t, newFakeRepo(t.TempDir()), agents)
	m = pressReviewBase(t, m)
	require.GreaterOrEqual(t, m.review.diff.chapter, 0)

	m.review.diff.cursor = reviewRowWithAnAnchor(t, m)

	next, _ := m.routeScreenMessage(keyMsg("c"))
	m = next.(model)
	require.True(t, m.review.threads.composing, "the composer is open on a line of the round")

	m = typeReviewMessage(t, m, "this should say hello")

	reloading := m
	reload := reloading.review.reload(reloading.shared())
	require.NotNil(t, reload)

	loaded, ok := reload().(reviewLoadedMsg)
	require.True(t, ok)
	loaded.chapters = nil

	m.review.applyLoaded(&m, m.shared(), loaded)
	require.Nil(t, m.review.diff.view)

	assert.False(t, m.review.threads.composing, "the composer goes with the diff it was anchored to")

	next, _ = m.routeScreenMessage(tea.KeyMsg{Type: tea.KeyCtrlS})
	m = next.(model)

	assert.Nil(t, agents.created, "and nothing is filed against a diff that is gone")
}

// TestACommentRecordsTheDiffItWasWrittenOn covers the two SHAs a thread is filed with: the
// numbers come off the patch on screen, so they are read from it rather than from what the last
// load happened to ask for.
func TestACommentRecordsTheDiffItWasWrittenOn(t *testing.T) {
	dir := reviewRepo(t)
	agents := &fakeReviewAgents{}
	m, _ := reviewModel(t, agents, dir)
	m = openReview(t, m)

	drawnBase := m.review.diff.view.GetBaseSHA()
	drawnSnapshot := m.review.diff.view.GetSnapshotSHA()
	require.NotEmpty(t, drawnBase)
	require.NotEmpty(t, drawnSnapshot)

	m.review.diff.base = "some-other-commit"
	m.review.diff.head = ""

	anchor := reviewAnchorInTheDiff(t, m)

	cmd := m.review.createThreadCmd(m.shared(), anchor, "this should say hello")
	require.NotNil(t, cmd)

	wrote, ok := cmd().(reviewWroteMsg)
	require.True(t, ok)
	require.NoError(t, wrote.err)

	require.NotNil(t, agents.created)
	assert.Equal(t, drawnBase, agents.created.GetBaseSha())
	assert.Equal(t, drawnSnapshot, agents.created.GetSnapshotSha())
}

// TestTheSendKeyIsOfferedOnlyWhereItAnswers covers the legend and the key disagreeing: the key
// refuses on a finished round, and the row offered a verdict on one.
func TestTheSendKeyIsOfferedOnlyWhereItAnswers(t *testing.T) {
	dir := reviewRepo(t)
	m, _ := reviewModel(t, &fakeReviewAgents{}, dir)
	m = openReview(t, m)

	row := reviewLegendRowFor(t, &m.review, actionReviewSend)
	assert.True(t, row.show(m.shared()), "reading the current work is where a verdict answers")

	out := m
	out.review.threads.owed = "r1"
	row = reviewLegendRowFor(t, &out.review, actionReviewSend)
	assert.True(t, row.show(out.shared()), "a review already out is still answered in its own threads")

	finished := standOnAFinishedRound(m)
	row = reviewLegendRowFor(t, &finished.review, actionReviewSend)
	assert.False(t, row.show(finished.shared()), "and a finished round holds nothing to send")
}

// reviewLegendRowFor is the legend row one key is drawn on.
func reviewLegendRowFor(t *testing.T, r *reviewScreen, id actionID) screenLegendRow {
	t.Helper()

	for _, row := range r.legend() {
		if len(row.of) == 1 && row.of[0] == id {
			return row
		}
	}

	t.Fatal("that key has no legend row")

	return screenLegendRow{}
}

// TestTheBackKeySaysWhichThingItLeaves covers the key having two jobs since the mode split: in
// writing it puts the review down, and the legend claimed it left the screen.
func TestTheBackKeySaysWhichThingItLeaves(t *testing.T) {
	m := model{}
	m.review.pass.mode = modeReviewing

	row := reviewLegendRowFor(t, &m.review, actionReviewBack)
	assert.NotContains(t, row.desc(m.shared()), "the agents",
		"in writing the key leaves the mode rather than the screen")
}

// TestComposingOnARowWithNoLineSaysSo covers the press that does nothing: a cursor on a heading
// has no line to hang a comment on, and silence there reads as the key being broken.
func TestComposingOnARowWithNoLineSaysSo(t *testing.T) {
	dir := reviewRepo(t)
	agents := &fakeReviewAgents{}
	m, _ := reviewModel(t, agents, dir)
	m = openReview(t, m)

	blank := -1
	for row := range m.review.diff.view.Len() {
		if _, ok := m.review.diff.view.GetAnchor(row); !ok {
			blank = row

			break
		}
	}

	require.GreaterOrEqual(t, blank, 0, "the diff draws a row with no line of its own")
	m.review.diff.cursor = blank

	next, _ := m.routeScreenMessage(keyMsg("c"))
	m = next.(model)

	assert.False(t, m.review.threads.composing, "no editor opened")
	assert.NotEmpty(t, m.review.notice, "and the key said why")
}

// TestACommentOnTheNewestRoundRecordsItsBase covers what a narrowed write is measured from, which
// the mode split left with no test of its own: the newest round is narrowed on its base and
// writing there is allowed, so the base it was counted from still has to reach the service.
func TestACommentOnTheNewestRoundRecordsItsBase(t *testing.T) {
	agents := &fakeReviewAgents{}

	m := reviewWithAnEarlierReview(t, newFakeRepo(t.TempDir()), agents)
	m = pressReviewBase(t, m)
	require.NotEmpty(t, m.review.diff.base, "the diff is measured from the round reviewed before")

	for row := range m.review.diff.view.Len() {
		if found, ok := m.review.diff.view.GetAnchor(row); ok && found.NewStart > 0 {
			m.review.diff.cursor = row

			break
		}
	}

	next, _ := m.routeScreenMessage(keyMsg("c"))
	m = next.(model)
	require.True(t, m.review.threads.composing, "writing is allowed on the newest round")

	m = typeReviewMessage(t, m, "this line, in the work since I last looked")

	cmd := m.review.updateComposer(&m, m.shared(), keyMsg("ctrl+s"))
	require.NotNil(t, cmd)
	cmd()

	require.NotNil(t, agents.created, "the thread reached the service")
	assert.Equal(t, m.review.diff.base, agents.created.GetBaseSha(),
		"and says which commit its old side was counted from")
	assert.Equal(t, m.review.round.GetSnapshotSha(), agents.created.GetSnapshotSha(),
		"and which snapshot its new side came from")
	assert.Positive(t, agents.created.GetNewStart())
}

// TestTheOpenCommentKeyWalksOnlyWhatIsUnsettled is how a person clears a review: the settled
// threads are history and the key steps past them.
func TestTheOpenCommentKeyWalksOnlyWhatIsUnsettled(t *testing.T) {
	dir := reviewRepo(t)
	agents := &fakeReviewAgents{}
	m, _ := reviewModel(t, agents, dir)
	m = openReview(t, m)

	m.review.applyWrote(&m, m.shared(), reviewWroteMsg{agentID: "a1", threads: threadsOnEveryLine(t)})

	next, _ := m.routeScreenMessage(keyMsg("u"))
	written, ok := next.(model)
	require.True(t, ok)

	thread, ok := written.review.diff.view.GetThreadAt(written.review.diff.cursor)
	require.True(t, ok, "the key lands on a thread")
	assert.Empty(t, thread.GetResolvedAt(), "and it is one nobody has settled")
	assert.Equal(t, "open-1", thread.GetId(), "which is the open one rather than the settled one above it")
}

// TestTheOpenCommentKeySaysWhenThereIsNothingLeft covers a review whose threads are all settled,
// where a key that silently did nothing would read as a key that did not register.
func TestTheOpenCommentKeySaysWhenThereIsNothingLeft(t *testing.T) {
	dir := reviewRepo(t)
	agents := &fakeReviewAgents{}
	m, _ := reviewModel(t, agents, dir)
	m = openReview(t, m)

	settled := threadsOnEveryLine(t)
	for _, thread := range settled {
		thread.ResolvedAt = "2026-09-13T12:00:00Z"
	}

	m.review.applyWrote(&m, m.shared(), reviewWroteMsg{agentID: "a1", threads: settled})

	next, _ := m.routeScreenMessage(keyMsg("u"))
	written, ok := next.(model)
	require.True(t, ok)

	assert.Contains(t, written.review.notice, "settled")
}

// threadsOnEveryLine pins one settled thread and one open one onto the first lines the diff
// draws, so a walk has something to step over.
func threadsOnEveryLine(t *testing.T) []*agentv1.ReviewThread {
	t.Helper()

	var threads []*agentv1.ReviewThread

	for i := range 2 {
		thread := &agentv1.ReviewThread{
			Id:          fmt.Sprintf("open-%d", i),
			AgentId:     "a1",
			FilePath:    "greet.go",
			NewStart:    int32(i + 1),
			NewEnd:      int32(i + 1),
			QuotedLines: []string{"+\treturn \"goodbye\""},
			Messages: []*agentv1.ReviewMessage{{
				Id:         fmt.Sprintf("m-%d", i),
				AuthorKind: agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_HUMAN,
				Body:       "worth another look",
			}},
		}

		if i == 0 {
			thread.ResolvedAt = "2026-09-13T12:00:00Z"
		}

		threads = append(threads, thread)
	}

	return threads
}

// TestTheOpenCommentKeySaysWhenYouAreOnTheLastOne covers the row every review ends on: the cursor
// standing on the only comment left, where a key that moved nowhere silently would read as a key
// that did not register.
func TestTheOpenCommentKeySaysWhenYouAreOnTheLastOne(t *testing.T) {
	dir := reviewRepo(t)
	agents := &fakeReviewAgents{}
	m, _ := reviewModel(t, agents, dir)
	m = openReview(t, m)

	m.review.applyWrote(&m, m.shared(), reviewWroteMsg{agentID: "a1", threads: threadsOnEveryLine(t)})

	next, _ := m.routeScreenMessage(keyMsg("u"))
	written, ok := next.(model)
	require.True(t, ok)
	require.Empty(t, written.review.notice, "the first press lands on the only open comment")

	next, _ = written.routeScreenMessage(keyMsg("u"))
	written, ok = next.(model)
	require.True(t, ok)

	assert.Contains(t, written.review.notice, "only comment left open")
}

// TestTheOpenCommentKeyDoesNotClaimAFoldedReviewIsSettled covers folding a file you have read,
// which is the ordinary way through a review: the thread is no longer pinned to any row, and the
// legend beside this key is still counting it.
func TestTheOpenCommentKeyDoesNotClaimAFoldedReviewIsSettled(t *testing.T) {
	dir := reviewRepo(t)
	agents := &fakeReviewAgents{}
	m, _ := reviewModel(t, agents, dir)
	m = openReview(t, m)

	m.review.applyWrote(&m, m.shared(), reviewWroteMsg{agentID: "a1", threads: threadsOnEveryLine(t)})

	next, _ := m.routeScreenMessage(keyMsg("v"))
	written, ok := next.(model)
	require.True(t, ok)
	require.Positive(t, written.review.diff.view.CountOpenThreads(), "the folded file's comment is still open")

	next, _ = written.routeScreenMessage(keyMsg("u"))
	written, ok = next.(model)
	require.True(t, ok)

	assert.Contains(t, written.review.notice, "folded away")
	assert.NotContains(t, written.review.notice, "settled")
}

// TestTheOpenCommentKeySaysBothWhenOneIsFoldedAway pins which of the two the key says.
func TestTheOpenCommentKeySaysBothWhenOneIsFoldedAway(t *testing.T) {
	repo := newFakeRepo(t.TempDir())
	agents := &fakeReviewAgents{}
	m, _ := reviewModelOver(t, agents, repo)

	repo.wrote(wroteParams{
		Tree:  "tree-with-a-second-file",
		Whole: fakePatchOfBothFiles,
		Since: fakePatchOfTheSecondFile,
		Sources: map[string]string{
			fakeFile:    fakeHeadSource,
			"second.go": "package greet\n\nfunc Second() {}\n",
		},
	})

	m = openReview(t, m)
	require.Equal(t, 2, m.review.diff.view.Stat().Files, "the diff draws both files")

	m.review.applyWrote(&m, m.shared(), reviewWroteMsg{agentID: "a1", threads: openThreadInEachFile(t)})
	require.Equal(t, 2, m.review.diff.view.CountOpenThreads())

	// Fold the file the cursor is standing in, which leaves one open comment drawn and one not.
	m.review.diff.cursor = firstRowInFile(t, m, "second.go")

	next, _ := m.routeScreenMessage(keyMsg("v"))
	written, ok := next.(model)
	require.True(t, ok)

	next, _ = written.routeScreenMessage(keyMsg("u"))
	written, ok = next.(model)
	require.True(t, ok)

	next, _ = written.routeScreenMessage(keyMsg("u"))
	written, ok = next.(model)
	require.True(t, ok)

	assert.Contains(t, written.review.notice, "folded away",
		"the comment nobody can reach is named rather than counted in silence")
	assert.NotContains(t, written.review.notice, "only comment",
		"since it is not the only one left open, whatever the walk can reach")
}

// openThreadInEachFile pins one unsettled comment into each of the two files.
func openThreadInEachFile(t *testing.T) []*agentv1.ReviewThread {
	t.Helper()

	var threads []*agentv1.ReviewThread

	for i, path := range []string{fakeFile, "second.go"} {
		threads = append(threads, &agentv1.ReviewThread{
			Id:          fmt.Sprintf("open-%d", i),
			AgentId:     "a1",
			FilePath:    path,
			NewStart:    1,
			NewEnd:      1,
			QuotedLines: []string{"+package greet"},
			Messages: []*agentv1.ReviewMessage{{
				Id:         fmt.Sprintf("m-%d", i),
				AuthorKind: agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_HUMAN,
				Body:       "worth another look",
			}},
		})
	}

	return threads
}

// firstRowInFile is a row the named file draws, so a fold lands on that file rather than another.
func firstRowInFile(t *testing.T, m model, path string) int {
	t.Helper()

	for row := range m.review.diff.view.Len() {
		if at, _, ok := m.review.diff.view.GetEditTarget(row); ok && at == path {
			return row
		}
	}

	t.Fatalf("the diff draws no row of %q", path)

	return 0
}

// TestEditingIsRefusedWhileYourReviewIsOwedAnAnswer pins the second of the two gates: an agent
// sitting on comments nobody has settled will answer them in the tree you would be editing.
func TestEditingIsRefusedWhileYourReviewIsOwedAnAnswer(t *testing.T) {
	dir := reviewRepo(t)
	agents := &fakeReviewAgents{}
	m, _ := reviewModel(t, agents, dir)
	m = openReview(t, m)

	m.review.diff.cursor = reviewRowWithAnEditTarget(t, m)
	m.review.threads.owed = "r1"

	next, _ := m.routeScreenMessage(editorKey())
	written, ok := next.(model)
	require.True(t, ok)

	assert.Equal(t, modeWatching, written.review.pass.mode, "nothing opened")
	assert.False(t, written.review.pass.atEditor)
	assert.Contains(t, written.review.notice, "owed an answer")
	assert.Contains(t, written.review.notice, keyLabel(m.keys.Review.Resolve), "and it names the key that settles one")
}

// TestEditingADormantAgentIsAllowedWhileOwedAnAnswer pins the escape the service has and this gate
// needs more.
func TestEditingADormantAgentIsAllowedWhileOwedAnAnswer(t *testing.T) {
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
	m.review.threads.owed = "r1"

	m = pressEdit(t, m)

	assert.Equal(t, modeReviewing, m.review.pass.mode, "an agent that will never answer is not a wait")
}

// TestEditingIsAllowedOnceNothingIsOwed is the other half.
func TestEditingIsAllowedOnceNothingIsOwed(t *testing.T) {
	dir := reviewRepo(t)
	agents := &fakeReviewAgents{}
	m, _ := reviewModel(t, agents, dir)
	m = openReview(t, m)

	m.review.diff.cursor = reviewRowWithAnEditTarget(t, m)
	m.review.threads.owed = ""

	m = pressEdit(t, m)

	assert.Equal(t, modeReviewing, m.review.pass.mode)
}
