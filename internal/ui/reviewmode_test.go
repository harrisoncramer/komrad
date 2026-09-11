package ui

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"weave/internal/diff"
	"weave/internal/review"
	agentstore "weave/internal/store/agent"
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

// TestReviewIsHistoryReadsTheDiffOnScreen covers the chapter list being recut under a stationary
// index: what decides whether a comment can be written is the snapshot the diff was drawn to, not
// what the chapter at that index happens to say now.
func TestReviewIsHistoryReadsTheDiffOnScreen(t *testing.T) {
	m := model{}
	m.review.round = &agentv1.ReviewRound{Id: "r3", SnapshotSha: "s3"}
	m.review.chapters = []reviewChapter{{ToSHA: "s3", Ordinal: 3}}
	m.review.chapter = 0
	m.review.head = "s2"

	said, history := m.reviewIsHistory()
	assert.True(t, history, "the diff was drawn to an older snapshot")
	assert.NotEmpty(t, said, "and the refusal has something to name")
}

// TestReloadWithNoChaptersLetsTheReaderOut covers the round listing failing under a reader
// standing on a round: the walk goes with it, and the diff that arrived was measured for the
// round they were on, so drawing it under the whole change's label reads as current work.
func TestReloadWithNoChaptersLetsTheReaderOut(t *testing.T) {
	dir := reviewRepo(t)
	agents := &fakeReviewAgents{}
	m := reviewWithAnEarlierReview(t, dir, agents)
	m = pressReviewBase(t, m)
	require.NotEmpty(t, m.review.base)
	require.GreaterOrEqual(t, m.review.chapter, 0)

	narrowed := m.review.view

	_, reload := m.reloadReview()
	require.NotNil(t, reload)

	loaded, ok := reload().(reviewLoadedMsg)
	require.True(t, ok)
	loaded.chapters = nil

	again := m.applyReviewLoaded(loaded)

	assert.Negative(t, m.review.chapter, "the index does not outlive the list")
	assert.Empty(t, m.review.base, "and the diff is back to the whole change")
	assert.Empty(t, m.review.head)
	assert.Nil(t, m.review.view, "the diff measured for the old round is not drawn")
	assert.NotSame(t, narrowed, m.review.view)
	assert.True(t, m.review.loading, "and the screen says so until the whole change lands")

	require.NotNil(t, again, "the whole change is asked for rather than assumed")

	answer, ok := again().(reviewLoadedMsg)
	require.True(t, ok)
	assert.Equal(t, m.review.round.GetSnapshotSha(), answer.patch.SnapshotSHA,
		"and what comes back is the work as it stands")
}

// TestReviewIsHistoryReadsTheSnapshotDrawn covers the base and head being cleared under a diff
// that is still an older round: what a comment would anchor to is the code on screen, so the
// refusal follows the patch rather than what was asked for.
func TestReviewIsHistoryReadsTheSnapshotDrawn(t *testing.T) {
	m := model{}
	m.review.round = &agentv1.ReviewRound{Id: "r3", SnapshotSha: "s3"}
	m.review.view = review.NewView(&diff.Patch{BaseSHA: "s1", SnapshotSHA: "s2"}, false)

	said, history := m.reviewIsHistory()
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

	next, _ := m.updateReview(keyMsg("c"))
	m = next.(model)
	require.Equal(t, modeReviewing, m.review.mode, "the compose key opens the mode")

	// The composer holds the keyboard while it is open, so it is put away before the verdict key
	// is pressed.
	next, _ = m.updateReview(keyMsg("esc"))
	m = next.(model)
	require.False(t, m.review.composing)
	require.Equal(t, modeReviewing, m.review.mode, "and the mode outlives one abandoned comment")

	next, _ = m.updateReview(tea.KeyMsg{Type: tea.KeyCtrlS})
	m = next.(model)
	require.True(t, m.review.releasing, "the verdict panel is open")

	next, cmd := m.updateReview(tea.KeyMsg{Type: tea.KeyCtrlS})
	m = next.(model)
	require.NotNil(t, cmd, "and the same key sends it")

	assert.Equal(t, modeWatching, m.review.mode, "the mode goes with the review it was for")
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

	next, _ := m.updateReview(tea.KeyMsg{Type: tea.KeyCtrlS})
	m = next.(model)

	assert.False(t, m.review.releasing, "the verdict panel stays shut")
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

	m.review.awaiting = true

	next, _ := m.updateReview(keyMsg("c"))
	m = next.(model)

	assert.Equal(t, modeWatching, m.review.mode, "the mode did not open")
	assert.False(t, m.review.composing, "and no editor did either")
	assert.Contains(t, m.review.notice, "answered")
	assert.Nil(t, agents.created, "and nothing was written")
}

// reviewRepoWithADeletion is a worktree whose round both changes a file and removes one, since a
// removed file is the row the editor key has nothing to open.
func reviewRepoWithADeletion(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()

	gitIn(t, dir, "init", "-b", "main")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "greet.go"),
		[]byte("package greet\n\nfunc Hello() string {\n\treturn \"hello\"\n}\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "gone.go"),
		[]byte("package greet\n\nfunc Gone() {}\n"), 0o644))
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-m", "first")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "greet.go"),
		[]byte("package greet\n\nfunc Hello() string {\n\treturn \"goodbye\"\n}\n"), 0o644))
	require.NoError(t, os.Remove(filepath.Join(dir, "gone.go")))

	return dir
}

// reviewRowInDeletedFile is a row of the file the round removed.
func reviewRowInDeletedFile(t *testing.T, m model) int {
	t.Helper()

	for row := range m.review.view.Len() {
		if _, _, ok := m.review.view.GetEditTarget(row); ok {
			continue
		}

		if file, ok := m.review.view.GetFileAt(row); ok && file.OldPath == "gone.go" {
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

	next, _ := m.updateReview(editorKey())
	written, ok := next.(model)
	require.True(t, ok)
	require.Empty(t, written.review.notice, "the press opened the editor")

	return written
}

// reviewRowWithAnEditTarget is a row the editor key has something to open.
func reviewRowWithAnEditTarget(t *testing.T, m model) int {
	t.Helper()

	for row := range m.review.view.Len() {
		if _, _, ok := m.review.view.GetEditTarget(row); ok {
			return row
		}
	}

	t.Fatal("the diff draws no row the editor can open")

	return -1
}

// TestAnEditRefusedOnADeletedFileOpensNoMode covers a press on a row with no file behind it,
// which used to switch the screen's mode on the strength of an editor it never opened.
func TestAnEditRefusedOnADeletedFileOpensNoMode(t *testing.T) {
	dir := reviewRepoWithADeletion(t)
	agents := &fakeReviewAgents{}
	m, _ := reviewModel(t, agents, dir)
	m = openReview(t, m)

	m.review.cursor = reviewRowInDeletedFile(t, m)

	next, _ := m.updateReview(editorKey())
	m = next.(model)

	assert.Equal(t, modeWatching, m.review.mode, "a press that opened nothing does not open a mode")
	assert.Contains(t, m.review.notice, "deleted")
}

// TestAnEditThatTakesOpensTheEditingMode pins that editing is its own mode rather than a review
// being written, since what it writes goes into the worktree rather than into a comment.
func TestAnEditThatTakesOpensTheEditingMode(t *testing.T) {
	dir := reviewRepo(t)
	agents := &fakeReviewAgents{}
	m, _ := reviewModel(t, agents, dir)
	m = openReview(t, m)

	m.review.cursor = reviewRowWithAnEditTarget(t, m)

	m = pressEdit(t, m)

	assert.Equal(t, modeEditing, m.review.mode)
	assert.True(t, m.review.atEditor, "the editor holds the stage")
	assert.NotEmpty(t, m.review.editingPath, "and the band can name what it opened")
}

// TestAnEditIsRefusedWhileTheAgentsTurnRuns pins the one thing standing between you and the
// worktree: two writers in one checkout.
func TestAnEditIsRefusedWhileTheAgentsTurnRuns(t *testing.T) {
	dir := reviewRepo(t)
	agents := &fakeReviewAgents{}
	m, _ := reviewModel(t, agents, dir)
	m = openReview(t, m)

	for i, item := range m.listing.agents {
		if item.agent.ID == m.review.agentID {
			m.listing.agents[i].agent.State = agentstore.StateThinking
		}
	}

	m.review.cursor = reviewRowWithAnEditTarget(t, m)

	next, _ := m.updateReview(editorKey())
	m = next.(model)

	assert.Equal(t, modeWatching, m.review.mode, "nothing opened")
	assert.False(t, m.review.atEditor)
	assert.Contains(t, m.review.notice, "stop it before you edit")
}

// TestTheEditorClosingGivesTheStageBack covers the editor being another process: it can be quit
// from inside, and the screen has to notice rather than keep drawing a mode nothing is in.
func TestTheEditorClosingGivesTheStageBack(t *testing.T) {
	dir := reviewRepo(t)
	agents := &fakeReviewAgents{}
	m, tmux := reviewModel(t, agents, dir)
	m = openReview(t, m)

	m.review.cursor = reviewRowWithAnEditTarget(t, m)
	m = pressEdit(t, m)
	require.Equal(t, modeEditing, m.review.mode)
	require.NotEmpty(t, m.review.editorPane, "the editor was given a pane")

	tmux.deadPanes = map[string]bool{m.review.editorPane: true}

	m, cmd := m.reconcileEditor()

	assert.Equal(t, modeWatching, m.review.mode, "the mode goes with the pane")
	assert.False(t, m.review.atEditor)
	assert.NotNil(t, cmd, "and the diff is read again, since the editor may have written to it")
}

// TestLeavingTheReviewClosesTheEditorFirst pins the back key against the editor outliving the
// screen it was opened from.
func TestLeavingTheReviewClosesTheEditorFirst(t *testing.T) {
	dir := reviewRepo(t)
	agents := &fakeReviewAgents{}
	m, _ := reviewModel(t, agents, dir)
	m = openReview(t, m)

	m.review.cursor = reviewRowWithAnEditTarget(t, m)
	m = pressEdit(t, m)
	require.Equal(t, modeEditing, m.review.mode)

	next, _ := m.leaveReview()
	m = next.(model)

	assert.Equal(t, screenReview, m.screen, "the first press gives up the editor rather than the screen")
	assert.Equal(t, modeWatching, m.review.mode)
	assert.False(t, m.review.atEditor)
}

// TestWatchingHoldsTheDiffWhileTheAgentWrites is what keeps watching from costing history: a
// catch-up reads a round, so following every save would leave one behind every few seconds.
func TestWatchingHoldsTheDiffWhileTheAgentWrites(t *testing.T) {
	dir := reviewRepo(t)
	agents := &fakeReviewAgents{}
	m, _ := reviewModel(t, agents, dir)
	m = openReview(t, m)

	setReviewAgentState(&m, agentstore.StateThinking)
	assert.False(t, m.doesReviewFollowLive(), "a diff is not redrawn under a writer")

	cmd := m.applyReviewDiffPolled(reviewDiffPolledMsg{agentID: m.review.agentID, moved: true})
	assert.Nil(t, cmd, "so nothing is read again while it writes")
	assert.Contains(t, m.reviewStaleSaid(), "catches up when it stops")

	setReviewAgentState(&m, agentstore.StateIdle)
	require.True(t, m.doesReviewFollowLive(), "the end of a turn is where it catches up")

	cmd = m.applyReviewDiffPolled(reviewDiffPolledMsg{agentID: m.review.agentID, moved: true})
	assert.NotNil(t, cmd, "and that is one read rather than one every poll")
}

// TestAWaitingAgentDoesNotHoldTheEditKey pins the gate against the half of an unfinished turn
// that writes nothing: an agent at a prompt is stopped until somebody answers it.
func TestAWaitingAgentDoesNotHoldTheEditKey(t *testing.T) {
	dir := reviewRepo(t)
	agents := &fakeReviewAgents{}
	m, _ := reviewModel(t, agents, dir)
	m = openReview(t, m)

	setReviewAgentState(&m, agentstore.StateWaiting)
	m.review.cursor = reviewRowWithAnEditTarget(t, m)

	m = pressEdit(t, m)

	assert.Equal(t, modeEditing, m.review.mode)
}

// TestAnUnreadablePaneRefusesTheEditKey pins the state that says weave could not read the pane
// rather than that the agent is idle, which is the one place the gate cannot claim the tree is
// free.
func TestAnUnreadablePaneRefusesTheEditKey(t *testing.T) {
	dir := reviewRepo(t)
	agents := &fakeReviewAgents{}
	m, _ := reviewModel(t, agents, dir)
	m = openReview(t, m)

	setReviewAgentState(&m, agentstore.StateUncertain)
	m.review.cursor = reviewRowWithAnEditTarget(t, m)

	next, _ := m.updateReview(editorKey())
	m = next.(model)

	assert.Equal(t, modeWatching, m.review.mode, "nothing opened")
	assert.Contains(t, m.review.notice, "cannot read this agent's pane")

	// The catch-up is the other reader of the agent's state, and an unreadable pane is not a
	// reason to hold the diff forever.
	assert.True(t, m.doesReviewFollowLive(), "the diff still catches up")
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

	m.review.cursor = reviewRowWithAnEditTarget(t, m)
	m = pressEdit(t, m)

	assert.False(t, spec.holds(m), "the editor has the window now")
}

// setReviewAgentState puts the agent whose diff is on screen into one state.
func setReviewAgentState(m *model, state agentstore.State) {
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
	m.review.chapters = []reviewChapter{{ToSHA: "s1", Ordinal: 1}, {ToSHA: "s2", Ordinal: 2}}
	m.review.chapter = 0
	m.review.mode = modeReviewing

	for _, row := range reviewLegend() {
		if len(row.of) == 1 && row.of[0] == actionReviewBaseForward {
			assert.False(t, row.show(m), "the forward key is not offered where it answers nothing")

			return
		}
	}

	t.Fatal("the forward round key has no legend row")
}

// reviewRowWithAnAnchor is a row of the diff a comment can hang on.
func reviewRowWithAnAnchor(t *testing.T, m model) int {
	t.Helper()

	for row := range m.review.view.Len() {
		if _, ok := m.review.view.GetAnchor(row); ok {
			return row
		}
	}

	t.Fatal("the diff draws no line a comment can hang on")

	return -1
}

// reviewAnchorInTheDiff is a line of the diff a comment can hang on.
func reviewAnchorInTheDiff(t *testing.T, m model) review.Anchor {
	t.Helper()

	anchor, ok := m.review.view.GetAnchor(reviewRowWithAnAnchor(t, m))
	require.True(t, ok)

	return *anchor
}

// TestAComposerDoesNotOutliveTheDiffItWasWrittenOn covers a comment being typed while the load
// that answers with a short walk lands: the diff it was anchored to is taken away, and the submit
// key read it anyway.
func TestAComposerDoesNotOutliveTheDiffItWasWrittenOn(t *testing.T) {
	dir := reviewRepo(t)
	agents := &fakeReviewAgents{}
	m := reviewWithAnEarlierReview(t, dir, agents)
	m = pressReviewBase(t, m)
	require.GreaterOrEqual(t, m.review.chapter, 0)

	m.review.cursor = reviewRowWithAnAnchor(t, m)

	next, _ := m.updateReview(keyMsg("c"))
	m = next.(model)
	require.True(t, m.review.composing, "the composer is open on a line of the round")

	m.review.editor.SetValue("this should say hello")

	_, reload := m.reloadReview()
	require.NotNil(t, reload)

	loaded, ok := reload().(reviewLoadedMsg)
	require.True(t, ok)
	loaded.chapters = nil

	m.applyReviewLoaded(loaded)
	require.Nil(t, m.review.view)

	assert.False(t, m.review.composing, "the composer goes with the diff it was anchored to")

	next, _ = m.updateReview(tea.KeyMsg{Type: tea.KeyCtrlS})
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

	drawnBase := m.review.view.GetBaseSHA()
	drawnSnapshot := m.review.view.GetSnapshotSHA()
	require.NotEmpty(t, drawnBase)
	require.NotEmpty(t, drawnSnapshot)

	m.review.base = "some-other-commit"
	m.review.head = ""

	anchor := reviewAnchorInTheDiff(t, m)

	cmd := m.createReviewThreadCmd(anchor, "this should say hello")
	require.NotNil(t, cmd)

	wrote, ok := cmd().(reviewWroteMsg)
	require.True(t, ok)
	require.NoError(t, wrote.err)

	require.NotNil(t, agents.created)
	assert.Equal(t, drawnBase, agents.created.GetBaseSha())
	assert.Equal(t, drawnSnapshot, agents.created.GetSnapshotSha())
}

// TestTheSendKeyIsOfferedOnlyWhereItAnswers covers the legend and the key disagreeing: the key
// refuses on a finished round and while a review is out, and the row offered a verdict in both.
func TestTheSendKeyIsOfferedOnlyWhereItAnswers(t *testing.T) {
	dir := reviewRepo(t)
	m, _ := reviewModel(t, &fakeReviewAgents{}, dir)
	m = openReview(t, m)

	row := reviewLegendRowFor(t, actionReviewSend)

	assert.True(t, row.show(m), "reading the current work is where a verdict answers")

	out := m
	out.review.awaiting = true
	assert.False(t, row.show(out), "a review already out is answered rather than added to")

	assert.False(t, row.show(standOnAFinishedRound(m)), "and a finished round holds nothing to send")
}

// reviewLegendRowFor is the legend row one key is drawn on.
func reviewLegendRowFor(t *testing.T, id actionID) legendRow {
	t.Helper()

	for _, row := range reviewLegend() {
		if len(row.of) == 1 && row.of[0] == id {
			return row
		}
	}

	t.Fatal("that key has no legend row")

	return legendRow{}
}

// TestTheBackKeySaysWhichThingItLeaves covers the key having two jobs since the mode split: in
// writing it puts the review down, and the legend claimed it left the screen.
func TestTheBackKeySaysWhichThingItLeaves(t *testing.T) {
	m := model{}
	m.review.mode = modeReviewing

	for _, row := range reviewLegend() {
		if len(row.of) == 1 && row.of[0] == actionReviewBack {
			assert.NotContains(t, row.desc(m), "the agents",
				"in writing the key leaves the mode rather than the screen")

			return
		}
	}

	t.Fatal("the back key has no legend row")
}

// TestComposingOnARowWithNoLineSaysSo covers the press that does nothing: a cursor on a heading
// has no line to hang a comment on, and silence there reads as the key being broken.
func TestComposingOnARowWithNoLineSaysSo(t *testing.T) {
	dir := reviewRepo(t)
	agents := &fakeReviewAgents{}
	m, _ := reviewModel(t, agents, dir)
	m = openReview(t, m)

	blank := -1
	for row := 0; row < m.review.view.Len(); row++ {
		if _, ok := m.review.view.GetAnchor(row); !ok {
			blank = row

			break
		}
	}

	require.GreaterOrEqual(t, blank, 0, "the diff draws a row with no line of its own")
	m.review.cursor = blank

	next, _ := m.updateReview(keyMsg("c"))
	m = next.(model)

	assert.False(t, m.review.composing, "no editor opened")
	assert.NotEmpty(t, m.review.notice, "and the key said why")
}

// TestACommentOnTheNewestRoundRecordsItsBase covers what a narrowed write is measured from, which
// the mode split left with no test of its own: the newest round is narrowed on its base and
// writing there is allowed, so the base it was counted from still has to reach the service.
func TestACommentOnTheNewestRoundRecordsItsBase(t *testing.T) {
	dir := reviewRepo(t)
	agents := &fakeReviewAgents{}

	m := reviewWithAnEarlierReview(t, dir, agents)
	m = pressReviewBase(t, m)
	require.NotEmpty(t, m.review.base, "the diff is measured from the round reviewed before")

	for row := 0; row < m.review.view.Len(); row++ {
		if found, ok := m.review.view.GetAnchor(row); ok && found.NewStart > 0 {
			m.review.cursor = row

			break
		}
	}

	next, _ := m.updateReview(keyMsg("c"))
	m = next.(model)
	require.True(t, m.review.composing, "writing is allowed on the newest round")

	m.review.editor.SetValue("this line, in the work since I last looked")

	next, cmd := m.updateReviewComposer(keyMsg("ctrl+s"))
	m = next.(model)
	require.NotNil(t, cmd)
	cmd()

	require.NotNil(t, agents.created, "the thread reached the service")
	assert.Equal(t, m.review.base, agents.created.GetBaseSha(),
		"and says which commit its old side was counted from")
	assert.Equal(t, m.review.round.GetSnapshotSha(), agents.created.GetSnapshotSha(),
		"and which snapshot its new side came from")
	assert.Positive(t, agents.created.GetNewStart())
}
