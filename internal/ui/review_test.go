package ui_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"komrad/config"
	"komrad/internal/gittest"
	"komrad/internal/reviews/history"
	"komrad/internal/ui"
	"komrad/internal/ui/paneltest"
	"komrad/internal/ui/screen"
	"komrad/lib/dates"
	"komrad/lib/xptr"
	"komrad/lib/xslice"
	agentv1 "komrad/proto/gen/agent/v1"
	"komrad/proto/gen/agent/v1/agentv1connect"
	taskv1 "komrad/proto/gen/task/v1"
	watchv1 "komrad/proto/gen/watch/v1"
	workflowv1 "komrad/proto/gen/workflow/v1"
	worktreev1 "komrad/proto/gen/worktree/v1"
	"komrad/proto/gen/worktree/v1/worktreev1connect"

	tea "charm.land/bubbletea/v2"
	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// TestReviewOpen covers which agents the review key opens a diff for and what it renders.
func TestReviewOpen(t *testing.T) {
	t.Run("an agent that has written nothing is refused", func(t *testing.T) {
		repo := gittest.NewRepo(t.TempDir())
		repo.Wrote(gittest.WroteParams{Tree: "tree-of-the-base"})

		harness := openReview(t, newReviewServerOver(repo))

		frame := harness.Frame()
		assert.Equal(t, screen.Agents, paneltest.Screen(harness.Frame()))
		assert.Contains(t, frame, "Nothing to review in auth")
		assert.Contains(t, readOpenMessages(harness), "no change against the base branch")
	})

	t.Run("a task's row opens the review of its task", func(t *testing.T) {
		server := newReviewServer(t)
		server.task.Slug = "login-fix"
		harness := paneltest.Start(t, paneltest.Params{
			Width:  reviewWidth,
			Height: reviewHeight,
			Git:    server.repo,
		})
		server.serve(harness)
		harness.Watch(server.buildSnapshot())
		require.Contains(t, paneltest.HoveredRow(harness.Frame()), "login-fix")

		harness.Press("space", "r")

		assert.True(t, isBeside(harness))
		harness.AssertSentTo(agentv1connect.AgentServiceOpenReviewHistoryProcedure, &agentv1.OpenReviewHistoryRequest{Task: &agentv1.OpenReviewHistoryRequest_TaskId{TaskId: "t1"}})
	})

	t.Run("an agent whose worktree was removed opens from its workflow's directory", func(t *testing.T) {
		server := newReviewServer(t)
		server.task.WorktreeRemovalReason = taskv1.WorktreeRemovalReason_WORKTREE_REMOVAL_REASON_GONE
		server.task.Snapshots = 1
		server.workflow.Cwd = server.repo.Root

		harness := openReview(t, server)

		assert.True(t, isBeside(harness))
		assert.Contains(t, paneltest.Stage(harness.Frame()), "greet.go")
	})

	t.Run("an agent whose worktree was removed in a workflow with no directory is not opened", func(t *testing.T) {
		server := newReviewServer(t)
		server.task.WorktreeRemovalReason = taskv1.WorktreeRemovalReason_WORKTREE_REMOVAL_REASON_GONE
		server.task.Snapshots = 1

		harness := openReview(t, server)

		assert.Equal(t, screen.Agents, paneltest.Screen(harness.Frame()))
		assert.Empty(t, harness.Sent(agentv1connect.AgentServiceOpenReviewHistoryProcedure))
	})

	t.Run("an agent whose worktree was removed with no snapshot is not opened", func(t *testing.T) {
		server := newReviewServer(t)
		server.task.WorktreeRemovalReason = taskv1.WorktreeRemovalReason_WORKTREE_REMOVAL_REASON_GONE
		server.workflow.Cwd = server.repo.Root

		harness := openReview(t, server)

		assert.Equal(t, screen.Agents, paneltest.Screen(harness.Frame()))
		assert.Empty(t, harness.Sent(agentv1connect.AgentServiceOpenReviewHistoryProcedure))
	})

	t.Run("an agent part way through a rebase opens on its newest snapshot", func(t *testing.T) {
		server := newEarlierReviewServer(t)
		server.takeErr = connect.NewError(connect.CodeFailedPrecondition, errors.New("a rebase is part way through"))

		harness := openReview(t, server)

		frame := harness.Frame()
		assert.True(t, isBeside(harness))
		assert.Contains(t, paneltest.SidebarProse(frame), "a rebase or merge is part way through, so this is the work as it stood before it")
		assert.Contains(t, paneltest.SidebarProse(frame), "1 file")
	})

	t.Run("a diff git cannot read is refused with the reason", func(t *testing.T) {
		repo := gittest.NewRepo(t.TempDir())
		repo.RootErr = gittest.ErrNoGit

		harness := openReview(t, newReviewServerOver(repo))

		frame := harness.Frame()
		assert.Equal(t, screen.Agents, paneltest.Screen(harness.Frame()))
		assert.Contains(t, frame, "Could not read the diff of")
		assert.Contains(t, readOpenMessages(harness), gittest.ErrNoGit.Error())
	})

	t.Run("a running agent stays on the stage beside the diff", func(t *testing.T) {
		server := newReviewServer(t)
		harness := paneltest.Start(t, paneltest.Params{Width: reviewWidth, Height: reviewHeight, Git: server.repo})
		server.serve(harness)
		harness.Watch(server.buildSnapshot())
		harness.Press("space")
		menuUp := len(harness.Tmux().Zooms())

		harness.Press("r")

		frame := harness.Frame()
		assert.False(t, harness.Tmux().IsZoomed())
		assert.True(t, isBeside(harness))
		assert.Equal(t, server.agent.GetPaneId(), harness.Tmux().GuestPane())
		assert.Equal(t, []bool{false}, harness.Tmux().Zooms()[menuUp:], "the menu's zoom is let go once")
		assert.Contains(t, paneltest.Stage(frame), "greet.go")
		assert.Contains(t, paneltest.SidebarProse(frame), "1 file, +1 -1")
		assert.Len(t, paneltest.SplitLines(frame), reviewHeight)
	})

	t.Run("a stopped agent's diff is rendered beside the sidebar over the whole window", func(t *testing.T) {
		server := newReviewServer(t)
		server.agent.Alive = xptr.Ptr(false)
		harness := openReview(t, server)

		frame := harness.Frame()
		assert.True(t, harness.Tmux().IsZoomed())
		assert.False(t, isBeside(harness))
		assert.Contains(t, paneltest.Stage(frame), "greet.go")
		assert.Len(t, paneltest.SplitLines(frame), reviewHeight)
	})

	t.Run("a running agent stopping hands the diff the whole window", func(t *testing.T) {
		server := newReviewServer(t)
		harness := openReview(t, server)
		require.True(t, isBeside(harness))

		server.agent.Alive = xptr.Ptr(false)
		harness.Watch(server.buildSnapshot())

		assert.True(t, harness.Tmux().IsZoomed())
		assert.False(t, isBeside(harness))
		assert.Contains(t, paneltest.Stage(harness.Frame()), "greet.go")
	})

	t.Run("a window too narrow for the agent beside the diff hands the diff the whole window", func(t *testing.T) {
		harness := openReviewAt(t, newReviewServer(t), paneltest.Params{Width: 120})

		assert.True(t, harness.Tmux().IsZoomed())
		assert.False(t, isBeside(harness))
		assert.Contains(t, paneltest.Stage(harness.Frame()), "greet.go")
	})

	t.Run("the window narrowing under the review hands the diff the whole window", func(t *testing.T) {
		harness := openReview(t, newReviewServer(t))
		require.True(t, isBeside(harness))

		harness.Tmux().SetWindowWidth(120)
		harness.Send(tea.WindowSizeMsg{Width: 120, Height: reviewHeight})

		assert.True(t, harness.Tmux().IsZoomed())
		assert.False(t, isBeside(harness))
	})

	t.Run("the frame renders while the diff loads", func(t *testing.T) {
		server := newReviewServer(t)
		harness := paneltest.Start(t, paneltest.Params{
			Width:  reviewWidth,
			Height: 40,
			Git:    server.repo,
		})
		server.serve(harness)
		harness.Watch(server.buildSnapshot())
		harness.Hold("git.Diff")

		harness.Press("space", "r", "?")

		frame := harness.Frame()
		assert.Contains(t, paneltest.StageText(frame), "loading the diff")
		assert.Contains(t, frame, "hide help")
	})

	t.Run("a diff that loads under the menu still lands", func(t *testing.T) {
		server := newReviewServer(t)
		harness := paneltest.Start(t, paneltest.Params{
			Width:  reviewWidth,
			Height: 40,
			Git:    server.repo,
		})
		server.serve(harness)
		harness.Watch(server.buildSnapshot())
		harness.Hold("git.Diff")
		harness.Press("space", "r", "space")
		require.Contains(t, harness.Frame(), "wrap lines")

		harness.Release("git.Diff")
		harness.Press("esc")

		assert.Contains(t, paneltest.Stage(harness.Frame()), "greet.go")
	})

	t.Run("an agent that has written nothing is refused under the menu, and the menu goes with the review", func(t *testing.T) {
		repo := gittest.NewRepo(t.TempDir())
		repo.Wrote(gittest.WroteParams{Tree: "tree-of-the-base"})
		server := newReviewServerOver(repo)
		harness := paneltest.Start(t, paneltest.Params{
			Width:  reviewWidth,
			Height: 40,
			Git:    server.repo,
		})
		server.serve(harness)
		harness.Watch(server.buildSnapshot())
		harness.Hold("git.Diff")
		harness.Press("space", "r", "space")
		require.Contains(t, harness.Frame(), "wrap lines")

		harness.Release("git.Diff")

		frame := harness.Frame()
		assert.Equal(t, screen.Agents, paneltest.Screen(frame))
		assert.NotContains(t, frame, "wrap lines")
	})

	t.Run("the wheel while the diff loads moves nothing", func(t *testing.T) {
		server := newReviewServer(t)
		harness := paneltest.Start(t, paneltest.Params{
			Width:  reviewWidth,
			Height: 40,
			Git:    server.repo,
		})
		server.serve(harness)
		harness.Watch(server.buildSnapshot())
		harness.Hold("git.Diff")
		harness.Press("space", "r")

		for _, x := range []int{1, reviewWidth - 1} {
			harness.Send(tea.MouseWheelMsg{
				X:      x,
				Y:      2,
				Button: tea.MouseWheelDown,
			})
		}

		assert.Contains(t, paneltest.StageText(harness.Frame()), "loading the diff")
	})

	t.Run("a worktree holding only an untracked file opens", func(t *testing.T) {
		repo := gittest.NewRepo(t.TempDir())
		repo.Wrote(gittest.WroteParams{
			Tree:    "tree-with-an-untracked-file",
			Whole:   gittest.PatchOfAnAddedFile,
			Sources: map[string]string{"new.go": "package greet\n"},
		})

		harness := openReview(t, newReviewServerOver(repo))

		assert.Contains(t, paneltest.Stage(harness.Frame()), "new.go")
	})

	t.Run("an agent in a workflow that cuts no worktrees opens", func(t *testing.T) {
		server := newReviewServer(t)
		server.workflow.Worktree = nil

		harness := openReview(t, server)

		assert.Contains(t, paneltest.Stage(harness.Frame()), "greet.go")
	})

	t.Run("an agent kept in the directory it was given opens", func(t *testing.T) {
		server := newReviewServer(t)
		server.agent.PaneId = ""
		server.agent.Alive = nil

		harness := openReview(t, server)

		assert.Contains(t, paneltest.Stage(harness.Frame()), "greet.go")
	})
}

// TestReviewCursor covers the keys that move through the diff.
func TestReviewCursor(t *testing.T) {
	t.Run("the movement keys walk the lines", func(t *testing.T) {
		harness := openReview(t, newReviewServer(t))
		require.Equal(t, "greet.go:1", paneltest.ReviewCursor(harness.Frame()))
		respawns, sentKeys := len(harness.Tmux().Respawns()), len(harness.Tmux().SentKeys())

		harness.Press("j")

		assert.Equal(t, "greet.go:2", paneltest.ReviewCursor(harness.Frame()))
		assert.Len(t, harness.Tmux().Respawns(), respawns)
		assert.Len(t, harness.Tmux().SentKeys(), sentKeys)
	})

	t.Run("the scroll keys turn the cursor a few rows and back", func(t *testing.T) {
		harness := openReview(t, newReviewServer(t))

		harness.Press("ctrl+n")
		assert.Equal(t, "greet.go:4 (removed)", paneltest.ReviewCursor(harness.Frame()))

		harness.Press("ctrl+p")
		assert.Equal(t, "greet.go:1", paneltest.ReviewCursor(harness.Frame()))
	})

	t.Run("the wheel over the diff turns the cursor", func(t *testing.T) {
		harness := openReview(t, newReviewServer(t))

		wheelOver(harness, paneltest.DefaultWidth-1)

		assert.Equal(t, "greet.go:4 (removed)", paneltest.ReviewCursor(harness.Frame()))
	})

	t.Run("the wheel over the diff leaves a comment being written on its line", func(t *testing.T) {
		harness := openReview(t, newReviewServer(t))
		harness.Press("space", "c")
		typeText(harness, "still here")

		wheelOver(harness, paneltest.DefaultWidth-1)
		harness.Press("enter")

		harness.AssertSentTo(agentv1connect.AgentServiceCreateReviewThreadProcedure, &agentv1.CreateReviewThreadRequest{
			Task:        &agentv1.CreateReviewThreadRequest_TaskId{TaskId: "t1"},
			FilePath:    gittest.File,
			OldStart:    1,
			OldEnd:      1,
			NewStart:    1,
			NewEnd:      1,
			QuotedLines: []string{" package greet"},
			Body:        "still here",
			BaseSha:     gittest.BaseSHA,
			SnapshotSha: gittest.SnapshotSHA,
		})
	})

	t.Run("the page keys settle on a line and come back to the top", func(t *testing.T) {
		harness := openReview(t, newReviewServer(t))
		top := paneltest.ReviewCursor(harness.Frame())

		harness.Press("ctrl+d")
		assert.Equal(t, "greet.go:5", paneltest.ReviewCursor(harness.Frame()))

		harness.Press("ctrl+u")
		assert.Equal(t, top, paneltest.ReviewCursor(harness.Frame()))
	})

	t.Run("panning moves the text a third of the diff's width", func(t *testing.T) {
		wide := strings.Repeat("a", (reviewWidth-1-config.DefaultSidebarWidth)/3) + "panned-to-here"
		repo := gittest.NewRepo(t.TempDir())
		repo.Wrote(gittest.WroteParams{
			Tree:    "tree-with-a-wide-line",
			Whole:   "diff --git a/wide.go b/wide.go\nnew file mode 100644\n--- /dev/null\n+++ b/wide.go\n@@ -0,0 +1 @@\n+" + wide + "\n",
			Sources: map[string]string{"wide.go": wide + "\n"},
		})
		harness := openReview(t, newReviewServerOver(repo))

		harness.Press("l")
		assert.Contains(t, findLine(paneltest.Stage(harness.Frame()), "1 +"), "1 +  panned-to-here")

		harness.Press("0")
		assert.Contains(t, harness.Frame(), wide)
	})

	t.Run("wrapping renders a wide line whole and stops the pan", func(t *testing.T) {
		wide := strings.Repeat("a", reviewWidth) + "wrapped"
		repo := gittest.NewRepo(t.TempDir())
		repo.Wrote(gittest.WroteParams{
			Tree:    "tree-with-a-wide-line",
			Whole:   "diff --git a/wide.go b/wide.go\nnew file mode 100644\n--- /dev/null\n+++ b/wide.go\n@@ -0,0 +1 @@\n+" + wide + "\n",
			Sources: map[string]string{"wide.go": wide + "\n"},
		})
		harness := openReviewAt(t, newReviewServerOver(repo), paneltest.Params{Height: 40})

		harness.Press("?")
		assert.Contains(t, paneltest.Legend(harness.Frame()), "pan sideways")

		harness.Press("space")
		assert.Contains(t, harness.Frame(), "wrap lines")

		harness.Press("w")
		stage := paneltest.Stage(harness.Frame())
		rendered := strings.Join(stage, "\n")
		assert.Contains(t, rendered, "wrapped")
		assert.Greater(t, strings.Count(rendered, "aaaaaaaaaa"), 1, "the line runs on under itself")

		harness.Press("l")
		assert.Equal(t, stage, paneltest.Stage(harness.Frame()), "the pan keys do nothing while lines wrap")

		assert.NotContains(t, paneltest.Legend(harness.Frame()), "pan sideways")

		harness.Press("space")
		assert.Contains(t, harness.Frame(), "stop wrapping lines")
	})

	t.Run("a file jump holds the window across a pan and a watch", func(t *testing.T) {
		repo := gittest.NewRepo(t.TempDir())
		repo.Wrote(gittest.WroteParams{
			Tree:  "tree-with-two-files",
			Whole: gittest.Patch + gittest.PatchOfAnotherFile,
			Sources: map[string]string{
				gittest.File: gittest.HeadSource,
				"second.go":  "package greet\n\nfunc Second() string {\n\treturn \"second\"\n}\n",
			},
		})
		server := newReviewServerOver(repo)
		harness := openReviewAt(t, server, paneltest.Params{Height: 20})

		harness.Press("]")
		jumped := paneltest.Stage(harness.Frame())
		require.Equal(t, "second.go:1", paneltest.ReviewCursor(harness.Frame()))

		harness.Press("l", "0")
		assert.Equal(t, jumped, paneltest.Stage(harness.Frame()))

		harness.Watch(server.buildSnapshot())
		assert.Equal(t, jumped, paneltest.Stage(harness.Frame()))
	})

	t.Run("the keys come from the keymap", func(t *testing.T) {
		keys := config.DefaultKeybindings()
		keys.Review.Compose = []string{"i"}
		harness := openReviewAt(t, newReviewServer(t), paneltest.Params{Height: 40, Keys: &keys})

		harness.Press("space")
		assert.Regexp(t, `comment on this line +i`, harness.Frame())
		harness.Press("esc")

		harness.Press("space", "c")
		assert.NotContains(t, harness.Frame(), "  Comment")

		harness.Press("i")
		assert.Contains(t, harness.Frame(), "  Comment")
	})
}

// TestReviewThreads covers writing, reading and settling the conversations pinned to the diff.
func TestReviewThreads(t *testing.T) {
	t.Run("the compose key opens a thread on the line under the cursor", func(t *testing.T) {
		harness := openReview(t, newReviewServer(t))

		harness.Press("space", "c")
		typeText(harness, "this should say hello")
		harness.Press("enter")

		harness.AssertSentTo(agentv1connect.AgentServiceCreateReviewThreadProcedure, &agentv1.CreateReviewThreadRequest{
			Task:        &agentv1.CreateReviewThreadRequest_TaskId{TaskId: "t1"},
			FilePath:    gittest.File,
			OldStart:    1,
			OldEnd:      1,
			NewStart:    1,
			NewEnd:      1,
			QuotedLines: []string{" package greet"},
			Body:        "this should say hello",
			BaseSha:     gittest.BaseSHA,
			SnapshotSha: gittest.SnapshotSHA,
		})
		assert.NotContains(t, harness.Frame(), "  Comment")
	})

	t.Run("enter releases the comment and the thread shows released then read", func(t *testing.T) {
		server := newReviewServer(t)
		harness := openReview(t, server)

		harness.Press("space", "c")
		typeText(harness, "this should say hello")
		harness.Press("enter")
		harness.Watch(server.buildSnapshot())

		frame := harness.Frame()
		assert.Contains(t, paneltest.PopupProse(frame), "this should say hello")
		assert.Contains(t, paneltest.PopupProse(frame), "→ released")

		server.threads[0].Messages[0].ReadAt = timestamppb.New(time.Date(2026, 9, 2, 10, 1, 0, 0, time.UTC))
		harness.Watch(server.buildSnapshot())

		assert.Contains(t, paneltest.PopupProse(harness.Frame()), "✓ read")
	})

	t.Run("a remark on the whole change stands at the foot of the diff", func(t *testing.T) {
		server := newReviewServer(t)
		server.threads = []*agentv1.ReviewThread{newWholeChangeThread("", 0, time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC))}
		harness := openReview(t, server)

		harness.Press("t")

		frame := harness.Frame()
		assert.Contains(t, paneltest.StageText(frame), "on the whole change")
		assert.Contains(t, paneltest.SidebarProse(frame), "the whole change")
		assert.Contains(t, paneltest.PopupProse(frame), "the shape is right")
		assert.NotContains(t, paneltest.PopupProse(frame), "has gone")
	})

	t.Run("a comment the server refuses stays in the composer", func(t *testing.T) {
		harness := openReview(t, newReviewServer(t))
		harness.Refuse(agentv1connect.AgentServiceCreateReviewThreadProcedure,
			connect.NewError(connect.CodeFailedPrecondition, errors.New("not now")))

		harness.Press("space", "c")
		typeText(harness, "this should say hello")
		harness.Press("enter")

		frame := harness.Frame()
		assert.Contains(t, frame, "  Comment")
		assert.Contains(t, frame, "this should say hello")
	})

	t.Run("a refused comment is sent again on the line it was written on", func(t *testing.T) {
		harness := openReview(t, newReviewServer(t))
		harness.Refuse(agentv1connect.AgentServiceCreateReviewThreadProcedure,
			connect.NewError(connect.CodeFailedPrecondition, errors.New("not now")))
		harness.Hold(agentv1connect.AgentServiceCreateReviewThreadProcedure)

		harness.Press("space", "c")
		typeText(harness, "this should say hello")
		harness.Press("enter")
		harness.Press("j", "j", "j")
		harness.Release(agentv1connect.AgentServiceCreateReviewThreadProcedure)
		harness.Press("enter")

		sent := harness.Sent(agentv1connect.AgentServiceCreateReviewThreadProcedure)
		require.Len(t, sent, 2)
		first, _ := sent[0].(*agentv1.CreateReviewThreadRequest)
		again, _ := sent[1].(*agentv1.CreateReviewThreadRequest)
		assert.Equal(t, first.GetNewStart(), again.GetNewStart())
		assert.Equal(t, first.GetQuotedLines(), again.GetQuotedLines())
		assert.Equal(t, "this should say hello", again.GetBody())
	})

	t.Run("the thread under the cursor is rendered in the sidebar", func(t *testing.T) {
		server := newReviewServer(t)
		server.threads = []*agentv1.ReviewThread{newThreadOnFirstLine(newHumanMessage("m1", "wrong greeting"))}

		harness := openReview(t, server)

		sidebar := paneltest.PopupProse(harness.Frame())
		assert.Contains(t, sidebar, "you wrong greeting")
	})

	t.Run("a thread nobody has read is marked read once the cursor rests on it", func(t *testing.T) {
		thread := newThreadOnFirstLine(newHumanMessage("m1", "wrap this"), newAgentMessage("m2", "wrapped it"))
		thread.UnreadByHuman = true
		server := newReviewServer(t)
		server.threads = []*agentv1.ReviewThread{thread}

		harness := openReview(t, server)

		harness.Press("j", "k")
		assert.Empty(t, harness.Sent(agentv1connect.AgentServiceMarkReviewThreadReadProcedure))

		harness.Press(paneltest.Rest)
		harness.AssertSentTo(agentv1connect.AgentServiceMarkReviewThreadReadProcedure, &agentv1.MarkReviewThreadReadRequest{Id: "t1"})
	})

	t.Run("the agent under review and its reviewer are each named by role in the thread", func(t *testing.T) {
		server := newReviewServer(t)
		server.onTask = []*agentv1.Agent{{
			Id:         "r1",
			Name:       "review-auth",
			TaskId:     xptr.Ptr("t1"),
			WorkflowId: "w1",
			Role:       agentv1.AgentRole_AGENT_ROLE_REVIEWER,
		}}
		asked := newAgentMessage("m1", "this drops the error")
		asked.AuthorAgentId = xptr.Ptr("r1")
		answered := newAgentMessage("m2", "wrapped it")
		server.threads = []*agentv1.ReviewThread{newThreadOnFirstLine(asked, answered)}

		harness := openReview(t, server)

		sidebar := paneltest.PopupProse(harness.Frame())
		assert.Contains(t, sidebar, "reviewer this drops the error")
		assert.Contains(t, sidebar, "writer wrapped it")
	})

	t.Run("a conversation an agent opened shows until the menu hides it", func(t *testing.T) {
		server := newReviewServer(t)
		server.threads = []*agentv1.ReviewThread{newThreadOnFirstLine(newAgentMessage("m1", "renamed it, is that what you meant?"))}
		harness := openReview(t, server)
		require.Contains(t, paneltest.PopupProse(harness.Frame()), "renamed it")

		harness.Press("space")
		require.Contains(t, harness.Frame(), "hide agents' conversations")
		harness.Press("b")
		assert.NotContains(t, harness.Frame(), "renamed it")

		harness.Press("space", "b")
		assert.Contains(t, paneltest.PopupProse(harness.Frame()), "renamed it")
	})

	t.Run("a conversation an agent opened stays rendered once the person has replied in it", func(t *testing.T) {
		server := newReviewServer(t)
		reply := newHumanMessage("m2", "yes, keep it")
		server.threads = []*agentv1.ReviewThread{newThreadOnFirstLine(newAgentMessage("m1", "renamed it, is that what you meant?"), reply)}
		harness := openReview(t, server)

		assert.Contains(t, paneltest.PopupProse(harness.Frame()), "yes, keep it")
	})

	t.Run("the compose key on a line with a thread replies in it", func(t *testing.T) {
		server := newReviewServer(t)
		server.threads = []*agentv1.ReviewThread{newThreadOnFirstLine(newHumanMessage("m1", "renamed it, is that what you meant?"))}
		harness := openReviewAt(t, server, paneltest.Params{Height: 40})

		harness.Press("space")
		require.Contains(t, harness.Frame(), "reply in this thread")

		harness.Press("c")
		typeText(harness, "yes, that is it")
		harness.Press("enter")

		harness.AssertSentTo(agentv1connect.AgentServiceAddReviewMessageProcedure, &agentv1.AddReviewMessageRequest{
			ThreadId: "t1",
			Body:     "yes, that is it",
		})
		assert.Empty(t, harness.Sent(agentv1connect.AgentServiceCreateReviewThreadProcedure))
	})

	t.Run("a refused reply reopens under the thread it replies in after the cursor moved on", func(t *testing.T) {
		server := newReviewServer(t)
		last := newThreadOnLastLine(newHumanMessage("m2", "close the brace"))
		last.Id = "t2"
		server.threads = []*agentv1.ReviewThread{newThreadOnFirstLine(newHumanMessage("m1", "rename the package")), last}
		harness := openReviewAt(t, server, paneltest.Params{Height: 40})
		harness.Refuse(agentv1connect.AgentServiceAddReviewMessageProcedure,
			connect.NewError(connect.CodeFailedPrecondition, errors.New("not now")))
		harness.Hold(agentv1connect.AgentServiceAddReviewMessageProcedure)

		harness.Press("space", "c")
		typeText(harness, "yes, that is it")
		harness.Press("enter")
		harness.Press("t")
		harness.Release(agentv1connect.AgentServiceAddReviewMessageProcedure)

		popup := paneltest.PopupProse(harness.Frame())
		assert.Contains(t, popup, "rename the package")
		assert.NotContains(t, popup, "close the brace")
		assert.Contains(t, popup, "yes, that is it")
	})

	t.Run("the thread key reaches the only thread and stays on it", func(t *testing.T) {
		server := newReviewServer(t)
		server.threads = []*agentv1.ReviewThread{newThreadOnLastLine()}
		harness := openReview(t, server)

		harness.Press("t")
		require.Equal(t, "greet.go:5", paneltest.ReviewCursor(harness.Frame()))

		harness.Press("t")
		assert.Equal(t, "greet.go:5", paneltest.ReviewCursor(harness.Frame()))
	})

	t.Run("a thread whose file left the diff is reachable and says where it was", func(t *testing.T) {
		server := newReviewServer(t)
		server.threads = []*agentv1.ReviewThread{newAdriftThread("t1", newHumanMessage("m1", "still worth reading"))}
		harness := openReview(t, server)

		harness.Press("t")

		frame := harness.Frame()
		assert.Contains(t, paneltest.PopupProse(frame), "still worth reading")
		assert.Contains(t, paneltest.PopupProse(frame), "+what this used to say")
	})

	t.Run("a thread whose file left the diff takes a reply", func(t *testing.T) {
		server := newReviewServer(t)
		server.threads = []*agentv1.ReviewThread{newAdriftThread("t1", newHumanMessage("m1", "why did this go?"))}
		harness := openReview(t, server)

		harness.Press("t", "space", "c")
		typeText(harness, "because")
		harness.Press("enter")

		harness.AssertSentTo(agentv1connect.AgentServiceAddReviewMessageProcedure, &agentv1.AddReviewMessageRequest{
			ThreadId: "t1",
			Body:     "because",
		})
	})

	t.Run("the settle key settles a thread and presses again to reopen it", func(t *testing.T) {
		server := newReviewServer(t)
		server.threads = []*agentv1.ReviewThread{newThreadOnFirstLine(newHumanMessage("m1", "why did this change?"))}
		harness := openReview(t, server)

		harness.Press("space", "r")
		harness.Watch(server.buildSnapshot())
		assert.Contains(t, findLine(paneltest.Stage(harness.Frame()), "package greet"), "✓")

		harness.Press("space", "r")
		harness.Watch(server.buildSnapshot())
		assert.NotContains(t, findLine(paneltest.Stage(harness.Frame()), "package greet"), "✓")

		harness.AssertSentTo(agentv1connect.AgentServiceSetReviewThreadResolvedProcedure,
			&agentv1.SetReviewThreadResolvedRequest{Id: "t1", Resolved: true},
			&agentv1.SetReviewThreadResolvedRequest{Id: "t1", Resolved: false},
		)
	})

	t.Run("a settle in flight is rendered until it lands", func(t *testing.T) {
		server := newReviewServer(t)
		server.threads = []*agentv1.ReviewThread{newThreadOnFirstLine(newHumanMessage("m1", "why did this change?"))}
		harness := openReview(t, server)
		harness.Hold(agentv1connect.AgentServiceSetReviewThreadResolvedProcedure)

		harness.Press("space", "r")
		assert.Contains(t, harness.Frame(), "settling")

		harness.Release(agentv1connect.AgentServiceSetReviewThreadResolvedProcedure)
		assert.NotContains(t, harness.Frame(), "settling")
	})

	t.Run("a thread whose file left the diff settles like any other", func(t *testing.T) {
		server := newReviewServer(t)
		server.threads = []*agentv1.ReviewThread{newAdriftThread("t1", newHumanMessage("m1", "why did this go?"))}
		harness := openReview(t, server)

		harness.Press("t", "space", "r")
		harness.Watch(server.buildSnapshot())

		assert.Contains(t, findLine(paneltest.Stage(harness.Frame()), "gone.go:12"), "✓")
		harness.AssertSentTo(agentv1connect.AgentServiceSetReviewThreadResolvedProcedure,
			&agentv1.SetReviewThreadResolvedRequest{Id: "t1", Resolved: true})
	})

	t.Run("a thread deleted from under the cursor leaves it on a row that exists", func(t *testing.T) {
		server := newReviewServer(t)
		server.threads = []*agentv1.ReviewThread{newAdriftThread("t1", newHumanMessage("m1", "why did this go?"))}
		harness := openReview(t, server)
		harness.Press("t")

		server.threads = nil
		harness.Watch(server.buildSnapshot())

		assert.Equal(t, "greet.go:5", paneltest.ReviewCursor(harness.Frame()))
	})

	t.Run("the composer takes every key but escape as a character", func(t *testing.T) {
		harness := openReview(t, newReviewServer(t))

		harness.Press("space", "c", "?", "l", "n")
		frame := harness.Frame()
		assert.Contains(t, paneltest.PopupProse(frame), "?ln")
		assert.Contains(t, frame, "space menu")

		harness.Press("esc")
		assert.NotContains(t, harness.Frame(), "  Comment")
	})
}

// TestReviewPopupScroll covers reading a conversation taller than its popup.
func TestReviewPopupScroll(t *testing.T) {
	t.Run("the popup opens on the end of the conversation", func(t *testing.T) {
		harness := openLongThread(t)

		popup := paneltest.PopupProse(harness.Frame())
		assert.Contains(t, popup, "note 5")
		assert.NotContains(t, popup, "note 0")
	})

	t.Run("the scroll keys move the conversation and leave the diff cursor", func(t *testing.T) {
		harness := openLongThread(t)
		for range 20 {
			harness.Press("ctrl+p")
		}

		frame := harness.Frame()
		assert.Contains(t, paneltest.PopupProse(frame), "note 0")
		assert.NotContains(t, paneltest.PopupProse(frame), "note 5")

		for range 20 {
			harness.Press("ctrl+n")
		}

		assert.Contains(t, paneltest.PopupProse(harness.Frame()), "note 5")
	})

	t.Run("a scroll key into the conversation drops a top key pressed once before it", func(t *testing.T) {
		server := newReviewServer(t)
		thread := newLongThread()
		thread.OldStart, thread.NewStart = 3, 3
		thread.QuotedLines = []string{" func Hello() string {"}
		server.threads = []*agentv1.ReviewThread{thread}
		harness := openReviewAt(t, server, paneltest.Params{Height: 24})
		harness.Press("j", "j")
		require.Contains(t, paneltest.PopupProse(harness.Frame()), "note 5")

		harness.Press("g", "ctrl+p", "g")

		assert.Contains(t, paneltest.PopupProse(harness.Frame()), "note")
	})

	t.Run("the wheel over the popup scrolls the conversation and leaves the diff", func(t *testing.T) {
		harness := openLongThread(t)
		x, y := paneltest.PopupMiddle(harness.Rendered())

		harness.Send(tea.MouseWheelMsg{
			X:      x,
			Y:      y,
			Button: tea.MouseWheelUp,
		})

		frame := harness.Frame()
		assert.NotContains(t, paneltest.PopupProse(frame), "note 5")
		assert.Contains(t, paneltest.PopupProse(frame), "note")
	})

	t.Run("the wheel beside the popup moves the diff", func(t *testing.T) {
		for _, x := range []int{1, reviewWidth - 1} {
			harness := openLongThread(t)
			require.Contains(t, paneltest.PopupProse(harness.Frame()), "note 5")

			harness.Send(tea.MouseWheelMsg{
				X:      x,
				Y:      2,
				Button: tea.MouseWheelDown,
			})

			assert.NotContains(t, paneltest.PopupProse(harness.Frame()), "note", "at column %d", x)
		}
	})

	t.Run("the wheel beside a conversation with nothing to scroll moves the diff", func(t *testing.T) {
		thread := newThreadOnFirstLine(newHumanMessage("m1", "short"))
		thread.OldStart, thread.NewStart, thread.QuotedLines = 3, 3, []string{" func Hello() string {"}
		server := newReviewServer(t)
		server.threads = []*agentv1.ReviewThread{thread}
		harness := openReviewAt(t, server, paneltest.Params{Height: 24})
		harness.Press("j", "j")
		frame := harness.Frame()
		require.NotEmpty(t, paneltest.Popup(frame))

		harness.Send(tea.MouseWheelMsg{
			X:      reviewWidth - 1,
			Y:      2,
			Button: tea.MouseWheelDown,
		})

		assert.Empty(t, paneltest.Popup(harness.Frame()))
	})

	t.Run("moving the diff cursor drops the scroll", func(t *testing.T) {
		harness := openLongThread(t)
		harness.Press("ctrl+p")
		require.NotContains(t, paneltest.PopupProse(harness.Frame()), "note 5")

		harness.Press("j", "k")

		frame := harness.Frame()
		assert.Contains(t, paneltest.PopupProse(frame), "note 5")
	})
}

// TestReviewFocus covers the legend and the menu following whichever of the diff, the file tree and
// the rows over it holds the keys.
func TestReviewFocus(t *testing.T) {
	tests := []struct {
		name        string
		keys        []string
		legend      []string
		legendLacks []string
		menu        []string
		menuLacks   []string
	}{
		{
			name:   "the diff offers its file, how it renders and the review",
			legend: []string{"next/previous file", "search"},
			menu:   []string{"mark the file viewed", "wrap lines", "read the diff again"},
		},
		{
			name:      "the file tree offers the file under its cursor",
			keys:      []string{"ctrl+space", "ctrl+space"},
			legend:    []string{"next/previous file", "search"},
			menu:      []string{"open in your editor", "mark the file viewed"},
			menuLacks: []string{"wrap lines", "show the whole file", "read the diff again"},
		},
		{
			name:        "the pull request offers nothing on the files",
			keys:        []string{"ctrl+space", "ctrl+space", "k"},
			legend:      []string{"open the description"},
			legendLacks: []string{"next/previous file", "search"},
			menu:        []string{"edit the title and description", "not yet"},
			menuLacks:   []string{"open in your editor", "mark the file viewed", "wrap lines"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := openReviewAt(t, newDescribedReviewServer(t, "Why it changed."), paneltest.Params{Height: 40})
			harness.Press(tt.keys...)

			harness.Press("?")
			legend := paneltest.Legend(harness.Frame())
			for _, want := range tt.legend {
				assert.Contains(t, legend, want)
			}
			for _, lacks := range tt.legendLacks {
				assert.NotContains(t, legend, lacks)
			}

			harness.Press("?", "space")
			frame := harness.Frame()
			for _, want := range tt.menu {
				assert.Contains(t, frame, want)
			}
			for _, lacks := range tt.menuLacks {
				assert.NotContains(t, frame, lacks)
			}
		})
	}
}

// TestReviewFitsThePane covers the frame staying the pane's height whatever the sidebar holds.
func TestReviewFitsThePane(t *testing.T) {
	for _, height := range []int{10, 12, 24, 50} {
		t.Run(fmt.Sprintf("a %d line pane", height), func(t *testing.T) {
			server := newReviewServer(t)
			server.threads = append([]*agentv1.ReviewThread{newLongThread()}, newAdriftThreads(4)...)
			harness := openReviewAt(t, server, paneltest.Params{Height: height})

			assert.Len(t, paneltest.SplitLines(harness.Frame()), height)

			harness.Press("ctrl+p", "ctrl+p", "ctrl+p", "ctrl+p", "ctrl+p")
			assert.Len(t, paneltest.SplitLines(harness.Frame()), height)

			harness.Press("space", "c")
			assert.Len(t, paneltest.SplitLines(harness.Frame()), height)
		})
	}
}

// TestReviewSearch covers the review's own search over the diff.
func TestReviewSearch(t *testing.T) {
	t.Run("the diff follows the pattern as it is typed and enter keeps it", func(t *testing.T) {
		harness := openReview(t, newReviewServer(t))

		harness.Press("/")
		typeText(harness, "goodbye")

		frame := harness.Frame()
		assert.Equal(t, "greet.go:4", paneltest.ReviewCursor(frame))
		assert.Contains(t, paneltest.StageText(frame), "1 of 1")

		harness.Press("enter")

		frame = harness.Frame()
		assert.Contains(t, paneltest.Stage(frame), "/ goodbye")
		assert.Contains(t, paneltest.StageText(frame), "1 of 1")
	})

	t.Run("escape at the prompt puts the search away", func(t *testing.T) {
		harness := openReview(t, newReviewServer(t))

		harness.Press("/")
		typeText(harness, "goodbye")
		harness.Press("esc")

		frame := harness.Frame()
		assert.NotContains(t, frame, "goodbye▏")
		assert.NotContains(t, frame, "1 of 1")
	})

	t.Run("the match keys step through the matches and wrap", func(t *testing.T) {
		harness := openReview(t, newReviewServer(t))

		harness.Press("/")
		typeText(harness, "return")
		harness.Press("enter")
		first := paneltest.ReviewCursor(harness.Frame())

		harness.Press("n")
		second := paneltest.ReviewCursor(harness.Frame())
		require.NotEqual(t, first, second)

		harness.Press("n")
		assert.Equal(t, first, paneltest.ReviewCursor(harness.Frame()))

		harness.Press("N")
		assert.Equal(t, second, paneltest.ReviewCursor(harness.Frame()))
	})

	t.Run("the count spells the matches out when the cursor is off them", func(t *testing.T) {
		harness := openReview(t, newReviewServer(t))

		harness.Press("/")
		typeText(harness, "return")
		harness.Press("enter", "g", "g")

		assert.Contains(t, paneltest.StageText(harness.Frame()), "2 matches")
	})

	t.Run("the match key says when there is nothing to step to", func(t *testing.T) {
		harness := openReview(t, newReviewServer(t))

		harness.Press("n")
		assert.Contains(t, readEngineLine(harness.Frame()), "nothing to search for yet")

		harness.Press("/")
		typeText(harness, "wallaby")
		harness.Press("enter", "n")

		frame := harness.Frame()
		assert.Contains(t, paneltest.Stage(frame), "/ wallaby")
		assert.Contains(t, readEngineLine(frame), "no matches")
	})

	t.Run("the prompt takes every key typed", func(t *testing.T) {
		harness := openReview(t, newReviewServer(t))

		harness.Press("/", "g", "o", "?")

		frame := harness.Frame()
		assert.Contains(t, paneltest.Stage(frame), "/ go?▏")
		assert.Contains(t, frame, "space menu")
	})

	t.Run("the search is the review's and leaves the agents list alone", func(t *testing.T) {
		server := newReviewServer(t)
		harness := paneltest.Start(t, paneltest.Params{
			Width:  reviewWidth,
			Height: reviewHeight,
			Git:    server.repo,
		})
		server.serve(harness)
		second := paneltest.Agent("a2", "billing", "w1")
		second.Cwd = server.repo.Root
		harness.Watch(paneltest.Snapshot(server.workflow, server.agent, server.task, second))
		harness.Press("j")
		require.Contains(t, paneltest.HoveredRow(harness.Frame()), "auth")

		harness.Press("space", "r", "/")
		typeText(harness, "goodbye")
		harness.Press("enter", "esc", "esc")

		frame := harness.Frame()
		assert.Len(t, paneltest.ListRows(frame), 3)
		assert.Contains(t, paneltest.HoveredRow(frame), "auth")
	})
}

// TestReviewViewed covers folding a file away once it has been read.
func TestReviewViewed(t *testing.T) {
	t.Run("the viewed key folds the file to its header and again opens it", func(t *testing.T) {
		harness := openReview(t, newReviewServer(t))

		harness.Press("space", "v")

		frame := harness.Frame()
		assert.NotContains(t, frame, "func Hello() string {")
		assert.Contains(t, paneltest.StageText(frame), "greet.go  +")

		harness.Press("space", "v")
		assert.Contains(t, harness.Frame(), "func Hello() string {")

		sent := harness.Sent(agentv1connect.AgentServiceSetReviewFileViewedProcedure)
		require.Len(t, sent, 2)
		marked, _ := sent[0].(*agentv1.SetReviewFileViewedRequest)
		assert.Equal(t, "t1", marked.GetTaskId())
		assert.Equal(t, gittest.File, marked.GetFilePath())
		assert.True(t, marked.GetViewed())
		assert.NotEmpty(t, marked.GetFingerprint())
		unmarked, _ := sent[1].(*agentv1.SetReviewFileViewedRequest)
		assert.False(t, unmarked.GetViewed())
	})

	t.Run("a mark holds while the tree is unmoved and a new snapshot has read nothing", func(t *testing.T) {
		server := newReviewServer(t)
		harness := openReview(t, server)

		harness.Press("space", "v", "esc", "esc", "space", "r")
		require.NotContains(t, harness.Frame(), "func Hello() string {")

		server.repo.Wrote(gittest.WroteParams{
			Tree:  "tree-written-again",
			Whole: gittest.PatchWrittenOver,
			Sources: map[string]string{
				gittest.File: "package greet\n\nfunc Hello() string {\n\treturn \"written by the agent\"\n}\n",
			},
		})
		harness.Press("esc", "esc", "space", "r")

		assert.Contains(t, harness.Frame(), "func Hello() string {")
	})
}

// TestReviewDeliveryBadge covers the sidebar saying where each of the reader's comments stands with
// the writer.
func TestReviewDeliveryBadge(t *testing.T) {
	tests := []struct {
		name     string
		messages []*agentv1.ReviewMessage
		want     string
		absent   string
	}{
		{
			name:     "nothing is said of what the agent wrote",
			messages: []*agentv1.ReviewMessage{newAgentMessage("m9", "renamed it")},
			want:     "renamed it",
			absent:   "released",
		},
		{
			name:     "a comment the writer has not read yet is released",
			messages: []*agentv1.ReviewMessage{newReleasedComment("m1")},
			want:     "→ released",
			absent:   "read",
		},
		{
			name:     "a comment the writer picked up is read",
			messages: []*agentv1.ReviewMessage{newReadComment("m1")},
			want:     "✓ read",
			absent:   "released",
		},
		{
			name:     "a comment the writer spoke to is answered",
			messages: []*agentv1.ReviewMessage{newReadComment("m1"), newAgentMessage("m2", "done")},
			want:     "✓ answered",
			absent:   "✓ read",
		},
		{
			name:     "a forge comment its author could not push is held from the agent",
			messages: []*agentv1.ReviewMessage{newHeldForgeComment("f1", "use a map")},
			want:     "! held",
			absent:   "released",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := newReviewServer(t)
			server.threads = []*agentv1.ReviewThread{newThreadOnFirstLine(tt.messages...)}

			harness := openReview(t, server)

			sidebar := paneltest.PopupProse(harness.Frame())
			assert.Contains(t, sidebar, tt.want)
			assert.NotContains(t, sidebar, tt.absent)
		})
	}
}

// TestReviewGutter covers the mark beside the diff saying what a thread is waiting on.
func TestReviewGutter(t *testing.T) {
	tests := []struct {
		name     string
		unread   bool
		resolved *timestamppb.Timestamp
		mirrors  bool
		replies  []*agentv1.ForgeReply
		message  *agentv1.ReviewMessage
		want     string
		absent   string
	}{
		{
			name:    "an answer nobody has read is filled in",
			unread:  true,
			message: newReadComment("m1"),
			want:    "●",
			absent:  "○",
		},
		{
			name:     "a settled thread says so",
			resolved: timestamppb.New(time.Date(2026, 9, 2, 11, 0, 0, 0, time.UTC)),
			message:  newReadComment("m1"),
			want:     "✓",
			absent:   "●",
		},
		{
			name:     "a settled thread the agent spoke in again is filled in",
			resolved: timestamppb.New(time.Date(2026, 9, 2, 11, 0, 0, 0, time.UTC)),
			unread:   true,
			message:  newReadComment("m1"),
			want:     "●",
			absent:   "✓",
		},
		{
			name:    "a comment that landed and was read is hollow",
			message: newReadComment("m1"),
			want:    "○",
			absent:  "●",
		},
		{
			name:    "a thread from the forge is marked as one",
			mirrors: true,
			message: newHeldForgeComment("f1", "use a map"),
			want:    "◆",
			absent:  "○",
		},
		{
			name:    "a forge thread with a refused reply is hollow",
			mirrors: true,
			replies: []*agentv1.ForgeReply{newFailedReply()},
			message: newHeldForgeComment("f1", "use a map"),
			want:    "◇",
			absent:  "◆",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			thread := newThreadOnFirstLine(tt.message)
			thread.UnreadByHuman = tt.unread
			thread.ResolvedAt = tt.resolved
			thread.MirrorsForge = tt.mirrors
			thread.ForgeReplies = tt.replies
			server := newReviewServer(t)
			server.threads = []*agentv1.ReviewThread{thread}

			harness := openReview(t, server)

			gutter := findLine(paneltest.Stage(harness.Frame()), "package greet")
			assert.Contains(t, gutter, tt.want)
			assert.NotContains(t, gutter, tt.absent)
		})
	}
}

// TestReviewWatch covers the conversation following the watch without a keypress.
func TestReviewWatch(t *testing.T) {
	t.Run("an answer the watch puts is rendered", func(t *testing.T) {
		harness := openReview(t, newReviewServer(t))

		thread := newThreadOnFirstLine(newHumanMessage("m0", "fix it"), newAgentMessage("m1", "fixed it"))
		thread.TaskId = "t1"
		harness.Watch(paneltest.Puts(thread))

		assert.Contains(t, paneltest.PopupProse(harness.Frame()), "fixed it")
	})

	t.Run("a comment the writer reads shows read", func(t *testing.T) {
		server := newReviewServer(t)
		comment := newReleasedComment("m1")
		server.threads = []*agentv1.ReviewThread{newThreadOnFirstLine(comment)}
		harness := openReview(t, server)
		require.Contains(t, paneltest.PopupProse(harness.Frame()), "released")

		comment.ReadAt = timestamppb.New(time.Date(2026, 9, 2, 10, 5, 0, 0, time.UTC))
		harness.Watch(server.buildSnapshot())

		assert.Contains(t, paneltest.PopupProse(harness.Frame()), "read")
	})

	t.Run("an answer arriving while the reader types waits for them to stop", func(t *testing.T) {
		server := newReviewServer(t)
		harness := openReview(t, server)
		harness.Press("j", "space", "c")
		typeText(harness, "half a thought")

		server.threads = []*agentv1.ReviewThread{newThreadOnFirstLine(newHumanMessage("m0", "fix it"), newAgentMessage("m1", "fixed it"))}
		harness.Watch(server.buildSnapshot())

		frame := harness.Frame()
		assert.Contains(t, paneltest.PopupProse(frame), "half a thought")
		assert.NotContains(t, paneltest.PopupProse(frame), "fixed it")

		harness.Press("esc")
		assert.Equal(t, "greet.go:2", paneltest.ReviewCursor(harness.Frame()))

		harness.Press("k")
		assert.Contains(t, paneltest.PopupProse(harness.Frame()), "fixed it")
	})

	t.Run("an archived agent's threads stay rendered and a settle reads them back", func(t *testing.T) {
		server := newReviewServer(t)
		server.threads = []*agentv1.ReviewThread{newThreadOnFirstLine(newReadComment("m1"))}
		harness := openReview(t, server)

		server.agent.ArchivedAt = timestamppb.New(time.Date(2026, 9, 2, 11, 0, 0, 0, time.UTC))
		harness.Watch(paneltest.Snapshot(server.workflow, server.agent, server.task))
		require.Contains(t, findLine(paneltest.Stage(harness.Frame()), "package greet"), "○")

		harness.Press("space", "R")
		require.Len(t, harness.Sent(agentv1connect.AgentServiceOpenReviewHistoryProcedure), 2)
		require.Contains(t, findLine(paneltest.Stage(harness.Frame()), "package greet"), "○")

		harness.Press("space", "r")

		assert.Contains(t, findLine(paneltest.Stage(harness.Frame()), "package greet"), "✓")
		assert.Len(t, harness.Sent(agentv1connect.AgentServiceListReviewThreadsProcedure), 1)
	})
}

// TestReviewOfAnArchivedAgent covers the diff of an archived agent opening read only.
func TestReviewOfAnArchivedAgent(t *testing.T) {
	server := newReviewServer(t)
	server.threads = []*agentv1.ReviewThread{newThreadOnFirstLine(newReleasedComment("m1"))}
	harness := openReviewAt(t, server, paneltest.Params{Height: 50})
	server.agent.ArchivedAt = timestamppb.New(time.Date(2026, 9, 2, 11, 0, 0, 0, time.UTC))
	harness.Watch(server.buildSnapshot())

	frame := harness.Frame()
	assert.NotContains(t, frame, "reply in this thread")

	assert.Contains(t, readEngineLine(frame), "this agent is archived")
	harness.Watch(server.buildSnapshot())
	harness.Press("space", "R")
	assert.Equal(t, 1, strings.Count(readOpenMessages(harness), "this agent is archived"))
	harness.Press("ctrl+g")

	harness.Press("space")
	menu := harness.Frame()
	assert.Contains(t, menu, "settle the thread")
	harness.Press("esc")

	harness.Press("space", "c")
	assert.NotContains(t, harness.Frame(), "  Comment")
	harness.Press("esc")
	assert.Empty(t, harness.Sent(agentv1connect.AgentServiceAddReviewMessageProcedure))

	harness.Press("space", "r", "space", "v")
	harness.AssertSentTo(agentv1connect.AgentServiceSetReviewThreadResolvedProcedure,
		&agentv1.SetReviewThreadResolvedRequest{Id: "t1", Resolved: true})
	assert.Len(t, harness.Sent(agentv1connect.AgentServiceSetReviewFileViewedProcedure), 1)
}

// TestReviewReload covers reading the worktree again, by the reload key and by the watch reporting a move.
func TestReviewReload(t *testing.T) {
	t.Run("a thread arriving keeps the cursor, the window, the pan and the search", func(t *testing.T) {
		server := newReviewServer(t)
		harness := openReview(t, server)
		harness.Press("/")
		typeText(harness, "hello")
		harness.Press("enter", "l")
		before := harness.Frame()

		server.threads = []*agentv1.ReviewThread{newThreadOnLastLine(newAgentMessage("m1", "done"))}
		harness.Watch(server.buildSnapshot())

		frame := harness.Frame()
		assert.Equal(t, paneltest.ReviewCursor(before), paneltest.ReviewCursor(frame))
		assert.Equal(t, findLine(paneltest.Stage(before), "package greet"), findLine(paneltest.Stage(frame), "package greet"))
		assert.Contains(t, paneltest.StageText(frame), "/ hello")
		assert.Contains(t, paneltest.StageText(frame), "1 of 2")
	})

	t.Run("a search by file name keeps its header across a thread arriving and a reload", func(t *testing.T) {
		server := newReviewServer(t)
		harness := openReview(t, server)
		harness.Press("/")
		typeText(harness, "greet.go")
		harness.Press("enter")
		header := paneltest.ReviewCursor(harness.Frame())
		require.Contains(t, paneltest.StageText(harness.Frame()), "1 of 1")

		server.threads = []*agentv1.ReviewThread{newThreadOnFirstLine(newAgentMessage("m1", "done"))}
		harness.Watch(server.buildSnapshot())
		assert.Equal(t, header, paneltest.ReviewCursor(harness.Frame()))
		assert.Contains(t, paneltest.StageText(harness.Frame()), "1 of 1")

		harness.Press("space", "R")
		assert.Equal(t, header, paneltest.ReviewCursor(harness.Frame()))
		assert.Contains(t, paneltest.StageText(harness.Frame()), "1 of 1")
	})

	t.Run("the reload key keeps the search and its matches", func(t *testing.T) {
		harness := openReview(t, newReviewServer(t))
		harness.Press("/")
		typeText(harness, "return")
		harness.Press("enter", "space", "R")

		assert.Contains(t, paneltest.StageText(harness.Frame()), "1 of 2")
		harness.Press("n")
		assert.Contains(t, paneltest.StageText(harness.Frame()), "2 of 2")
	})

	t.Run("the reload key reads again with nothing moved", func(t *testing.T) {
		harness := openReview(t, newReviewServer(t))

		harness.Press("space", "R")

		assert.Len(t, harness.Sent(agentv1connect.AgentServiceOpenReviewHistoryProcedure), 2)
		assert.True(t, isBeside(harness))
	})

	t.Run("the reload key opens the next snapshot where the reader stood", func(t *testing.T) {
		server := newReviewServer(t)
		harness := openReview(t, server)
		harness.Press("j", "j")

		server.repo.Wrote(newSnapshotWrittenOver())
		harness.Press("space", "R")

		frame := harness.Frame()
		assert.Equal(t, "greet.go:3", paneltest.ReviewCursor(frame))
		assert.Contains(t, frame, "written by the agent")
	})

	t.Run("a reload finding nothing written backs out to the listing", func(t *testing.T) {
		server := newReviewServer(t)
		harness := openReview(t, server)

		server.repo.Wrote(gittest.WroteParams{Tree: "tree-of-the-base"})
		harness.Press("space", "R")

		assert.Equal(t, screen.Agents, paneltest.Screen(harness.Frame()))
		assert.Contains(t, harness.Frame(), "Nothing to review in auth")
	})

	t.Run("a diff whose every path is excluded says so rather than calling the agent idle", func(t *testing.T) {
		repo := gittest.NewRepo(t.TempDir())
		repo.Wrote(gittest.WroteParams{
			Tree:   "tree-inside-an-excluded-path",
			Whole:  gittest.PatchInAnExcludedPath,
			Hidden: true,
		})
		server := newReviewServerOver(repo)
		server.workflow.Worktree.DiffExclude = []string{"proto/gen"}

		harness := openReview(t, server)

		frame := harness.Frame()
		assert.Equal(t, screen.Agents, paneltest.Screen(harness.Frame()))
		assert.Contains(t, readOpenMessages(harness), "excluded from the diff")
		assert.NotContains(t, frame, "Nothing to review")
	})

	t.Run("a move the watch reports leaves the diff on its snapshot and says so", func(t *testing.T) {
		server := newReviewServer(t)
		harness := openReview(t, server)

		server.repo.Wrote(newSnapshotWrittenOver())
		server.task.MovedSinceSnapshot = gittest.SnapshotSHA
		harness.Watch(server.buildSnapshot())

		frame := harness.Frame()
		assert.NotContains(t, frame, "written by the agent")
		assert.Contains(t, readEngineLine(frame), "the worktree has moved")
		assert.Len(t, harness.Sent(agentv1connect.AgentServiceOpenReviewHistoryProcedure), 1)

		harness.Press("space", "R")

		assert.Contains(t, harness.Frame(), "written by the agent")
		assert.NotContains(t, readOpenMessages(harness), "the worktree has moved")
		assert.Len(t, harness.Sent(agentv1connect.AgentServiceOpenReviewHistoryProcedure), 2)
	})

	t.Run("a move from a snapshot other than the one on screen raises nothing", func(t *testing.T) {
		server := newReviewServer(t)
		harness := openReview(t, server)

		server.task.MovedSinceSnapshot = "commit-of-another-snapshot"
		harness.Watch(server.buildSnapshot())

		assert.NotContains(t, readOpenMessages(harness), "the worktree has moved")
		assert.Len(t, harness.Sent(agentv1connect.AgentServiceOpenReviewHistoryProcedure), 1)
	})

	t.Run("the reload key catches up while the writer is still writing", func(t *testing.T) {
		server := newReviewServer(t)
		server.agent.State = agentv1.AgentState_AGENT_STATE_THINKING
		harness := openReview(t, server)

		server.repo.Wrote(newSnapshotWrittenOver())
		server.task.MovedSinceSnapshot = gittest.SnapshotSHA
		harness.Watch(server.buildSnapshot())
		require.Contains(t, readEngineLine(harness.Frame()), "the worktree has moved")

		harness.Press("space", "R")

		assert.Contains(t, harness.Frame(), "written by the agent")
		assert.NotContains(t, readOpenMessages(harness), "the worktree has moved")
	})

	t.Run("the history failing to read fails the load", func(t *testing.T) {
		server := newReviewServer(t)
		server.historyErr = errors.New("the server went away")

		harness := openReview(t, server)

		assert.Equal(t, screen.Agents, paneltest.Screen(harness.Frame()))
		assert.Contains(t, readOpenMessages(harness), "the server went away")
	})

	t.Run("a move while the editor is open raises nothing", func(t *testing.T) {
		server := newReviewServer(t)
		server.threads = []*agentv1.ReviewThread{newThreadOnFirstLine(newReleasedComment("m1"))}
		harness := openReview(t, server)
		harness.Press("space", "e")

		server.repo.Wrote(newSnapshotWrittenOver())
		server.task.MovedSinceSnapshot = gittest.SnapshotSHA
		harness.Watch(server.buildSnapshot())

		assert.NotContains(t, readOpenMessages(harness), "the worktree has moved")
	})
}

// TestReviewOwnComments covers the comments the reader started and the keys that walk the open ones.
func TestReviewOwnComments(t *testing.T) {
	t.Run("an open comment leaves the reader free to write another", func(t *testing.T) {
		harness := openReview(t, newYoursServer(t, newYourThread("a.txt", 2)))

		harness.Press("space", "c")

		assert.Contains(t, harness.Frame(), "  Comment")
	})

	t.Run("an open comment leaves the editor free to open", func(t *testing.T) {
		harness := openReview(t, newYoursServer(t, newYourThread("a.txt", 2)))

		harness.Press("space", "e")

		assert.Contains(t, harness.Frame(), "editing greet.go")
	})

	t.Run("the compose key replies in a comment the cursor stands on", func(t *testing.T) {
		harness := openReview(t, newYoursServer(t, newYourThread(gittest.File, 1)))

		harness.Press("space", "c")
		typeText(harness, "still reads like a changelog")
		harness.Press("enter")

		harness.AssertSentTo(agentv1connect.AgentServiceAddReviewMessageProcedure, &agentv1.AddReviewMessageRequest{
			ThreadId: "greet.go:1",
			Body:     "still reads like a changelog",
		})
	})

	t.Run("the open comment keys walk both ways and stop at the ends", func(t *testing.T) {
		first := newYourThread(gittest.File, 2)
		first.Messages[0].Body = "the first open comment"
		second := newYourThread(gittest.File, 3)
		second.Messages[0].Body = "the second open comment"
		third := newYourThread(gittest.File, 5)
		third.Messages[0].Body = "the third open comment"
		harness := openReview(t, newYoursServer(t, first, second, third))

		harness.Press("u")
		assert.Contains(t, paneltest.PopupProse(harness.Frame()), "the first open comment")
		harness.Press("U")
		assert.Contains(t, paneltest.PopupProse(harness.Frame()), "the first open comment")

		harness.Press("u", "u")
		assert.Contains(t, paneltest.PopupProse(harness.Frame()), "the third open comment")
		harness.Press("U")
		assert.Contains(t, paneltest.PopupProse(harness.Frame()), "the second open comment")

		harness.Press("u", "u")
		frame := harness.Frame()
		assert.Contains(t, paneltest.PopupProse(frame), "the third open comment")
		assert.Contains(t, readEngineLine(frame), "comment")
	})

	t.Run("the place row counts the open comment the cursor is on", func(t *testing.T) {
		harness := openReviewAt(t, newYoursServer(t,
			newYourThread(gittest.File, 2), newYourThread(gittest.File, 3), newYourThread(gittest.File, 5),
		), paneltest.Params{Width: 45})

		harness.Press("u", "u", "u")
		assert.Equal(t, "greet.go:5  3 of 3", paneltest.ReviewCursor(harness.Frame()))
		harness.Press("U")
		assert.Equal(t, "greet.go:3  2 of 3", paneltest.ReviewCursor(harness.Frame()))
	})

	t.Run("the open comment key says why it moved nowhere", func(t *testing.T) {
		settled := newYourThread(gittest.File, 2)
		settled.ResolvedAt = timestamppb.New(time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC))
		tests := []struct {
			name    string
			threads []*agentv1.ReviewThread
			keys    []string
			want    string
			absent  string
		}{
			{
				name:    "every comment is settled",
				threads: []*agentv1.ReviewThread{settled},
				keys:    []string{"u"},
				want:    "settled",
			},
			{
				name:    "the cursor stands on the only one left",
				threads: []*agentv1.ReviewThread{settled, newYourThread(gittest.File, 5)},
				keys:    []string{"u", "u"},
				want:    "only comment left open",
			},
			{
				name:    "the only one left is folded away",
				threads: []*agentv1.ReviewThread{newYourThread(gittest.File, 3)},
				keys:    []string{"space", "v", "u"},
				want:    "folded away",
				absent:  "settled",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				harness := openReview(t, newYoursServer(t, tt.threads...))

				harness.Press(tt.keys...)

				sidebar := readOpenMessages(harness)
				assert.Contains(t, sidebar, tt.want)
				if tt.absent != "" {
					assert.NotContains(t, sidebar, tt.absent)
				}
			})
		}
	})
}

// TestReviewExpand covers rendering a file whole rather than as its hunks.
func TestReviewExpand(t *testing.T) {
	t.Run("the expand key renders the lines the patch left out and gives the hunks back", func(t *testing.T) {
		harness, _ := openTallReview(t)
		before := harness.Frame()
		require.Contains(t, before, "func Two()")
		require.Contains(t, before, "@@")
		require.NotContains(t, before, "func Four()")

		harness.Press("space", "E")
		expanded := harness.Frame()
		assert.Contains(t, expanded, "func Four()")
		assert.Contains(t, expanded, `import "fmt"`)
		assert.NotContains(t, expanded, "@@")

		harness.Press("space", "E")
		tight := harness.Frame()
		assert.NotContains(t, tight, "func Four()")
		assert.Contains(t, tight, "func Two()")
	})

	t.Run("the cursor stays on the line it stood on", func(t *testing.T) {
		harness, _ := openTallReview(t)
		harness.Press("j", "j")
		before := paneltest.ReviewCursor(harness.Frame())

		harness.Press("space", "E")

		assert.Equal(t, before, paneltest.ReviewCursor(harness.Frame()))
	})

	t.Run("a reload keeps the file rendered whole", func(t *testing.T) {
		harness, _ := openTallReview(t)

		harness.Press("space", "E", "space", "R")

		assert.Contains(t, harness.Frame(), "func Four()")
	})

	t.Run("a file with nothing more to render refuses and says so", func(t *testing.T) {
		harness := openReview(t, newReviewServer(t))

		harness.Press("space", "E")

		assert.Contains(t, readEngineLine(harness.Frame()), "no more of that file")
	})

	t.Run("a folded file refuses and says so", func(t *testing.T) {
		harness, _ := openTallReview(t)

		harness.Press("space", "v", "space", "E")

		frame := harness.Frame()
		assert.NotContains(t, frame, "func Four()")
		assert.Contains(t, readEngineLine(frame), "folded away")
	})

	t.Run("a line only the expansion renders takes a comment", func(t *testing.T) {
		harness, _ := openTallReview(t)

		harness.Press("space", "E", "G")
		require.Equal(t, "tall.go:13", paneltest.ReviewCursor(harness.Frame()))
		harness.Press("space", "c")
		typeText(harness, "unchanged and wrong")
		harness.Press("enter")

		sent := harness.Sent(agentv1connect.AgentServiceCreateReviewThreadProcedure)
		require.Len(t, sent, 1)
		created, _ := sent[0].(*agentv1.CreateReviewThreadRequest)
		assert.Equal(t, gittest.TallFile, created.GetFilePath())
		assert.Equal(t, int32(13), created.GetNewStart())
		assert.Equal(t, []string{" func Four() {}"}, created.GetQuotedLines())
	})

	t.Run("collapsing puts that comment off screen rather than calling it changed", func(t *testing.T) {
		harness, server := openTallReview(t)
		server.threads = []*agentv1.ReviewThread{{
			Id:          "t1",
			FilePath:    gittest.TallFile,
			OldStart:    13,
			NewStart:    13,
			QuotedLines: []string{" func Four() {}"},
			Messages:    []*agentv1.ReviewMessage{newHumanMessage("m1", "unchanged and wrong")},
		}}

		harness.Press("space", "E")
		harness.Watch(server.buildSnapshot())
		harness.Press("space", "E")

		frame := harness.Frame()
		assert.Contains(t, frame, "outside what is on screen")
		assert.NotContains(t, frame, "has changed")
	})

	t.Run("a file folded while rendered whole stays folded after a reload", func(t *testing.T) {
		harness, _ := openTallReview(t)

		harness.Press("space", "E", "space", "v", "space", "R")

		assert.Contains(t, paneltest.StageText(harness.Frame()), "tall.go  +")
	})
}

// TestReviewSnapshots covers walking the snapshots of a review with the base keys.
func TestReviewSnapshots(t *testing.T) {
	t.Run("the base key narrows to the work since the last review and forward widens it back", func(t *testing.T) {
		harness := openReview(t, newEarlierReviewServer(t))
		frame := harness.Frame()
		require.Contains(t, paneltest.SidebarProse(frame), "2 files, +4 -1")
		require.Contains(t, paneltest.SidebarProse(frame), "1 file changed since you last reviewed")

		harness.Press("<")
		frame = harness.Frame()
		assert.Contains(t, paneltest.StageText(frame), "snapshot 1 ▸ snapshot 2")
		assert.Contains(t, paneltest.SidebarProse(frame), "1 file, +3 -0")
		assert.NotRegexp(t, `\d+/\d+ viewed`, frame)
		assert.NotContains(t, paneltest.SidebarProse(frame), "since you last reviewed")

		harness.Press(">")
		frame = harness.Frame()
		assert.Contains(t, paneltest.StageText(frame), "where the task starts ▸ snapshot 2")
		assert.Contains(t, paneltest.SidebarProse(frame), "2 files, +4 -1")
	})

	t.Run("a range the reader has already stepped past never lands", func(t *testing.T) {
		harness := openReview(t, newEarlierReviewServer(t))
		harness.Hold(agentv1connect.AgentServiceOpenReviewHistoryProcedure)

		harness.Press("<", "<")
		harness.Release(agentv1connect.AgentServiceOpenReviewHistoryProcedure)
		frame := harness.Frame()
		assert.Contains(t, paneltest.StageText(frame), "where the task starts ▸ snapshot 1")
		assert.Contains(t, paneltest.SidebarProse(frame), "2 files, +4 -1")

		harness.Release(agentv1connect.AgentServiceOpenReviewHistoryProcedure)
		assert.Contains(t, paneltest.SidebarProse(harness.Frame()), "1 file, +1 -1")
	})

	t.Run("one load opens the history once", func(t *testing.T) {
		harness := openReview(t, newEarlierReviewServer(t))

		assert.Len(t, harness.Sent(agentv1connect.AgentServiceOpenReviewHistoryProcedure), 1)
	})

	t.Run("the line saying what changed survives a keypress and a notice", func(t *testing.T) {
		harness := openReview(t, newEarlierReviewServer(t))

		harness.Press("j")
		require.Contains(t, paneltest.SidebarProse(harness.Frame()), "1 file changed since you last reviewed")

		harness.Press("n")
		frame := harness.Frame()
		assert.Contains(t, readEngineLine(frame), "nothing to search for yet")
		assert.Contains(t, paneltest.SidebarProse(frame), "1 file changed since you last reviewed")
	})

	t.Run("narrowing onto a snapshot with nothing new keeps the whole change and says so", func(t *testing.T) {
		server := newEarlierReviewServer(t)
		harness := openReview(t, server)
		server.repo.Wrote(gittest.WroteParams{Tree: gittest.UnmovedTree, Whole: gittest.Patch})
		harness.Press("esc", "esc", "space", "r")

		harness.Press("<")

		frame := harness.Frame()
		assert.True(t, isBeside(harness))
		assert.Contains(t, paneltest.StageText(frame), "greet.go")
		assert.Contains(t, readEngineLine(frame), "nothing written")
	})

	t.Run("the newest snapshot takes a comment measured from the snapshot reviewed before", func(t *testing.T) {
		harness := openReview(t, newEarlierReviewServer(t))

		harness.Press("<", "space", "c")
		typeText(harness, "this line")
		harness.Press("enter")

		harness.AssertSentTo(agentv1connect.AgentServiceCreateReviewThreadProcedure, &agentv1.CreateReviewThreadRequest{
			Task:        &agentv1.CreateReviewThreadRequest_TaskId{TaskId: "t1"},
			FilePath:    "second.go",
			NewStart:    1,
			NewEnd:      1,
			QuotedLines: []string{"+package greet"},
			Body:        "this line",
			BaseSha:     gittest.SnapshotSHA,
			SnapshotSha: gittest.NameSnapshot("tree-with-a-second-file"),
		})
	})

	t.Run("a range ending before the newest snapshot draws commenting off", func(t *testing.T) {
		harness := openReviewAt(t, newEarlierReviewServer(t), paneltest.Params{Height: 50})
		harness.Press("<", "<")
		require.Contains(t, paneltest.StageText(harness.Frame()), "where the task starts ▸ snapshot 1")

		harness.Press("space")
		assert.Contains(t, harness.Frame(), "new comments go on snapshot 2")

		harness.Press("c")
		assert.NotContains(t, harness.Frame(), "  Comment")
		assert.Empty(t, harness.Sent(agentv1connect.AgentServiceCreateReviewThreadProcedure))
	})

	t.Run("a viewed mark taken in a snapshot leaves the whole change's mark alone", func(t *testing.T) {
		server := newEarlierReviewServer(t)
		server.repo = gittest.NewRepo(server.repo.Root)
		server.repo.Wrote(gittest.WroteParams{
			Tree:  "tree-written-over",
			Whole: gittest.PatchWrittenOver,
			Since: patchOfGreetWrittenOverSince,
			Sources: map[string]string{
				gittest.File: "package greet\n\nfunc Hello() string {\n\treturn \"written by the agent\"\n}\n",
			},
		})
		harness := openReview(t, server)

		harness.Press("space", "v")
		require.Len(t, harness.Sent(agentv1connect.AgentServiceSetReviewFileViewedProcedure), 1)

		harness.Press("<")
		require.Contains(t, paneltest.StageText(harness.Frame()), "snapshot 1 ▸ snapshot 2")
		require.NotContains(t, paneltest.StageText(harness.Frame()), "greet.go  +")
		harness.Press("space", "v")
		frame := harness.Frame()
		assert.Contains(t, frame, "viewed marks belong to the whole change")
		assert.NotContains(t, paneltest.StageText(frame), "greet.go  +")
		assert.Len(t, harness.Sent(agentv1connect.AgentServiceSetReviewFileViewedProcedure), 1)

		harness.Press("esc", ">")
		assert.Contains(t, paneltest.StageText(harness.Frame()), "greet.go  +")
	})

	t.Run("a comment being written goes with the snapshot a landing load takes away", func(t *testing.T) {
		server := newEarlierReviewServer(t)
		harness := openReview(t, server)
		harness.Press("<")
		harness.Hold(agentv1connect.AgentServiceOpenReviewHistoryProcedure)
		harness.Press("space", "R")
		server.historyErr = errors.New("the server went away")

		harness.Press("space", "c")
		typeText(harness, "hello")
		require.Contains(t, harness.Frame(), "  Comment")
		harness.Release(agentv1connect.AgentServiceOpenReviewHistoryProcedure)

		assert.NotContains(t, harness.Frame(), "  Comment")
		harness.Press("enter")
		assert.Empty(t, harness.Sent(agentv1connect.AgentServiceCreateReviewThreadProcedure))
	})

	t.Run("a range splits the replies written after the snapshot it ends on from the ones before", func(t *testing.T) {
		harness := openReviewAt(t, newLaterRepliesServer(t), paneltest.Params{Height: 50})

		harness.Press("<", "<", "t")

		frame := harness.Frame()
		require.Contains(t, paneltest.StageText(frame), "where the task starts ▸ snapshot 1")
		said := paneltest.PopupProse(frame)
		assert.Contains(t, said, "thanks")
		assert.Less(t, strings.Index(said, "looking"), strings.Index(said, laterRule))
		assert.Less(t, strings.Index(said, laterRule), strings.Index(said, "added it"))
		assert.Contains(t, said, "addressed in snapshot 2")
	})
}

// TestReviewReplay covers a snapshot across a moved base carried onto it before it is compared.
func TestReviewReplay(t *testing.T) {
	t.Run("a snapshot across a moved base is replayed onto it", func(t *testing.T) {
		harness := openReview(t, newMovedBaseServer(t, replayedTree))

		harness.Press("<")

		frame := harness.Frame()
		assert.Contains(t, paneltest.StageText(frame), "snapshot 1 ▸ snapshot 2, replayed across the rebase")
		assert.Contains(t, paneltest.SidebarProse(frame), "1 file, +3 -0")
	})

	t.Run("a snapshot the base rewrote under the agent is refused as conflicted", func(t *testing.T) {
		harness := openReview(t, newMovedBaseServer(t, "a-tree-with-markers-in-it", gittest.File, "second.go"))

		harness.Press("<")

		assert.Contains(t, paneltest.StageText(harness.Frame()), "snapshot 1 ▸ snapshot 2, conflicts in greet.go, second.go")
	})

	t.Run("a git that cannot replay says the base moved between the snapshots", func(t *testing.T) {
		harness := openReview(t, newMovedBaseServer(t, ""))

		harness.Press("<")

		frame := harness.Frame()
		assert.Contains(t, paneltest.StageText(frame), "snapshot 1 ▸ snapshot 2, the base moved between these snapshots")
		assert.NotContains(t, frame, "replayed")
	})

	t.Run("the first snapshot is measured from its own base", func(t *testing.T) {
		server := newMovedBaseServer(t, replayedTree)
		server.repo.Patches[gittest.NamePair(olderBase, gittest.SnapshotSHA)] = gittest.Patch
		server.repo.Patches[gittest.NamePair(gittest.BaseSHA, gittest.SnapshotSHA)] = gittest.PatchOfBothFiles
		harness := openReview(t, server)

		harness.Press("<", "<")

		frame := harness.Frame()
		assert.Contains(t, paneltest.StageText(frame), "where the task starts ▸ snapshot 1")
		assert.Contains(t, paneltest.SidebarProse(frame), "1 file, +1 -1")
	})

	t.Run("an old side counted in the replayed tree pins only there", func(t *testing.T) {
		server := newMovedBaseServer(t, replayedTree)
		server.repo.Patches[gittest.NamePair(replayedTree, gittest.NameSnapshot(secondTree))] = gittest.Patch
		server.threads = append(server.threads, &agentv1.ReviewThread{
			Id:          "t1",
			SnapshotId:  "snapshot-2",
			FilePath:    gittest.File,
			BaseSha:     replayedTree,
			OldStart:    4,
			OldEnd:      4,
			QuotedLines: []string{"-\treturn \"nowhere\""},
			Messages:    []*agentv1.ReviewMessage{newHumanMessage("m1", "written on the replayed snapshot")},
		})
		harness := openReview(t, server)

		harness.Press("<")
		assert.Contains(t, findLine(paneltest.Stage(harness.Frame()), `return "hello"`), "○")

		harness.Press(">")
		frame := harness.Frame()
		assert.NotContains(t, findLine(paneltest.Stage(frame), `return "hello"`), "○")
		assert.Contains(t, paneltest.StageText(frame), "the code these were about has changed")
	})
}

// TestReviewComposer covers the field comments are typed into.
func TestReviewComposer(t *testing.T) {
	t.Run("ctrl+j makes a line rather than sending", func(t *testing.T) {
		harness := openReview(t, newReviewServer(t))

		harness.Press("space", "c", "o", "n", "e", "ctrl+j", "t", "w", "o")

		assert.Contains(t, paneltest.PopupProse(harness.Frame()), "one")
		assert.Contains(t, paneltest.PopupProse(harness.Frame()), "two")
		assert.Empty(t, harness.Sent(agentv1connect.AgentServiceCreateReviewThreadProcedure))
	})

	t.Run("a row with no line of its own refuses the key and says why", func(t *testing.T) {
		harness := openReview(t, newReviewServer(t))

		harness.Press("/")
		typeText(harness, "greet.go")
		harness.Press("enter", "space", "c")

		frame := harness.Frame()
		assert.NotContains(t, frame, "  Comment")
		assert.Contains(t, readOpenMessages(harness), "put the cursor on a line of the diff")
	})
}

// TestReviewEdit covers the reader opening an editor over the agent's worktree.
func TestReviewEdit(t *testing.T) {
	t.Run("the edit key opens the editor over the diff", func(t *testing.T) {
		server := newReviewServer(t)
		harness := openReview(t, server)

		harness.Press("space", "e")

		frame := harness.Frame()
		assert.Contains(t, frame, "editing greet.go")
		assert.NotContains(t, frame, "package greet")

		editor := harness.Tmux().RespawnPanes()
		require.NotEmpty(t, editor)
		assert.Equal(t, paneltest.ColumnPane, editor[len(editor)-1], "the editor is split off the sidebar rather than put on the stage")
		assert.Equal(t, server.agent.GetPaneId(), harness.Tmux().GuestPane())
		assert.Equal(t, paneltest.ColumnPane, harness.Tmux().SelectedPanes()[len(harness.Tmux().SelectedPanes())-1])
		assert.False(t, isBeside(harness))
	})

	t.Run("a working agent refuses the edit key", func(t *testing.T) {
		server := newReviewServer(t)
		server.agent.State = agentv1.AgentState_AGENT_STATE_THINKING
		harness := openReview(t, server)

		harness.Press("space", "e")

		assert.Contains(t, readOpenMessages(harness), "stop it before you edit")
		assert.NotContains(t, harness.Frame(), "editing")
	})

	t.Run("a waiting agent takes the edit key", func(t *testing.T) {
		server := newReviewServer(t)
		server.agent.State = agentv1.AgentState_AGENT_STATE_WAITING
		harness := openReview(t, server)

		harness.Press("space", "e")

		assert.Contains(t, harness.Frame(), "editing greet.go")
	})

	t.Run("a removed worktree refuses the edit key", func(t *testing.T) {
		server := newReviewServer(t)
		server.task.WorktreeRemovalReason = taskv1.WorktreeRemovalReason_WORKTREE_REMOVAL_REASON_GONE
		server.task.Snapshots = 1
		server.workflow.Cwd = server.repo.Root
		harness := openReview(t, server)

		harness.Press("space")
		require.Contains(t, harness.Frame(), "the worktree was removed, so there is no checkout")
		harness.Press("e")

		assert.NotContains(t, harness.Frame(), "editing")
	})

	t.Run("an unreadable pane refuses the edit key", func(t *testing.T) {
		server := newReviewServer(t)
		server.agent.State = agentv1.AgentState_AGENT_STATE_UNCERTAIN
		harness := openReview(t, server)

		harness.Press("space", "e")

		assert.Contains(t, readOpenMessages(harness), "cannot read this agent's pane")
	})

	t.Run("a dormant agent's worktree opens whatever it stopped in", func(t *testing.T) {
		server := newReviewServer(t)
		server.agent.Alive = xptr.Ptr(false)
		server.agent.State = agentv1.AgentState_AGENT_STATE_THINKING
		harness := openReview(t, server)

		harness.Press("space", "e")

		assert.Contains(t, harness.Frame(), "editing greet.go")
	})

	t.Run("a deleted file refuses the edit key and opens no editor", func(t *testing.T) {
		repo := gittest.NewRepo(t.TempDir())
		repo.Sources[gittest.NameSource(gittest.BaseSHA, "gone.go")] = "package greet\n\nfunc Gone() {}\n"
		repo.Wrote(gittest.WroteParams{
			Tree:    "tree-with-a-deletion",
			Whole:   gittest.PatchWithADeletion,
			Sources: map[string]string{gittest.File: gittest.HeadSource},
		})
		harness := openReview(t, newReviewServerOver(repo))

		harness.Press("/")
		typeText(harness, "Gone")
		harness.Press("enter", "space", "e")

		frame := harness.Frame()
		assert.Contains(t, readOpenMessages(harness), "deleted")
		assert.NotContains(t, frame, "editing ·")
	})

	t.Run("the editor quitting gives the stage back and reads the diff again", func(t *testing.T) {
		harness := openReview(t, newReviewServer(t))
		harness.Press("space", "e")
		require.Contains(t, harness.Frame(), "editing greet.go")
		loads := len(harness.Sent(agentv1connect.AgentServiceOpenReviewHistoryProcedure))

		editor := harness.Tmux().RespawnPanes()
		require.NotEmpty(t, editor)
		harness.Tmux().SetPaneDead(editor[len(editor)-1])
		harness.Advance(time.Second)

		assert.NotContains(t, harness.Frame(), "editing ·")
		assert.Contains(t, readMailSaid(t, harness), "had your worktree open in my editor")
		assert.Len(t, harness.Sent(agentv1connect.AgentServiceOpenReviewHistoryProcedure), loads+1)
	})

	t.Run("the cursor resting while the editor is open leaves the editor on the stage", func(t *testing.T) {
		harness := openReview(t, newReviewServer(t))
		harness.Press("space", "e")
		require.Contains(t, harness.Frame(), "editing greet.go")
		swaps, respawns := harness.Tmux().Swaps(), len(harness.Tmux().Respawns())

		wheelOver(harness, paneltest.DefaultWidth-1)
		harness.Press(paneltest.Rest)
		harness.Advance(time.Second)

		assert.Equal(t, swaps, harness.Tmux().Swaps())
		assert.Len(t, harness.Tmux().Respawns(), respawns)
		assert.Empty(t, harness.Sent(agentv1connect.AgentServiceSendMailProcedure))
	})

	t.Run("the editor opens at the repository root the paths are named from", func(t *testing.T) {
		server := newReviewServer(t)
		server.agent.Cwd = filepath.Join(server.repo.Root, "cmd", "greet")
		harness := openReviewAt(t, server, paneltest.Params{Settings: ui.PanelSettings{Editor: "vi"}})

		harness.Press("space", "e")

		require.NotEmpty(t, harness.Tmux().RespawnDirs())
		assert.Equal(t, server.repo.Root, harness.Tmux().RespawnDirs()[0])
		assert.Contains(t, harness.Tmux().Respawns()[0], "'greet.go'")
	})

	t.Run("on an engine elsewhere the editor runs there, through its ssh host", func(t *testing.T) {
		server := newReviewServer(t)
		harness := openReviewAt(t, server, paneltest.Params{Settings: remoteEditSettings(t, "/opt/homebrew/bin/nvim")})

		harness.Press("space", "e")

		require.NotEmpty(t, harness.Tmux().Respawns())
		assert.Contains(t, harness.Tmux().Respawns()[0], "-t 'komrad-remote'")
		assert.Contains(t, harness.Tmux().Respawns()[0], "exec nvim +")
		assert.Contains(t, harness.Tmux().Respawns()[0], filepath.Join(server.repo.Root, "greet.go"))
	})

	t.Run("on an engine elsewhere an editor that cannot reach it opens nothing", func(t *testing.T) {
		harness := openReviewAt(t, newReviewServer(t), paneltest.Params{Settings: remoteEditSettings(t, "goland")})

		harness.Press("space", "e")

		assert.Empty(t, harness.Tmux().Respawns())
		assert.Contains(t, harness.Frame(), "goland cannot open files")
	})

	t.Run("on an engine elsewhere an editor with a window of its own needs the ssh hosts included", func(t *testing.T) {
		settings := remoteEditSettings(t, "code")
		settings.IsSSHIncluded = false
		harness := openReviewAt(t, newReviewServer(t), paneltest.Params{Settings: settings})

		harness.Press("space", "e")

		assert.Empty(t, harness.Tmux().Respawns())
		assert.Contains(t, harness.Frame(), "code cannot find the engine")
	})

	t.Run("on an engine elsewhere with no ssh host written the editor opens nothing", func(t *testing.T) {
		settings := remoteEditSettings(t, "nvim")
		settings.SSHConfig = filepath.Join(t.TempDir(), "missing.conf")
		harness := openReviewAt(t, newReviewServer(t), paneltest.Params{Settings: settings})

		harness.Press("space", "e")

		assert.Empty(t, harness.Tmux().Respawns())
		assert.Contains(t, harness.Frame(), "this machine has no ssh host")
	})

	t.Run("the back key closes the editor before it leaves the screen", func(t *testing.T) {
		harness := openReview(t, newReviewServer(t))

		harness.Press("space", "e", "esc")

		frame := harness.Frame()
		assert.NotContains(t, frame, "editing ·")
		assert.Contains(t, frame, "auth › review")
	})

	t.Run("closing the editor mails the agent the files it opened", func(t *testing.T) {
		server := newReviewServer(t)
		harness := openReview(t, server)

		harness.Press("space", "e", "space", "e")

		said := readMailSaid(t, harness)
		assert.Contains(t, said, "greet.go")
		assert.Contains(t, harness.Frame(), "auth › review")
		assert.Contains(t, harness.Tmux().KilledPanes(), paneltest.ColumnPane)
		assert.NotContains(t, harness.Tmux().KilledPanes(), server.agent.GetPaneId())
		assert.True(t, isBeside(harness))
	})

	t.Run("a diff that stops being readable mails the agent the files", func(t *testing.T) {
		server := newReviewServer(t)
		harness := openReview(t, server)
		harness.Press("space", "e")

		server.repo.RootErr = gittest.ErrNoGit
		harness.Press("space", "R")

		assert.Equal(t, screen.Agents, paneltest.Screen(harness.Frame()))
		assert.Contains(t, readMailSaid(t, harness), "greet.go")
		assert.Contains(t, harness.Tmux().KilledPanes(), paneltest.ColumnPane, "the editor goes with the review")
	})

	t.Run("closing the editor renders the reader's own edits over a pinned diff", func(t *testing.T) {
		server := newReviewServer(t)
		server.threads = []*agentv1.ReviewThread{newThreadOnFirstLine(newReleasedComment("m1"))}
		harness := openReview(t, server)

		harness.Press("space", "e")
		server.repo.Wrote(gittest.WroteParams{Tree: "tree-the-reader-edited", Whole: gittest.PatchWrittenOver})
		harness.Press("space", "e", "j")

		assert.Contains(t, paneltest.StageText(harness.Frame()), "written by the agent")
	})

	t.Run("the gate reads the agent under review rather than the first row", func(t *testing.T) {
		server := newReviewServer(t)
		server.agent.State = agentv1.AgentState_AGENT_STATE_THINKING
		harness := paneltest.Start(t, paneltest.Params{
			Width:  reviewWidth,
			Height: reviewHeight,
			Git:    server.repo,
		})
		server.serve(harness)
		idle := paneltest.Agent("a0", "able", "w1")
		harness.Watch(paneltest.Snapshot(server.workflow, idle, server.agent, server.task))
		require.Contains(t, paneltest.HoveredRow(harness.Frame()), "auth")

		harness.Press("space", "r")
		server.repo.Wrote(newSnapshotWrittenOver())
		server.task.MovedSinceSnapshot = gittest.SnapshotSHA
		harness.Watch(paneltest.Snapshot(server.workflow, server.agent, server.task, idle))
		assert.NotContains(t, harness.Frame(), "written by the agent")

		harness.Press("space", "e")
		assert.Contains(t, readOpenMessages(harness), "stop it before you edit")
		assert.NotContains(t, harness.Frame(), "editing greet.go")
	})
}

const (
	reviewWidth  = 200
	reviewHeight = 30
	olderBase    = "1111111111111111111111111111111111111111"
	replayedTree = "replayed-snapshot-one"
	secondTree   = "tree-with-a-second-file"
)

const patchOfGreetWrittenOverSince = `diff --git a/greet.go b/greet.go
index 23cef252a97433f151c14725c88e0c2dafce63ef..5586caad9270132f074ee5374d4652b83d0acc98 100644
--- a/greet.go
+++ b/greet.go
@@ -1,5 +1,5 @@
 package greet
 
 func Hello() string {
-	return "goodbye"
+	return "written by the agent"
 }
`

type reviewServer struct {
	repo       *gittest.Repo
	workflow   *workflowv1.Workflow
	agent      *agentv1.Agent
	task       *taskv1.Task
	onTask     []*agentv1.Agent
	threads    []*agentv1.ReviewThread
	viewed     []*agentv1.ReviewViewedFile
	snapshots  []*agentv1.ReviewSnapshot
	reviews    []*agentv1.Review
	forge      []proto.Message
	historyErr error
	takeErr    error
}

func newReviewServer(t *testing.T) *reviewServer {
	t.Helper()

	return newReviewServerOver(gittest.NewRepo(t.TempDir()))
}

func newReviewServerOver(repo *gittest.Repo) *reviewServer {
	return &reviewServer{
		repo: repo,
		workflow: &workflowv1.Workflow{
			Id:       "w1",
			Name:     "backend",
			Worktree: &workflowv1.WorktreeConfig{Enabled: true, BaseBranch: "main"},
		},
		agent: &agentv1.Agent{
			Id:         "a1",
			Name:       "auth",
			WorkflowId: "w1",
			Role:       agentv1.AgentRole_AGENT_ROLE_WRITER,
			TaskId:     xptr.Ptr("t1"),
			Cwd:        repo.Root,
			PaneId:     "%9",
			Alive:      xptr.Ptr(true),
			State:      agentv1.AgentState_AGENT_STATE_IDLE,
		},
		task: &taskv1.Task{
			Id:         "t1",
			WorkflowId: "w1",
			Slug:       "auth",
			Cwd:        xptr.Ptr(repo.Root),
		},
	}
}

func openReview(t *testing.T, server *reviewServer) *paneltest.Panel {
	t.Helper()

	return openReviewAt(t, server, paneltest.Params{})
}

func openLongThread(t *testing.T) *paneltest.Panel {
	t.Helper()

	server := newReviewServer(t)
	server.threads = []*agentv1.ReviewThread{newLongThread()}

	return openReviewAt(t, server, paneltest.Params{Height: 24})
}

func openTallReview(t *testing.T) (*paneltest.Panel, *reviewServer) {
	t.Helper()

	server := newReviewServerOver(gittest.NewTallRepo(t.TempDir()))

	return openReviewAt(t, server, paneltest.Params{Height: 40}), server
}

func openReviewAt(t *testing.T, server *reviewServer, params paneltest.Params) *paneltest.Panel {
	t.Helper()

	if params.Width == 0 {
		params.Width = reviewWidth
	}
	if params.Height == 0 {
		params.Height = reviewHeight
	}
	params.Git = server.repo

	harness := paneltest.Start(t, params)
	server.serve(harness)
	harness.Watch(server.buildSnapshot())
	harness.Press("space", "r")

	return harness
}

func (s *reviewServer) buildSnapshot() *watchv1.WatchEvent {
	rows := []proto.Message{s.workflow, s.agent, s.task}
	for _, agent := range s.onTask {
		rows = append(rows, agent)
	}
	for _, thread := range s.threads {
		if thread.GetTaskId() == "" {
			thread.TaskId = s.agent.GetTaskId()
		}
		fillThreadAuthor(thread)
		rows = append(rows, thread)
	}
	for _, review := range s.reviews {
		taskID := review.GetTaskId()
		if taskID == "" {
			taskID = s.agent.GetTaskId()
		}
		rows = append(rows, &agentv1.WatchedReview{
			Id:              review.GetId(),
			TaskId:          taskID,
			SnapshotId:      review.GetSnapshotId(),
			SnapshotOrdinal: review.GetSnapshotOrdinal(),
			AuthorKind:      review.GetAuthorKind(),
			Verdict:         review.GetVerdict(),
			ReleasedAt:      review.GetReleasedAt(),
		})
	}
	rows = append(rows, s.forge...)

	return paneltest.Snapshot(rows...)
}

func (s *reviewServer) serve(harness *paneltest.Panel) {
	harness.Answer(worktreev1connect.WorktreeServiceGetWorktreeProcedure, func(connect.AnyRequest) (connect.AnyResponse, error) {
		return connect.NewResponse(&worktreev1.GetWorktreeResponse{Worktree: &worktreev1.Worktree{Dir: s.repo.Root}}), nil
	})
	harness.Answer(agentv1connect.AgentServiceOpenReviewHistoryProcedure, s.openHistory)
	harness.Answer(agentv1connect.AgentServiceListReviewThreadsProcedure, s.listThreads)
	harness.Answer(agentv1connect.AgentServiceListReviewViewedFilesProcedure, s.listViewed)
	harness.Answer(agentv1connect.AgentServiceSetReviewFileViewedProcedure, s.markViewed)
	harness.Answer(agentv1connect.AgentServiceAddReviewMessageProcedure, s.reply)
	harness.Answer(agentv1connect.AgentServiceSetReviewThreadResolvedProcedure, s.settle)
	harness.Answer(agentv1connect.AgentServiceCreateReviewThreadProcedure, s.createThread)
	harness.Answer(agentv1connect.AgentServiceSendMailProcedure, func(connect.AnyRequest) (connect.AnyResponse, error) {
		return connect.NewResponse(&agentv1.SendMailResponse{}), nil
	})
}

func (s *reviewServer) takeSnapshot() (*agentv1.ReviewSnapshot, error) {
	if s.takeErr != nil {
		return nil, s.takeErr
	}

	sha := gittest.NameSnapshot(s.repo.Tree)
	if len(s.snapshots) == 0 || s.snapshots[len(s.snapshots)-1].GetSha() != sha {
		s.snapshots = append(s.snapshots, &agentv1.ReviewSnapshot{
			Id:      fmt.Sprintf("snapshot-%d", len(s.snapshots)+1),
			TaskId:  s.agent.GetTaskId(),
			Ordinal: int32(len(s.snapshots) + 1),
			BaseSha: gittest.BaseSHA,
			Sha:     sha,
		})
	}

	return s.snapshots[len(s.snapshots)-1], nil
}

func (s *reviewServer) listThreads(connect.AnyRequest) (connect.AnyResponse, error) {
	for _, thread := range s.threads {
		fillThreadAuthor(thread)
	}

	return connect.NewResponse(&agentv1.ListReviewThreadsResponse{Threads: s.threads}), nil
}

func (s *reviewServer) listViewed(connect.AnyRequest) (connect.AnyResponse, error) {
	return connect.NewResponse(&agentv1.ListReviewViewedFilesResponse{Viewed: s.viewed}), nil
}

func (s *reviewServer) markViewed(req connect.AnyRequest) (connect.AnyResponse, error) {
	mark, _ := req.Any().(*agentv1.SetReviewFileViewedRequest)

	kept := make([]*agentv1.ReviewViewedFile, 0, len(s.viewed))
	for _, viewed := range s.viewed {
		if viewed.GetFilePath() != mark.GetFilePath() {
			kept = append(kept, viewed)
		}
	}
	if mark.GetViewed() {
		kept = append(kept, &agentv1.ReviewViewedFile{
			TaskId:      s.agent.GetTaskId(),
			FilePath:    mark.GetFilePath(),
			Fingerprint: mark.GetFingerprint(),
		})
	}
	s.viewed = kept

	return connect.NewResponse(&agentv1.SetReviewFileViewedResponse{}), nil
}

func (s *reviewServer) reply(req connect.AnyRequest) (connect.AnyResponse, error) {
	reply, _ := req.Any().(*agentv1.AddReviewMessageRequest)
	message := &agentv1.ReviewMessage{
		Id:         "m2",
		ThreadId:   reply.GetThreadId(),
		Body:       reply.GetBody(),
		AuthorKind: agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_HUMAN,
		CreatedAt:  timestamppb.New(time.Date(2026, 9, 2, 10, 2, 0, 0, time.UTC)),
	}
	for _, thread := range s.threads {
		if thread.GetId() == reply.GetThreadId() {
			thread.Messages = append(thread.Messages, message)
		}
	}

	return connect.NewResponse(&agentv1.AddReviewMessageResponse{Message: message}), nil
}

func (s *reviewServer) settle(req connect.AnyRequest) (connect.AnyResponse, error) {
	settle, _ := req.Any().(*agentv1.SetReviewThreadResolvedRequest)
	for _, thread := range s.threads {
		if thread.GetId() != settle.GetId() {
			continue
		}
		thread.ResolvedAt = nil
		if settle.GetResolved() {
			thread.ResolvedAt = timestamppb.New(time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC))
		}
	}
	for _, review := range s.reviews {
		var open int32
		for _, thread := range s.threads {
			if thread.GetReviewId() == review.GetId() && thread.GetResolvedAt() == nil {
				open++
			}
		}
		review.OpenThreads = open
	}

	return connect.NewResponse(&agentv1.SetReviewThreadResolvedResponse{}), nil
}

func (s *reviewServer) createThread(req connect.AnyRequest) (connect.AnyResponse, error) {
	create, _ := req.Any().(*agentv1.CreateReviewThreadRequest)
	comment := newHumanMessage(fmt.Sprintf("m%d", len(s.threads)+1), create.GetBody())
	comment.CreatedAt = timestamppb.New(time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC))
	thread := &agentv1.ReviewThread{
		Id:          fmt.Sprintf("t%d", len(s.threads)+1),
		TaskId:      s.agent.GetTaskId(),
		FilePath:    create.GetFilePath(),
		OldStart:    create.GetOldStart(),
		OldEnd:      create.GetOldEnd(),
		NewStart:    create.GetNewStart(),
		NewEnd:      create.GetNewEnd(),
		QuotedLines: create.GetQuotedLines(),
		Messages:    []*agentv1.ReviewMessage{comment},
	}
	s.threads = append(s.threads, thread)

	return connect.NewResponse(&agentv1.CreateReviewThreadResponse{Thread: thread}), nil
}

func newThreadOnFirstLine(messages ...*agentv1.ReviewMessage) *agentv1.ReviewThread {
	return &agentv1.ReviewThread{
		Id:          "t1",
		FilePath:    gittest.File,
		OldStart:    1,
		NewStart:    1,
		QuotedLines: []string{" package greet"},
		Messages:    messages,
	}
}

func newAdriftThread(id string, messages ...*agentv1.ReviewMessage) *agentv1.ReviewThread {
	return &agentv1.ReviewThread{
		Id:          id,
		FilePath:    "gone.go",
		NewStart:    12,
		QuotedLines: []string{"+what this used to say"},
		Messages:    messages,
	}
}

func newThreadOnLastLine(messages ...*agentv1.ReviewMessage) *agentv1.ReviewThread {
	return &agentv1.ReviewThread{
		Id:          "t1",
		FilePath:    gittest.File,
		OldStart:    5,
		NewStart:    5,
		QuotedLines: []string{" }"},
		Messages:    messages,
	}
}

func newLongThread() *agentv1.ReviewThread {
	thread := newThreadOnFirstLine()
	for i := range 6 {
		thread.Messages = append(thread.Messages, newHumanMessage(fmt.Sprintf("m%d", i), fmt.Sprintf("note %d", i)))
	}

	return thread
}

func newAdriftThreads(count int) []*agentv1.ReviewThread {
	threads := make([]*agentv1.ReviewThread, 0, count)
	for i := range count {
		thread := newAdriftThread(fmt.Sprintf("d%d", i), newHumanMessage(fmt.Sprintf("dm%d", i), "still worth reading"))
		thread.FilePath = fmt.Sprintf("gone%d.go", i)
		thread.NewStart = int32(i + 1)
		threads = append(threads, thread)
	}

	return threads
}

func newHumanMessage(id, body string) *agentv1.ReviewMessage {
	return &agentv1.ReviewMessage{
		Id:         id,
		AuthorKind: agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_HUMAN,
		Body:       body,
	}
}

func newAgentMessage(id, body string) *agentv1.ReviewMessage {
	return &agentv1.ReviewMessage{
		Id:            id,
		AuthorKind:    agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_AGENT,
		AuthorAgentId: xptr.Ptr("a1"),
		Body:          body,
	}
}

func typeText(harness *paneltest.Panel, text string) {
	for _, letter := range text {
		key := string(letter)
		if key == " " {
			key = "space"
		}
		harness.Press(key)
	}
}

func findLine(lines []string, text string) string {
	for _, line := range lines {
		if strings.Contains(line, text) {
			return line
		}
	}

	return ""
}

func newReleasedComment(id string) *agentv1.ReviewMessage {
	message := newHumanMessage(id, "")
	message.CreatedAt = timestamppb.New(time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC))

	return message
}

func newReadComment(id string) *agentv1.ReviewMessage {
	message := newReleasedComment(id)
	message.ReadAt = timestamppb.New(time.Date(2026, 9, 2, 10, 1, 0, 0, time.UTC))

	return message
}

func newHeldForgeComment(id, body string) *agentv1.ReviewMessage {
	return &agentv1.ReviewMessage{
		Id:         id,
		AuthorKind: agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_FORGE,
		Body:       body,
		Held:       true,
	}
}

func newSnapshotWrittenOver() gittest.WroteParams {
	return gittest.WroteParams{
		Tree:  "tree-written-again",
		Whole: gittest.PatchWrittenOver,
		Sources: map[string]string{
			gittest.File: "package greet\n\nfunc Hello() string {\n\treturn \"written by the agent\"\n}\n",
		},
	}
}

func newYoursServer(t *testing.T, threads ...*agentv1.ReviewThread) *reviewServer {
	t.Helper()

	server := newReviewServer(t)
	server.threads = threads

	return server
}

func newYourThread(path string, line int32) *agentv1.ReviewThread {
	source := map[int32]string{
		1: " package greet",
		2: " ",
		3: " func Hello() string {",
		5: " }",
	}
	id := fmt.Sprintf("%s:%d", path, line)

	return &agentv1.ReviewThread{
		Id:          id,
		FilePath:    path,
		OldStart:    line,
		NewStart:    line,
		QuotedLines: []string{source[line]},
		Messages:    []*agentv1.ReviewMessage{newReadComment(id + "-m1")},
	}
}

func newEarlierReviewServer(t *testing.T) *reviewServer {
	t.Helper()

	server := newReviewServer(t)
	server.snapshots = []*agentv1.ReviewSnapshot{{
		Id:      "snapshot-1",
		TaskId:  "t1",
		Ordinal: 1,
		BaseSha: gittest.BaseSHA,
		Sha:     gittest.SnapshotSHA,
	}}
	server.threads = []*agentv1.ReviewThread{newWholeChangeThread("snapshot-1", 1, time.Date(2026, 9, 8, 10, 0, 0, 0, time.UTC))}
	server.repo.Wrote(gittest.WroteParams{
		Tree:  "tree-with-a-second-file",
		Whole: gittest.PatchOfBothFiles,
		Since: gittest.PatchOfTheSecondFile,
		Sources: map[string]string{
			gittest.File: gittest.HeadSource,
			"second.go":  "package greet\n\nfunc Second() {}\n",
		},
	})

	return server
}

func newMovedBaseServer(t *testing.T, tree string, conflicts ...string) *reviewServer {
	t.Helper()

	server := newEarlierReviewServer(t)
	server.snapshots[0].BaseSha = olderBase
	server.repo.Patches[gittest.NamePair(gittest.SnapshotSHA, gittest.NameSnapshot(secondTree))] = gittest.PatchOfBothFiles
	server.repo.Patches[gittest.NamePair(replayedTree, gittest.NameSnapshot(secondTree))] = gittest.PatchOfTheSecondFile
	if tree != "" {
		server.repo.StateReplay(gittest.StateReplayParams{
			FromBase:    olderBase,
			OntoBase:    gittest.BaseSHA,
			SnapshotSHA: gittest.SnapshotSHA,
			Tree:        tree,
			Conflicts:   conflicts,
		})
	}

	return server
}

func readMailSaid(t *testing.T, harness *paneltest.Panel) string {
	t.Helper()

	sent := harness.Sent(agentv1connect.AgentServiceSendMailProcedure)
	require.Len(t, sent, 1)
	mail, _ := sent[0].(*agentv1.SendMailRequest)
	assert.Equal(t, "a1", mail.GetToAgentId())

	return mail.GetBody()
}

const (
	midSequenceNotice = "a rebase or merge is part way through, so this is the work as it stood before it"
	historyTimeout    = time.Minute
)

func (s *reviewServer) openHistory(req connect.AnyRequest) (connect.AnyResponse, error) {
	asked, _ := req.Any().(*agentv1.OpenReviewHistoryRequest)
	if s.historyErr != nil {
		return nil, s.historyErr
	}

	answer, err := s.pickSnapshot()
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), historyTimeout)
	defer cancel()

	built := history.Build(ctx, history.BuildParams{
		Git:       s.repo,
		Tracer:    history.NewTracer(s.repo),
		Dir:       s.repo.Root,
		Exclude:   s.workflow.GetWorktree().GetDiffExclude(),
		Snapshot:  historySnapshotOf(answer.GetSnapshot()),
		Snapshots: xslice.Map(s.snapshots, historySnapshotOf),
		Comments:  historyCommentsOf(s.threads, s.snapshots),
		Threads:   xslice.Map(s.threads, historyThreadOf),
		Span:      historySpanOf(asked),
	})

	answer.Snapshots = s.snapshots
	answer.Threads = s.threads
	answer.Resolved = &agentv1.ReviewResolvedSpan{
		BaseSha:               built.Span.BaseSHA,
		HeadSha:               built.Span.HeadSHA,
		Replayed:              built.Span.Replayed,
		AnswerBaseSha:         built.Span.AnswerBaseSHA,
		AnswerHeadSha:         built.Span.AnswerHeadSHA,
		OriginBaseSha:         built.Span.OriginBaseSHA,
		OriginHeadSha:         built.Span.OriginHeadSHA,
		OriginSnapshotBaseSha: built.Span.OriginSnapshotBaseSHA,
		Conflicts:             built.Span.Conflicts,
	}
	answer.Widened = reviewWideningOf(built.Widened)
	answer.ChangedSinceEarlier = int32(built.ChangedSinceEarlier)
	names := map[uuid.UUID]string{}
	for _, snapshot := range s.snapshots {
		names[historyID(snapshot.GetId())] = snapshot.GetId()
	}
	for _, thread := range s.threads {
		names[historyID(thread.GetId())] = thread.GetId()
	}
	answer.Traced = map[string]*agentv1.ReviewTracedPlace{}
	if built.Earlier != nil {
		answer.EarlierSnapshotId = names[built.Earlier.ID]
	}
	if built.EarlierReleasedAt != nil {
		answer.EarlierReleasedAt = dates.Stamp(*built.EarlierReleasedAt)
	}
	for id, place := range built.Traced {
		answer.Traced[names[id]] = &agentv1.ReviewTracedPlace{
			FilePath: place.FilePath,
			NewStart: place.NewStart,
			FileOnly: place.FileOnly,
			Exact:    place.Exact,
		}
	}

	return connect.NewResponse(answer), nil
}

func (s *reviewServer) pickSnapshot() (*agentv1.OpenReviewHistoryResponse, error) {
	snapshot, err := s.takeSnapshot()
	switch {
	case connect.CodeOf(err) == connect.CodeFailedPrecondition && len(s.snapshots) > 0:
		return &agentv1.OpenReviewHistoryResponse{Snapshot: s.snapshots[len(s.snapshots)-1], Notice: midSequenceNotice}, nil
	case err != nil:
		return nil, err
	}

	return &agentv1.OpenReviewHistoryResponse{Snapshot: snapshot}, nil
}

func historySpanOf(req *agentv1.OpenReviewHistoryRequest) history.Span {
	switch {
	case req.GetSnapshots() != nil:
		return history.Span{Snapshots: &history.SnapshotSpan{
			FromSnapshotID: historyID(req.GetSnapshots().GetFromSnapshotId()),
			ToSnapshotID:   historyID(req.GetSnapshots().GetToSnapshotId()),
		}}
	case req.GetAnswer() != nil:
		return history.Span{Answer: &history.AnswerSpan{
			FromSnapshotID: historyID(req.GetAnswer().GetFromSnapshotId()),
			ToSnapshotID:   historyID(req.GetAnswer().GetToSnapshotId()),
		}}
	case req.GetOrigin() != nil:
		return history.Span{Origin: &history.OriginSpan{
			SnapshotID: historyID(req.GetOrigin().GetSnapshotId()),
			BaseSHA:    req.GetOrigin().GetBaseSha(),
		}}
	}

	return history.Span{}
}

func reviewWideningOf(widened *history.Widening) *agentv1.ReviewWidening {
	if widened == nil {
		return nil
	}

	reasons := map[history.WidenedReason]agentv1.ReviewWidenedReason{
		history.WidenedAnswerGone:          agentv1.ReviewWidenedReason_REVIEW_WIDENED_REASON_ANSWER_GONE,
		history.WidenedCommentSnapshotGone: agentv1.ReviewWidenedReason_REVIEW_WIDENED_REASON_COMMENT_SNAPSHOT_GONE,
		history.WidenedRangeSnapshotGone:   agentv1.ReviewWidenedReason_REVIEW_WIDENED_REASON_RANGE_SNAPSHOT_GONE,
		history.WidenedRangeBackwards:      agentv1.ReviewWidenedReason_REVIEW_WIDENED_REASON_RANGE_BACKWARDS,
		history.WidenedAnswerAcrossRebase:  agentv1.ReviewWidenedReason_REVIEW_WIDENED_REASON_ANSWER_ACROSS_REBASE,
	}

	return &agentv1.ReviewWidening{Reason: reasons[widened.Reason], Conflicts: widened.Conflicts}
}

func historyID(id string) uuid.UUID {
	if id == "" {
		return uuid.Nil
	}

	return uuid.NewSHA1(uuid.Nil, []byte(id))
}

func historySnapshotOf(snapshot *agentv1.ReviewSnapshot) history.Snapshot {
	return history.Snapshot{
		ID:      historyID(snapshot.GetId()),
		Ordinal: snapshot.GetOrdinal(),
		BaseSHA: snapshot.GetBaseSha(),
		SHA:     snapshot.GetSha(),
	}
}

func historyCommentsOf(threads []*agentv1.ReviewThread, snapshots []*agentv1.ReviewSnapshot) []history.Comment {
	var comments []history.Comment
	for _, thread := range threads {
		for _, snapshot := range snapshots {
			if snapshot.GetId() != thread.GetSnapshotId() {
				continue
			}
			for _, message := range thread.GetMessages() {
				if message.GetAuthorKind() != agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_HUMAN {
					continue
				}
				released := message.GetCreatedAt().AsTime()
				comments = append(comments, history.Comment{
					SnapshotID:      historyID(snapshot.GetId()),
					SnapshotOrdinal: snapshot.GetOrdinal(),
					ReleasedAt:      released,
				})
			}
		}
	}

	return comments
}

func historyThreadOf(thread *agentv1.ReviewThread) history.Thread {
	return history.Thread{
		ID:         historyID(thread.GetId()),
		SnapshotID: historyID(thread.GetSnapshotId()),
		FilePath:   thread.GetFilePath(),
		NewStart:   thread.GetNewStart(),
		NewEnd:     thread.GetNewEnd(),
	}
}

func remoteEditSettings(t *testing.T, editor string) ui.PanelSettings {
	t.Helper()

	config := filepath.Join(t.TempDir(), "komrad-remote.conf")
	require.NoError(t, os.WriteFile(config, []byte("Host komrad-remote\n"), 0o600))

	return ui.PanelSettings{Editor: editor, SSHHost: "komrad-remote", SSHConfig: config, IsSSHIncluded: true}
}

func newWholeChangeThread(snapshotID string, ordinal int32, at time.Time) *agentv1.ReviewThread {
	remark := newHumanMessage("w1", "the shape is right")
	remark.CreatedAt = dates.Stamp(at)

	return &agentv1.ReviewThread{
		Id:              "w1",
		SnapshotId:      snapshotID,
		SnapshotOrdinal: ordinal,
		Messages:        []*agentv1.ReviewMessage{remark},
	}
}

// fillThreadAuthor gives a thread the author the server would, the one who opened it.
func fillThreadAuthor(thread *agentv1.ReviewThread) {
	if thread.GetAuthorKind() != agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_UNSPECIFIED || len(thread.GetMessages()) == 0 {
		return
	}

	opening := thread.GetMessages()[0]
	thread.AuthorKind = opening.GetAuthorKind()
	thread.AuthorAgentId = opening.AuthorAgentId
}

func isBeside(harness *paneltest.Panel) bool {
	return !harness.Tmux().IsZoomed() && harness.Tmux().SidebarWidth() > config.DefaultSidebarWidth
}
