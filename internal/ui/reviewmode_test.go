package ui

import (
	"errors"
	"testing"

	agentv1 "weave/proto/gen/agent/v1"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// chapterRounds is the shape the chapter tests share: rounds one to three, all off one base.
func chapterRounds() []*agentv1.ReviewRound {
	return []*agentv1.ReviewRound{
		{Id: "r1", Ordinal: 1, BaseSha: "b", SnapshotSha: "s1", CreatedAt: "2026-09-08T13:00:00-04:00"},
		{Id: "r2", Ordinal: 2, BaseSha: "b", SnapshotSha: "s2", CreatedAt: "2026-09-08T14:00:00-04:00"},
		{Id: "r3", Ordinal: 3, BaseSha: "b", SnapshotSha: "s3", CreatedAt: "2026-09-08T15:00:00-04:00"},
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

// TestReloadWithNoChaptersLetsTheReaderOut covers the two round-listing calls failing: the walk
// goes with them, so an index left pointing into the old list strands the reader on a narrowed
// diff with no key back.
func TestReloadWithNoChaptersLetsTheReaderOut(t *testing.T) {
	dir := reviewRepo(t)
	agents := &fakeReviewAgents{}
	m := reviewWithAnEarlierReview(t, dir, agents)
	m = pressReviewBase(t, m)
	require.NotEmpty(t, m.review.base)
	require.GreaterOrEqual(t, m.review.chapter, 0)

	agents.roundsErr = errors.New("the server went away")
	m = reloadReviewFully(t, m)

	assert.Negative(t, m.review.chapter, "the index does not outlive the list")
	assert.Empty(t, m.review.base, "and the diff is back to the whole change")
	assert.Empty(t, m.review.head)
	assert.Contains(t, m.review.notice, "back to the whole change")
}

// TestTheWidenedDiffIsTheOneOnScreen covers the widen the lost walk forces: the model saying the
// whole change over a patch still measured from a round files a comment against a base its own
// line numbers were never read from.
func TestTheWidenedDiffIsTheOneOnScreen(t *testing.T) {
	dir := reviewRepo(t)
	agents := &fakeReviewAgents{}
	m := reviewWithAnEarlierReview(t, dir, agents)
	m = pressReviewBase(t, m)
	require.Equal(t, 1, m.review.view.Stat().Files, "the narrowed diff holds the second file only")

	agents.roundsErr = errors.New("the server went away")
	m = reloadReviewFully(t, m)

	require.Empty(t, m.review.chapters)
	assert.Equal(t, 2, m.review.view.Stat().Files, "the diff on screen widened with the model")

	m = writeReviewComment(t, m, "about this line")

	require.NotNil(t, agents.created)
	assert.Equal(t, m.review.base, agents.created.GetBaseSha(),
		"the base the thread records is the one the diff on screen was measured from")
}

// TestTheWidenIsNotWritableWhileItsDiffIsInFlight covers the gap between the widen and the diff
// it sends for, where the model saying the whole change over the narrowed patch would file a
// comment against a base its numbers were never read from.
func TestTheWidenIsNotWritableWhileItsDiffIsInFlight(t *testing.T) {
	dir := reviewRepo(t)
	agents := &fakeReviewAgents{}
	m := reviewWithAnEarlierReview(t, dir, agents)
	m = pressReviewBase(t, m)
	require.Equal(t, 1, m.review.view.Stat().Files)

	agents.roundsErr = errors.New("the server went away")

	next, cmd := m.reloadReview()
	m = next
	require.NotNil(t, cmd)

	loaded, ok := cmd().(reviewLoadedMsg)
	require.True(t, ok)
	require.NoError(t, loaded.err)
	require.NotNil(t, m.applyReviewLoaded(loaded), "the widen sends for its diff")

	require.Empty(t, m.review.base, "the model widened")
	assert.Nil(t, m.review.view, "so the narrowed patch goes with it")
	assert.True(t, m.review.loading)

	after, _ := m.updateReview(keyMsg("c"))
	assert.False(t, after.(model).review.composing, "and nothing can be written until the diff lands")
}

// reloadReviewFully presses the reload key and follows whatever the load answers with, including
// the second load a widened reader is sent back for.
func reloadReviewFully(t *testing.T, m model) model {
	t.Helper()

	next, cmd := m.reloadReview()
	m = next
	require.NotNil(t, cmd)

	loaded, ok := cmd().(reviewLoadedMsg)
	require.True(t, ok)
	require.NoError(t, loaded.err)

	again := m.applyReviewLoaded(loaded)
	if again == nil {
		return m
	}

	switch answer := again().(type) {
	case reviewLoadedMsg:
		require.NoError(t, answer.err)
		m.applyReviewLoaded(answer)
	case roundLeftoverMsg:
		m.applyRoundLeftover(answer)
	}

	return m
}

// writeReviewComment opens the composer on the row the cursor is standing on and sends what is
// typed into it.
func writeReviewComment(t *testing.T, m model, body string) model {
	t.Helper()

	next, _ := m.updateReview(keyMsg("c"))
	m = next.(model)
	require.True(t, m.review.composing)
	m.review.editor.SetValue(body)

	next, cmd := m.updateReview(tea.KeyMsg{Type: tea.KeyCtrlS})
	m = next.(model)
	require.NotNil(t, cmd)

	wrote, ok := cmd().(reviewWroteMsg)
	require.True(t, ok)
	require.NoError(t, wrote.err)

	return m
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
	require.True(t, m.review.writing, "the compose key opens the mode")

	// The composer holds the keyboard while it is open, so it is put away before the verdict key
	// is pressed.
	next, _ = m.updateReview(keyMsg("esc"))
	m = next.(model)
	require.False(t, m.review.composing)
	require.True(t, m.review.writing, "and the mode outlives one abandoned comment")

	next, _ = m.updateReview(tea.KeyMsg{Type: tea.KeyCtrlS})
	m = next.(model)
	require.True(t, m.review.releasing, "the verdict panel is open")

	next, cmd := m.updateReview(tea.KeyMsg{Type: tea.KeyCtrlS})
	m = next.(model)
	require.NotNil(t, cmd, "and the same key sends it")

	assert.False(t, m.review.writing, "the mode goes with the review it was for")
}

// TestTheVerdictIsRefusedOnAFinishedRound covers rule three reaching the third write key: the
// release panel opened on a round the agent had moved past, and the review went, while the line
// above it said nothing could be written there.
func TestTheVerdictIsRefusedOnAFinishedRound(t *testing.T) {
	dir := reviewRepo(t)
	agents := &fakeReviewAgents{}
	m, _ := reviewModel(t, agents, dir)
	m = openReview(t, m)

	m.review.chapters = []reviewChapter{
		{ToSHA: "an-older-snapshot", Ordinal: 1},
		{ToSHA: m.review.round.GetSnapshotSha(), Ordinal: 2},
	}
	m.review.chapter = 0
	m.review.head = "an-older-snapshot"

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

	assert.False(t, m.review.writing, "the mode did not open")
	assert.False(t, m.review.composing, "and no editor did either")
	assert.Contains(t, m.review.notice, "answered")
	assert.Nil(t, agents.created, "and nothing was written")
}

// TestAFailedEditLeavesTheModeAlone covers the edit key's early returns: it opens the writing mode
// on the way in, so a press that opens no editor used to switch the mode anyway and take the round
// keys away with it.
func TestAFailedEditLeavesTheModeAlone(t *testing.T) {
	dir := reviewRepo(t)
	agents := &fakeReviewAgents{}
	m, _ := reviewModel(t, agents, dir)
	m = openReview(t, m)

	// A checkout already being made is the deterministic refusal: a second one would reap the
	// directory the first is writing into, so the key returns having opened nothing.
	m.review.materialising = true
	require.NotEqual(t, m.review.roundSha, m.review.round.GetSnapshotSha())
	require.False(t, m.review.writing)

	next, cmd := m.updateReview(keyMsg("e"))
	m = next.(model)

	require.Nil(t, cmd, "the key opened nothing")
	assert.False(t, m.review.writing, "so it did not change the mode either")
}

// TestAnEditThatTakesOpensTheMode is the other side of it, since a write that reaches the editor
// is a review being started.
func TestAnEditThatTakesOpensTheMode(t *testing.T) {
	dir := reviewRepo(t)
	agents := &fakeReviewAgents{}
	m, _ := reviewModel(t, agents, dir)
	m = openReview(t, m)

	line := -1
	for row := 0; row < m.review.view.Len(); row++ {
		if _, _, ok := m.review.view.GetEditTarget(row); ok {
			line = row

			break
		}
	}

	require.GreaterOrEqual(t, line, 0, "the diff draws a line the editor can open")
	m.review.cursor = line

	next, _ := m.updateReview(keyMsg("e"))
	m = next.(model)

	assert.True(t, m.review.writing, "the editor opening is a review being written")
}

// TestTheForwardRoundKeyIsOfferedOnlyWhereItAnswers covers the two halves of one walk disagreeing:
// the key was drawn in the writing mode, where its own action refuses to run.
func TestTheForwardRoundKeyIsOfferedOnlyWhereItAnswers(t *testing.T) {
	m := model{}
	m.review.chapters = []reviewChapter{{ToSHA: "s1", Ordinal: 1}, {ToSHA: "s2", Ordinal: 2}}
	m.review.chapter = 0
	m.review.writing = true

	for _, row := range reviewLegend() {
		if len(row.of) == 1 && row.of[0] == actionReviewBaseForward {
			assert.False(t, row.show(m), "the forward key is not offered where it answers nothing")

			return
		}
	}

	t.Fatal("the forward round key has no legend row")
}

// TestTheBackKeySaysWhichThingItLeaves covers the key having two jobs since the mode split: in
// writing it puts the review down, and the legend claimed it left the screen.
func TestTheBackKeySaysWhichThingItLeaves(t *testing.T) {
	m := model{}
	m.review.writing = true

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
