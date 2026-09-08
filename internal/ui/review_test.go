package ui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"strconv"
	"weave/internal/git"
	"weave/internal/review"
	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"
	agentv1 "weave/proto/gen/agent/v1"
	"weave/proto/gen/agent/v1/agentv1connect"

	"connectrpc.com/connect"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
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
	rounds     []*agentv1.ReviewRound
	reviews    []*agentv1.Review
	marked     *agentv1.SetReviewFileViewedRequest
	harvested  *agentv1.HarvestRoundEditsRequest
	harvests   int
	harvestErr error
}

// HarvestRoundEdits records what the panel sent rather than reconciling anything.
func (f *fakeReviewAgents) HarvestRoundEdits(
	_ context.Context,
	req *connect.Request[agentv1.HarvestRoundEditsRequest],
) (*connect.Response[agentv1.HarvestRoundEditsResponse], error) {
	f.harvested = req.Msg
	f.harvests++

	if f.harvestErr != nil {
		return nil, f.harvestErr
	}

	return connect.NewResponse(&agentv1.HarvestRoundEditsResponse{
		Opened: int32(len(req.Msg.GetRuns())),
	}), nil
}

func (f *fakeReviewAgents) ListReviewViewedFiles(
	_ context.Context,
	req *connect.Request[agentv1.ListReviewViewedFilesRequest],
) (*connect.Response[agentv1.ListReviewViewedFilesResponse], error) {
	held := make([]*agentv1.ReviewViewedFile, 0, len(f.viewed))
	for _, mark := range f.viewed {
		if mark.GetRoundId() == req.Msg.GetRoundId() {
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
			RoundId:  req.Msg.GetRoundId(),
			FilePath: req.Msg.GetFilePath(),
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

	return connect.NewResponse(&agentv1.ReleaseReviewResponse{Comments: 1}), nil
}

// OpenReviewRound takes a real snapshot of the repository under test.
func (f *fakeReviewAgents) OpenReviewRound(
	ctx context.Context,
	req *connect.Request[agentv1.OpenReviewRoundRequest],
) (*connect.Response[agentv1.OpenReviewRoundResponse], error) {
	snapshot, err := git.TakeSnapshot(ctx, git.TakeSnapshotParams{Dir: f.dir, Base: "main"})
	if err != nil {
		return nil, err
	}

	if len(f.rounds) == 0 || f.rounds[len(f.rounds)-1].GetSnapshotSha() != snapshot.SHA {
		if err := git.HoldRef(ctx, f.dir, "refs/weave/rounds/"+snapshot.SHA, snapshot.SHA); err != nil {
			return nil, err
		}

		f.rounds = append(f.rounds, &agentv1.ReviewRound{
			Id:          fmt.Sprintf("round-%d", len(f.rounds)+1),
			AgentId:     req.Msg.GetAgentId(),
			Ordinal:     int32(len(f.rounds) + 1),
			BaseSha:     snapshot.BaseSHA,
			SnapshotSha: snapshot.SHA,
		})
	}

	return connect.NewResponse(&agentv1.OpenReviewRoundResponse{Round: f.rounds[len(f.rounds)-1]}), nil
}

func (f *fakeReviewAgents) ListReviewRounds(
	context.Context,
	*connect.Request[agentv1.ListReviewRoundsRequest],
) (*connect.Response[agentv1.ListReviewRoundsResponse], error) {
	return connect.NewResponse(&agentv1.ListReviewRoundsResponse{Rounds: f.rounds}), nil
}

func (f *fakeReviewAgents) ListReviews(
	context.Context,
	*connect.Request[agentv1.ListReviewsRequest],
) (*connect.Response[agentv1.ListReviewsResponse], error) {
	return connect.NewResponse(&agentv1.ListReviewsResponse{Reviews: f.reviews}), nil
}

func (f *fakeReviewAgents) AddReviewMessage(
	_ context.Context,
	req *connect.Request[agentv1.AddReviewMessageRequest],
) (*connect.Response[agentv1.AddReviewMessageResponse], error) {
	f.replied = req.Msg

	return connect.NewResponse(&agentv1.AddReviewMessageResponse{
		Message: &agentv1.ReviewMessage{
			Id:       "m2",
			ThreadId: req.Msg.GetThreadId(),
			Body:     req.Msg.GetBody(),
		},
	}), nil
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

	return connect.NewResponse(&agentv1.SetReviewThreadResolvedResponse{}), nil
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

// reviewRepo is a worktree with one committed file and one uncommitted change, which is the
// shape the diff loader expects: a base branch to measure against and work sitting past it.
func reviewRepo(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()

	gitIn(t, dir, "init", "-b", "main")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "greet.go"),
		[]byte("package greet\n\nfunc Hello() string {\n\treturn \"hello\"\n}\n"), 0o644))
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-m", "first")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "greet.go"),
		[]byte("package greet\n\nfunc Hello() string {\n\treturn \"goodbye\"\n}\n"), 0o644))

	return dir
}

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()

	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=weave", "GIT_AUTHOR_EMAIL=weave@example.com",
		"GIT_COMMITTER_NAME=weave", "GIT_COMMITTER_EMAIL=weave@example.com")
	require.NoError(t, cmd.Run(), strings.Join(args, " "))
}

func reviewModel(t *testing.T, agents agentv1connect.AgentServiceClient, dir string) (model, *recordingTmux) {
	t.Helper()

	tmux := &recordingTmux{width: 200, height: 30}
	if fake, ok := agents.(*fakeReviewAgents); ok {
		fake.dir = dir
	}

	m := infoModel(tmux)
	m.api = api{agents: agents}
	m.roundsDir = t.TempDir()
	// The review holds the whole window, so this is the size the rest of the panel's screens are
	// measured at.
	m.width, m.height = 200, 30
	m.listing = listing{
		workflows: []workflowstore.Workflow{
			{
				ID:       "w1",
				Name:     "backend",
				Worktree: workflowstore.WorktreeConfig{Enabled: true, BaseBranch: "main"},
			},
		},
		agents: []agentItem{{agent: reviewAgent(dir), alive: true}},
	}

	return m, tmux
}

func reviewAgent(dir string) agentstore.Agent {
	return agentstore.Agent{
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
	require.NoError(t, m.review.err)

	return m
}

// enterReviewWithoutPickup is the screen opening with the look for an earlier sitting's checkout
// still in flight, which is what a reader who presses a key straight away is looking at.
func enterReviewWithoutPickup(t *testing.T, m model) model {
	t.Helper()

	next, cmd := m.enterReview(reviewAgent(m.listing.agents[0].agent.Cwd))
	m = next.(model)
	require.NotNil(t, cmd)

	loaded, ok := cmd().(reviewLoadedMsg)
	require.True(t, ok)

	m.applyReviewLoaded(loaded)

	return m
}

// enterReview is the same path without the diff having to be readable, for the worktree that
// holds nothing and the one that cannot be read at all.
func enterReview(t *testing.T, m model) model {
	t.Helper()

	next, cmd := m.enterReview(reviewAgent(m.listing.agents[0].agent.Cwd))
	m = next.(model)
	require.NotNil(t, cmd)

	loaded, ok := cmd().(reviewLoadedMsg)
	require.True(t, ok)

	// The load answers with the look for a checkout an earlier sitting left behind, which is part
	// of opening the screen rather than of anything the reader does on it.
	if found := m.applyReviewLoaded(loaded); found != nil {
		if left, ok := found().(roundLeftoverMsg); ok {
			m.applyRoundLeftover(left)
		}
	}

	return m
}

// TestEnterReviewRefusesAnEmptyWorktree pins the drop back to the listing for an agent that has
// written nothing.
func TestEnterReviewRefusesAnEmptyWorktree(t *testing.T) {
	dir := reviewRepo(t)
	gitIn(t, dir, "commit", "-am", "second")

	m, _ := reviewModel(t, &fakeReviewAgents{}, dir)

	m = enterReview(t, m)

	assert.Equal(t, screenAgents, m.screen, "the reader is put back on the listing")
	assert.Zero(t, m.review, "the refused review keeps nothing")
	require.NotEmpty(t, m.messages.items)
	assert.Contains(t, newestMessage(m).summary, "Nothing to review")
	assert.Contains(t, newestMessage(m).detail, "no change")
}

// TestEnterReviewOpensAnUntrackedFile covers the worktree whose only work is a file git has
// never seen, which is a diff to read even though git will draw none of it.
func TestEnterReviewOpensAnUntrackedFile(t *testing.T) {
	dir := reviewRepo(t)
	gitIn(t, dir, "commit", "-am", "second")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "new.go"), []byte("package greet\n"), 0o644))

	m, _ := reviewModel(t, &fakeReviewAgents{}, dir)

	m = openReview(t, m)

	assert.Equal(t, screenReview, m.screen)
	assert.Contains(t, reviewFrameText(t, m), "new.go")
}

// TestEnterReviewDrawsTheDiffBesideTheSidebar pins that opening a review moves to the review
// screen and draws the patch itself, in the panel's own frame rather than in a pane of its own.
func TestEnterReviewDrawsTheDiffBesideTheSidebar(t *testing.T) {
	dir := reviewRepo(t)
	m, tmux := reviewModel(t, &fakeReviewAgents{}, dir)

	m = openReview(t, m)

	require.Equal(t, screenReview, m.screen)
	require.NotNil(t, m.review.view)

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
	restore := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(restore) })

	before, respawns := m.review.cursor, len(tmux.respawns)
	was := reviewFrameRaw(t, m)
	tmux.sentKeys = nil

	next, _ := m.updateReview(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	m = next.(model)

	require.Greater(t, m.review.cursor, before)
	assert.Len(t, tmux.respawns, respawns, "nothing is respawned")
	assert.Empty(t, tmux.sentKeys, "and nothing is tapped to re-read a file")

	// Compared raw: a diff that fits the pane does not scroll, so the only thing a move changes is
	// which row is painted as the cursor.
	assert.NotEqual(t, was, reviewFrameRaw(t, m), "the cursor moved, so the frame changed")
	assert.Equal(t, m.height, reviewFrameRows(t, m))
}

func reviewFrameText(t *testing.T, m model) string {
	t.Helper()

	return ansi.Strip(reviewFrameRaw(t, m))
}

// reviewProse is what the sidebar alone says with its wrapping taken back out, the diff beside
// it left out.
func reviewProse(m model) string {
	return strings.Join(strings.Fields(ansi.Strip(m.reviewSidebarView())), " ")
}

func reviewFrameRaw(t *testing.T, m model) string {
	t.Helper()

	require.Equal(t, screenReview, m.screen)

	return m.View()
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

	rebound, _ := m.updateReview(keyMsg("i"))
	assert.True(t, asModel(t, rebound).review.composing, "the rebound key opens the editor")

	replaced, _ := m.updateReview(keyMsg("c"))
	assert.False(t, asModel(t, replaced).review.composing, "and the key it replaced does nothing")

	assert.Contains(t, m.reviewHelp().short, binding([]string{"i"}, m.reviewComposeDesc()),
		"the legend names the key that is bound rather than the one that shipped")
}

// TestReviewComposeOpensAThread covers the write path: c opens the editor on the line under
// the cursor, ctrl+s sends it, and what is sent is the anchor the cursor was standing on.
func TestReviewComposeOpensAThread(t *testing.T) {
	dir := reviewRepo(t)
	agents := &fakeReviewAgents{}
	m, _ := reviewModel(t, agents, dir)
	m = openReview(t, m)

	next, _ := m.updateReview(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	m = next.(model)
	require.True(t, m.review.composing)

	m.review.editor.SetValue("this should say hello")

	next, cmd := m.updateReview(tea.KeyMsg{Type: tea.KeyCtrlS})
	m = next.(model)
	require.False(t, m.review.composing)
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

	anchor, ok := m.review.view.GetAnchor(m.review.cursor)
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

	m.applyReviewWrote(reviewWroteMsg{agentID: "a1", threads: agents.threads})

	shown := ansi.Strip(m.reviewView())
	assert.Contains(t, shown, "Review · auth")
	assert.Contains(t, shown, "wrong greeting")
	assert.Contains(t, shown, "you")
}

// TestReviewSidebarSizesTheDiff pins the summary beside the diff, which is how many files
// moved and by how many lines.
func TestReviewSidebarSizesTheDiff(t *testing.T) {
	dir := reviewRepo(t)
	m, _ := reviewModel(t, &fakeReviewAgents{}, dir)
	m = openReview(t, m)

	shown := ansi.Strip(m.reviewView())
	assert.Contains(t, shown, "1 file changed")
	assert.Contains(t, shown, "+1")
	assert.Contains(t, shown, "-1")
}

// TestReviewIsRefusedWithoutAWorktree pins that an agent sharing a checkout has no diff of
// its own to review, so the screen is left where it was.
func TestReviewIsRefusedWithoutAWorktree(t *testing.T) {
	m, tmux := reviewModel(t, &fakeReviewAgents{}, t.TempDir())
	m.listing.workflows[0].Worktree.Enabled = false

	next, cmd := m.enterReview(reviewAgent(m.listing.agents[0].agent.Cwd))

	assert.Nil(t, cmd)
	assert.Equal(t, screenAgents, next.(model).screen)
	assert.Empty(t, tmux.respawns)
}

// TestReviewScrollsOnCtrlNAndCtrlP covers the readline pair, which moves a few lines at a time
// and settles on a row the cursor is allowed to hold rather than on a file or hunk header.
func TestReviewScrollsOnCtrlNAndCtrlP(t *testing.T) {
	dir := reviewRepo(t)
	m, _ := reviewModel(t, &fakeReviewAgents{}, dir)
	m = openReview(t, m)

	first := m.review.cursor

	next, _ := m.updateReview(tea.KeyMsg{Type: tea.KeyCtrlN})
	m = next.(model)

	assert.Greater(t, m.review.cursor, first)
	_, ok := m.review.view.GetAnchor(m.review.cursor)
	assert.True(t, ok)

	next, _ = m.updateReview(tea.KeyMsg{Type: tea.KeyCtrlP})
	m = next.(model)

	assert.Equal(t, first, m.review.cursor)
	_, ok = m.review.view.GetAnchor(m.review.cursor)
	assert.True(t, ok)
}

// TestReviewJumpsToASingleThread covers what happens with one thread in the diff: n has to
// reach it from anywhere, including from the thread itself, or it reads as a dead key.
func TestReviewJumpsToASingleThread(t *testing.T) {
	dir := reviewRepo(t)
	agents := &fakeReviewAgents{}
	m, _ := reviewModel(t, agents, dir)
	m = openReview(t, m)

	first := m.review.cursor
	anchor, ok := m.review.view.GetAnchor(m.review.view.GetLastRow())
	require.True(t, ok)
	require.NotEqual(t, first, m.review.view.GetLastRow())

	agents.threads = []*agentv1.ReviewThread{{
		Id:          "t1",
		FilePath:    anchor.FilePath,
		OldStart:    int32(anchor.OldStart),
		NewStart:    int32(anchor.NewStart),
		QuotedLines: anchor.QuotedLines,
	}}
	m.applyReviewWrote(reviewWroteMsg{agentID: "a1", threads: agents.threads})

	next, _ := m.updateReview(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("t")})
	m = next.(model)
	require.Equal(t, m.review.view.GetLastRow(), m.review.cursor, "t should reach the only thread")

	// Standing on it, t has nowhere forward to go and has to stay put.
	next, _ = m.updateReview(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("t")})
	m = next.(model)
	assert.Equal(t, m.review.view.GetLastRow(), m.review.cursor)
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
	m.applyReviewWrote(reviewWroteMsg{agentID: "a1", threads: agents.threads})

	next, _ := m.updateReview(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("t")})
	m = next.(model)

	thread, ok := m.review.view.GetThreadAt(m.review.cursor)
	require.True(t, ok, "t should reach the thread that came adrift")
	require.Equal(t, "t1", thread.GetId())

	shown := ansi.Strip(m.reviewView())
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
	m.applyReviewWrote(reviewWroteMsg{agentID: "a1", threads: agents.threads})

	next, _ := m.updateReview(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("t")})
	m = next.(model)

	next, _ = m.updateReview(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	m = next.(model)
	require.True(t, m.review.composing)
	assert.Equal(t, "t1", m.review.editing, "composing answers the thread rather than opening a new one")
}

// TestReviewViewNeverOutgrowsThePane covers the sidebar staying inside the pane.
func TestReviewViewNeverOutgrowsThePane(t *testing.T) {
	dir := reviewRepo(t)
	agents := &fakeReviewAgents{}
	m, _ := reviewModel(t, agents, dir)
	m.height = 12
	m = openReview(t, m)

	anchor, ok := m.review.view.GetAnchor(m.review.cursor)
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

	m.applyReviewWrote(reviewWroteMsg{agentID: "a1", threads: threads})

	for _, pane := range []int{10, 12, 24, 50} {
		for _, composing := range []bool{false, true} {
			m.height, m.review.composing = pane, composing

			// Measured the way bubbletea receives it: View trims the trailing newline off the screen.
			height := strings.Count(strings.TrimRight(m.reviewView(), "\n"), "\n") + 1
			assert.LessOrEqual(t, height, pane,
				"the sidebar drew %d lines into a %d line pane, composing=%v", height, pane, composing)
		}
	}
}

// reviewLongThread is a conversation taller than a short pane, standing on the line the cursor
// is on so the sidebar draws it.
func reviewLongThread(t *testing.T, m model) []*agentv1.ReviewThread {
	t.Helper()

	anchor, ok := m.review.view.GetAnchor(m.review.cursor)
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

	m.applyReviewWrote(reviewWroteMsg{agentID: "a1", threads: reviewLongThread(t, m)})
	m.height = 14

	require.NotContains(t, ansi.Strip(m.reviewView()), "note 0", "the start of the thread is off the sidebar")

	next, _ := m.updateReview(tea.KeyMsg{Type: tea.KeyCtrlF})
	m = next.(model)
	require.True(t, m.isReviewThreadFocused())

	cursor := m.review.cursor
	for range 20 {
		next, _ = m.updateReview(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
		m = next.(model)
	}

	shown := ansi.Strip(m.reviewView())
	assert.Contains(t, shown, "note 0", "scrolling up reaches the first message")
	assert.NotContains(t, shown, "note 5", "the end of the thread has scrolled away")
	assert.Equal(t, cursor, m.review.cursor, "the diff cursor stays where it was")

	next, _ = m.updateReview(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(model)

	assert.False(t, m.isReviewThreadFocused())
	assert.Equal(t, screenReview, m.screen, "esc hands the keys back rather than leaving the review")
	assert.NotContains(t, ansi.Strip(m.reviewView()), "note 0", "the thread is drawn from its end again")
}

// TestReviewThreadFocusIsRefusedWithoutAThread pins that the key does nothing on a line with no
// conversation on it.
func TestReviewThreadFocusIsRefusedWithoutAThread(t *testing.T) {
	dir := reviewRepo(t)
	m, _ := reviewModel(t, &fakeReviewAgents{}, dir)
	m = openReview(t, m)

	next, _ := m.updateReview(tea.KeyMsg{Type: tea.KeyCtrlF})
	m = next.(model)

	assert.False(t, m.isReviewThreadFocused())
	assert.NotEmpty(t, m.review.notice)
}

// TestReviewThreadCursorMoveDropsTheScroll covers the window being counted from the end of one
// conversation, which says nothing about the next one the cursor lands on.
func TestReviewThreadCursorMoveDropsTheScroll(t *testing.T) {
	dir := reviewRepo(t)
	agents := &fakeReviewAgents{}
	m, _ := reviewModel(t, agents, dir)
	m = openReview(t, m)

	m.applyReviewWrote(reviewWroteMsg{agentID: "a1", threads: reviewLongThread(t, m)})
	m.height = 14

	next, _ := m.updateReview(tea.KeyMsg{Type: tea.KeyCtrlF})
	m = next.(model)
	next, _ = m.updateReview(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
	m = next.(model)
	require.Positive(t, m.review.threadScroll)

	next, _ = m.updateReview(tea.KeyMsg{Type: tea.KeyCtrlN})
	m = next.(model)

	assert.Zero(t, m.review.threadScroll)
	assert.False(t, m.review.threadFocus, "the keys go back to the diff with the thread they were on")
	assert.Equal(t, "read the thread", m.reviewThreadDesc(),
		"the legend says where the keys are rather than where they were")
}

// TestReviewThreadWindowNeverOutgrowsThePane covers the sidebar staying inside the pane at every
// place the conversation can be scrolled to.
func TestReviewThreadWindowNeverOutgrowsThePane(t *testing.T) {
	dir := reviewRepo(t)
	agents := &fakeReviewAgents{}
	m, _ := reviewModel(t, agents, dir)
	m = openReview(t, m)

	m.applyReviewWrote(reviewWroteMsg{agentID: "a1", threads: reviewLongThread(t, m)})
	m.height = 14
	m.review.threadFocus = true

	for _, pane := range []int{8, 10, 14, 30} {
		for _, scroll := range []int{0, 1, 5, 40} {
			m.height, m.review.threadScroll = pane, scroll

			height := strings.Count(strings.TrimRight(m.reviewView(), "\n"), "\n") + 1
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

	next, cmd := m.enterReview(reviewAgent(dir))
	m = next.(model)
	require.NotNil(t, cmd)
	require.True(t, m.review.loading)
	require.Nil(t, m.review.view, "the view only exists once the patch has arrived")

	// The legend asks what the compose key would do, which asks the view where the cursor is.
	m.helpExpanded = true
	m.height = 40

	shown := ansi.Strip(m.reviewView())
	assert.Contains(t, shown, "loading the diff")
	assert.Contains(t, shown, "start a thread")
}

// TestReviewSurvivesAFailedLoad is the other frame with no view: a worktree the diff loader
// could not read at all.
func TestReviewSurvivesAFailedLoad(t *testing.T) {
	m, _ := reviewModel(t, &fakeReviewAgents{}, t.TempDir())

	m = enterReview(t, m)
	require.Error(t, m.review.err, "a directory that is not a repository has no diff")
	require.Equal(t, screenReview, m.screen, "a failure to read is the review's to show, not the listing's")

	shown := ansi.Strip(m.reviewView())
	assert.Contains(t, shown, "failed to find the repository")

	// Every key has to be answerable with no view behind it.
	for _, key := range []tea.KeyMsg{
		{Type: tea.KeyRunes, Runes: []rune("j")},
		{Type: tea.KeyRunes, Runes: []rune("c")},
		{Type: tea.KeyRunes, Runes: []rune("c")},
		{Type: tea.KeyRunes, Runes: []rune("D")},
		{Type: tea.KeyPgDown},
	} {
		next, _ := m.updateReview(key)
		_ = next.(model).reviewView()
	}
}

// TestReviewKeepsDrawingAfterAWrite covers the stage still repainting after a save.
func TestReviewKeepsDrawingAfterAWrite(t *testing.T) {
	dir := reviewRepo(t)
	agents := &fakeReviewAgents{}
	m, tmux := reviewModel(t, agents, dir)
	m = openReview(t, m)

	// What saving a comment does to the screen: the threads come back and the rows are dropped.
	m.applyReviewWrote(reviewWroteMsg{agentID: "a1", notice: "thread opened"})

	respawns := len(tmux.respawns)
	tmux.sentKeys = nil

	next, _ := m.updateReview(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
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

	next, _ := m.updateReview(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")})
	m = next.(model)

	require.Positive(t, m.review.pan)
	assert.NotEqual(t, was, reviewFrameText(t, m), "panning moves the text under the gutter")
}

// TestReviewRepliesInAThread covers the compose key on a line that already carries a thread,
// which answers it rather than opening a second one.
func TestReviewRepliesInAThread(t *testing.T) {
	dir := reviewRepo(t)
	agents := &fakeReviewAgents{}
	m, _ := reviewModel(t, agents, dir)
	m = openReview(t, m)

	anchor, ok := m.review.view.GetAnchor(m.review.cursor)
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
	m.applyReviewWrote(reviewWroteMsg{agentID: "a1", threads: agents.threads})

	m.helpExpanded, m.height = true, 40
	assert.Contains(t, ansi.Strip(m.reviewView()), "reply in this thread",
		"the legend says what the key will do on a line that already has a thread")

	next, _ := m.updateReview(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	m = next.(model)
	require.True(t, m.review.composing)
	require.Equal(t, "t1", m.review.editing, "the reply goes into the thread under the cursor")

	m.review.editor.SetValue("yes, that is it")

	next, cmd := m.updateReview(tea.KeyMsg{Type: tea.KeyCtrlS})
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

	assert.Equal(t, 56, m.reviewPanStep(), "a third of the 170 columns the diff is drawn in")

	next, _ := m.updateReview(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")})
	m = next.(model)
	assert.Equal(t, 56, m.review.pan)

	// Until the pane has been measured there is nothing to take a third of.
	m.width = 0
	assert.Equal(t, reviewPanStep, m.reviewPanStep())
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

// TestReviewDeliveryViewCountsTheComments covers the line at the top of the sidebar, which
// counts the person's own comments by how far each has got rather than summarising the lot of
// them into one phrase.
func TestReviewDeliveryViewCountsTheComments(t *testing.T) {
	tests := []struct {
		name    string
		threads []*agentv1.ReviewThread
		want    string
		absent  string
	}{
		{
			name:   "a review with nothing in it",
			want:   "nothing written yet",
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
			want:   "nothing written yet",
			absent: "waiting",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := model{review: reviewState{
				agentID:  "a1",
				threads:  tt.threads,
				activity: reviewActivityOf(tt.threads),
			}}

			shown := ansi.Strip(m.reviewDeliveryView())
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

// TestReviewReleaseIsTheSubmitKey covers there being one way to send a review, on the same key
// that submits everywhere else, which asks what to send rather than deciding for the reader.
func TestReviewReleaseIsTheSubmitKey(t *testing.T) {
	dir := reviewRepo(t)
	agents := &fakeReviewAgents{}
	m, _ := reviewModel(t, agents, dir)
	m = openReview(t, m)

	next, _ := m.updateReview(tea.KeyMsg{Type: tea.KeyCtrlS})
	m = next.(model)
	require.True(t, m.review.releasing, "the key opens the panel")
	require.False(t, m.review.composing, "and not the line composer")
	require.Nil(t, agents.released, "and sends nothing on its own")

	next, cmd := m.updateReview(tea.KeyMsg{Type: tea.KeyCtrlS})
	m = next.(model)
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

	anchor, ok := m.review.view.GetAnchor(m.review.cursor)
	require.True(t, ok)

	agents.threads = []*agentv1.ReviewThread{{
		Id:          "t1",
		FilePath:    anchor.FilePath,
		OldStart:    int32(anchor.OldStart),
		NewStart:    int32(anchor.NewStart),
		QuotedLines: anchor.QuotedLines,
		Messages:    []*agentv1.ReviewMessage{{Id: "m1", Body: "why did this change?"}},
	}}
	m.applyReviewWrote(reviewWroteMsg{agentID: "a1", threads: agents.threads})

	next, cmd := m.updateReview(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	m = next.(model)
	require.NotNil(t, cmd)

	wrote, ok := cmd().(reviewWroteMsg)
	require.True(t, ok)
	require.NoError(t, wrote.err)
	m.applyReviewWrote(wrote)

	require.NotNil(t, agents.settled)
	assert.Equal(t, "t1", agents.settled.GetId())
	assert.True(t, agents.settled.GetResolved())

	assert.Contains(t, reviewGutterFor(t, m, "t1"), "✓",
		"the gutter is where settling is said, now the line above counts comments instead")

	// Pressing it again reopens.
	next, cmd = m.updateReview(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	m = next.(model)
	require.NotNil(t, cmd)

	wrote, ok = cmd().(reviewWroteMsg)
	require.True(t, ok)
	m.applyReviewWrote(wrote)

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
	m.applyReviewWrote(reviewWroteMsg{agentID: "a1", threads: agents.threads})

	next, _ := m.updateReview(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("t")})
	m = next.(model)

	next, cmd := m.updateReview(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	m = next.(model)
	require.NotNil(t, cmd)

	wrote, ok := cmd().(reviewWroteMsg)
	require.True(t, ok)
	m.applyReviewWrote(wrote)

	require.NotNil(t, agents.settled, "a thread that came adrift settles like any other")
	assert.Equal(t, "t1", agents.settled.GetId())
	assert.Contains(t, reviewGutterFor(t, m, "t1"), "✓")
}

// reviewGutterFor is the row a thread is drawn on, which is where the marks that say how a
// thread stands are.
func reviewGutterFor(t *testing.T, m model, id string) string {
	t.Helper()

	for y := range m.review.view.Len() {
		if thread, ok := m.review.view.GetThreadAt(y); ok && thread.GetId() == id {
			return ansi.Strip(m.review.view.RenderRow(y, review.NoCursor, 120, 0))
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
	m.applyReviewWrote(reviewWroteMsg{agentID: "a1", threads: agents.threads})

	next, _ := m.updateReview(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("t")})
	m = next.(model)
	require.Equal(t, m.review.view.GetLastRow(), m.review.cursor)

	m.applyReviewWrote(reviewWroteMsg{agentID: "a1", threads: nil})

	assert.LessOrEqual(t, m.review.cursor, m.review.view.GetLastRow(), "the cursor stays on a row that exists")
	assert.NotPanics(t, func() { m.scrollReviewToCursor() })
}

// typeReview presses one key after another, through the panel's own routing.
func typeReview(t *testing.T, m model, keys string) model {
	t.Helper()

	for _, r := range keys {
		next, _ := m.route(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
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

	start := m.review.cursor

	m = typeReview(t, m, "/")
	require.True(t, m.review.filter.typing)

	m = typeReview(t, m, "goodbye")

	require.Equal(t, 1, m.review.view.MatchCount())
	assert.NotEqual(t, start, m.review.cursor, "the diff moved to the match while it was typed")
	assert.Contains(t, ansi.Strip(m.reviewView()), "1 of 1")

	next, _ := m.route(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(model)

	require.False(t, m.review.filter.typing, "enter keeps the search and closes the prompt")
	shown := ansi.Strip(m.reviewView())
	assert.Contains(t, shown, "/ goodbye")
	assert.Contains(t, shown, "1 of 1")
}

// TestReviewSearchCancelsBackToWhereItStarted covers esc at the prompt: the pattern goes, the
// highlighting goes, and the cursor is put back where the reader was before they pressed /.
func TestReviewSearchCancelsBackToWhereItStarted(t *testing.T) {
	dir := reviewRepo(t)
	m, _ := reviewModel(t, &fakeReviewAgents{}, dir)
	m = openReview(t, m)

	start := m.review.cursor

	m = typeReview(t, m, "/")
	m = typeReview(t, m, "goodbye")
	require.NotEqual(t, start, m.review.cursor)

	next, _ := m.route(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(model)

	assert.False(t, m.review.filter.typing)
	assert.Zero(t, m.review.view.MatchCount(), "esc puts the search away")
	assert.NotContains(t, ansi.Strip(m.reviewView()), "/ goodbye", "and takes the pattern with it")
}

// TestReviewSearchStepsThroughMatches covers n and N, which are the search's now that threads
// have t and T.
func TestReviewSearchStepsThroughMatches(t *testing.T) {
	dir := reviewRepo(t)
	m, _ := reviewModel(t, &fakeReviewAgents{}, dir)
	m = openReview(t, m)

	m = typeReview(t, m, "/")
	m = typeReview(t, m, "return")

	next, _ := m.route(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(model)

	require.Equal(t, 2, m.review.view.MatchCount(), "the line as it was and the line as it is")

	first := m.review.cursor

	next, _ = m.updateReview(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	m = next.(model)
	second := m.review.cursor
	require.NotEqual(t, first, second)

	next, _ = m.updateReview(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	m = next.(model)
	assert.Equal(t, first, m.review.cursor, "past the last match is the first again")

	next, _ = m.updateReview(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("N")})
	m = next.(model)
	assert.Equal(t, second, m.review.cursor)
}

// TestReviewSearchPaintsWhatItFound covers the highlighting: a match is drawn on its own
// background, so the stage changes even though the cursor has not moved off the row.
func TestReviewSearchPaintsWhatItFound(t *testing.T) {
	dir := reviewRepo(t)
	m, _ := reviewModel(t, &fakeReviewAgents{}, dir)
	m = openReview(t, m)

	restore := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(restore) })

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

	next, _ := m.updateReview(keyMsg("c"))
	m = next.(model)
	require.True(t, m.review.composing)

	for _, pressed := range []string{"?", "l", "n"} {
		next, _ = m.route(keyMsg(pressed))
		m = next.(model)
	}

	assert.False(t, m.helpExpanded, "the help key is a character while typing")
	assert.Equal(t, "?ln", m.review.editor.Value(), "and so is every other bound key")

	next, _ = m.route(keyMsg("esc"))
	m = next.(model)

	assert.False(t, m.review.composing, "escape is the one key the editor gives up")
}

// TestReviewSearchPromptOwnsTheKeyboard covers a key bound elsewhere being a character in the
// pattern while one is typed, which bites hardest on the help key.
func TestReviewSearchPromptOwnsTheKeyboard(t *testing.T) {
	dir := reviewRepo(t)
	m, _ := reviewModel(t, &fakeReviewAgents{}, dir)
	m = openReview(t, m)

	m = typeReview(t, m, "/go?")
	require.True(t, m.review.filter.typing)

	assert.Equal(t, "go?", m.review.filter.query, "every key typed reaches the pattern")
	assert.False(t, m.helpExpanded, "and none of them reaches the panel's own bindings")
}

// TestReviewSearchWithNoMatchesSaysSo covers the difference between a search that found
// nothing and no search at all, which read the same before.
func TestReviewSearchWithNoMatchesSaysSo(t *testing.T) {
	dir := reviewRepo(t)
	m, _ := reviewModel(t, &fakeReviewAgents{}, dir)
	m = openReview(t, m)

	next, _ := m.updateReview(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	m = next.(model)
	assert.Equal(t, "nothing to search for yet", m.review.notice)

	m = typeReview(t, m, "/")
	m = typeReview(t, m, "wallaby")

	next, _ = m.route(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(model)

	require.Zero(t, m.review.view.MatchCount())
	assert.Contains(t, ansi.Strip(m.reviewView()), "wallaby", "the pattern is still standing")

	next, _ = m.updateReview(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	m = next.(model)
	assert.Equal(t, "no matches", m.review.notice)
}

// TestReviewFileJumpSurvivesARepaint covers a window pinned by a file jump holding across a
// repaint.
func TestReviewFileJumpSurvivesARepaint(t *testing.T) {
	dir := reviewRepo(t)

	// A jump needs somewhere to land, and the repository the other tests share holds one file.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "second.go"),
		[]byte("package greet\n\nfunc Second() string {\n\treturn \"second\"\n}\n"), 0o644))

	agents := &fakeReviewAgents{}
	m, _ := reviewModel(t, agents, dir)
	m = openReview(t, m)

	next, _ := m.updateReview(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("]")})
	m = next.(model)

	pinned := m.review.top
	require.True(t, m.review.pinTop, "a file jump says where the window goes")

	next, _ = m.updateReview(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")})
	m = next.(model)
	assert.Equal(t, pinned, m.review.top, "panning redraws the same rows and must not move them")

	m.applyReviewWrote(reviewWroteMsg{agentID: "a1", threads: nil})
	assert.Equal(t, pinned, m.review.top, "and neither does a write landing")

	next, _ = m.updateReview(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
	m = next.(model)
	assert.False(t, m.review.pinTop, "moving the cursor hands the window back to the margin")
}

// TestReviewSearchLeavesTheAgentsListAlone covers the review keeping its own filter.
func TestReviewSearchLeavesTheAgentsListAlone(t *testing.T) {
	dir := reviewRepo(t)
	m, _ := reviewModel(t, &fakeReviewAgents{}, dir)
	m = openReview(t, m)

	m.tree.cursor = 1
	m.tree.placed = true

	m = typeReview(t, m, "/")
	m = typeReview(t, m, "goodbye")
	require.Positive(t, m.review.view.MatchCount())

	assert.Empty(t, m.filter.query, "the pattern is the review's, not the panel's")
	require.Len(t, m.agentRows(), 2, "so the agents list is still whole behind it")

	m.placeTreeCursor(rowKeyAt(m.agentRows(), m.tree.cursor))
	assert.Equal(t, 1, m.tree.cursor, "and a refresh leaves the cursor where it was")

	// Enter puts the prompt away and keeps the pattern, which is the state the bug needed: a
	// search standing while the reader walks the diff and then leaves.
	next, _ := m.route(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(model)
	require.False(t, m.review.filter.typing)
	require.True(t, m.review.filter.isStanding())

	next, _ = m.updateReview(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(model)

	assert.Equal(t, screenAgents, m.screen)
	assert.Equal(t, 1, m.tree.cursor, "so esc lands back on the row it came from")
}

// TestMarkingAFileViewedFoldsItAway covers the toggle itself: the file goes down to its header,
// the mark is written with the shape the file had, and pressing the key again opens it.
func TestMarkingAFileViewedFoldsItAway(t *testing.T) {
	dir := reviewRepo(t)
	agents := &fakeReviewAgents{}
	m, _ := reviewModel(t, agents, dir)
	m = openReview(t, m)

	whole := m.review.view.Len()

	next, cmd := m.updateReview(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("v")})
	m = next.(model)
	require.NotNil(t, cmd)

	assert.True(t, m.review.view.IsViewed("greet.go"))
	assert.Less(t, m.review.view.Len(), whole, "the file is down to its header")
	_, onLine := m.review.view.GetAnchor(m.review.cursor)
	assert.False(t, onLine, "the cursor is left on the header, which is no line to write against")

	wrote, ok := cmd().(reviewWroteMsg)
	require.True(t, ok)
	require.NoError(t, wrote.err)

	require.NotNil(t, agents.marked)
	assert.Equal(t, "greet.go", agents.marked.GetFilePath())
	assert.True(t, agents.marked.GetViewed())
	assert.NotEmpty(t, agents.marked.GetRoundId(), "the mark is held against the round it was made in")

	next, cmd = m.updateReview(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("v")})
	m = next.(model)
	require.NotNil(t, cmd)
	cmd()

	assert.False(t, m.review.view.IsViewed("greet.go"))
	assert.Equal(t, whole, m.review.view.Len())
	assert.False(t, agents.marked.GetViewed(), "the mark is dropped rather than rewritten")
}

// TestAViewedFileComesBackInTheNextRound covers the mark belonging to the round it was made in:
// reopening an unmoved tree finds the file still folded, and a round the agent has written into
// starts with nothing read.
func TestAViewedFileComesBackInTheNextRound(t *testing.T) {
	dir := reviewRepo(t)
	agents := &fakeReviewAgents{}
	m, _ := reviewModel(t, agents, dir)
	m = openReview(t, m)

	next, cmd := m.updateReview(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("v")})
	m = next.(model)
	cmd()

	m = openReview(t, m)
	require.True(t, m.review.view.IsViewed("greet.go"), "nothing changed, so the mark still holds")
	assert.Empty(t, m.review.notice)

	require.NoError(t, os.WriteFile(filepath.Join(dir, "greet.go"),
		[]byte("package greet\n\nfunc Hello() string {\n\treturn \"hi again\"\n}\n"), 0o644))

	m = openReview(t, m)

	assert.Greater(t, m.review.round.GetOrdinal(), int32(1), "the agent writing opened a round")
	assert.False(t, m.review.view.IsViewed("greet.go"), "a new round has read nothing yet")
}

// TestReviewMatchCountSpellsMatchesOffAMatch covers the branch the count falls back to when the
// cursor is not standing on a hit, which is the only place it says the word rather than a number.
func TestReviewMatchCountSpellsMatchesOffAMatch(t *testing.T) {
	dir := reviewRepo(t)
	m, _ := reviewModel(t, &fakeReviewAgents{}, dir)
	m = openReview(t, m)

	m = typeReview(t, m, "/")
	m = typeReview(t, m, "return")

	next, _ := m.route(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(model)
	require.Equal(t, 2, m.review.view.MatchCount())

	for y := range m.review.view.Len() {
		if _, on := m.review.view.MatchNumber(y); !on {
			m.review.cursor = y

			break
		}
	}

	_, on := m.review.view.MatchNumber(m.review.cursor)
	require.False(t, on, "the cursor is standing off every match")

	assert.Contains(t, ansi.Strip(m.reviewMatchCount()), "2 matches")
}

// TestReviewBaseNarrowsToTheLastReview covers the question a second round is about: the diff
// measured from the round the person last reviewed rather than from the fork point.
func TestReviewBaseNarrowsToTheLastReview(t *testing.T) {
	dir := reviewRepo(t)
	agents := &fakeReviewAgents{}
	m, _ := reviewModel(t, agents, dir)

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

	require.NoError(t, os.WriteFile(filepath.Join(dir, "second.go"),
		[]byte("package greet\n\nfunc Second() {}\n"), 0o644))

	m = openReview(t, m)
	require.Len(t, agents.rounds, 2, "the tree moved, so the second look takes another round")
	require.Equal(t, first.GetBaseSha(), agents.rounds[1].GetBaseSha(), "the base has not moved")
	require.Equal(t, 2, m.review.view.Stat().Files, "the whole change is both files")

	next, cmd := m.updateReview(keyMsg("d"))
	m = next.(model)
	require.NotNil(t, cmd, "narrowing the base reloads the diff")

	loaded, ok := cmd().(reviewLoadedMsg)
	require.True(t, ok)
	m.applyReviewLoaded(loaded)

	assert.Equal(t, 1, m.review.view.Stat().Files, "only the file written since that review")
	assert.Contains(t, ansi.Strip(m.reviewView()), "since round 1")
	assert.NotRegexp(t, `\d+/\d+ viewed`, ansi.Strip(m.reviewView()),
		"the viewed marks belong to the round rather than to the pair on screen")
	assert.NotContains(t, ansi.Strip(m.reviewView()), "since you last reviewed",
		"the courtesy sentence describes the pair the diff is already measured over")

	next, cmd = m.updateReview(keyMsg("d"))
	m = next.(model)
	require.NotNil(t, cmd, "pressing it again widens the diff back out")

	loaded, ok = cmd().(reviewLoadedMsg)
	require.True(t, ok)
	m.applyReviewLoaded(loaded)

	assert.Equal(t, 2, m.review.view.Stat().Files, "the whole change again")
	assert.NotContains(t, ansi.Strip(m.reviewView()), "since round 1")
}

// reviewWithAnEarlierReview is the panel on a second round with the first one reviewed and
// released, which is the state every base key case starts from.
func reviewWithAnEarlierReview(t *testing.T, dir string, agents *fakeReviewAgents) model {
	t.Helper()

	m, _ := reviewModel(t, agents, dir)
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

	require.NoError(t, os.WriteFile(filepath.Join(dir, "second.go"),
		[]byte("package greet\n\nfunc Second() {}\n"), 0o644))

	m = openReview(t, m)
	require.Len(t, agents.rounds, 2)
	require.NotNil(t, m.review.baseRound)

	return m
}

// pressReviewBase walks the base key the way the panel does, since narrowing answers off the UI
// thread and the reply has to be applied for the screen to change.
func pressReviewBase(t *testing.T, m model) model {
	t.Helper()

	next, cmd := m.updateReview(keyMsg("d"))
	m = next.(model)
	require.NotNil(t, cmd, "the base key reloads the diff")

	msg := cmd()
	loaded, ok := msg.(reviewLoadedMsg)
	require.True(t, ok, "the reload answers with a load")

	// A load can answer with more than one thing: a refused narrowing asks for the whole change
	// again, and an earlier sitting's checkout reports itself.
	if again := m.applyReviewLoaded(loaded); again != nil {
		switch answer := again().(type) {
		case reviewLoadedMsg:
			m.applyReviewLoaded(answer)
		case roundLeftoverMsg:
			m.applyRoundLeftover(answer)
		}
	}

	return m
}

// TestReviewBaseSaysSoWhenNothingChangedSinceThatReview covers the round whose tree matches the
// one already reviewed, where narrowing draws nothing and used to throw the reader out of the
// screen with the notice an empty agent gets.
func TestReviewBaseSaysSoWhenNothingChangedSinceThatReview(t *testing.T) {
	dir := reviewRepo(t)
	agents := &fakeReviewAgents{}

	m, _ := reviewModel(t, agents, dir)
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
	require.NoError(t, os.WriteFile(filepath.Join(dir, "second.go"), []byte("package greet\n"), 0o644))
	m = openReview(t, m)
	require.NoError(t, os.Remove(filepath.Join(dir, "second.go")))
	m = openReview(t, m)
	require.NotNil(t, m.review.baseRound)

	m = pressReviewBase(t, m)

	require.Equal(t, screenReview, m.screen, "the reader stays on the diff")
	require.NotNil(t, m.review.view, "and keeps the whole change")
	assert.Empty(t, m.review.base, "the base goes back to the fork point")
	assert.Contains(t, m.review.notice, "nothing written")
}

// TestReviewBaseRecordsWhatItWasMeasuredFrom covers the durable half: a comment written while the
// diff is narrowed records the commit its old side was counted from, so a later reader is not left
// assuming the round's own base.
func TestReviewBaseRecordsWhatItWasMeasuredFrom(t *testing.T) {
	dir := reviewRepo(t)
	agents := &fakeReviewAgents{}

	m := reviewWithAnEarlierReview(t, dir, agents)
	m = pressReviewBase(t, m)
	require.NotEmpty(t, m.review.base)

	for row := 0; row < m.review.view.Len(); row++ {
		if found, ok := m.review.view.GetAnchor(row); ok && found.NewStart > 0 {
			m.review.cursor = row

			break
		}
	}

	next, _ := m.updateReview(keyMsg("c"))
	m = next.(model)
	require.True(t, m.review.composing, "the composer opens on a narrowed diff")

	m.review.editor.SetValue("this line, in the work since I last looked")
	next, cmd := m.updateReviewComposer(keyMsg("ctrl+s"))
	m = next.(model)
	require.NotNil(t, cmd)
	cmd()

	require.NotNil(t, agents.created, "the thread reached the service")
	assert.Equal(t, m.review.base, agents.created.GetBaseSha(),
		"and says which commit its old side was counted from")
	assert.Positive(t, agents.created.GetNewStart())
}

// TestReviewBaseIsRefusedWhenTheBaseMoved covers two rounds measured from different points, where
// a diff of the two snapshots would carry whatever else moved on the base.
func TestReviewBaseIsRefusedWhenTheBaseMoved(t *testing.T) {
	dir := reviewRepo(t)
	agents := &fakeReviewAgents{}
	m, _ := reviewModel(t, agents, dir)

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

	require.NoError(t, os.WriteFile(filepath.Join(dir, "second.go"),
		[]byte("package greet\n\nfunc Second() {}\n"), 0o644))

	m = openReview(t, m)

	assert.Nil(t, m.review.baseRound, "a round measured from somewhere else is not offered")

	next, cmd := m.updateReview(keyMsg("d"))

	assert.Nil(t, cmd, "and the key does nothing")
	assert.Empty(t, next.(model).review.base)
}
