package ui

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"strconv"

	"weave/internal/diff"
	"weave/internal/gittest"
	"weave/internal/ui/diffview"
	"weave/internal/ui/paint"
	"weave/internal/ui/panel"
	agentv1 "weave/proto/gen/agent/v1"
	"weave/proto/gen/agent/v1/agentv1connect"
	watchv1 "weave/proto/gen/watch/v1"

	"connectrpc.com/connect"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

// loadReviewMsg runs the load off the UI thread and hands back what it answered, for a test
// about the answer itself rather than about the screen drawn from it.
func loadReviewMsg(t *testing.T, m model) reviewLoadedMsg {
	t.Helper()

	holdServedThreads(&m)
	cmd := m.enterReview(reviewAgent(m.listing.agents[0].Agent.Cwd))
	require.NotNil(t, cmd)

	loaded, ok := cmd().(reviewLoadedMsg)
	require.True(t, ok)

	return loaded
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

// TestEnterReviewRefusesAnEmptyWorktree pins the drop back to the listing for an agent that has
// written nothing.
func TestEnterReviewRefusesAnEmptyWorktree(t *testing.T) {
	repo := gittest.NewRepo(t.TempDir())
	repo.Wrote(gittest.WroteParams{Tree: "tree-of-the-base"})

	m, _ := buildReviewModel(t, &fakeReviewAgents{}, repo)

	m = enterReview(t, m)

	assert.Equal(t, screenAgents, m.screen, "the reader is put back on the listing")
	assert.Zero(t, m.review, "the refused review keeps nothing")
	require.NotEmpty(t, m.messages.items)
	assert.Contains(t, newestMessage(m).summary, "Nothing to review")
	assert.Contains(t, newestMessage(m).detail, "no change")
}

// TestEnterReviewOpensAnUntrackedFile covers the worktree whose only work is a file git has never
// seen.
func TestEnterReviewOpensAnUntrackedFile(t *testing.T) {
	repo := gittest.NewRepo(t.TempDir())
	repo.Wrote(gittest.WroteParams{
		Tree:    "tree-with-an-untracked-file",
		Whole:   gittest.PatchOfAnAddedFile,
		Sources: map[string]string{"new.go": "package greet\n"},
	})

	m, _ := buildReviewModel(t, &fakeReviewAgents{}, repo)

	m = openReview(t, m)

	assert.Equal(t, screenReview, m.screen)
	assert.Contains(t, reviewFrameText(t, m), "new.go")
}

// TestEnterReviewOpensInAPlainWorkflow covers the agent weave cut no worktree for: it can hold
// a review, so the key that opens one has to answer on its row.
func TestEnterReviewOpensInAPlainWorkflow(t *testing.T) {
	dir := reviewRepo(t)
	m, _ := reviewModel(t, &fakeReviewAgents{}, dir)
	m.listing.workflows[0].Worktree = panel.WorktreeConfig{}

	m = openReview(t, m)

	assert.Equal(t, screenReview, m.screen)
	assert.Contains(t, reviewFrameText(t, m), "greet.go")
}

// TestEnterReviewDrawsTheDiffBesideTheSidebar pins that opening a review moves to the review
// screen and draws the patch itself, in the panel's own frame rather than in a pane of its own.
func TestEnterReviewDrawsTheDiffBesideTheSidebar(t *testing.T) {
	dir := reviewRepo(t)
	m, tmux := reviewModel(t, &fakeReviewAgents{}, dir)

	m = openReview(t, m)

	require.Equal(t, screenReview, m.screen)
	require.NotNil(t, m.review.diff.view)

	assert.True(t, tmux.zoomed, "the review takes the window off the panes it was sharing with")
	assert.Empty(t, tmux.respawns, "and starts no drawer of its own")

	frame := reviewFrameText(t, m)
	assert.Contains(t, frame, "greet.go", "the diff is drawn")
	assert.Contains(t, frame, "Review · auth", "and the sidebar beside it")
	assert.Equal(t, m.height, reviewFrameRows(t, m))
}

// TestReviewCursorRedrawsInPlace covers a cursor move changing the frame without anything being
// respawned or told to re-read.
func TestReviewCursorRedrawsInPlace(t *testing.T) {
	dir := reviewRepo(t)
	m, tmux := reviewModel(t, &fakeReviewAgents{}, dir)
	m = openReview(t, m)

	// Without a colour profile the cursor row paints as bare text and moving it would change
	// nothing in the frame.

	before, respawns := m.review.diff.cursor, len(tmux.respawns)
	was := reviewFrameRaw(t, m)
	tmux.sentKeys = nil

	next, _ := m.routeScreenMessage(keyMsg("j"))
	m = next.(model)

	require.Greater(t, m.review.diff.cursor, before)
	assert.Len(t, tmux.respawns, respawns, "nothing is respawned")
	assert.Empty(t, tmux.sentKeys, "and nothing is tapped to re-read a file")

	// Compared raw: a diff that fits the pane does not scroll, so the only thing a move changes is
	// which row is painted as the cursor.
	assert.NotEqual(t, was, reviewFrameRaw(t, m), "the cursor moved, so the frame changed")
	assert.Equal(t, m.height, reviewFrameRows(t, m))
}

// TestReviewScrollKeysMoveTheCursorAWheelTurn pins the column scroll keys on the diff, which is
// what the mouse wheel over the panel presses.
func TestReviewScrollKeysMoveTheCursorAWheelTurn(t *testing.T) {
	dir := reviewRepo(t)
	m, _ := reviewModel(t, &fakeReviewAgents{}, dir)
	m = openReview(t, m)

	before := m.review.diff.cursor

	next, _ := m.routeScreenMessage(keyMsg("ctrl+n"))
	m = next.(model)

	down := m.review.diff.cursor
	require.Greater(t, down, before)
	assert.LessOrEqual(t, down-before, reviewWheelStep, "a turn is a few rows, not a page")

	next, _ = m.routeScreenMessage(keyMsg("ctrl+p"))
	m = next.(model)

	assert.Equal(t, before, m.review.diff.cursor, "the other key turns it back")
}

func reviewFrameText(t *testing.T, m model) string {
	t.Helper()

	return ansi.Strip(reviewFrameRaw(t, m))
}

// reviewProse is what the sidebar alone says with its wrapping taken back out, the diff beside
// it left out.
func reviewProse(m model) string {
	return strings.Join(strings.Fields(ansi.Strip(m.review.sidebarView(m.shared()))), " ")
}

func reviewFrameRaw(t *testing.T, m model) string {
	t.Helper()

	require.Equal(t, screenReview, m.screen)

	return m.render()
}

func reviewFrameRows(t *testing.T, m model) int {
	t.Helper()

	return strings.Count(reviewFrameRaw(t, m), "\n") + 1
}

// TestReviewKeysFollowTheKeymap covers the review screen reading its keys from the config like
// every other screen.
func TestReviewKeysFollowTheKeymap(t *testing.T) {
	dir := reviewRepo(t)
	m, _ := reviewModel(t, &fakeReviewAgents{}, dir)
	m.keys.Review.Compose = []string{"i"}
	m = openReview(t, m)

	rebound, _ := m.routeScreenMessage(keyMsg("i"))
	assert.True(t, asModel(t, rebound).review.threads.composing, "the rebound key opens the editor")

	replaced, _ := m.routeScreenMessage(keyMsg("c"))
	assert.False(t, asModel(t, replaced).review.threads.composing, "and the key it replaced does nothing")

	assert.Contains(t, m.review.help(m.shared()).short, binding([]string{"i"}, m.review.composeDesc()),
		"the legend names the key that is bound rather than the one that shipped")
}

// TestReviewComposeOpensAThread covers the write path: c opens the editor on the line under
// the cursor, ctrl+s sends it, and what is sent is the anchor the cursor was standing on.
func TestReviewComposeOpensAThread(t *testing.T) {
	dir := reviewRepo(t)
	agents := &fakeReviewAgents{}
	m, _ := reviewModel(t, agents, dir)
	m = openReview(t, m)

	next, _ := m.routeScreenMessage(keyMsg("c"))
	m = next.(model)
	require.True(t, m.review.threads.composing)

	m = typeReviewMessage(t, m, "this should say hello")

	next, cmd := m.routeScreenMessage(keyMsg("ctrl+s"))
	m = next.(model)
	require.False(t, m.review.threads.composing)
	require.NotNil(t, cmd)

	wrote, ok := cmd().(reviewWroteMsg)
	require.True(t, ok)
	require.NoError(t, wrote.err)

	require.NotNil(t, agents.created)
	assert.Equal(t, "greet.go", agents.created.GetFilePath())
	assert.Equal(t, "this should say hello", agents.created.GetBody())
	require.NotEmpty(t, agents.created.GetQuotedLines())
}

// TestReviewSidebarShowsTheThread covers the other half of the split: a thread whose quoted
// line still matches the diff is drawn in the sidebar when the cursor reaches it.
func TestReviewSidebarShowsTheThread(t *testing.T) {
	dir := reviewRepo(t)
	agents := &fakeReviewAgents{}
	m, _ := reviewModel(t, agents, dir)
	m = openReview(t, m)

	anchor, ok := m.review.diff.view.GetAnchor(m.review.diff.cursor)
	require.True(t, ok)

	agents.threads = []*agentv1.ReviewThread{{
		Id:          "t1",
		FilePath:    anchor.FilePath,
		OldStart:    int32(anchor.OldStart),
		NewStart:    int32(anchor.NewStart),
		QuotedLines: anchor.QuotedLines,
		Messages: []*agentv1.ReviewMessage{{
			Id:         "m1",
			AuthorKind: agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_HUMAN,
			Body:       "wrong greeting",
		}},
	}}

	m.review.applyWrote(&m, m.shared(), reviewWroteMsg{agentID: "a1"})
	streamThreads(t, &m, agents.threads)

	shown := ansi.Strip(m.review.view(m.shared()))
	assert.Contains(t, shown, "Review · auth")
	assert.Contains(t, shown, "wrong greeting")
	assert.Contains(t, shown, "you")
}

// TestReviewSidebarSizesTheDiff pins the summary beside the diff.
func TestReviewSidebarSizesTheDiff(t *testing.T) {
	dir := reviewRepo(t)
	m, _ := reviewModel(t, &fakeReviewAgents{}, dir)
	m = openReview(t, m)

	shown := ansi.Strip(m.review.view(m.shared()))
	assert.Contains(t, shown, "1 file,")
	assert.Contains(t, shown, "+1")
	assert.Contains(t, shown, "-1")
}

// TestReviewIsRefusedWithoutADirectory pins that an agent whose directory has gone has nothing
// left to read, so the screen is left where it was.
func TestReviewIsRefusedWithoutADirectory(t *testing.T) {
	m, tmux := reviewModel(t, &fakeReviewAgents{}, t.TempDir())

	agent := reviewAgent(m.listing.agents[0].Agent.Cwd)
	agent.WorktreeRemoved = true

	cmd := m.enterReview(agent)

	assert.Nil(t, cmd)
	assert.Equal(t, screenAgents, m.screen)
	assert.Empty(t, tmux.respawns)
}

// TestReviewPagesOnThePageKeys covers the shared pair, which moves a few lines at a time and
// settles on a row the cursor is allowed to hold rather than on a file or hunk header.
func TestReviewPagesOnThePageKeys(t *testing.T) {
	dir := reviewRepo(t)
	m, _ := reviewModel(t, &fakeReviewAgents{}, dir)
	m = openReview(t, m)

	first := m.review.diff.cursor

	next, _ := m.routeScreenMessage(keyMsg("ctrl+d"))
	m = next.(model)

	assert.Greater(t, m.review.diff.cursor, first)
	_, ok := m.review.diff.view.GetAnchor(m.review.diff.cursor)
	assert.True(t, ok)

	next, _ = m.routeScreenMessage(keyMsg("ctrl+u"))
	m = next.(model)

	assert.Equal(t, first, m.review.diff.cursor)
	_, ok = m.review.diff.view.GetAnchor(m.review.diff.cursor)
	assert.True(t, ok)
}

// TestReviewJumpsToASingleThread covers what happens with one thread in the diff: n has to
// reach it from anywhere, including from the thread itself, or it reads as a dead key.
func TestReviewJumpsToASingleThread(t *testing.T) {
	dir := reviewRepo(t)
	agents := &fakeReviewAgents{}
	m, _ := reviewModel(t, agents, dir)
	m = openReview(t, m)

	first := m.review.diff.cursor
	anchor, ok := m.review.diff.view.GetAnchor(m.review.diff.view.GetLastRow())
	require.True(t, ok)
	require.NotEqual(t, first, m.review.diff.view.GetLastRow())

	agents.threads = []*agentv1.ReviewThread{{
		Id:          "t1",
		FilePath:    anchor.FilePath,
		OldStart:    int32(anchor.OldStart),
		NewStart:    int32(anchor.NewStart),
		QuotedLines: anchor.QuotedLines,
	}}
	m.review.applyWrote(&m, m.shared(), reviewWroteMsg{agentID: "a1"})
	streamThreads(t, &m, agents.threads)

	next, _ := m.routeScreenMessage(keyMsg("t"))
	m = next.(model)
	require.Equal(t, m.review.diff.view.GetLastRow(), m.review.diff.cursor, "t should reach the only thread")

	// Standing on it, t has nowhere forward to go and has to stay put.
	next, _ = m.routeScreenMessage(keyMsg("t"))
	m = next.(model)
	assert.Equal(t, m.review.diff.view.GetLastRow(), m.review.diff.cursor)
}

// TestReviewAdriftThreadIsReachable covers a thread whose file is not in the diff, which gets
// a row at the foot so the cursor can still stand on it and answer.
func TestReviewAdriftThreadIsReachable(t *testing.T) {
	dir := reviewRepo(t)
	agents := &fakeReviewAgents{}
	m, _ := reviewModel(t, agents, dir)
	m = openReview(t, m)

	agents.threads = []*agentv1.ReviewThread{{
		Id:          "t1",
		FilePath:    "gone.go",
		NewStart:    12,
		QuotedLines: []string{"+what this used to say"},
		Messages: []*agentv1.ReviewMessage{{
			Id:         "m1",
			AuthorKind: agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_HUMAN,
			Body:       "still worth reading",
		}},
	}}
	m.review.applyWrote(&m, m.shared(), reviewWroteMsg{agentID: "a1"})
	streamThreads(t, &m, agents.threads)

	next, _ := m.routeScreenMessage(keyMsg("t"))
	m = next.(model)

	thread, ok := m.review.diff.view.GetThreadAt(m.review.diff.cursor)
	require.True(t, ok, "t should reach the thread that came adrift")
	require.Equal(t, "t1", thread.GetId())

	shown := ansi.Strip(m.review.view(m.shared()))
	assert.Contains(t, shown, "still worth reading", "the conversation is readable where it stands")
	assert.Contains(t, shown, "gone.go:12", "and it says where it was written")
	assert.Contains(t, shown, "+what this used to say", "carrying the line it was written against")
}

// TestReviewAdriftThreadTakesAReply is the point of keeping it: a question nobody has answered
// is still a question, whatever happened to the code underneath it.
func TestReviewAdriftThreadTakesAReply(t *testing.T) {
	dir := reviewRepo(t)
	agents := &fakeReviewAgents{}
	m, _ := reviewModel(t, agents, dir)
	m = openReview(t, m)

	agents.threads = []*agentv1.ReviewThread{{
		Id:          "t1",
		FilePath:    "gone.go",
		NewStart:    12,
		QuotedLines: []string{"+what this used to say"},
		Messages:    []*agentv1.ReviewMessage{{Id: "m1", Body: "why did this go?"}},
	}}
	m.review.applyWrote(&m, m.shared(), reviewWroteMsg{agentID: "a1"})
	streamThreads(t, &m, agents.threads)

	next, _ := m.routeScreenMessage(keyMsg("t"))
	m = next.(model)

	next, _ = m.routeScreenMessage(keyMsg("c"))
	m = next.(model)
	require.True(t, m.review.threads.composing)
	assert.Equal(t, "t1", m.review.threads.editing, "composing answers the thread rather than opening a new one")
}

// TestReviewViewNeverOutgrowsThePane covers the sidebar staying inside the pane.
func TestReviewViewNeverOutgrowsThePane(t *testing.T) {
	dir := reviewRepo(t)
	agents := &fakeReviewAgents{}
	m, _ := reviewModel(t, agents, dir)
	m.height = 12
	m = openReview(t, m)

	anchor, ok := m.review.diff.view.GetAnchor(m.review.diff.cursor)
	require.True(t, ok)

	long := &agentv1.ReviewThread{
		Id:          "t1",
		FilePath:    anchor.FilePath,
		OldStart:    int32(anchor.OldStart),
		NewStart:    int32(anchor.NewStart),
		QuotedLines: anchor.QuotedLines,
	}
	for i := range 6 {
		long.Messages = append(long.Messages, &agentv1.ReviewMessage{
			Id:         "m" + strconv.Itoa(i),
			AuthorKind: agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_HUMAN,
			Body:       strings.Repeat("this is a long note that has to wrap more than once ", 3),
		})
	}

	threads := []*agentv1.ReviewThread{long}
	for i := range 4 {
		threads = append(threads, &agentv1.ReviewThread{
			Id:          "d" + strconv.Itoa(i),
			FilePath:    "gone" + strconv.Itoa(i) + ".go",
			NewStart:    int32(i + 1),
			QuotedLines: []string{"+what this used to say"},
			Messages: []*agentv1.ReviewMessage{{
				Id:         "dm" + strconv.Itoa(i),
				AuthorKind: agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_HUMAN,
				Body:       "still worth reading",
			}},
		})
	}

	m.review.applyWrote(&m, m.shared(), reviewWroteMsg{agentID: "a1"})
	streamThreads(t, &m, threads)

	for _, pane := range []int{10, 12, 24, 50} {
		for _, composing := range []bool{false, true} {
			m.height, m.review.threads.composing = pane, composing

			// Measured the way bubbletea receives it: View trims the trailing newline off the screen.
			height := strings.Count(strings.TrimRight(m.review.view(m.shared()), "\n"), "\n") + 1
			assert.LessOrEqual(t, height, pane,
				"the sidebar drew %d lines into a %d line pane, composing=%v", height, pane, composing)
		}
	}
}

// reviewLongThread is a conversation taller than a short pane, standing on the line the cursor
// is on so the sidebar draws it.
func reviewLongThread(t *testing.T, m model) []*agentv1.ReviewThread {
	t.Helper()

	anchor, ok := m.review.diff.view.GetAnchor(m.review.diff.cursor)
	require.True(t, ok)

	long := &agentv1.ReviewThread{
		Id:          "t1",
		FilePath:    anchor.FilePath,
		OldStart:    int32(anchor.OldStart),
		NewStart:    int32(anchor.NewStart),
		QuotedLines: anchor.QuotedLines,
	}
	for i := range 6 {
		long.Messages = append(long.Messages, &agentv1.ReviewMessage{
			Id:         "m" + strconv.Itoa(i),
			AuthorKind: agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_HUMAN,
			Body:       "note " + strconv.Itoa(i),
		})
	}

	return []*agentv1.ReviewThread{long}
}

// TestReviewThreadScrollsToItsStart covers reading a conversation taller than the sidebar, which
// is drawn from its end and was reachable no further back than that.
func TestReviewThreadScrollsToItsStart(t *testing.T) {
	dir := reviewRepo(t)
	agents := &fakeReviewAgents{}
	m, _ := reviewModel(t, agents, dir)
	m = openReview(t, m)

	m.review.applyWrote(&m, m.shared(), reviewWroteMsg{agentID: "a1"})
	streamThreads(t, &m, reviewLongThread(t, m))
	m.height = 14

	require.NotContains(t, ansi.Strip(m.review.view(m.shared())), "note 0", "the start of the thread is off the sidebar")

	next, _ := m.routeScreenMessage(keyMsg("ctrl+f"))
	m = next.(model)
	require.True(t, m.review.isThreadFocused(m.shared()))

	cursor := m.review.diff.cursor
	for range 20 {
		next, _ = m.routeScreenMessage(keyMsg("k"))
		m = next.(model)
	}

	shown := ansi.Strip(m.review.view(m.shared()))
	assert.Contains(t, shown, "note 0", "scrolling up reaches the first message")
	assert.NotContains(t, shown, "note 5", "the end of the thread has scrolled away")
	assert.Equal(t, cursor, m.review.diff.cursor, "the diff cursor stays where it was")

	next, _ = m.routeScreenMessage(keyMsg("esc"))
	m = next.(model)

	assert.False(t, m.review.isThreadFocused(m.shared()))
	assert.Equal(t, screenReview, m.screen, "esc hands the keys back rather than leaving the review")
	assert.NotContains(t, ansi.Strip(m.review.view(m.shared())), "note 0", "the thread is drawn from its end again")
}

// TestReviewThreadFocusIsRefusedWithoutAThread pins that the key does nothing on a line with no
// conversation on it.
func TestReviewThreadFocusIsRefusedWithoutAThread(t *testing.T) {
	dir := reviewRepo(t)
	m, _ := reviewModel(t, &fakeReviewAgents{}, dir)
	m = openReview(t, m)

	next, _ := m.routeScreenMessage(keyMsg("ctrl+f"))
	m = next.(model)

	assert.False(t, m.review.isThreadFocused(m.shared()))
	assert.NotEmpty(t, m.review.notice)
}

// TestReviewScrollKeysMoveTheThreadWhileItHoldsTheKeys pins the wheel reading further up a
// conversation rather than dropping the reader back onto the diff.
func TestReviewScrollKeysMoveTheThreadWhileItHoldsTheKeys(t *testing.T) {
	dir := reviewRepo(t)
	m, _ := reviewModel(t, &fakeReviewAgents{}, dir)
	m = openReview(t, m)

	m.review.applyWrote(&m, m.shared(), reviewWroteMsg{agentID: "a1"})
	streamThreads(t, &m, reviewLongThread(t, m))
	m.height = 14

	next, _ := m.routeScreenMessage(keyMsg("ctrl+f"))
	m = next.(model)
	require.True(t, m.review.isThreadFocused(m.shared()))

	cursor := m.review.diff.cursor

	next, _ = m.routeScreenMessage(keyMsg("ctrl+p"))
	m = next.(model)

	assert.True(t, m.review.isThreadFocused(m.shared()), "the conversation keeps the keys")
	assert.Equal(t, reviewWheelStep, m.review.threads.scroll)
	assert.Equal(t, cursor, m.review.diff.cursor, "and the diff did not move")

	next, _ = m.routeScreenMessage(keyMsg("ctrl+n"))
	m = next.(model)

	assert.Zero(t, m.review.threads.scroll)
}

// TestReviewThreadCursorMoveDropsTheScroll covers the window being counted from the end of one
// conversation, which says nothing about the next one the cursor lands on.
func TestReviewThreadCursorMoveDropsTheScroll(t *testing.T) {
	dir := reviewRepo(t)
	agents := &fakeReviewAgents{}
	m, _ := reviewModel(t, agents, dir)
	m = openReview(t, m)

	m.review.applyWrote(&m, m.shared(), reviewWroteMsg{agentID: "a1"})
	streamThreads(t, &m, reviewLongThread(t, m))
	m.height = 14

	next, _ := m.routeScreenMessage(keyMsg("ctrl+f"))
	m = next.(model)
	next, _ = m.routeScreenMessage(keyMsg("k"))
	m = next.(model)
	require.Positive(t, m.review.threads.scroll)

	next, _ = m.routeScreenMessage(keyMsg("ctrl+d"))
	m = next.(model)

	assert.Zero(t, m.review.threads.scroll)
	assert.False(t, m.review.threads.focus, "the keys go back to the diff with the thread they were on")
	assert.Equal(t, "read the thread", m.review.threadDesc(),
		"the legend says where the keys are rather than where they were")
}

// TestReviewThreadWindowNeverOutgrowsThePane covers the sidebar staying inside the pane at every
// place the conversation can be scrolled to.
func TestReviewThreadWindowNeverOutgrowsThePane(t *testing.T) {
	dir := reviewRepo(t)
	agents := &fakeReviewAgents{}
	m, _ := reviewModel(t, agents, dir)
	m = openReview(t, m)

	m.review.applyWrote(&m, m.shared(), reviewWroteMsg{agentID: "a1"})
	streamThreads(t, &m, reviewLongThread(t, m))
	m.height = 14
	m.review.threads.focus = true

	for _, pane := range []int{8, 10, 14, 30} {
		for _, scroll := range []int{0, 1, 5, 40} {
			m.height, m.review.threads.scroll = pane, scroll

			height := strings.Count(strings.TrimRight(m.review.view(m.shared()), "\n"), "\n") + 1
			assert.LessOrEqual(t, height, pane,
				"the sidebar drew %d lines into a %d line pane, scroll=%d", height, pane, scroll)
		}
	}
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

// TestReviewReportsAFailedLoad covers a worktree the diff loader could not read at all: the
// reader is put back on the listing and told through the messages region rather than left in a
// viewer with the raw error drawn in the sidebar.
func TestReviewReportsAFailedLoad(t *testing.T) {
	repo := gittest.NewRepo(t.TempDir())
	repo.RootErr = gittest.ErrNoGit

	m, _ := buildReviewModel(t, &fakeReviewAgents{}, repo)

	m = enterReview(t, m)

	require.Equal(t, screenAgents, m.screen, "a diff that cannot be read is not a screen to sit in")
	require.Len(t, m.messages.items, 1)
	assert.Equal(t, messageError, m.messages.items[0].kind)
	assert.Contains(t, m.messages.items[0].summary, "Could not read the diff of")
	assert.Contains(t, m.messages.items[0].detail, gittest.ErrNoGit.Error(),
		"the error the read failed with is what the reader is told")
}

// TestReviewKeepsDrawingAfterAWrite covers the stage still repainting after a save.
func TestReviewKeepsDrawingAfterAWrite(t *testing.T) {
	dir := reviewRepo(t)
	agents := &fakeReviewAgents{}
	m, tmux := reviewModel(t, agents, dir)
	m = openReview(t, m)

	// What saving a comment does to the screen: the threads come back and the rows are dropped.
	m.review.applyWrote(&m, m.shared(), reviewWroteMsg{agentID: "a1", notice: "thread opened"})

	respawns := len(tmux.respawns)
	tmux.sentKeys = nil

	next, _ := m.routeScreenMessage(keyMsg("j"))
	m = next.(model)

	assert.Len(t, tmux.respawns, respawns, "nothing is respawned")
	assert.Empty(t, tmux.sentKeys, "and nothing is tapped to re-read a file")
	assert.Contains(t, reviewFrameText(t, m), "greet.go", "and the frame is drawn again")
	assert.Equal(t, m.height, reviewFrameRows(t, m))
}

// TestReviewPanRedrawsTheRows covers the same seam sideways: panning changes what every row
// looks like, so the rows have to be drawn again for it to show.
func TestReviewPanRedrawsTheRows(t *testing.T) {
	dir := reviewRepo(t)
	m, _ := reviewModel(t, &fakeReviewAgents{}, dir)
	m = openReview(t, m)

	was := reviewFrameText(t, m)

	next, _ := m.routeScreenMessage(keyMsg("l"))
	m = next.(model)

	require.Positive(t, m.review.diff.pan)
	assert.NotEqual(t, was, reviewFrameText(t, m), "panning moves the text under the gutter")
}

// TestReviewRepliesInAThread covers the compose key on a line that already carries a thread,
// which answers it rather than opening a second one.
func TestReviewRepliesInAThread(t *testing.T) {
	dir := reviewRepo(t)
	agents := &fakeReviewAgents{}
	m, _ := reviewModel(t, agents, dir)
	m = openReview(t, m)

	anchor, ok := m.review.diff.view.GetAnchor(m.review.diff.cursor)
	require.True(t, ok)

	agents.threads = []*agentv1.ReviewThread{{
		Id:          "t1",
		FilePath:    anchor.FilePath,
		OldStart:    int32(anchor.OldStart),
		NewStart:    int32(anchor.NewStart),
		QuotedLines: anchor.QuotedLines,
		Messages: []*agentv1.ReviewMessage{{
			Id:         "m1",
			AuthorKind: agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_AGENT,
			Body:       "renamed it, is that what you meant?",
		}},
	}}
	m.review.applyWrote(&m, m.shared(), reviewWroteMsg{agentID: "a1"})
	streamThreads(t, &m, agents.threads)

	m.helpExpanded, m.height = true, 40
	assert.Contains(t, ansi.Strip(m.review.view(m.shared())), "reply in this thread",
		"the legend says what the key will do on a line that already has a thread")

	next, _ := m.routeScreenMessage(keyMsg("c"))
	m = next.(model)
	require.True(t, m.review.threads.composing)
	require.Equal(t, "t1", m.review.threads.editing, "the reply goes into the thread under the cursor")

	m = typeReviewMessage(t, m, "yes, that is it")

	next, cmd := m.routeScreenMessage(keyMsg("ctrl+s"))
	m = next.(model)
	require.NotNil(t, cmd)

	wrote, ok := cmd().(reviewWroteMsg)
	require.True(t, ok)
	require.NoError(t, wrote.err)

	require.NotNil(t, agents.replied, "a line with a thread replies")
	assert.Equal(t, "t1", agents.replied.GetThreadId())
	assert.Equal(t, "yes, that is it", agents.replied.GetBody())
	assert.Nil(t, agents.created, "and does not open a second thread on the same line")
}

// TestReviewPanStepScalesWithThePane covers how far sideways a press goes.
func TestReviewPanStepScalesWithThePane(t *testing.T) {
	dir := reviewRepo(t)
	m, _ := reviewModel(t, &fakeReviewAgents{}, dir)
	m = openReview(t, m)

	assert.Equal(t, 56, panStep(m.shared()), "a third of the 170 columns the diff is drawn in")

	next, _ := m.routeScreenMessage(keyMsg("l"))
	m = next.(model)
	assert.Equal(t, 56, m.review.diff.pan)

	// Until the pane has been measured there is nothing to take a third of.
	m.width = 0
	assert.Equal(t, reviewPanStep, panStep(m.shared()))
}

func unsentComment() *agentv1.ReviewMessage {
	return &agentv1.ReviewMessage{
		Id:         "m1",
		AuthorKind: agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_HUMAN,
		Delivery:   agentv1.ReviewDeliveryState_REVIEW_DELIVERY_STATE_UNSENT,
	}
}

func sentComment() *agentv1.ReviewMessage {
	return &agentv1.ReviewMessage{
		Id:         "m1",
		AuthorKind: agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_HUMAN,
		Delivery:   agentv1.ReviewDeliveryState_REVIEW_DELIVERY_STATE_SENT,
		SentAt:     "2026-09-02T10:00:00Z",
	}
}

// TestReviewDeliveryViewCountsTheComments covers the line at the top of the sidebar, which counts the person's own comments by how far each has got.
func TestReviewDeliveryViewCountsTheComments(t *testing.T) {
	tests := []struct {
		name    string
		threads []*agentv1.ReviewThread
		want    string
		absent  string
	}{
		{
			name:   "a review with nothing in it",
			want:   "",
			absent: "unsent",
		},
		{
			name: "written and never let go of",
			threads: []*agentv1.ReviewThread{{
				Id:       "t1",
				Messages: []*agentv1.ReviewMessage{unsentComment()},
			}},
			want:   "1 unsent",
			absent: "delivered",
		},
		{
			name: "some sent and one still to go",
			threads: []*agentv1.ReviewThread{{
				Id: "t1",
				Messages: []*agentv1.ReviewMessage{
					unsentComment(),
					sentComment(),
					deliveredComment(),
				},
			}},
			want:   "1 unsent · 1 waiting · 1 delivered",
			absent: "nothing written yet",
		},
		{
			name: "what the agent said is nobody's delivery",
			threads: []*agentv1.ReviewThread{{
				Id: "t1",
				Messages: []*agentv1.ReviewMessage{{
					Id:         "m9",
					AuthorKind: agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_AGENT,
					Body:       "done",
				}},
			}},
			want:   "",
			absent: "waiting",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := model{review: reviewScreen{
				agentID: "a1",
				threads: reviewThreads{
					list:     tt.threads,
					delivery: countDeliveries(tt.threads),
				},
			}}

			shown := ansi.Strip(m.review.threads.deliveryCountView())
			assert.Equal(t, tt.want, shown)
			assert.NotContains(t, shown, tt.absent)
		})
	}
}

func deliveredComment() *agentv1.ReviewMessage {
	return &agentv1.ReviewMessage{
		Id:          "m2",
		AuthorKind:  agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_HUMAN,
		Delivery:    agentv1.ReviewDeliveryState_REVIEW_DELIVERY_STATE_DELIVERED,
		SentAt:      "2026-09-02T10:00:00Z",
		DeliveredAt: "2026-09-02T10:01:00Z",
	}
}

// TestReviewVerdictKeepsItsColour covers the chosen verdict wearing what it means rather than
// the muted tone of the options it was picked out of.
func TestReviewVerdictKeepsItsColour(t *testing.T) {

	dir := reviewRepo(t)
	m, _ := reviewModel(t, &fakeReviewAgents{}, dir)
	m = openReview(t, m)

	next, _ := m.routeScreenMessage(keyMsg("ctrl+s"))
	m = next.(model)
	require.True(t, m.review.threads.releasing)

	shown := m.review.releaseView(m.shared())
	chosen := "  ▸ " + describeVerdict(reviewVerdicts[m.review.threads.verdict])
	want := paint.Panel.Style(verdictTone(reviewVerdicts[m.review.threads.verdict])).Render(chosen)
	assert.Contains(t, shown, want)
	assert.NotContains(t, shown, paint.Panel.Style(paint.ToneMuted).Render(chosen))
}

// TestReviewReleaseIsTheSubmitKey covers there being one way to send a review, on the same key
// that submits everywhere else, which asks what to send rather than deciding for the reader.
func TestReviewReleaseIsTheSubmitKey(t *testing.T) {
	dir := reviewRepo(t)
	agents := &fakeReviewAgents{}
	m, _ := reviewModel(t, agents, dir)
	m = openReview(t, m)

	next, _ := m.routeScreenMessage(keyMsg("ctrl+s"))
	m = next.(model)
	require.True(t, m.review.threads.releasing, "the key opens the panel")
	require.False(t, m.review.threads.composing, "and not the line composer")
	require.Nil(t, agents.released, "and sends nothing on its own")

	_, cmd := m.routeScreenMessage(keyMsg("ctrl+s"))
	require.NotNil(t, cmd, "the same key sends from inside it")

	wrote, ok := cmd().(reviewWroteMsg)
	require.True(t, ok)
	require.NoError(t, wrote.err)

	require.NotNil(t, agents.released)
	assert.Equal(t, "a1", agents.released.GetAgentId())
}

// TestSettlingAThreadIsWhatQuietsIt covers settling being a key the reviewer presses.
func TestSettlingAThreadIsWhatQuietsIt(t *testing.T) {
	dir := reviewRepo(t)
	agents := &fakeReviewAgents{}
	m, _ := reviewModel(t, agents, dir)
	m = openReview(t, m)

	anchor, ok := m.review.diff.view.GetAnchor(m.review.diff.cursor)
	require.True(t, ok)

	agents.threads = []*agentv1.ReviewThread{{
		Id:          "t1",
		FilePath:    anchor.FilePath,
		OldStart:    int32(anchor.OldStart),
		NewStart:    int32(anchor.NewStart),
		QuotedLines: anchor.QuotedLines,
		Messages:    []*agentv1.ReviewMessage{{Id: "m1", Body: "why did this change?"}},
	}}
	m.review.applyWrote(&m, m.shared(), reviewWroteMsg{agentID: "a1"})
	streamThreads(t, &m, agents.threads)

	next, cmd := m.routeScreenMessage(keyMsg("r"))
	m = next.(model)
	require.NotNil(t, cmd)

	wrote, ok := cmd().(reviewWroteMsg)
	require.True(t, ok)
	require.NoError(t, wrote.err)
	m.review.applyWrote(&m, m.shared(), wrote)
	streamServed(&m)

	require.NotNil(t, agents.settled)
	assert.Equal(t, "t1", agents.settled.GetId())
	assert.True(t, agents.settled.GetResolved())

	assert.Contains(t, reviewGutterFor(t, m, "t1"), "✓",
		"the gutter is where settling is said, now the line above counts comments instead")

	// Pressing it again reopens.
	next, cmd = m.routeScreenMessage(keyMsg("r"))
	m = next.(model)
	require.NotNil(t, cmd)

	wrote, ok = cmd().(reviewWroteMsg)
	require.True(t, ok)
	m.review.applyWrote(&m, m.shared(), wrote)
	streamServed(&m)

	assert.False(t, agents.settled.GetResolved())
}

// TestSettlingSurvivesTheCodeMoving is the pair of rules working together: a settled thread
// whose line has since been rewritten stays settled, and a question nobody answered stays
// loud wherever it ends up.
func TestSettlingSurvivesTheCodeMoving(t *testing.T) {
	dir := reviewRepo(t)
	agents := &fakeReviewAgents{}
	m, _ := reviewModel(t, agents, dir)
	m = openReview(t, m)

	agents.threads = []*agentv1.ReviewThread{{
		Id:          "t1",
		FilePath:    "gone.go",
		NewStart:    12,
		QuotedLines: []string{"+what this used to say"},
		Messages:    []*agentv1.ReviewMessage{{Id: "m1", Body: "why did this go?"}},
	}}
	m.review.applyWrote(&m, m.shared(), reviewWroteMsg{agentID: "a1"})
	streamThreads(t, &m, agents.threads)

	next, _ := m.routeScreenMessage(keyMsg("t"))
	m = next.(model)

	next, cmd := m.routeScreenMessage(keyMsg("r"))
	m = next.(model)
	require.NotNil(t, cmd)

	wrote, ok := cmd().(reviewWroteMsg)
	require.True(t, ok)
	m.review.applyWrote(&m, m.shared(), wrote)
	streamServed(&m)

	require.NotNil(t, agents.settled, "a thread that came adrift settles like any other")
	assert.Equal(t, "t1", agents.settled.GetId())
	assert.Contains(t, reviewGutterFor(t, m, "t1"), "✓")
}

// reviewGutterFor is the row a thread is drawn on.
func reviewGutterFor(t *testing.T, m model, id string) string {
	t.Helper()

	for y := range m.review.diff.view.Len() {
		if thread, ok := m.review.diff.view.GetThreadAt(y); ok && thread.GetId() == id {
			return ansi.Strip(m.review.diff.view.RenderRow(y, diffview.NoCursor, 120, 0))
		}
	}

	t.Fatalf("thread %q is drawn on no row", id)

	return ""
}

// TestDeletingAnAdriftThreadLeavesTheCursorInTheDiff covers the one write that can pull a row
// out from under the cursor: the section of threads with nowhere to stand is rebuilt on every
// write, and deleting the thread the cursor is on takes its row away.
func TestDeletingAnAdriftThreadLeavesTheCursorInTheDiff(t *testing.T) {
	dir := reviewRepo(t)
	agents := &fakeReviewAgents{}
	m, _ := reviewModel(t, agents, dir)
	m = openReview(t, m)

	agents.threads = []*agentv1.ReviewThread{{
		Id:          "t1",
		FilePath:    "gone.go",
		NewStart:    12,
		QuotedLines: []string{"+what this used to say"},
		Messages:    []*agentv1.ReviewMessage{{Id: "m1", Body: "why did this go?"}},
	}}
	m.review.applyWrote(&m, m.shared(), reviewWroteMsg{agentID: "a1"})
	streamThreads(t, &m, agents.threads)

	next, _ := m.routeScreenMessage(keyMsg("t"))
	m = next.(model)
	require.Equal(t, m.review.diff.view.GetLastRow(), m.review.diff.cursor)

	m.review.applyWrote(&m, m.shared(), reviewWroteMsg{agentID: "a1"})
	streamThreads(t, &m, nil)

	assert.LessOrEqual(t, m.review.diff.cursor, m.review.diff.view.GetLastRow(), "the cursor stays on a row that exists")
	assert.NotPanics(t, func() { m.review.scrollToCursor(m.shared()) })
}

// typeReview presses one key after another, through the panel's own routing.
func typeReview(t *testing.T, m model, keys string) model {
	t.Helper()

	for _, r := range keys {
		next, _ := m.route(keyMsg(string(r)))
		m = next.(model)
	}

	return m
}

// TestReviewSearchFollowsThePatternAsItIsTyped covers the diff moving to what a half-typed
// pattern matches, with the sidebar saying where in the matches the cursor stands.
func TestReviewSearchFollowsThePatternAsItIsTyped(t *testing.T) {
	dir := reviewRepo(t)
	m, _ := reviewModel(t, &fakeReviewAgents{}, dir)
	m = openReview(t, m)

	start := m.review.diff.cursor

	m = typeReview(t, m, "/")
	require.True(t, m.review.diff.filter.typing)

	m = typeReview(t, m, "goodbye")

	require.Equal(t, 1, m.review.diff.view.MatchCount())
	assert.NotEqual(t, start, m.review.diff.cursor, "the diff moved to the match while it was typed")
	assert.Contains(t, ansi.Strip(m.review.view(m.shared())), "1 of 1")

	next, _ := m.route(keyMsg("enter"))
	m = next.(model)

	require.False(t, m.review.diff.filter.typing, "enter keeps the search and closes the prompt")
	shown := ansi.Strip(m.review.view(m.shared()))
	assert.Contains(t, shown, "/ goodbye")
	assert.Contains(t, shown, "1 of 1")
}

// TestReviewSearchCancelsBackToWhereItStarted covers esc at the prompt: the pattern goes, the
// highlighting goes, and the cursor is put back where the reader was before they pressed /.
func TestReviewSearchCancelsBackToWhereItStarted(t *testing.T) {
	dir := reviewRepo(t)
	m, _ := reviewModel(t, &fakeReviewAgents{}, dir)
	m = openReview(t, m)

	start := m.review.diff.cursor

	m = typeReview(t, m, "/")
	m = typeReview(t, m, "goodbye")
	require.NotEqual(t, start, m.review.diff.cursor)

	next, _ := m.route(keyMsg("esc"))
	m = next.(model)

	assert.False(t, m.review.diff.filter.typing)
	assert.Zero(t, m.review.diff.view.MatchCount(), "esc puts the search away")
	assert.NotContains(t, ansi.Strip(m.review.view(m.shared())), "/ goodbye", "and takes the pattern with it")
}

// TestReviewSearchStepsThroughMatches covers n and N, which are the search's now that threads
// have t and T.
func TestReviewSearchStepsThroughMatches(t *testing.T) {
	dir := reviewRepo(t)
	m, _ := reviewModel(t, &fakeReviewAgents{}, dir)
	m = openReview(t, m)

	m = typeReview(t, m, "/")
	m = typeReview(t, m, "return")

	next, _ := m.route(keyMsg("enter"))
	m = next.(model)

	require.Equal(t, 2, m.review.diff.view.MatchCount(), "the line as it was and the line as it is")

	first := m.review.diff.cursor

	next, _ = m.routeScreenMessage(keyMsg("n"))
	m = next.(model)
	second := m.review.diff.cursor
	require.NotEqual(t, first, second)

	next, _ = m.routeScreenMessage(keyMsg("n"))
	m = next.(model)
	assert.Equal(t, first, m.review.diff.cursor, "past the last match is the first again")

	next, _ = m.routeScreenMessage(keyMsg("N"))
	m = next.(model)
	assert.Equal(t, second, m.review.diff.cursor)
}

// TestReviewSearchPaintsWhatItFound covers the highlighting: a match is drawn on its own
// background, so the stage changes even though the cursor has not moved off the row.
func TestReviewSearchPaintsWhatItFound(t *testing.T) {
	dir := reviewRepo(t)
	m, _ := reviewModel(t, &fakeReviewAgents{}, dir)
	m = openReview(t, m)

	was := reviewFrameRaw(t, m)

	m = typeReview(t, m, "/")
	m = typeReview(t, m, "package")

	assert.NotEqual(t, was, reviewFrameRaw(t, m), "the match is painted")
	assert.Contains(t, reviewFrameText(t, m), "package greet", "and the line still reads the same")
}

// TestReviewComposerOwnsTheKeyboard covers typing a message: while the editor is up nothing
// global is bound, so a key that means something on the screen behind it is just a character.
func TestReviewComposerOwnsTheKeyboard(t *testing.T) {
	dir := reviewRepo(t)
	m, _ := reviewModel(t, &fakeReviewAgents{}, dir)
	m = openReview(t, m)

	next, _ := m.routeScreenMessage(keyMsg("c"))
	m = next.(model)
	require.True(t, m.review.threads.composing)

	for _, pressed := range []string{"?", "l", "n"} {
		next, _ = m.route(keyMsg(pressed))
		m = next.(model)
	}

	assert.False(t, m.helpExpanded, "the help key is a character while typing")
	assert.Equal(t, "?ln", m.review.threads.editor.value(), "and so is every other bound key")

	next, _ = m.route(keyMsg("esc"))
	m = next.(model)

	assert.False(t, m.review.threads.composing, "escape is the one key the editor gives up")
}

// TestReviewSearchPromptOwnsTheKeyboard covers a key bound elsewhere being a character in the
// pattern while one is typed, which bites hardest on the help key.
func TestReviewSearchPromptOwnsTheKeyboard(t *testing.T) {
	dir := reviewRepo(t)
	m, _ := reviewModel(t, &fakeReviewAgents{}, dir)
	m = openReview(t, m)

	m = typeReview(t, m, "/go?")
	require.True(t, m.review.diff.filter.typing)

	assert.Equal(t, "go?", m.review.diff.filter.query, "every key typed reaches the pattern")
	assert.False(t, m.helpExpanded, "and none of them reaches the panel's own bindings")
}

// TestReviewSearchWithNoMatchesSaysSo covers the difference between a search that found
// nothing and no search at all, which read the same before.
func TestReviewSearchWithNoMatchesSaysSo(t *testing.T) {
	dir := reviewRepo(t)
	m, _ := reviewModel(t, &fakeReviewAgents{}, dir)
	m = openReview(t, m)

	next, _ := m.routeScreenMessage(keyMsg("n"))
	m = next.(model)
	assert.Equal(t, "nothing to search for yet", m.review.notice)

	m = typeReview(t, m, "/")
	m = typeReview(t, m, "wallaby")

	next, _ = m.route(keyMsg("enter"))
	m = next.(model)

	require.Zero(t, m.review.diff.view.MatchCount())
	assert.Contains(t, ansi.Strip(m.review.view(m.shared())), "wallaby", "the pattern is still standing")

	next, _ = m.routeScreenMessage(keyMsg("n"))
	m = next.(model)
	assert.Equal(t, "no matches", m.review.notice)
}

// TestReviewFileJumpSurvivesARepaint covers a window pinned by a file jump holding across a
// repaint.
func TestReviewFileJumpSurvivesARepaint(t *testing.T) {
	// A jump needs somewhere to land, and the round the other tests share holds one file.
	repo := gittest.NewRepo(t.TempDir())
	repo.Wrote(gittest.WroteParams{
		Tree:  "tree-with-two-files",
		Whole: gittest.Patch + gittest.PatchOfAnotherFile,
		Sources: map[string]string{
			gittest.File: gittest.HeadSource,
			"second.go":  "package greet\n\nfunc Second() string {\n\treturn \"second\"\n}\n",
		},
	})

	agents := &fakeReviewAgents{}
	m, _ := buildReviewModel(t, agents, repo)
	m = openReview(t, m)

	next, _ := m.routeScreenMessage(keyMsg("]"))
	m = next.(model)

	pinned := m.review.diff.top
	require.True(t, m.review.diff.pinTop, "a file jump says where the window goes")

	next, _ = m.routeScreenMessage(keyMsg("l"))
	m = next.(model)
	assert.Equal(t, pinned, m.review.diff.top, "panning redraws the same rows and must not move them")

	m.review.applyWrote(&m, m.shared(), reviewWroteMsg{agentID: "a1"})
	streamThreads(t, &m, nil)
	assert.Equal(t, pinned, m.review.diff.top, "and neither does a write landing")

	next, _ = m.routeScreenMessage(keyMsg("k"))
	m = next.(model)
	assert.False(t, m.review.diff.pinTop, "moving the cursor hands the window back to the margin")
}

// TestReviewSearchLeavesTheAgentsListAlone covers the review keeping its own filter.
func TestReviewSearchLeavesTheAgentsListAlone(t *testing.T) {
	dir := reviewRepo(t)
	m, _ := reviewModel(t, &fakeReviewAgents{}, dir)
	m = openReview(t, m)

	m.agents.cursor = 1
	m.agents.placed = true

	m = typeReview(t, m, "/")
	m = typeReview(t, m, "goodbye")
	require.Positive(t, m.review.diff.view.MatchCount())

	assert.Empty(t, m.filter.query, "the pattern is the review's, not the panel's")
	require.Len(t, m.agents.rows(m.shared()), 2, "so the agents list is still whole behind it")

	m.agents.apply(m.shared(), rowKeyAt(m.agents.rows(m.shared()), m.agents.cursor))
	assert.Equal(t, 1, m.agents.cursor, "and a refresh leaves the cursor where it was")

	// Enter puts the prompt away and keeps the pattern.
	next, _ := m.route(keyMsg("enter"))
	m = next.(model)
	require.False(t, m.review.diff.filter.typing)
	require.True(t, m.review.diff.filter.isStanding())

	next, _ = m.routeScreenMessage(keyMsg("esc"))
	m = next.(model)

	assert.Equal(t, screenAgents, m.screen)
	assert.Equal(t, 1, m.agents.cursor, "so esc lands back on the row it came from")
}

// TestMarkingAFileViewedFoldsItAway covers the toggle itself: the file goes down to its header,
// the mark is written with the shape the file had, and pressing the key again opens it.
func TestMarkingAFileViewedFoldsItAway(t *testing.T) {
	dir := reviewRepo(t)
	agents := &fakeReviewAgents{}
	m, _ := reviewModel(t, agents, dir)
	m = openReview(t, m)

	whole := m.review.diff.view.Len()

	next, cmd := m.routeScreenMessage(keyMsg("v"))
	m = next.(model)
	require.NotNil(t, cmd)

	assert.True(t, m.review.diff.view.IsViewed("greet.go"))
	assert.Less(t, m.review.diff.view.Len(), whole, "the file is down to its header")
	_, onLine := m.review.diff.view.GetAnchor(m.review.diff.cursor)
	assert.False(t, onLine, "the cursor is left on the header, which is no line to write against")

	wrote, ok := cmd().(reviewWroteMsg)
	require.True(t, ok)
	require.NoError(t, wrote.err)

	require.NotNil(t, agents.marked)
	assert.Equal(t, "greet.go", agents.marked.GetFilePath())
	assert.True(t, agents.marked.GetViewed())
	assert.NotEmpty(t, agents.marked.GetAgentId(), "the mark is held against the agent")
	assert.NotEmpty(t, agents.marked.GetFingerprint(), "the mark carries the file's fingerprint")

	next, cmd = m.routeScreenMessage(keyMsg("v"))
	m = next.(model)
	require.NotNil(t, cmd)
	cmd()

	assert.False(t, m.review.diff.view.IsViewed("greet.go"))
	assert.Equal(t, whole, m.review.diff.view.Len())
	assert.False(t, agents.marked.GetViewed(), "the mark is dropped rather than rewritten")
}

// TestAViewedFileComesBackInTheNextRound covers the mark belonging to the round it was made in:
// reopening an unmoved tree finds the file still folded, and a round the agent has written into
// starts with nothing read.
func TestAViewedFileComesBackInTheNextRound(t *testing.T) {
	repo := gittest.NewRepo(t.TempDir())
	agents := &fakeReviewAgents{}
	m, _ := buildReviewModel(t, agents, repo)
	m = openReview(t, m)

	next, cmd := m.routeScreenMessage(keyMsg("v"))
	m = next.(model)
	cmd()

	m = openReview(t, m)
	require.True(t, m.review.diff.view.IsViewed("greet.go"), "nothing changed, so the mark still holds")
	assert.Empty(t, m.review.notice)

	repo.Wrote(gittest.WroteParams{
		Tree:  "tree-written-again",
		Whole: gittest.PatchWrittenOver,
		Sources: map[string]string{
			gittest.File: "package greet\n\nfunc Hello() string {\n\treturn \"written by the agent\"\n}\n",
		},
	})

	m = openReview(t, m)

	assert.Greater(t, m.review.round.GetOrdinal(), int32(1), "the agent writing opened a round")
	assert.False(t, m.review.diff.view.IsViewed("greet.go"), "a new round has read nothing yet")
}

// TestReviewMatchCountSpellsMatchesOffAMatch covers the branch the count falls back to when the
// cursor is not standing on a hit.
func TestReviewMatchCountSpellsMatchesOffAMatch(t *testing.T) {
	dir := reviewRepo(t)
	m, _ := reviewModel(t, &fakeReviewAgents{}, dir)
	m = openReview(t, m)

	m = typeReview(t, m, "/")
	m = typeReview(t, m, "return")

	next, _ := m.route(keyMsg("enter"))
	m = next.(model)
	require.Equal(t, 2, m.review.diff.view.MatchCount())

	for y := range m.review.diff.view.Len() {
		if _, on := m.review.diff.view.MatchNumber(y); !on {
			m.review.diff.cursor = y

			break
		}
	}

	_, on := m.review.diff.view.MatchNumber(m.review.diff.cursor)
	require.False(t, on, "the cursor is standing off every match")

	assert.Contains(t, ansi.Strip(m.review.diff.matchCount()), "2 matches")
}

// TestReviewBaseNarrowsToTheLastReview covers the question a second round is about: the diff
// measured from the round the person last reviewed rather than from the fork point.
func TestReviewBaseNarrowsToTheLastReview(t *testing.T) {
	repo := gittest.NewRepo(t.TempDir())
	agents := &fakeReviewAgents{}
	m, _ := buildReviewModel(t, agents, repo)

	m = openReview(t, m)
	require.Len(t, agents.rounds, 1, "the first look takes a round")
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
	require.Len(t, agents.rounds, 2, "the tree moved, so the second look takes another round")
	require.Equal(t, first.GetBaseSha(), agents.rounds[1].GetBaseSha(), "the base has not moved")
	require.Equal(t, 2, m.review.diff.view.Stat().Files, "the whole change is both files")

	next, cmd := m.routeScreenMessage(keyMsg("<"))
	m = next.(model)
	require.NotNil(t, cmd, "narrowing the base reloads the diff")

	loaded, ok := cmd().(reviewLoadedMsg)
	require.True(t, ok)
	m.review.applyLoaded(&m, m.shared(), loaded)

	assert.Equal(t, 1, m.review.diff.view.Stat().Files, "only the file written since that review")
	assert.Contains(t, ansi.Strip(m.review.view(m.shared())), "round 2 of 2",
		"and the band names the round the reader is standing on")
	assert.NotRegexp(t, `\d+/\d+ viewed`, ansi.Strip(m.review.view(m.shared())),
		"the viewed marks belong to the round rather than to the pair on screen")
	assert.NotContains(t, ansi.Strip(m.review.view(m.shared())), "since you last reviewed",
		"the courtesy sentence describes the pair the diff is already measured over")

	// The walk stops at the oldest round rather than cycling, so the way back to the whole
	// change is the other key.
	next, cmd = m.routeScreenMessage(keyMsg(">"))
	m = next.(model)
	require.NotNil(t, cmd, "stepping forward past the newest round widens the diff back out")

	loaded, ok = cmd().(reviewLoadedMsg)
	require.True(t, ok)
	m.review.applyLoaded(&m, m.shared(), loaded)

	assert.Equal(t, 2, m.review.diff.view.Stat().Files, "the whole change again")
	assert.Contains(t, ansi.Strip(m.review.view(m.shared())), "all changes")
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

// standOnAFinishedRound puts the reader on the older of two rounds, drawn to the snapshot that
// round ended at rather than to the work the agent has since written.
func standOnAFinishedRound(m model) model {
	m.review.diff.chapters = []reviewChapter{
		{ToSHA: "an-older-snapshot", Ordinal: 1},
		{ToSHA: m.review.round.GetSnapshotSha(), Ordinal: 2},
	}
	m.review.diff.chapter = 0
	m.review.diff.head = "an-older-snapshot"
	m.review.diff.view = diffview.NewView(&diff.Patch{SnapshotSHA: m.review.diff.head}, false)

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

// TestReviewBaseSaysSoWhenNothingChangedSinceThatReview covers the round whose tree matches the
// one already reviewed, where narrowing draws nothing and used to throw the reader out of the
// screen with the notice an empty agent gets.
func TestReviewBaseSaysSoWhenNothingChangedSinceThatReview(t *testing.T) {
	repo := gittest.NewRepo(t.TempDir())
	agents := &fakeReviewAgents{}

	m, _ := buildReviewModel(t, agents, repo)
	m = openReview(t, m)
	first := agents.rounds[0]

	agents.reviews = []*agentv1.Review{{
		Id:           "v1",
		RoundId:      first.GetId(),
		RoundOrdinal: first.GetOrdinal(),
		AuthorKind:   agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_HUMAN,
		ReleasedAt:   "2026-09-08T10:00:00Z",
	}}

	// The tree moves and moves back, so the newest round is the reviewed round's snapshot again.
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
	repo.Wrote(gittest.WroteParams{
		Tree:  gittest.UnmovedTree,
		Whole: gittest.Patch,
	})
	m = openReview(t, m)
	require.NotNil(t, m.review.diff.baseRound)

	m = pressReviewBase(t, m)

	require.Equal(t, screenReview, m.screen, "the reader stays on the diff")
	require.NotNil(t, m.review.diff.view, "and keeps the whole change")
	assert.Empty(t, m.review.diff.base, "the base goes back to the fork point")
	assert.Contains(t, m.review.notice, "nothing written")
}

// TestWritingIsRefusedOnAFinishedRound covers the round keys meeting the write keys: an older
// round draws code the agent has rewritten, so the line under the cursor is not in the work a
// comment could ask it to change, and carrying the reader forward would land them somewhere they
// never chose.
func TestWritingIsRefusedOnAFinishedRound(t *testing.T) {
	agents := &fakeReviewAgents{}

	m := buildReviewWithAnEarlierReview(t, gittest.NewRepo(t.TempDir()), agents)
	m = pressReviewBase(t, m)
	require.NotEmpty(t, m.review.diff.base, "the diff narrowed to a round")

	for row := range m.review.diff.view.Len() {
		if found, ok := m.review.diff.view.GetAnchor(row); ok && found.NewStart > 0 {
			m.review.diff.cursor = row

			break
		}
	}

	// The newest round ends at the work in hand, so its new side is the current snapshot and a
	// comment on it is about code the agent still has.
	_, history := m.review.isHistory()
	require.False(t, history, "the newest round is not finished")

	next, _ := m.routeScreenMessage(keyMsg("c"))
	m = next.(model)
	assert.Equal(t, modeReviewing, m.review.pass.mode, "so the key opens the mode that writes")
	assert.True(t, m.review.threads.composing, "on the line it was pressed on")

	// An older round is a different matter, and the same key refuses rather than moving anybody.
	m.review.threads.composing = false
	m.review.pass.mode = modeWatching
	m = standOnAFinishedRound(m)

	said, history := m.review.isHistory()
	require.True(t, history)
	require.Equal(t, "round 1 of 2", said)

	next, _ = m.routeScreenMessage(keyMsg("c"))
	m = next.(model)

	assert.Equal(t, modeWatching, m.review.pass.mode, "the mode did not open")
	assert.False(t, m.review.threads.composing, "and no editor did either")
	assert.Contains(t, m.review.notice, "round 1 of 2 is finished")
	assert.Nil(t, agents.created, "and nothing was written")
}

// TestReviewBaseIsRefusedWhenTheBaseMoved covers two rounds measured from different points, where
// a diff of the two snapshots would carry whatever else moved on the base.
func TestReviewBaseIsRefusedWhenTheBaseMoved(t *testing.T) {
	repo := gittest.NewRepo(t.TempDir())
	agents := &fakeReviewAgents{}
	m, _ := buildReviewModel(t, agents, repo)

	m = openReview(t, m)
	first := agents.rounds[0]
	first.BaseSha = "0000000000000000000000000000000000000000"

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

	assert.Nil(t, m.review.diff.baseRound, "a round measured from somewhere else is not offered")

	// The round is still a stop on the walk.
	next, cmd := m.routeScreenMessage(keyMsg("<"))
	m = next.(model)
	require.NotNil(t, cmd, "the walk still moves")

	shown := ansi.Strip(m.review.view(m.shared()))
	assert.Contains(t, shown, "not comparable, the base moved",
		"and says the base moved rather than drawing somebody else's work")
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

// reviewHelp is the review legend as the frame draws it.
func (m model) reviewHelp() keyMap {
	return m.review.help(m.shared())
}

// holdServedThreads puts what the fake server holds of the review into the world, as the watch
// would.
func holdServedThreads(m *model) {
	if fake, ok := m.api.agents.(*fakeReviewAgents); ok {
		holdThreads(m, fake.threads)
		holdReviews(m, fake.reviews)
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

// streamThreads is the watch delivering the conversation after a write, and the screen following it.
func streamThreads(t *testing.T, m *model, threads []*agentv1.ReviewThread) {
	t.Helper()

	holdThreads(m, threads)
	m.review.followWorld(m.shared())
}
