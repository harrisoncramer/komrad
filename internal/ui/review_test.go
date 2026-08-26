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
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeReviewAgents struct {
	agentv1connect.AgentServiceClient

	threads []*agentv1.ReviewThread

	created *agentv1.CreateReviewThreadRequest
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

	tmux := &recordingTmux{}
	m := infoModel(tmux)
	m.api = api{agents: agents}
	m.width, m.height = 40, 30
	m.listing = listing{
		workflows: []workflowstore.Workflow{
			{ID: "w1", Name: "backend", Worktree: workflowstore.WorktreeConfig{Enabled: true, BaseBranch: "main"}},
		},
		agents: []agentItem{{agent: reviewAgent(dir), alive: true}},
	}

	return m, tmux
}

func reviewAgent(dir string) agentstore.Agent {
	return agentstore.Agent{ID: "a1", Name: "auth", Cwd: dir, WorkflowID: "w1", Pane: "%9"}
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

func TestEnterReviewStagesTheDiff(t *testing.T) {
	dir := reviewRepo(t)
	m, tmux := reviewModel(t, &fakeReviewAgents{}, dir)

	m = openReview(t, m)

	require.Equal(t, screenReview, m.screen)
	require.NotNil(t, m.review.view)

	last := tmux.respawns[len(tmux.respawns)-1]
	assert.Contains(t, last, "less -R -X -Ps -j")
	assert.Contains(t, last, reviewStagePath)

	body, err := os.ReadFile(reviewStagePath)
	require.NoError(t, err)

	rows := strings.Split(string(body), "\n")
	require.Equal(t, m.review.view.Len(), len(rows))
	assert.Contains(t, ansi.Strip(strings.Join(rows, "\n")), "greet.go")

	// The pager is opened on the cursor's own row, one-based.
	assert.Contains(t, last, "+"+strconv.Itoa(m.review.cursor+1))
}

func TestReviewCursorMovesTheStage(t *testing.T) {
	dir := reviewRepo(t)
	m, tmux := reviewModel(t, &fakeReviewAgents{}, dir)
	m = openReview(t, m)

	before := m.review.cursor
	respawns := len(tmux.respawns)

	next, _ := m.updateReview(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	m = next.(model)

	require.Greater(t, m.review.cursor, before)
	require.Greater(t, len(tmux.respawns), respawns)

	last := tmux.respawns[len(tmux.respawns)-1]
	assert.Contains(t, last, "+"+strconv.Itoa(m.review.cursor+1))
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

	next, _ := m.updateReview(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	m = next.(model)
	require.Equal(t, m.review.view.Last(), m.review.cursor, "n should reach the only thread")

	// Standing on it, n has nowhere forward to go and has to come back round to it.
	next, _ = m.updateReview(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	m = next.(model)
	assert.Equal(t, m.review.view.Last(), m.review.cursor)
}

// TestReviewDriftedThreadIsVisible covers a thread whose code has since changed. It cannot be
// pinned to a row, so the cursor can never reach it, and the sidebar has to say it is there.
func TestReviewDriftedThreadIsVisible(t *testing.T) {
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

	_, pinned := m.review.view.ThreadAt(m.review.cursor)
	require.False(t, pinned)

	shown := ansi.Strip(m.reviewView())
	assert.Contains(t, shown, "gone.go:12")
	assert.Contains(t, shown, "moved")
}

// TestReviewViewNeverOutgrowsThePane is the one invariant the sidebar has to hold: bubbletea
// draws whatever it is handed, so a view taller than the pane corrupts the frame and the
// sidebar stops appearing at all. A long conversation and a pile of moved threads is the case
// that breaks it.
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

// TestReviewRendersWhileLoading is the frame the panel drew before the patch arrived. The
// sidebar and its legend are drawn on every frame, so anything reaching into the view has to
// survive not having one yet: this panicked the whole panel, which took its pane with it and
// left the stage's pager filling the window.
func TestReviewRendersWhileLoading(t *testing.T) {
	dir := reviewRepo(t)
	m, _ := reviewModel(t, &fakeReviewAgents{}, dir)

	next, cmd := m.enterReview(reviewAgent(dir))
	m = next.(model)
	require.NotNil(t, cmd)
	require.True(t, m.review.loading)
	require.Nil(t, m.review.view, "the view only exists once the patch has arrived")

	// The legend asks what the compose key would do, which asks the view where the cursor is, so
	// it is drawn expanded here rather than collapsed to a strip that elides it.
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
		{Type: tea.KeyRunes, Runes: []rune("n")},
		{Type: tea.KeyRunes, Runes: []rune("D")},
		{Type: tea.KeyPgDown},
	} {
		next, _ := m.updateReview(key)
		_ = next.(model).reviewView()
	}
}
