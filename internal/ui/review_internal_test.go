package ui

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"
	"connectrpc.com/connect"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"weave/internal/diff"
	"weave/internal/gittest"
	"weave/internal/ui/diffview"
	"weave/internal/ui/paint"
	"weave/internal/ui/panel"
	agentv1 "weave/proto/gen/agent/v1"
	"weave/proto/gen/agent/v1/agentv1connect"
	watchv1 "weave/proto/gen/watch/v1"
)

type fakeReviewAgents struct {
	agentv1connect.AgentServiceClient
	threads    []*agentv1.ReviewThread
	viewed     []*agentv1.ReviewViewedFile
	settled    *agentv1.SetReviewThreadResolvedRequest
	created    *agentv1.CreateReviewThreadRequest
	replied    *agentv1.AddReviewMessageRequest
	released   *agentv1.ReleaseReviewRequest
	dir        string
	repo       *gittest.Repo
	rounds     []*agentv1.ReviewRound
	reviews    []*agentv1.Review
	marked     *agentv1.SetReviewFileViewedRequest
	reviewsErr error
	roundsErr  error
	// reviewReads and roundReads count what one load asked the server for.
	reviewReads int
	roundReads  int
	// held and let go are what the pass did to the agent, and holdErr is an agent that would not
	// stop.
	held    *agentv1.HoldAgentRequest
	letGo   *agentv1.ReleaseAgentRequest
	holdErr error
}

func (f *fakeReviewAgents) HoldAgent(
	_ context.Context,
	req *connect.Request[agentv1.HoldAgentRequest],
) (*connect.Response[agentv1.HoldAgentResponse], error) {
	if f.holdErr != nil {
		return nil, f.holdErr
	}

	f.held = req.Msg

	return connect.NewResponse(&agentv1.HoldAgentResponse{}), nil
}

func (f *fakeReviewAgents) ReleaseAgent(
	_ context.Context,
	req *connect.Request[agentv1.ReleaseAgentRequest],
) (*connect.Response[agentv1.ReleaseAgentResponse], error) {
	f.letGo = req.Msg

	return connect.NewResponse(&agentv1.ReleaseAgentResponse{}), nil
}

func (f *fakeReviewAgents) ListReviewViewedFiles(
	_ context.Context,
	req *connect.Request[agentv1.ListReviewViewedFilesRequest],
) (*connect.Response[agentv1.ListReviewViewedFilesResponse], error) {
	held := make([]*agentv1.ReviewViewedFile, 0, len(f.viewed))
	for _, mark := range f.viewed {
		if mark.GetAgentId() == req.Msg.GetAgentId() {
			held = append(held, mark)
		}
	}

	return connect.NewResponse(&agentv1.ListReviewViewedFilesResponse{Viewed: held}), nil
}

func (f *fakeReviewAgents) SetReviewFileViewed(
	_ context.Context,
	req *connect.Request[agentv1.SetReviewFileViewedRequest],
) (*connect.Response[agentv1.SetReviewFileViewedResponse], error) {
	f.marked = req.Msg

	kept := make([]*agentv1.ReviewViewedFile, 0, len(f.viewed))
	for _, mark := range f.viewed {
		if mark.GetFilePath() != req.Msg.GetFilePath() {
			kept = append(kept, mark)
		}
	}

	if req.Msg.GetViewed() {
		kept = append(kept, &agentv1.ReviewViewedFile{
			AgentId:     req.Msg.GetAgentId(),
			FilePath:    req.Msg.GetFilePath(),
			Fingerprint: req.Msg.GetFingerprint(),
		})
	}

	f.viewed = kept

	return connect.NewResponse(&agentv1.SetReviewFileViewedResponse{}), nil
}

func (f *fakeReviewAgents) ReleaseReview(
	_ context.Context,
	req *connect.Request[agentv1.ReleaseReviewRequest],
) (*connect.Response[agentv1.ReleaseReviewResponse], error) {
	f.released = req.Msg

	var sent int32

	for _, thread := range f.threads {
		for _, message := range thread.GetMessages() {
			if message.GetDelivery() != agentv1.ReviewDeliveryState_REVIEW_DELIVERY_STATE_UNSENT {
				continue
			}

			message.Delivery = agentv1.ReviewDeliveryState_REVIEW_DELIVERY_STATE_SENT
			sent++
		}
	}

	return connect.NewResponse(&agentv1.ReleaseReviewResponse{Comments: sent}), nil
}

// OpenReviewRound hands back the round the worktree stands at.
func (f *fakeReviewAgents) OpenReviewRound(
	_ context.Context,
	req *connect.Request[agentv1.OpenReviewRoundRequest],
) (*connect.Response[agentv1.OpenReviewRoundResponse], error) {
	sha, baseSHA := gittest.NameSnapshot(f.repo.Tree), gittest.BaseSHA

	if len(f.rounds) == 0 || f.rounds[len(f.rounds)-1].GetSnapshotSha() != sha {
		f.rounds = append(f.rounds, &agentv1.ReviewRound{
			Id:          fmt.Sprintf("round-%d", len(f.rounds)+1),
			AgentId:     req.Msg.GetAgentId(),
			Ordinal:     int32(len(f.rounds) + 1),
			BaseSha:     baseSHA,
			SnapshotSha: sha,
		})
	}

	return connect.NewResponse(&agentv1.OpenReviewRoundResponse{Round: f.rounds[len(f.rounds)-1]}), nil
}

func (f *fakeReviewAgents) ListReviewRounds(
	context.Context,
	*connect.Request[agentv1.ListReviewRoundsRequest],
) (*connect.Response[agentv1.ListReviewRoundsResponse], error) {
	f.roundReads++

	if f.roundsErr != nil {
		return nil, f.roundsErr
	}

	return connect.NewResponse(&agentv1.ListReviewRoundsResponse{Rounds: f.rounds}), nil
}

func (f *fakeReviewAgents) ListReviews(
	context.Context,
	*connect.Request[agentv1.ListReviewsRequest],
) (*connect.Response[agentv1.ListReviewsResponse], error) {
	f.reviewReads++

	if f.reviewsErr != nil {
		return nil, f.reviewsErr
	}

	return connect.NewResponse(&agentv1.ListReviewsResponse{Reviews: f.reviews}), nil
}

func (f *fakeReviewAgents) AddReviewMessage(
	_ context.Context,
	req *connect.Request[agentv1.AddReviewMessageRequest],
) (*connect.Response[agentv1.AddReviewMessageResponse], error) {
	f.replied = req.Msg

	message := &agentv1.ReviewMessage{
		Id:         "m2",
		ThreadId:   req.Msg.GetThreadId(),
		Body:       req.Msg.GetBody(),
		AuthorKind: agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_HUMAN,
		Delivery:   agentv1.ReviewDeliveryState_REVIEW_DELIVERY_STATE_UNSENT,
	}

	for _, thread := range f.threads {
		if thread.GetId() == req.Msg.GetThreadId() {
			thread.Messages = append(thread.Messages, message)
		}
	}

	return connect.NewResponse(&agentv1.AddReviewMessageResponse{Message: message}), nil
}

func (f *fakeReviewAgents) SetReviewThreadResolved(
	_ context.Context,
	req *connect.Request[agentv1.SetReviewThreadResolvedRequest],
) (*connect.Response[agentv1.SetReviewThreadResolvedResponse], error) {
	f.settled = req.Msg

	for _, thread := range f.threads {
		if thread.GetId() != req.Msg.GetId() {
			continue
		}

		thread.ResolvedAt = ""
		if req.Msg.GetResolved() {
			thread.ResolvedAt = "2026-08-27T12:00:00Z"
		}
	}

	f.countOpenThreads()

	return connect.NewResponse(&agentv1.SetReviewThreadResolvedResponse{}), nil
}

// countOpenThreads recounts the open threads of every review, the way ListReviews does.
func (f *fakeReviewAgents) countOpenThreads() {
	for _, review := range f.reviews {
		var open int32

		for _, thread := range f.threads {
			if thread.GetReviewId() == review.GetId() && thread.GetResolvedAt() == "" {
				open++
			}
		}

		review.OpenThreads = open
	}
}

func (f *fakeReviewAgents) ListReviewThreads(
	context.Context,
	*connect.Request[agentv1.ListReviewThreadsRequest],
) (*connect.Response[agentv1.ListReviewThreadsResponse], error) {
	return connect.NewResponse(&agentv1.ListReviewThreadsResponse{Threads: f.threads}), nil
}

func (f *fakeReviewAgents) CreateReviewThread(
	_ context.Context,
	req *connect.Request[agentv1.CreateReviewThreadRequest],
) (*connect.Response[agentv1.CreateReviewThreadResponse], error) {
	f.created = req.Msg

	return connect.NewResponse(&agentv1.CreateReviewThreadResponse{
		Thread: &agentv1.ReviewThread{Id: "t1", FilePath: req.Msg.GetFilePath()},
	}), nil
}

// reviewRepo is the directory a review is read from, holding no repository.
func reviewRepo(t *testing.T) string {
	t.Helper()

	return t.TempDir()
}

func reviewModel(t *testing.T, agents agentv1connect.AgentServiceClient, dir string) (model, *recordingTmux) {
	t.Helper()

	return buildReviewModel(t, agents, gittest.NewRepo(dir))
}

// buildReviewModel is a review screen over a repository the test holds on to, for the tests that
// move it between rounds.
func buildReviewModel(t *testing.T, agents agentv1connect.AgentServiceClient, repo *gittest.Repo) (model, *recordingTmux) {
	t.Helper()

	dir := repo.Root

	tmux := &recordingTmux{width: 200, height: 30}
	if fake, ok := agents.(*fakeReviewAgents); ok {
		fake.dir = dir
		fake.repo = repo
	}

	m := infoModel(tmux)
	m.api = api{agents: agents}
	m.git = repo
	// The review holds the whole window, so this is the size the rest of the panel's screens are
	// measured at.
	m.width, m.height = 200, 30
	m.listing = listing{
		workflows: []panel.Workflow{
			{
				ID:       "w1",
				Name:     "backend",
				Worktree: panel.WorktreeConfig{Enabled: true, BaseBranch: "main"},
			},
		},
		agents: []panel.AgentRow{{Agent: reviewAgent(dir), Alive: true}},
	}

	return m, tmux
}

func reviewAgent(dir string) panel.Agent {
	return panel.Agent{
		ID:         "a1",
		Name:       "auth",
		Cwd:        dir,
		WorkflowID: "w1",
		Pane:       "%9",
	}
}

// openReview walks the whole path a keypress takes: the screen opens, the load command runs
// off the UI thread, and its message comes back to be applied.
func openReview(t *testing.T, m model) model {
	t.Helper()

	m = enterReview(t, m)
	require.Equal(t, screenReview, m.screen)

	return m
}

// enterReview is the same path without the diff having to be readable, for the worktree that
// holds nothing and the one that cannot be read at all.
func enterReview(t *testing.T, m model) model {
	t.Helper()

	holdServedThreads(&m)
	cmd := m.enterReview(reviewAgent(m.listing.agents[0].Agent.Cwd))
	require.NotNil(t, cmd)

	loaded, ok := cmd().(reviewLoadedMsg)
	require.True(t, ok)

	m.review.applyLoaded(&m, m.shared(), loaded)

	return m
}

// holdServedThreads puts what the fake server holds of the review into the world, as the watch
// would.
func holdServedThreads(m *model) {
	if fake, ok := m.api.agents.(*fakeReviewAgents); ok {
		holdThreads(m, fake.threads)
		holdReviews(m, fake.reviews)
	}
}

// holdReviews puts the reviews the fake server holds into the world, as the watch carries them.
func holdReviews(m *model, reviews []*agentv1.Review) {
	for _, review := range reviews {
		agentID := review.GetAgentId()
		if agentID == "" {
			agentID = m.listing.agents[0].Agent.ID
		}

		m.listing.world.Put(&watchv1.Row{Row: &watchv1.Row_Review{Review: &agentv1.WatchedReview{
			Id:           review.GetId(),
			AgentId:      agentID,
			RoundId:      review.GetRoundId(),
			RoundOrdinal: review.GetRoundOrdinal(),
			AuthorKind:   review.GetAuthorKind(),
			Verdict:      review.GetVerdict(),
			ReleasedAt:   review.GetReleasedAt(),
			DeliveredAt:  review.GetDeliveredAt(),
		}}})
	}
}

// holdThreads replaces the conversation the world holds on the agent under review.
func holdThreads(m *model, threads []*agentv1.ReviewThread) {
	agentID := m.listing.agents[0].Agent.ID
	for id, held := range m.listing.world.ReviewThreads {
		if held.GetAgentId() == agentID {
			delete(m.listing.world.ReviewThreads, id)
		}
	}

	for _, thread := range threads {
		if thread.GetAgentId() == "" {
			thread.AgentId = agentID
		}

		m.listing.world.Put(&watchv1.Row{Row: &watchv1.Row_ReviewThread{ReviewThread: thread}})
	}
}

// typeReviewMessage types words into the composer the way a person does, the field taking only
// what its own keys give it.
func typeReviewMessage(t *testing.T, m model, words string) model {
	t.Helper()

	for _, letter := range words {
		next, _ := m.routeScreenMessage(keyMsg(string(letter)))
		written, ok := next.(model)
		require.True(t, ok)
		m = written
	}

	require.Equal(t, words, m.review.threads.editor.value())

	return m
}

// TestReviewRendersWhileLoading covers the frame drawn before the patch arrives, which used to
// panic the whole panel and take its pane with it.
func TestReviewRendersWhileLoading(t *testing.T) {
	dir := reviewRepo(t)
	m, _ := reviewModel(t, &fakeReviewAgents{}, dir)

	cmd := m.enterReview(reviewAgent(dir))
	require.NotNil(t, cmd)
	require.True(t, m.review.loading)
	require.Nil(t, m.review.diff.view, "the view only exists once the patch has arrived")

	// The legend asks what the compose key would do, which asks the view where the cursor is.
	m.helpExpanded = true
	m.height = 40

	shown := ansi.Strip(m.review.view(m.shared()))
	assert.Contains(t, shown, "loading the diff")
	assert.Contains(t, shown, "start a review here")
}

// TestAComposerDoesNotOutliveTheDiffItWasWrittenOn covers a comment being typed while the load
// that answers with a short walk lands: the diff it was anchored to is taken away, and the submit
// key read it anyway.
func TestAComposerDoesNotOutliveTheDiffItWasWrittenOn(t *testing.T) {
	agents := &fakeReviewAgents{}
	m := buildReviewWithAnEarlierReview(t, gittest.NewRepo(t.TempDir()), agents)
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

	next, _ = m.routeScreenMessage(keyMsg("ctrl+s"))
	m = next.(model)

	assert.Nil(t, agents.created, "and nothing is filed against a diff that is gone")
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

// TestASecondEditPressWaitsRatherThanOpening covers the press that arrives while the agent is
// still stopping, which would otherwise walk past the wait the first press is serving.
func TestASecondEditPressWaitsRatherThanOpening(t *testing.T) {
	dir := reviewRepo(t)
	agents := &fakeReviewAgents{}
	m, _ := reviewModel(t, agents, dir)
	m = openReview(t, m)

	setReviewAgentState(&m, panel.StateIdle)
	m.review.diff.cursor = reviewRowWithAnEditTarget(t, m)

	next, cmd := m.routeScreenMessage(editorKey())
	m, ok := next.(model)
	require.True(t, ok)
	require.True(t, m.review.pass.pausing)

	assert.Contains(t, ansi.Strip(m.review.view(m.shared())), "reviewing · stopping the age")

	next, _ = m.routeScreenMessage(editorKey())
	m, ok = next.(model)
	require.True(t, ok)

	assert.False(t, m.review.pass.atEditor, "the second press is the wait, not a second thing to do")
	assert.Contains(t, m.review.notice, "still stopping")

	m = settlePause(t, m, cmd)
	assert.True(t, m.review.pass.atEditor, "and the editor opens once, when the agent has stopped")
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

	next, cmd := m.routeScreenMessage(editorKey())
	m, ok := next.(model)
	require.True(t, ok)
	require.True(t, m.review.pass.pausing)

	next, _ = m.routeScreenMessage(keyMsg("esc"))
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

// TestDroppingTheReviewWhileTheAgentIsStillStoppingLetsItGo covers the hold landing after the
// reader has left the review screen altogether.
func TestDroppingTheReviewWhileTheAgentIsStillStoppingLetsItGo(t *testing.T) {
	for _, key := range []tea.KeyPressMsg{editorKey(), keyMsg("c")} {
		dir := reviewRepo(t)
		agents := &fakeReviewAgents{}
		m, _ := reviewModel(t, agents, dir)
		m = openReview(t, m)

		setReviewAgentState(&m, panel.StateThinking)
		if key.String() == editorKey().String() {
			m.review.diff.cursor = reviewRowWithAnEditTarget(t, m)
		}

		next, cmd := m.routeScreenMessage(key)
		m, ok := next.(model)
		require.True(t, ok)
		require.True(t, m.review.pass.pausing)

		agentID := m.review.agentID

		for m.screen == screenReview {
			next, _ = m.Update(keyMsg("esc"))
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

// TestEditorOpensAtTheRepositoryRoot covers an agent whose own directory is somewhere inside the
// repository: the diff names every file from the root, so opening one from the agent's directory
// names a file that is not there.
func TestEditorOpensAtTheRepositoryRoot(t *testing.T) {
	dir := reviewRepo(t)
	m, tmux := reviewModel(t, &fakeReviewAgents{}, dir)
	m.settings.Editor = "vi"
	m.listing.agents[0].Agent.Cwd = filepath.Join(dir, "cmd", "greet")

	m = openReview(t, m)
	require.NotNil(t, m.review.diff.view)
	require.Equal(t, dir, m.review.root)

	written := pressEdit(t, m)
	require.True(t, written.review.pass.atEditor, "the edit key opened an editor")

	require.NotEmpty(t, tmux.respawnDirs)
	assert.Equal(t, dir, tmux.respawnDirs[0], "the editor is opened at the root the paths are named from")
	assert.Contains(t, tmux.respawns[0], "'greet.go'")
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

// buildReviewWithAnEarlierReview is the panel on a second round with the first one reviewed and
// released.
func buildReviewWithAnEarlierReview(t *testing.T, repo *gittest.Repo, agents *fakeReviewAgents) model {
	t.Helper()

	m, _ := buildReviewModel(t, agents, repo)
	m = openReview(t, m)
	require.Len(t, agents.rounds, 1)

	first := agents.rounds[0]
	agents.reviews = []*agentv1.Review{{
		Id:           "v1",
		RoundId:      first.GetId(),
		RoundOrdinal: first.GetOrdinal(),
		AuthorKind:   agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_HUMAN,
		ReleasedAt:   "2026-09-08T10:00:00Z",
	}}

	repo.Wrote(gittest.WroteParams{
		Tree:  "tree-with-a-second-file",
		Whole: gittest.PatchOfBothFiles,
		Since: gittest.PatchOfTheSecondFile,
		Sources: map[string]string{
			gittest.File: gittest.HeadSource,
			"second.go":  "package greet\n\nfunc Second() {}\n",
		},
	})

	m = openReview(t, m)
	require.Len(t, agents.rounds, 2)
	require.NotNil(t, m.review.diff.baseRound)

	return m
}

// pressReviewBase walks the base key the way the panel does.
func pressReviewBase(t *testing.T, m model) model {
	t.Helper()

	holdServedThreads(&m)
	next, cmd := m.routeScreenMessage(keyMsg("<"))
	m = next.(model)
	require.NotNil(t, cmd, "the base key reloads the diff")

	msg := cmd()
	loaded, ok := msg.(reviewLoadedMsg)
	require.True(t, ok, "the reload answers with a load")

	// A refused narrowing asks for the whole change again.
	if again := m.review.applyLoaded(&m, m.shared(), loaded); again != nil {
		if answer, ok := again().(reviewLoadedMsg); ok {
			m.review.applyLoaded(&m, m.shared(), answer)
		}
	}

	return m
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

// editorKey is the key the edit action is bound to by default.
func editorKey() tea.KeyPressMsg {
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

// setReviewAgentState puts the agent whose diff is on screen into one state.
func setReviewAgentState(m *model, state panel.State) {
	for i, item := range m.listing.agents {
		if item.Agent.ID == m.review.agentID {
			m.listing.agents[i].Agent.State = state
		}
	}
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

// reviewHelp is the review legend as the frame draws it.
func (m model) reviewHelp() keyMap {
	return m.review.help(m.shared())
}

// TestTheOwedListBrightensWhatIsOnScreen covers a comment in view being drawn in the panel's own
// colour while one that is not stays dim.
func TestTheOwedListBrightensWhatIsOnScreen(t *testing.T) {
	m := owedModel(t, []*agentv1.ReviewThread{owedThread("t1", gittest.File, 2)})

	rows := m.review.owedComments()
	require.Len(t, rows, 1)
	require.True(t, m.review.isCommentNear(m.shared(), rows[0]), "its line is drawn beside the list")

	bright := paint.Panel.Style(reviewOwedTone).Bold(true).Render(gittest.File + ":2")
	assert.Contains(t, m.review.sidebarView(m.shared()), bright)

	// The reader scrolls past it.
	m.review.diff.top = m.review.diff.view.Len() + 100

	require.False(t, m.review.isCommentNear(m.shared(), m.review.owedComments()[0]), "and now it is not")
	assert.NotContains(t, m.review.sidebarView(m.shared()), bright)
	assert.Contains(t, m.review.sidebarView(m.shared()), dimStyle.Render(gittest.File+":2"), "the row is still named, dim")
}

// owedModel is a review screen holding a gating review and the comments under it.
func owedModel(t *testing.T, threads []*agentv1.ReviewThread) model {
	t.Helper()

	dir := reviewRepo(t)
	agents := &fakeReviewAgents{}
	m, _ := reviewModel(t, agents, dir)
	m = openReview(t, m)

	agents.threads = threads
	agents.reviews = []*agentv1.Review{{
		Id:          "r1",
		AuthorKind:  agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_HUMAN,
		ReleasedAt:  "2026-09-13T10:00:00Z",
		OpenThreads: int32(len(threads)),
	}}

	streamServed(&m)

	return m
}

// owedThread is a comment of the reader's holding the review the gate reads.
func owedThread(id, path string, line int32, messages ...*agentv1.ReviewMessage) *agentv1.ReviewThread {
	return &agentv1.ReviewThread{
		Id:       id,
		ReviewId: "r1",
		FilePath: path,
		NewStart: line,
		Messages: append([]*agentv1.ReviewMessage{{
			Id:         id + "-m1",
			AuthorKind: agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_HUMAN,
			Body:       "this needs plain english",
			Delivery:   agentv1.ReviewDeliveryState_REVIEW_DELIVERY_STATE_DELIVERED,
		}}, messages...),
	}
}

// streamServed is the watch delivering what the fake server holds of the review, and the screen
// following it.
func streamServed(m *model) {
	fake, ok := m.api.agents.(*fakeReviewAgents)
	if !ok {
		return
	}

	holdThreads(m, fake.threads)
	holdReviews(m, fake.reviews)
	m.review.followWorld(m.shared())
}
