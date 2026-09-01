package ui

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"strconv"
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
	threads  []*agentv1.ReviewThread
	settled  *agentv1.SetReviewThreadResolvedRequest
	created  *agentv1.CreateReviewThreadRequest
	replied  *agentv1.AddReviewMessageRequest
	notified *agentv1.NotifyReviewThreadsRequest
}

func (f *fakeReviewAgents) NotifyReviewThreads(
	_ context.Context,
	req *connect.Request[agentv1.NotifyReviewThreadsRequest],
) (*connect.Response[agentv1.NotifyReviewThreadsResponse], error) {
	f.notified = req.Msg

	return connect.NewResponse(&agentv1.NotifyReviewThreadsResponse{Unread: 1}), nil
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
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=weave", "GIT_AUTHOR_EMAIL=weave@example.com",
			"GIT_COMMITTER_NAME=weave", "GIT_COMMITTER_EMAIL=weave@example.com")
		require.NoError(t, cmd.Run(), strings.Join(args, " "))
	}

	run("init", "-b", "main")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "greet.go"),
		[]byte("package greet\n\nfunc Hello() string {\n\treturn \"hello\"\n}\n"), 0o644))
	run("add", ".")
	run("commit", "-m", "first")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "greet.go"),
		[]byte("package greet\n\nfunc Hello() string {\n\treturn \"goodbye\"\n}\n"), 0o644))

	return dir
}

func reviewModel(t *testing.T, agents agentv1connect.AgentServiceClient, dir string) (model, *recordingTmux) {
	t.Helper()

	// The stage's own geometry, which is what the diff is drawn against and what the panel writes
	// a screenful of.
	tmux := &recordingTmux{width: 120, height: 24}
	m := infoModel(tmux)
	m.api = api{agents: agents}
	m.width, m.height = 40, 30
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

	next, cmd := m.enterReview(reviewAgent(m.listing.agents[0].agent.Cwd))
	m = next.(model)
	require.NotNil(t, cmd)

	loaded, ok := cmd().(reviewLoadedMsg)
	require.True(t, ok)
	require.NoError(t, loaded.err)

	m.applyReviewLoaded(loaded)

	return m
}

// TestEnterReviewStagesTheDiff pins that opening a review moves to the review screen and
// draws the diff through the stage pane.
func TestEnterReviewStagesTheDiff(t *testing.T) {
	dir := reviewRepo(t)
	m, tmux := reviewModel(t, &fakeReviewAgents{}, dir)

	m = openReview(t, m)

	require.Equal(t, screenReview, m.screen)
	require.NotNil(t, m.review.view)

	last := tmux.respawns[len(tmux.respawns)-1]
	assert.Equal(t, "weave stage "+reviewStagePath, last,
		"the stage draws the written screen, with nothing to position")

	// The panel writes exactly the screenful the pane can hold, cursor already placed, so the
	// pager has no scrolling of its own to do.
	assert.Equal(t, tmux.height, reviewStageRows(t))
	assert.Contains(t, reviewStageText(t), "greet.go")
}

// TestReviewCursorRepaintsWithoutRestartingThePager covers a cursor move rewriting the screen
// and reloading it, since restarting the pager reached the client as a flicker.
func TestReviewCursorRepaintsWithoutRestartingThePager(t *testing.T) {
	dir := reviewRepo(t)
	m, tmux := reviewModel(t, &fakeReviewAgents{}, dir)
	m = openReview(t, m)

	// Without a colour profile the cursor row paints as bare text and moving it would change
	// nothing in the file.
	restore := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(restore) })

	m.stageReview()

	before, respawns := m.review.cursor, len(tmux.respawns)
	was := reviewStageRaw(t)
	tmux.sentKeys = nil

	next, _ := m.updateReview(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	m = next.(model)

	require.Greater(t, m.review.cursor, before)
	assert.Len(t, tmux.respawns, respawns, "the pager is left running")
	assert.Equal(t, []string{"%1 R"}, tmux.sentKeys, "and told to re-read the screen")

	// Compared raw: a diff that fits the pane does not scroll, so the only thing a move changes is
	// which row is painted as the cursor.
	assert.NotEqual(t, was, reviewStageRaw(t), "the cursor moved, so the screen changed")
	assert.Equal(t, tmux.height, reviewStageRows(t))
}

func reviewStageText(t *testing.T) string {
	t.Helper()

	return ansi.Strip(reviewStageRaw(t))
}

func reviewStageRaw(t *testing.T) string {
	t.Helper()

	body, err := os.ReadFile(reviewStagePath)
	require.NoError(t, err)

	return string(body)
}

func reviewStageRows(t *testing.T) int {
	t.Helper()

	body, err := os.ReadFile(reviewStagePath)
	require.NoError(t, err)

	return strings.Count(string(body), "\n")
}

// TestReviewKeysFollowTheKeymap covers the review screen reading its keys from the config like
// every other screen, since a binding some screens honour and others ignore is worse than one
// nothing honours.
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

// TestReviewPagingSettlesOnALine covers paging past the headers: a page lands on a row the
// cursor is allowed to hold rather than on a file or hunk header, so the keypress is never
// swallowed.
func TestReviewPagingSettlesOnALine(t *testing.T) {
	dir := reviewRepo(t)
	m, _ := reviewModel(t, &fakeReviewAgents{}, dir)
	m = openReview(t, m)

	first := m.review.cursor

	next, _ := m.updateReview(tea.KeyMsg{Type: tea.KeyPgDown})
	m = next.(model)

	assert.Greater(t, m.review.cursor, first)
	_, ok := m.review.view.GetAnchor(m.review.cursor)
	assert.True(t, ok)

	next, _ = m.updateReview(tea.KeyMsg{Type: tea.KeyPgUp})
	m = next.(model)

	assert.Equal(t, first, m.review.cursor)
	_, ok = m.review.view.GetAnchor(m.review.cursor)
	assert.True(t, ok)
}

// TestReviewPagesOnCtrlNAndCtrlP covers the readline pair, which pages the diff the same way the
// page keys do.
func TestReviewPagesOnCtrlNAndCtrlP(t *testing.T) {
	dir := reviewRepo(t)
	m, _ := reviewModel(t, &fakeReviewAgents{}, dir)
	m = openReview(t, m)

	first := m.review.cursor

	next, _ := m.updateReview(tea.KeyMsg{Type: tea.KeyCtrlN})
	m = next.(model)

	assert.Greater(t, m.review.cursor, first)

	next, _ = m.updateReview(tea.KeyMsg{Type: tea.KeyCtrlP})
	m = next.(model)

	assert.Equal(t, first, m.review.cursor)
}

// TestReviewJumpsToASingleThread covers what happens with one thread in the diff: n has to
// reach it from anywhere, including from the thread itself, or it reads as a dead key.
func TestReviewJumpsToASingleThread(t *testing.T) {
	dir := reviewRepo(t)
	agents := &fakeReviewAgents{}
	m, _ := reviewModel(t, agents, dir)
	m = openReview(t, m)

	first := m.review.cursor
	anchor, ok := m.review.view.GetAnchor(m.review.view.Last())
	require.True(t, ok)
	require.NotEqual(t, first, m.review.view.Last())

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
	require.Equal(t, m.review.view.Last(), m.review.cursor, "t should reach the only thread")

	// Standing on it, t has nowhere forward to go and has to stay put.
	next, _ = m.updateReview(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("t")})
	m = next.(model)
	assert.Equal(t, m.review.view.Last(), m.review.cursor)
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

	thread, ok := m.review.view.ThreadAt(m.review.cursor)
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

// TestReviewViewNeverOutgrowsThePane covers the sidebar staying inside the pane, since
// bubbletea draws whatever it is handed and a taller view corrupts the frame.
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

	for _, pane := range []int{8, 12, 24, 50} {
		for _, composing := range []bool{false, true} {
			m.height, m.review.composing = pane, composing

			// Measured the way bubbletea receives it: View trims the trailing newline off the screen.
			height := strings.Count(strings.TrimRight(m.reviewView(), "\n"), "\n") + 1
			assert.LessOrEqual(t, height, pane,
				"the sidebar drew %d lines into a %d line pane, composing=%v", height, pane, composing)
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

	next, cmd := m.enterReview(reviewAgent(m.listing.agents[0].agent.Cwd))
	m = next.(model)
	require.NotNil(t, cmd)

	loaded, ok := cmd().(reviewLoadedMsg)
	require.True(t, ok)
	require.Error(t, loaded.err, "a directory that is not a repository has no diff")

	m.applyReviewLoaded(loaded)

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

// TestReviewKeepsDrawingAfterAWrite covers the stage still repainting after a save, since the
// drawer is started once and a repaint that gave up froze the stage.
func TestReviewKeepsDrawingAfterAWrite(t *testing.T) {
	dir := reviewRepo(t)
	agents := &fakeReviewAgents{}
	m, tmux := reviewModel(t, agents, dir)
	m = openReview(t, m)

	// What saving a comment does to the screen: the threads come back and the rows are dropped,
	// since a gutter mark may have appeared.
	m.applyReviewWrote(reviewWroteMsg{agentID: "a1", notice: "thread opened"})

	respawns := len(tmux.respawns)
	tmux.sentKeys = nil

	next, _ := m.updateReview(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	m = next.(model)

	assert.Len(t, tmux.respawns, respawns, "the drawer is still the one that was started")
	assert.Equal(t, []string{"%1 R"}, tmux.sentKeys, "and it is still being told to redraw")
	assert.Contains(t, reviewStageText(t), "greet.go", "and the screen is drawn again")
	assert.Equal(t, tmux.height, reviewStageRows(t))
}

// TestReviewPanRedrawsTheRows covers the same seam sideways: panning changes what every row
// looks like, so the rows have to be drawn again for it to show.
func TestReviewPanRedrawsTheRows(t *testing.T) {
	dir := reviewRepo(t)
	m, _ := reviewModel(t, &fakeReviewAgents{}, dir)
	m = openReview(t, m)

	was := reviewStageText(t)

	next, _ := m.updateReview(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")})
	m = next.(model)

	require.Positive(t, m.review.pan)
	assert.NotEqual(t, was, reviewStageText(t), "panning moves the text under the gutter")
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

// TestReviewPanStepScalesWithThePane covers how far sideways a press goes, since a fixed few
// characters means dozens of presses to cross a wide diff.
func TestReviewPanStepScalesWithThePane(t *testing.T) {
	dir := reviewRepo(t)
	m, _ := reviewModel(t, &fakeReviewAgents{}, dir)
	m = openReview(t, m)

	assert.Equal(t, 40, m.reviewPanStep(), "a third of the 120 column pane")

	next, _ := m.updateReview(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")})
	m = next.(model)
	assert.Equal(t, 40, m.review.pan)

	// Until the pane has been measured there is nothing to take a third of.
	m.review.frames = &reviewFrames{}
	assert.Equal(t, reviewPanStep, m.reviewPanStep())
}

// TestReviewConversationStateSaysWhoseTurnItIs covers the line at the top of the sidebar, which
// is the one place the review says where it stands.
func TestReviewConversationStateSaysWhoseTurnItIs(t *testing.T) {
	tests := []struct {
		name       string
		threads    []*agentv1.ReviewThread
		unreadMail bool
		want       string
	}{
		{name: "a review with nothing in it", want: "nothing said yet"},
		{
			name: "an answer the reviewer has not read comes before anything else",
			threads: []*agentv1.ReviewThread{{
				Id:            "t1",
				UnreadByAgent: true,
				UnreadByHuman: true,
			}},
			want: "the agent has replied",
		},
		{
			name:    "everything read on both sides",
			threads: []*agentv1.ReviewThread{{Id: "t1"}},
			want:    "the agent is up to date",
		},
		{
			name:       "written, told, and not picked up yet",
			threads:    []*agentv1.ReviewThread{{Id: "t1", UnreadByAgent: true}},
			unreadMail: true,
			want:       "waiting on the agent",
		},
		{
			name:    "written and the agent never told",
			threads: []*agentv1.ReviewThread{{Id: "t1", UnreadByAgent: true}},
			want:    "not sent yet",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := model{
				review: reviewState{agentID: "a1", threads: tt.threads},
				listing: listing{agents: []agentItem{
					{agent: agentstore.Agent{ID: "a1"}, unreadMail: tt.unreadMail},
					{agent: agentstore.Agent{ID: "a2"}, unreadMail: true},
				}},
			}

			assert.Equal(t, tt.want, m.reviewConversationState())
		})
	}
}

// TestReviewNotifyIsTheSubmitKey covers telling the agent its review is waiting on the same key
// that submits everywhere else.
func TestReviewNotifyIsTheSubmitKey(t *testing.T) {
	dir := reviewRepo(t)
	agents := &fakeReviewAgents{}
	m, _ := reviewModel(t, agents, dir)
	m = openReview(t, m)

	next, cmd := m.updateReview(tea.KeyMsg{Type: tea.KeyCtrlS})
	m = next.(model)
	require.NotNil(t, cmd, "the key tells the agent")
	require.False(t, m.review.composing, "and does not open the editor on the way")

	wrote, ok := cmd().(reviewWroteMsg)
	require.True(t, ok)
	require.NoError(t, wrote.err)

	require.NotNil(t, agents.notified)
	assert.Equal(t, "a1", agents.notified.GetAgentId())
}

// TestSettlingAThreadIsWhatQuietsIt covers settling being a key the reviewer presses, since
// nothing about the code moving says a question was answered.
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

	assert.Contains(t, ansi.Strip(m.reviewView()), "everything is settled",
		"the state line is where settling is said, not a line above the messages")

	// Pressing it again reopens, since an answer that was not the end of it has to be able to
	// come back.
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
	assert.Contains(t, ansi.Strip(m.reviewView()), "everything is settled")
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
	require.Equal(t, m.review.view.Last(), m.review.cursor)

	m.applyReviewWrote(reviewWroteMsg{agentID: "a1", threads: nil})

	assert.LessOrEqual(t, m.review.cursor, m.review.view.Last(), "the cursor stays on a row that exists")
	assert.NotPanics(t, func() { m.stageReview() })
}

// typeReview presses one key after another, through the panel's own routing, since the diff's
// search is the panel's filter and the filter takes keys before any screen does.
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

	m.stageReview()
	was := reviewStageRaw(t)

	m = typeReview(t, m, "/")
	m = typeReview(t, m, "package")

	assert.NotEqual(t, was, reviewStageRaw(t), "the match is painted")
	assert.Contains(t, reviewStageText(t), "package greet", "and the line still reads the same")
}

// TestReviewRecentringHoldsTheWindow covers zt overriding the scroll margin, which otherwise
// drags the window back to keep the cursor clear of the edge.
func TestReviewRecentringHoldsTheWindow(t *testing.T) {
	dir := reviewRepo(t)
	m, _ := reviewModel(t, &fakeReviewAgents{}, dir)
	m = openReview(t, m)

	require.Positive(t, m.review.frames.height)

	for range 3 {
		next, _ := m.updateReview(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
		m = next.(model)
	}

	next, _ := m.updateReview(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("z")})
	m = next.(model)
	require.Equal(t, "z", m.review.pending, "z waits for the key that says where")

	next, _ = m.updateReview(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("t")})
	m = next.(model)

	assert.Empty(t, m.review.pending)
	assert.Equal(t, m.review.cursor, m.review.top, "zt puts the cursor's row at the top")
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

// TestReviewRecentringSurvivesARepaint covers a recentred window holding, since the scroll
// margin runs on every repaint rather than only on a move.
func TestReviewRecentringSurvivesARepaint(t *testing.T) {
	dir := reviewRepo(t)
	agents := &fakeReviewAgents{}
	m, _ := reviewModel(t, agents, dir)
	m = openReview(t, m)

	for range 6 {
		next, _ := m.updateReview(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
		m = next.(model)
	}

	for _, key := range []string{"z", "t"} {
		next, _ := m.updateReview(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
		m = next.(model)
	}

	require.Equal(t, m.review.cursor, m.review.top)

	next, _ := m.updateReview(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")})
	m = next.(model)
	assert.Equal(t, m.review.cursor, m.review.top, "panning redraws the same rows and must not move them")

	m.applyReviewWrote(reviewWroteMsg{agentID: "a1", threads: nil})
	assert.Equal(t, m.review.cursor, m.review.top, "and neither does a write landing")

	next, _ = m.updateReview(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
	m = next.(model)
	assert.Less(t, m.review.top, m.review.cursor, "moving the cursor hands the window back to the margin")
}

// TestReviewSearchLeavesTheAgentsListAlone covers the review keeping its own filter, since the
// agents list narrows by the model's filter whatever screen is up.
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
	require.True(t, m.review.filter.standing())

	next, _ = m.updateReview(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(model)

	assert.Equal(t, screenAgents, m.screen)
	assert.Equal(t, 1, m.tree.cursor, "so esc lands back on the row it came from")
}
