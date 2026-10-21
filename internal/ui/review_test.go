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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

// TestReviewOpen covers which agents the review key opens a diff for and what it renders.
func TestReviewOpen(t *testing.T) {
	t.Run("an agent that has written nothing is refused", func(t *testing.T) {
		repo := gittest.NewRepo(t.TempDir())
		repo.Wrote(gittest.WroteParams{Tree: "tree-of-the-base"})

		harness := openReview(t, newReviewServerOver(repo))

		frame := harness.readFrame()
		assert.Equal(t, agentsScreen, harness.readScreen())
		assert.Contains(t, frame, "Nothing to review in auth")
		assert.Contains(t, readOpenMessages(harness), "no change against the base branch")
	})

	t.Run("a task's row opens the review of its task", func(t *testing.T) {
		server := newReviewServer(t)
		server.task.Slug = "login-fix"
		harness := newPanelHarness(t, panelHarnessParams{
			width:  reviewWidth,
			height: reviewHeight,
			git:    server.repo,
		})
		server.serve(harness)
		harness.watch(server.buildSnapshot())
		require.Contains(t, readHoveredRow(harness.readFrame()), "login-fix")

		harness.press("space", "r")

		assert.True(t, harness.panel.IsBeside())
		harness.assertSentTo(agentv1connect.AgentServiceOpenReviewHistoryProcedure, &agentv1.OpenReviewHistoryRequest{Task: "t1"})
	})

	t.Run("an agent whose worktree was removed opens from its workflow's directory", func(t *testing.T) {
		server := newReviewServer(t)
		server.task.WorktreeRemovalReason = taskv1.WorktreeRemovalReason_WORKTREE_REMOVAL_REASON_GONE
		server.task.Snapshots = 1
		server.workflow.Cwd = server.repo.Root

		harness := openReview(t, server)

		assert.True(t, harness.panel.IsBeside())
		assert.Contains(t, readStage(harness.readFrame()), "greet.go")
	})

	t.Run("an agent whose worktree was removed in a workflow with no directory is not opened", func(t *testing.T) {
		server := newReviewServer(t)
		server.task.WorktreeRemovalReason = taskv1.WorktreeRemovalReason_WORKTREE_REMOVAL_REASON_GONE
		server.task.Snapshots = 1

		harness := openReview(t, server)

		assert.Equal(t, agentsScreen, harness.readScreen())
		assert.Empty(t, harness.readSent(agentv1connect.AgentServiceOpenReviewHistoryProcedure))
	})

	t.Run("an agent whose worktree was removed with no snapshot is not opened", func(t *testing.T) {
		server := newReviewServer(t)
		server.task.WorktreeRemovalReason = taskv1.WorktreeRemovalReason_WORKTREE_REMOVAL_REASON_GONE
		server.workflow.Cwd = server.repo.Root

		harness := openReview(t, server)

		assert.Equal(t, agentsScreen, harness.readScreen())
		assert.Empty(t, harness.readSent(agentv1connect.AgentServiceOpenReviewHistoryProcedure))
	})

	t.Run("an agent part way through a rebase opens on its newest snapshot", func(t *testing.T) {
		server := newEarlierReviewServer(t)
		server.takeErr = connect.NewError(connect.CodeFailedPrecondition, errors.New("a rebase is part way through"))

		harness := openReview(t, server)

		frame := harness.readFrame()
		assert.True(t, harness.panel.IsBeside())
		assert.Contains(t, readSidebarProse(frame), "a rebase or merge is part way through, so this is the work as it stood before it")
		assert.Contains(t, readSidebarProse(frame), "1 file")
	})

	t.Run("a diff git cannot read is refused with the reason", func(t *testing.T) {
		repo := gittest.NewRepo(t.TempDir())
		repo.RootErr = gittest.ErrNoGit

		harness := openReview(t, newReviewServerOver(repo))

		frame := harness.readFrame()
		assert.Equal(t, agentsScreen, harness.readScreen())
		assert.Contains(t, frame, "Could not read the diff of")
		assert.Contains(t, readOpenMessages(harness), gittest.ErrNoGit.Error())
	})

	t.Run("a running agent stays on the stage beside the diff", func(t *testing.T) {
		server := newReviewServer(t)
		harness := newPanelHarness(t, panelHarnessParams{width: reviewWidth, height: reviewHeight, git: server.repo})
		server.serve(harness)
		harness.watch(server.buildSnapshot())
		harness.press("space")
		menuUp := len(harness.panel.ReadZooms())

		harness.press("r")

		frame := harness.readFrame()
		assert.False(t, harness.panel.IsZoomed())
		assert.True(t, harness.panel.IsBeside())
		assert.Equal(t, server.agent.GetPaneId(), harness.panel.ReadGuestPane())
		assert.Equal(t, []bool{false}, harness.panel.ReadZooms()[menuUp:], "the menu's zoom is let go once")
		assert.Contains(t, readStage(frame), "greet.go")
		assert.Contains(t, readSidebarProse(frame), "1 file, +1 -1")
		assert.Len(t, splitLines(frame), reviewHeight)
	})

	t.Run("a stopped agent's diff is rendered beside the sidebar over the whole window", func(t *testing.T) {
		server := newReviewServer(t)
		server.agent.Alive = xptr.Ptr(false)
		harness := openReview(t, server)

		frame := harness.readFrame()
		assert.True(t, harness.panel.IsZoomed())
		assert.False(t, harness.panel.IsBeside())
		assert.Contains(t, readStage(frame), "greet.go")
		assert.Len(t, splitLines(frame), reviewHeight)
	})

	t.Run("a running agent stopping hands the diff the whole window", func(t *testing.T) {
		server := newReviewServer(t)
		harness := openReview(t, server)
		require.True(t, harness.panel.IsBeside())

		server.agent.Alive = xptr.Ptr(false)
		harness.watch(server.buildSnapshot())

		assert.True(t, harness.panel.IsZoomed())
		assert.False(t, harness.panel.IsBeside())
		assert.Contains(t, readStage(harness.readFrame()), "greet.go")
	})

	t.Run("a window too narrow for the agent beside the diff hands the diff the whole window", func(t *testing.T) {
		harness := openReviewAt(t, newReviewServer(t), panelHarnessParams{width: 120})

		assert.True(t, harness.panel.IsZoomed())
		assert.False(t, harness.panel.IsBeside())
		assert.Contains(t, readStage(harness.readFrame()), "greet.go")
	})

	t.Run("the window narrowing under the review hands the diff the whole window", func(t *testing.T) {
		harness := openReview(t, newReviewServer(t))
		require.True(t, harness.panel.IsBeside())

		harness.panel.SetWindowWidth(120)
		harness.send(tea.WindowSizeMsg{Width: 120, Height: reviewHeight})

		assert.True(t, harness.panel.IsZoomed())
		assert.False(t, harness.panel.IsBeside())
	})

	t.Run("the frame renders while the diff loads", func(t *testing.T) {
		server := newReviewServer(t)
		harness := newPanelHarness(t, panelHarnessParams{
			width:  reviewWidth,
			height: 40,
			git:    server.repo,
		})
		server.serve(harness)
		harness.watch(server.buildSnapshot())
		harness.hold("git.Diff")

		harness.press("space", "r", "?")

		frame := harness.readFrame()
		assert.Contains(t, readStageText(frame), "loading the diff")
		assert.Contains(t, frame, "hide help")
	})

	t.Run("the wheel while the diff loads moves nothing", func(t *testing.T) {
		server := newReviewServer(t)
		harness := newPanelHarness(t, panelHarnessParams{
			width:  reviewWidth,
			height: 40,
			git:    server.repo,
		})
		server.serve(harness)
		harness.watch(server.buildSnapshot())
		harness.hold("git.Diff")
		harness.press("space", "r")

		for _, x := range []int{1, reviewWidth - 1} {
			harness.send(tea.MouseWheelMsg{
				X:      x,
				Y:      2,
				Button: tea.MouseWheelDown,
			})
		}

		assert.Contains(t, readStageText(harness.readFrame()), "loading the diff")
	})

	t.Run("a worktree holding only an untracked file opens", func(t *testing.T) {
		repo := gittest.NewRepo(t.TempDir())
		repo.Wrote(gittest.WroteParams{
			Tree:    "tree-with-an-untracked-file",
			Whole:   gittest.PatchOfAnAddedFile,
			Sources: map[string]string{"new.go": "package greet\n"},
		})

		harness := openReview(t, newReviewServerOver(repo))

		assert.Contains(t, readStage(harness.readFrame()), "new.go")
	})

	t.Run("an agent in a workflow that cuts no worktrees opens", func(t *testing.T) {
		server := newReviewServer(t)
		server.workflow.Worktree = nil

		harness := openReview(t, server)

		assert.Contains(t, readStage(harness.readFrame()), "greet.go")
	})

	t.Run("an agent kept in the directory it was given opens", func(t *testing.T) {
		server := newReviewServer(t)
		server.agent.PaneId = ""
		server.agent.Alive = nil

		harness := openReview(t, server)

		assert.Contains(t, readStage(harness.readFrame()), "greet.go")
	})
}

// TestReviewCursor covers the keys that move through the diff.
func TestReviewCursor(t *testing.T) {
	t.Run("the movement keys walk the lines", func(t *testing.T) {
		harness := openReview(t, newReviewServer(t))
		require.Equal(t, "greet.go:1", readReviewCursor(harness.readFrame()))
		respawns, sentKeys := harness.panel.CountRespawns(), harness.panel.CountSentKeys()

		harness.press("j")

		assert.Equal(t, "greet.go:2", readReviewCursor(harness.readFrame()))
		assert.Equal(t, respawns, harness.panel.CountRespawns())
		assert.Equal(t, sentKeys, harness.panel.CountSentKeys())
	})

	t.Run("the scroll keys turn the cursor a few rows and back", func(t *testing.T) {
		harness := openReview(t, newReviewServer(t))

		harness.press("ctrl+n")
		assert.Equal(t, "greet.go:4 (removed)", readReviewCursor(harness.readFrame()))

		harness.press("ctrl+p")
		assert.Equal(t, "greet.go:1", readReviewCursor(harness.readFrame()))
	})

	t.Run("the wheel over the diff turns the cursor", func(t *testing.T) {
		harness := openReview(t, newReviewServer(t))

		wheelOver(harness, defaultHarnessWidth-1)

		assert.Equal(t, "greet.go:4 (removed)", readReviewCursor(harness.readFrame()))
	})

	t.Run("the wheel over the diff leaves a comment being written on its line", func(t *testing.T) {
		harness := openReview(t, newReviewServer(t))
		harness.press("space", "c")
		typeText(harness, "still here")

		wheelOver(harness, defaultHarnessWidth-1)
		harness.press("enter")

		harness.assertSentTo(agentv1connect.AgentServiceCreateReviewThreadProcedure, &agentv1.CreateReviewThreadRequest{
			Task:        xptr.Ptr("t1"),
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
		top := readReviewCursor(harness.readFrame())

		harness.press("ctrl+d")
		assert.Equal(t, "greet.go:5", readReviewCursor(harness.readFrame()))

		harness.press("ctrl+u")
		assert.Equal(t, top, readReviewCursor(harness.readFrame()))
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

		harness.press("l")
		assert.Contains(t, findLine(readStage(harness.readFrame()), "1 +"), "1 +  panned-to-here")

		harness.press("0")
		assert.Contains(t, harness.readFrame(), wide)
	})

	t.Run("wrapping renders a wide line whole and stops the pan", func(t *testing.T) {
		wide := strings.Repeat("a", reviewWidth) + "wrapped"
		repo := gittest.NewRepo(t.TempDir())
		repo.Wrote(gittest.WroteParams{
			Tree:    "tree-with-a-wide-line",
			Whole:   "diff --git a/wide.go b/wide.go\nnew file mode 100644\n--- /dev/null\n+++ b/wide.go\n@@ -0,0 +1 @@\n+" + wide + "\n",
			Sources: map[string]string{"wide.go": wide + "\n"},
		})
		harness := openReviewAt(t, newReviewServerOver(repo), panelHarnessParams{height: 40})

		harness.press("?")
		assert.Contains(t, readLegend(harness.readFrame()), "pan sideways")

		harness.press("space")
		assert.Contains(t, harness.readFrame(), "wrap lines")

		harness.press("w")
		stage := readStage(harness.readFrame())
		rendered := strings.Join(stage, "\n")
		assert.Contains(t, rendered, "wrapped")
		assert.Greater(t, strings.Count(rendered, "aaaaaaaaaa"), 1, "the line runs on under itself")

		harness.press("l")
		assert.Equal(t, stage, readStage(harness.readFrame()), "the pan keys do nothing while lines wrap")

		assert.NotContains(t, readLegend(harness.readFrame()), "pan sideways")

		harness.press("space")
		assert.Contains(t, harness.readFrame(), "stop wrapping lines")
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
		harness := openReviewAt(t, server, panelHarnessParams{height: 20})

		harness.press("]")
		jumped := readStage(harness.readFrame())
		require.Equal(t, "second.go:1", readReviewCursor(harness.readFrame()))

		harness.press("l", "0")
		assert.Equal(t, jumped, readStage(harness.readFrame()))

		harness.watch(server.buildSnapshot())
		assert.Equal(t, jumped, readStage(harness.readFrame()))
	})

	t.Run("the keys come from the keymap", func(t *testing.T) {
		keys := config.DefaultKeybindings()
		keys.Review.Compose = []string{"i"}
		harness := openReviewAt(t, newReviewServer(t), panelHarnessParams{height: 40, keys: &keys})

		harness.press("space")
		assert.Regexp(t, `comment on this line +i`, harness.readFrame())
		harness.press("esc")

		harness.press("space", "c")
		assert.NotContains(t, harness.readFrame(), "  Comment")

		harness.press("i")
		assert.Contains(t, harness.readFrame(), "  Comment")
	})
}

// TestReviewThreads covers writing, reading and settling the conversations pinned to the diff.
func TestReviewThreads(t *testing.T) {
	t.Run("the compose key opens a thread on the line under the cursor", func(t *testing.T) {
		harness := openReview(t, newReviewServer(t))

		harness.press("space", "c")
		typeText(harness, "this should say hello")
		harness.press("enter")

		harness.assertSentTo(agentv1connect.AgentServiceCreateReviewThreadProcedure, &agentv1.CreateReviewThreadRequest{
			Task:        xptr.Ptr("t1"),
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
		assert.NotContains(t, harness.readFrame(), "  Comment")
	})

	t.Run("enter releases the comment and the thread shows released then read", func(t *testing.T) {
		server := newReviewServer(t)
		harness := openReview(t, server)

		harness.press("space", "c")
		typeText(harness, "this should say hello")
		harness.press("enter")
		harness.watch(server.buildSnapshot())

		frame := harness.readFrame()
		assert.Contains(t, readPopupProse(frame), "this should say hello")
		assert.Contains(t, readPopupProse(frame), "→ released")

		server.threads[0].Messages[0].ReadAt = "2026-09-02T10:01:00Z"
		harness.watch(server.buildSnapshot())

		assert.Contains(t, readPopupProse(harness.readFrame()), "✓ read")
	})

	t.Run("a remark on the whole change stands at the foot of the diff", func(t *testing.T) {
		server := newReviewServer(t)
		server.threads = []*agentv1.ReviewThread{newWholeChangeThread("", 0, "2026-09-02T10:00:00Z")}
		harness := openReview(t, server)

		harness.press("t")

		frame := harness.readFrame()
		assert.Contains(t, readStageText(frame), "on the whole change")
		assert.Contains(t, readSidebarProse(frame), "the whole change")
		assert.Contains(t, readPopupProse(frame), "the shape is right")
		assert.NotContains(t, readPopupProse(frame), "has gone")
	})

	t.Run("a comment the server refuses stays in the composer", func(t *testing.T) {
		harness := openReview(t, newReviewServer(t))
		harness.refuse(agentv1connect.AgentServiceCreateReviewThreadProcedure,
			connect.NewError(connect.CodeFailedPrecondition, errors.New("not now")))

		harness.press("space", "c")
		typeText(harness, "this should say hello")
		harness.press("enter")

		frame := harness.readFrame()
		assert.Contains(t, frame, "  Comment")
		assert.Contains(t, frame, "this should say hello")
	})

	t.Run("a refused comment is sent again on the line it was written on", func(t *testing.T) {
		harness := openReview(t, newReviewServer(t))
		harness.refuse(agentv1connect.AgentServiceCreateReviewThreadProcedure,
			connect.NewError(connect.CodeFailedPrecondition, errors.New("not now")))
		harness.hold(agentv1connect.AgentServiceCreateReviewThreadProcedure)

		harness.press("space", "c")
		typeText(harness, "this should say hello")
		harness.press("enter")
		harness.press("j", "j", "j")
		harness.release(agentv1connect.AgentServiceCreateReviewThreadProcedure)
		harness.press("enter")

		sent := harness.readSent(agentv1connect.AgentServiceCreateReviewThreadProcedure)
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

		sidebar := readPopupProse(harness.readFrame())
		assert.Contains(t, sidebar, "you wrong greeting")
	})

	t.Run("a thread nobody has read is marked read once the cursor rests on it", func(t *testing.T) {
		thread := newThreadOnFirstLine(newHumanMessage("m1", "wrap this"), newAgentMessage("m2", "wrapped it"))
		thread.UnreadByHuman = true
		server := newReviewServer(t)
		server.threads = []*agentv1.ReviewThread{thread}

		harness := openReview(t, server)

		harness.press("j", "k")
		assert.Empty(t, harness.readSent(agentv1connect.AgentServiceMarkReviewThreadReadProcedure))

		harness.press(rest)
		harness.assertSentTo(agentv1connect.AgentServiceMarkReviewThreadReadProcedure, &agentv1.MarkReviewThreadReadRequest{Id: "t1"})
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

		sidebar := readPopupProse(harness.readFrame())
		assert.Contains(t, sidebar, "reviewer this drops the error")
		assert.Contains(t, sidebar, "writer wrapped it")
	})

	t.Run("a conversation an agent opened shows until the menu hides it", func(t *testing.T) {
		server := newReviewServer(t)
		server.threads = []*agentv1.ReviewThread{newThreadOnFirstLine(newAgentMessage("m1", "renamed it, is that what you meant?"))}
		harness := openReview(t, server)
		require.Contains(t, readPopupProse(harness.readFrame()), "renamed it")

		harness.press("space")
		require.Contains(t, harness.readFrame(), "hide agents' conversations")
		harness.press("b")
		assert.NotContains(t, harness.readFrame(), "renamed it")

		harness.press("space", "b")
		assert.Contains(t, readPopupProse(harness.readFrame()), "renamed it")
	})

	t.Run("a conversation an agent opened stays rendered once the person has replied in it", func(t *testing.T) {
		server := newReviewServer(t)
		reply := newHumanMessage("m2", "yes, keep it")
		server.threads = []*agentv1.ReviewThread{newThreadOnFirstLine(newAgentMessage("m1", "renamed it, is that what you meant?"), reply)}
		harness := openReview(t, server)

		assert.Contains(t, readPopupProse(harness.readFrame()), "yes, keep it")
	})

	t.Run("the compose key on a line with a thread replies in it", func(t *testing.T) {
		server := newReviewServer(t)
		server.threads = []*agentv1.ReviewThread{newThreadOnFirstLine(newHumanMessage("m1", "renamed it, is that what you meant?"))}
		harness := openReviewAt(t, server, panelHarnessParams{height: 40})

		harness.press("space")
		require.Contains(t, harness.readFrame(), "reply in this thread")

		harness.press("c")
		typeText(harness, "yes, that is it")
		harness.press("enter")

		harness.assertSentTo(agentv1connect.AgentServiceAddReviewMessageProcedure, &agentv1.AddReviewMessageRequest{
			ThreadId: "t1",
			Body:     "yes, that is it",
		})
		assert.Empty(t, harness.readSent(agentv1connect.AgentServiceCreateReviewThreadProcedure))
	})

	t.Run("a refused reply reopens under the thread it replies in after the cursor moved on", func(t *testing.T) {
		server := newReviewServer(t)
		last := newThreadOnLastLine(newHumanMessage("m2", "close the brace"))
		last.Id = "t2"
		server.threads = []*agentv1.ReviewThread{newThreadOnFirstLine(newHumanMessage("m1", "rename the package")), last}
		harness := openReviewAt(t, server, panelHarnessParams{height: 40})
		harness.refuse(agentv1connect.AgentServiceAddReviewMessageProcedure,
			connect.NewError(connect.CodeFailedPrecondition, errors.New("not now")))
		harness.hold(agentv1connect.AgentServiceAddReviewMessageProcedure)

		harness.press("space", "c")
		typeText(harness, "yes, that is it")
		harness.press("enter")
		harness.press("t")
		harness.release(agentv1connect.AgentServiceAddReviewMessageProcedure)

		popup := readPopupProse(harness.readFrame())
		assert.Contains(t, popup, "rename the package")
		assert.NotContains(t, popup, "close the brace")
		assert.Contains(t, popup, "yes, that is it")
	})

	t.Run("the thread key reaches the only thread and stays on it", func(t *testing.T) {
		server := newReviewServer(t)
		server.threads = []*agentv1.ReviewThread{newThreadOnLastLine()}
		harness := openReview(t, server)

		harness.press("t")
		require.Equal(t, "greet.go:5", readReviewCursor(harness.readFrame()))

		harness.press("t")
		assert.Equal(t, "greet.go:5", readReviewCursor(harness.readFrame()))
	})

	t.Run("a thread whose file left the diff is reachable and says where it was", func(t *testing.T) {
		server := newReviewServer(t)
		server.threads = []*agentv1.ReviewThread{newAdriftThread("t1", newHumanMessage("m1", "still worth reading"))}
		harness := openReview(t, server)

		harness.press("t")

		frame := harness.readFrame()
		assert.Contains(t, readPopupProse(frame), "still worth reading")
		assert.Contains(t, readPopupProse(frame), "+what this used to say")
	})

	t.Run("a thread whose file left the diff takes a reply", func(t *testing.T) {
		server := newReviewServer(t)
		server.threads = []*agentv1.ReviewThread{newAdriftThread("t1", newHumanMessage("m1", "why did this go?"))}
		harness := openReview(t, server)

		harness.press("t", "space", "c")
		typeText(harness, "because")
		harness.press("enter")

		harness.assertSentTo(agentv1connect.AgentServiceAddReviewMessageProcedure, &agentv1.AddReviewMessageRequest{
			ThreadId: "t1",
			Body:     "because",
		})
	})

	t.Run("the settle key settles a thread and presses again to reopen it", func(t *testing.T) {
		server := newReviewServer(t)
		server.threads = []*agentv1.ReviewThread{newThreadOnFirstLine(newHumanMessage("m1", "why did this change?"))}
		harness := openReview(t, server)

		harness.press("space", "r")
		harness.watch(server.buildSnapshot())
		assert.Contains(t, findLine(readStage(harness.readFrame()), "package greet"), "✓")

		harness.press("space", "r")
		harness.watch(server.buildSnapshot())
		assert.NotContains(t, findLine(readStage(harness.readFrame()), "package greet"), "✓")

		harness.assertSentTo(agentv1connect.AgentServiceSetReviewThreadResolvedProcedure,
			&agentv1.SetReviewThreadResolvedRequest{Id: "t1", Resolved: true},
			&agentv1.SetReviewThreadResolvedRequest{Id: "t1", Resolved: false},
		)
	})

	t.Run("a settle in flight is rendered until it lands", func(t *testing.T) {
		server := newReviewServer(t)
		server.threads = []*agentv1.ReviewThread{newThreadOnFirstLine(newHumanMessage("m1", "why did this change?"))}
		harness := openReview(t, server)
		harness.hold(agentv1connect.AgentServiceSetReviewThreadResolvedProcedure)

		harness.press("space", "r")
		assert.Contains(t, harness.readFrame(), "settling")

		harness.release(agentv1connect.AgentServiceSetReviewThreadResolvedProcedure)
		assert.NotContains(t, harness.readFrame(), "settling")
	})

	t.Run("a thread whose file left the diff settles like any other", func(t *testing.T) {
		server := newReviewServer(t)
		server.threads = []*agentv1.ReviewThread{newAdriftThread("t1", newHumanMessage("m1", "why did this go?"))}
		harness := openReview(t, server)

		harness.press("t", "space", "r")
		harness.watch(server.buildSnapshot())

		assert.Contains(t, findLine(readStage(harness.readFrame()), "gone.go:12"), "✓")
		harness.assertSentTo(agentv1connect.AgentServiceSetReviewThreadResolvedProcedure,
			&agentv1.SetReviewThreadResolvedRequest{Id: "t1", Resolved: true})
	})

	t.Run("a thread deleted from under the cursor leaves it on a row that exists", func(t *testing.T) {
		server := newReviewServer(t)
		server.threads = []*agentv1.ReviewThread{newAdriftThread("t1", newHumanMessage("m1", "why did this go?"))}
		harness := openReview(t, server)
		harness.press("t")

		server.threads = nil
		harness.watch(server.buildSnapshot())

		assert.Equal(t, "greet.go:5", readReviewCursor(harness.readFrame()))
	})

	t.Run("the composer takes every key but escape as a character", func(t *testing.T) {
		harness := openReview(t, newReviewServer(t))

		harness.press("space", "c", "?", "l", "n")
		frame := harness.readFrame()
		assert.Contains(t, readPopupProse(frame), "?ln")
		assert.Contains(t, frame, "space menu")

		harness.press("esc")
		assert.NotContains(t, harness.readFrame(), "  Comment")
	})
}

// TestReviewPopupScroll covers reading a conversation taller than its popup.
func TestReviewPopupScroll(t *testing.T) {
	t.Run("the popup opens on the end of the conversation", func(t *testing.T) {
		harness := openLongThread(t)

		popup := readPopupProse(harness.readFrame())
		assert.Contains(t, popup, "note 5")
		assert.NotContains(t, popup, "note 0")
	})

	t.Run("the scroll keys move the conversation and leave the diff cursor", func(t *testing.T) {
		harness := openLongThread(t)
		for range 20 {
			harness.press("ctrl+p")
		}

		frame := harness.readFrame()
		assert.Contains(t, readPopupProse(frame), "note 0")
		assert.NotContains(t, readPopupProse(frame), "note 5")

		for range 20 {
			harness.press("ctrl+n")
		}

		assert.Contains(t, readPopupProse(harness.readFrame()), "note 5")
	})

	t.Run("a scroll key into the conversation drops a top key pressed once before it", func(t *testing.T) {
		server := newReviewServer(t)
		thread := newLongThread()
		thread.OldStart, thread.NewStart = 3, 3
		thread.QuotedLines = []string{" func Hello() string {"}
		server.threads = []*agentv1.ReviewThread{thread}
		harness := openReviewAt(t, server, panelHarnessParams{height: 24})
		harness.press("j", "j")
		require.Contains(t, readPopupProse(harness.readFrame()), "note 5")

		harness.press("g", "ctrl+p", "g")

		assert.Contains(t, readPopupProse(harness.readFrame()), "note")
	})

	t.Run("the wheel over the popup scrolls the conversation and leaves the diff", func(t *testing.T) {
		harness := openLongThread(t)
		x, y := readPopupMiddle(harness.readRendered())

		harness.send(tea.MouseWheelMsg{
			X:      x,
			Y:      y,
			Button: tea.MouseWheelUp,
		})

		frame := harness.readFrame()
		assert.NotContains(t, readPopupProse(frame), "note 5")
		assert.Contains(t, readPopupProse(frame), "note")
	})

	t.Run("the wheel beside the popup moves the diff", func(t *testing.T) {
		for _, x := range []int{1, reviewWidth - 1} {
			harness := openLongThread(t)
			require.Contains(t, readPopupProse(harness.readFrame()), "note 5")

			harness.send(tea.MouseWheelMsg{
				X:      x,
				Y:      2,
				Button: tea.MouseWheelDown,
			})

			assert.NotContains(t, readPopupProse(harness.readFrame()), "note", "at column %d", x)
		}
	})

	t.Run("the wheel beside a conversation with nothing to scroll moves the diff", func(t *testing.T) {
		thread := newThreadOnFirstLine(newHumanMessage("m1", "short"))
		thread.OldStart, thread.NewStart, thread.QuotedLines = 3, 3, []string{" func Hello() string {"}
		server := newReviewServer(t)
		server.threads = []*agentv1.ReviewThread{thread}
		harness := openReviewAt(t, server, panelHarnessParams{height: 24})
		harness.press("j", "j")
		frame := harness.readFrame()
		require.NotEmpty(t, readPopup(frame))

		harness.send(tea.MouseWheelMsg{
			X:      reviewWidth - 1,
			Y:      2,
			Button: tea.MouseWheelDown,
		})

		assert.Empty(t, readPopup(harness.readFrame()))
	})

	t.Run("moving the diff cursor drops the scroll", func(t *testing.T) {
		harness := openLongThread(t)
		harness.press("ctrl+p")
		require.NotContains(t, readPopupProse(harness.readFrame()), "note 5")

		harness.press("j", "k")

		frame := harness.readFrame()
		assert.Contains(t, readPopupProse(frame), "note 5")
	})
}

// TestReviewFitsThePane covers the frame staying the pane's height whatever the sidebar holds.
func TestReviewFitsThePane(t *testing.T) {
	for _, height := range []int{10, 12, 24, 50} {
		t.Run(fmt.Sprintf("a %d line pane", height), func(t *testing.T) {
			server := newReviewServer(t)
			server.threads = append([]*agentv1.ReviewThread{newLongThread()}, newAdriftThreads(4)...)
			harness := openReviewAt(t, server, panelHarnessParams{height: height})

			assert.Len(t, splitLines(harness.readFrame()), height)

			harness.press("ctrl+p", "ctrl+p", "ctrl+p", "ctrl+p", "ctrl+p")
			assert.Len(t, splitLines(harness.readFrame()), height)

			harness.press("space", "c")
			assert.Len(t, splitLines(harness.readFrame()), height)
		})
	}
}

// TestReviewSearch covers the review's own search over the diff.
func TestReviewSearch(t *testing.T) {
	t.Run("the diff follows the pattern as it is typed and enter keeps it", func(t *testing.T) {
		harness := openReview(t, newReviewServer(t))

		harness.press("/")
		typeText(harness, "goodbye")

		frame := harness.readFrame()
		assert.Equal(t, "greet.go:4", readReviewCursor(frame))
		assert.Contains(t, readStageText(frame), "1 of 1")

		harness.press("enter")

		frame = harness.readFrame()
		assert.Contains(t, readStage(frame), "/ goodbye")
		assert.Contains(t, readStageText(frame), "1 of 1")
	})

	t.Run("escape at the prompt puts the search away", func(t *testing.T) {
		harness := openReview(t, newReviewServer(t))

		harness.press("/")
		typeText(harness, "goodbye")
		harness.press("esc")

		frame := harness.readFrame()
		assert.NotContains(t, frame, "goodbye▏")
		assert.NotContains(t, frame, "1 of 1")
	})

	t.Run("the match keys step through the matches and wrap", func(t *testing.T) {
		harness := openReview(t, newReviewServer(t))

		harness.press("/")
		typeText(harness, "return")
		harness.press("enter")
		first := readReviewCursor(harness.readFrame())

		harness.press("n")
		second := readReviewCursor(harness.readFrame())
		require.NotEqual(t, first, second)

		harness.press("n")
		assert.Equal(t, first, readReviewCursor(harness.readFrame()))

		harness.press("N")
		assert.Equal(t, second, readReviewCursor(harness.readFrame()))
	})

	t.Run("the count spells the matches out when the cursor is off them", func(t *testing.T) {
		harness := openReview(t, newReviewServer(t))

		harness.press("/")
		typeText(harness, "return")
		harness.press("enter", "g", "g")

		assert.Contains(t, readStageText(harness.readFrame()), "2 matches")
	})

	t.Run("the match key says when there is nothing to step to", func(t *testing.T) {
		harness := openReview(t, newReviewServer(t))

		harness.press("n")
		assert.Contains(t, readEngineLine(harness.readFrame()), "nothing to search for yet")

		harness.press("/")
		typeText(harness, "wallaby")
		harness.press("enter", "n")

		frame := harness.readFrame()
		assert.Contains(t, readStage(frame), "/ wallaby")
		assert.Contains(t, readEngineLine(frame), "no matches")
	})

	t.Run("the prompt takes every key typed", func(t *testing.T) {
		harness := openReview(t, newReviewServer(t))

		harness.press("/", "g", "o", "?")

		frame := harness.readFrame()
		assert.Contains(t, readStage(frame), "/ go?▏")
		assert.Contains(t, frame, "space menu")
	})

	t.Run("the search is the review's and leaves the agents list alone", func(t *testing.T) {
		server := newReviewServer(t)
		harness := newPanelHarness(t, panelHarnessParams{
			width:  reviewWidth,
			height: reviewHeight,
			git:    server.repo,
		})
		server.serve(harness)
		second := newAgent("a2", "billing", "w1")
		second.Cwd = server.repo.Root
		harness.watch(buildSnapshot(server.workflow, server.agent, server.task, second))
		harness.press("j")
		require.Contains(t, readHoveredRow(harness.readFrame()), "auth")

		harness.press("space", "r", "/")
		typeText(harness, "goodbye")
		harness.press("enter", "esc", "esc")

		frame := harness.readFrame()
		assert.Len(t, readListRows(frame), 3)
		assert.Contains(t, readHoveredRow(frame), "auth")
	})
}

// TestReviewViewed covers folding a file away once it has been read.
func TestReviewViewed(t *testing.T) {
	t.Run("the viewed key folds the file to its header and again opens it", func(t *testing.T) {
		harness := openReview(t, newReviewServer(t))

		harness.press("space", "v")

		frame := harness.readFrame()
		assert.NotContains(t, frame, "func Hello() string {")
		assert.Contains(t, readStageText(frame), "greet.go  +")

		harness.press("space", "v")
		assert.Contains(t, harness.readFrame(), "func Hello() string {")

		sent := harness.readSent(agentv1connect.AgentServiceSetReviewFileViewedProcedure)
		require.Len(t, sent, 2)
		marked, _ := sent[0].(*agentv1.SetReviewFileViewedRequest)
		assert.Equal(t, "t1", marked.GetTask())
		assert.Equal(t, gittest.File, marked.GetFilePath())
		assert.True(t, marked.GetViewed())
		assert.NotEmpty(t, marked.GetFingerprint())
		unmarked, _ := sent[1].(*agentv1.SetReviewFileViewedRequest)
		assert.False(t, unmarked.GetViewed())
	})

	t.Run("a mark holds while the tree is unmoved and a new snapshot has read nothing", func(t *testing.T) {
		server := newReviewServer(t)
		harness := openReview(t, server)

		harness.press("space", "v", "esc", "esc", "space", "r")
		require.NotContains(t, harness.readFrame(), "func Hello() string {")

		server.repo.Wrote(gittest.WroteParams{
			Tree:  "tree-written-again",
			Whole: gittest.PatchWrittenOver,
			Sources: map[string]string{
				gittest.File: "package greet\n\nfunc Hello() string {\n\treturn \"written by the agent\"\n}\n",
			},
		})
		harness.press("esc", "esc", "space", "r")

		assert.Contains(t, harness.readFrame(), "func Hello() string {")
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

			sidebar := readPopupProse(harness.readFrame())
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
		resolved string
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
			resolved: "2026-09-02T11:00:00Z",
			message:  newReadComment("m1"),
			want:     "✓",
			absent:   "●",
		},
		{
			name:     "a settled thread the agent spoke in again is filled in",
			resolved: "2026-09-02T11:00:00Z",
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

			gutter := findLine(readStage(harness.readFrame()), "package greet")
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
		harness.watch(buildPuts(thread))

		assert.Contains(t, readPopupProse(harness.readFrame()), "fixed it")
	})

	t.Run("a comment the writer reads shows read", func(t *testing.T) {
		server := newReviewServer(t)
		comment := newReleasedComment("m1")
		server.threads = []*agentv1.ReviewThread{newThreadOnFirstLine(comment)}
		harness := openReview(t, server)
		require.Contains(t, readPopupProse(harness.readFrame()), "released")

		comment.ReadAt = "2026-09-02T10:05:00Z"
		harness.watch(server.buildSnapshot())

		assert.Contains(t, readPopupProse(harness.readFrame()), "read")
	})

	t.Run("an answer arriving while the reader types waits for them to stop", func(t *testing.T) {
		server := newReviewServer(t)
		harness := openReview(t, server)
		harness.press("j", "space", "c")
		typeText(harness, "half a thought")

		server.threads = []*agentv1.ReviewThread{newThreadOnFirstLine(newHumanMessage("m0", "fix it"), newAgentMessage("m1", "fixed it"))}
		harness.watch(server.buildSnapshot())

		frame := harness.readFrame()
		assert.Contains(t, readPopupProse(frame), "half a thought")
		assert.NotContains(t, readPopupProse(frame), "fixed it")

		harness.press("esc")
		assert.Equal(t, "greet.go:2", readReviewCursor(harness.readFrame()))

		harness.press("k")
		assert.Contains(t, readPopupProse(harness.readFrame()), "fixed it")
	})

	t.Run("an archived agent's threads stay rendered and a settle reads them back", func(t *testing.T) {
		server := newReviewServer(t)
		server.threads = []*agentv1.ReviewThread{newThreadOnFirstLine(newReadComment("m1"))}
		harness := openReview(t, server)

		server.agent.ArchivedAt = xptr.Ptr("2026-09-02T11:00:00Z")
		harness.watch(buildSnapshot(server.workflow, server.agent, server.task))
		require.Contains(t, findLine(readStage(harness.readFrame()), "package greet"), "○")

		harness.press("space", "R")
		require.Len(t, harness.readSent(agentv1connect.AgentServiceOpenReviewHistoryProcedure), 2)
		require.Contains(t, findLine(readStage(harness.readFrame()), "package greet"), "○")

		harness.press("space", "r")

		assert.Contains(t, findLine(readStage(harness.readFrame()), "package greet"), "✓")
		assert.Len(t, harness.readSent(agentv1connect.AgentServiceListReviewThreadsProcedure), 1)
	})
}

// TestReviewOfAnArchivedAgent covers the diff of an archived agent opening read only.
func TestReviewOfAnArchivedAgent(t *testing.T) {
	server := newReviewServer(t)
	server.threads = []*agentv1.ReviewThread{newThreadOnFirstLine(newReleasedComment("m1"))}
	harness := openReviewAt(t, server, panelHarnessParams{height: 50})
	server.agent.ArchivedAt = xptr.Ptr("2026-09-02T11:00:00Z")
	harness.watch(server.buildSnapshot())

	frame := harness.readFrame()
	assert.NotContains(t, frame, "reply in this thread")

	assert.Contains(t, readEngineLine(frame), "this agent is archived")
	harness.watch(server.buildSnapshot())
	harness.press("space", "R")
	assert.Equal(t, 1, strings.Count(readOpenMessages(harness), "this agent is archived"))
	harness.press("ctrl+g")

	harness.press("space")
	menu := harness.readFrame()
	assert.Contains(t, menu, "settle the thread")
	harness.press("esc")

	harness.press("space", "c")
	assert.NotContains(t, harness.readFrame(), "  Comment")
	harness.press("esc")
	assert.Empty(t, harness.readSent(agentv1connect.AgentServiceAddReviewMessageProcedure))

	harness.press("space", "r", "space", "v")
	harness.assertSentTo(agentv1connect.AgentServiceSetReviewThreadResolvedProcedure,
		&agentv1.SetReviewThreadResolvedRequest{Id: "t1", Resolved: true})
	assert.Len(t, harness.readSent(agentv1connect.AgentServiceSetReviewFileViewedProcedure), 1)
}

// TestReviewReload covers reading the worktree again, by the reload key and by the watch reporting a move.
func TestReviewReload(t *testing.T) {
	t.Run("a thread arriving keeps the cursor, the window, the pan and the search", func(t *testing.T) {
		server := newReviewServer(t)
		harness := openReview(t, server)
		harness.press("/")
		typeText(harness, "hello")
		harness.press("enter", "l")
		before := harness.readFrame()

		server.threads = []*agentv1.ReviewThread{newThreadOnLastLine(newAgentMessage("m1", "done"))}
		harness.watch(server.buildSnapshot())

		frame := harness.readFrame()
		assert.Equal(t, readReviewCursor(before), readReviewCursor(frame))
		assert.Equal(t, findLine(readStage(before), "package greet"), findLine(readStage(frame), "package greet"))
		assert.Contains(t, readStageText(frame), "/ hello")
		assert.Contains(t, readStageText(frame), "1 of 2")
	})

	t.Run("a search by file name keeps its header across a thread arriving and a reload", func(t *testing.T) {
		server := newReviewServer(t)
		harness := openReview(t, server)
		harness.press("/")
		typeText(harness, "greet.go")
		harness.press("enter")
		header := readReviewCursor(harness.readFrame())
		require.Contains(t, readStageText(harness.readFrame()), "1 of 1")

		server.threads = []*agentv1.ReviewThread{newThreadOnFirstLine(newAgentMessage("m1", "done"))}
		harness.watch(server.buildSnapshot())
		assert.Equal(t, header, readReviewCursor(harness.readFrame()))
		assert.Contains(t, readStageText(harness.readFrame()), "1 of 1")

		harness.press("space", "R")
		assert.Equal(t, header, readReviewCursor(harness.readFrame()))
		assert.Contains(t, readStageText(harness.readFrame()), "1 of 1")
	})

	t.Run("the reload key keeps the search and its matches", func(t *testing.T) {
		harness := openReview(t, newReviewServer(t))
		harness.press("/")
		typeText(harness, "return")
		harness.press("enter", "space", "R")

		assert.Contains(t, readStageText(harness.readFrame()), "1 of 2")
		harness.press("n")
		assert.Contains(t, readStageText(harness.readFrame()), "2 of 2")
	})

	t.Run("the reload key reads again with nothing moved", func(t *testing.T) {
		harness := openReview(t, newReviewServer(t))

		harness.press("space", "R")

		assert.Len(t, harness.readSent(agentv1connect.AgentServiceOpenReviewHistoryProcedure), 2)
		assert.True(t, harness.panel.IsBeside())
	})

	t.Run("the reload key opens the next snapshot where the reader stood", func(t *testing.T) {
		server := newReviewServer(t)
		harness := openReview(t, server)
		harness.press("j", "j")

		server.repo.Wrote(newSnapshotWrittenOver())
		harness.press("space", "R")

		frame := harness.readFrame()
		assert.Equal(t, "greet.go:3", readReviewCursor(frame))
		assert.Contains(t, frame, "written by the agent")
	})

	t.Run("a reload finding nothing written backs out to the listing", func(t *testing.T) {
		server := newReviewServer(t)
		harness := openReview(t, server)

		server.repo.Wrote(gittest.WroteParams{Tree: "tree-of-the-base"})
		harness.press("space", "R")

		assert.Equal(t, agentsScreen, harness.readScreen())
		assert.Contains(t, harness.readFrame(), "Nothing to review in auth")
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

		frame := harness.readFrame()
		assert.Equal(t, agentsScreen, harness.readScreen())
		assert.Contains(t, readOpenMessages(harness), "excluded from the diff")
		assert.NotContains(t, frame, "Nothing to review")
	})

	t.Run("a move the watch reports leaves the diff on its snapshot and says so", func(t *testing.T) {
		server := newReviewServer(t)
		harness := openReview(t, server)

		server.repo.Wrote(newSnapshotWrittenOver())
		server.task.MovedSinceSnapshot = gittest.SnapshotSHA
		harness.watch(server.buildSnapshot())

		frame := harness.readFrame()
		assert.NotContains(t, frame, "written by the agent")
		assert.Contains(t, readEngineLine(frame), "the worktree has moved")
		assert.Len(t, harness.readSent(agentv1connect.AgentServiceOpenReviewHistoryProcedure), 1)

		harness.press("space", "R")

		assert.Contains(t, harness.readFrame(), "written by the agent")
		assert.NotContains(t, readOpenMessages(harness), "the worktree has moved")
		assert.Len(t, harness.readSent(agentv1connect.AgentServiceOpenReviewHistoryProcedure), 2)
	})

	t.Run("a move from a snapshot other than the one on screen raises nothing", func(t *testing.T) {
		server := newReviewServer(t)
		harness := openReview(t, server)

		server.task.MovedSinceSnapshot = "commit-of-another-snapshot"
		harness.watch(server.buildSnapshot())

		assert.NotContains(t, readOpenMessages(harness), "the worktree has moved")
		assert.Len(t, harness.readSent(agentv1connect.AgentServiceOpenReviewHistoryProcedure), 1)
	})

	t.Run("the reload key catches up while the writer is still writing", func(t *testing.T) {
		server := newReviewServer(t)
		server.agent.State = agentv1.AgentState_AGENT_STATE_THINKING
		harness := openReview(t, server)

		server.repo.Wrote(newSnapshotWrittenOver())
		server.task.MovedSinceSnapshot = gittest.SnapshotSHA
		harness.watch(server.buildSnapshot())
		require.Contains(t, readEngineLine(harness.readFrame()), "the worktree has moved")

		harness.press("space", "R")

		assert.Contains(t, harness.readFrame(), "written by the agent")
		assert.NotContains(t, readOpenMessages(harness), "the worktree has moved")
	})

	t.Run("the history failing to read fails the load", func(t *testing.T) {
		server := newReviewServer(t)
		server.historyErr = errors.New("the server went away")

		harness := openReview(t, server)

		assert.Equal(t, agentsScreen, harness.readScreen())
		assert.Contains(t, readOpenMessages(harness), "the server went away")
	})

	t.Run("a move while the editor is open raises nothing", func(t *testing.T) {
		server := newReviewServer(t)
		server.threads = []*agentv1.ReviewThread{newThreadOnFirstLine(newReleasedComment("m1"))}
		harness := openReview(t, server)
		harness.press("space", "e")

		server.repo.Wrote(newSnapshotWrittenOver())
		server.task.MovedSinceSnapshot = gittest.SnapshotSHA
		harness.watch(server.buildSnapshot())

		assert.NotContains(t, readOpenMessages(harness), "the worktree has moved")
	})
}

// TestReviewOwnComments covers the comments the reader started and the keys that walk the open ones.
func TestReviewOwnComments(t *testing.T) {
	t.Run("an open comment leaves the reader free to write another", func(t *testing.T) {
		harness := openReview(t, newYoursServer(t, newYourThread("a.txt", 2)))

		harness.press("space", "c")

		assert.Contains(t, harness.readFrame(), "  Comment")
	})

	t.Run("an open comment leaves the editor free to open", func(t *testing.T) {
		harness := openReview(t, newYoursServer(t, newYourThread("a.txt", 2)))

		harness.press("space", "e")

		assert.Contains(t, harness.readFrame(), "editing greet.go")
	})

	t.Run("the compose key replies in a comment the cursor stands on", func(t *testing.T) {
		harness := openReview(t, newYoursServer(t, newYourThread(gittest.File, 1)))

		harness.press("space", "c")
		typeText(harness, "still reads like a changelog")
		harness.press("enter")

		harness.assertSentTo(agentv1connect.AgentServiceAddReviewMessageProcedure, &agentv1.AddReviewMessageRequest{
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

		harness.press("u")
		assert.Contains(t, readPopupProse(harness.readFrame()), "the first open comment")
		harness.press("U")
		assert.Contains(t, readPopupProse(harness.readFrame()), "the first open comment")

		harness.press("u", "u")
		assert.Contains(t, readPopupProse(harness.readFrame()), "the third open comment")
		harness.press("U")
		assert.Contains(t, readPopupProse(harness.readFrame()), "the second open comment")

		harness.press("u", "u")
		frame := harness.readFrame()
		assert.Contains(t, readPopupProse(frame), "the third open comment")
		assert.Contains(t, readEngineLine(frame), "comment")
	})

	t.Run("the place row counts the open comment the cursor is on", func(t *testing.T) {
		harness := openReviewAt(t, newYoursServer(t,
			newYourThread(gittest.File, 2), newYourThread(gittest.File, 3), newYourThread(gittest.File, 5),
		), panelHarnessParams{width: 45})

		harness.press("u", "u", "u")
		assert.Equal(t, "greet.go:5  3 of 3", readReviewCursor(harness.readFrame()))
		harness.press("U")
		assert.Equal(t, "greet.go:3  2 of 3", readReviewCursor(harness.readFrame()))
	})

	t.Run("the open comment key says why it moved nowhere", func(t *testing.T) {
		settled := newYourThread(gittest.File, 2)
		settled.ResolvedAt = "2026-09-13T12:00:00Z"
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

				harness.press(tt.keys...)

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
		before := harness.readFrame()
		require.Contains(t, before, "func Two()")
		require.Contains(t, before, "@@")
		require.NotContains(t, before, "func Four()")

		harness.press("space", "E")
		expanded := harness.readFrame()
		assert.Contains(t, expanded, "func Four()")
		assert.Contains(t, expanded, `import "fmt"`)
		assert.NotContains(t, expanded, "@@")

		harness.press("space", "E")
		tight := harness.readFrame()
		assert.NotContains(t, tight, "func Four()")
		assert.Contains(t, tight, "func Two()")
	})

	t.Run("the cursor stays on the line it stood on", func(t *testing.T) {
		harness, _ := openTallReview(t)
		harness.press("j", "j")
		before := readReviewCursor(harness.readFrame())

		harness.press("space", "E")

		assert.Equal(t, before, readReviewCursor(harness.readFrame()))
	})

	t.Run("a reload keeps the file rendered whole", func(t *testing.T) {
		harness, _ := openTallReview(t)

		harness.press("space", "E", "space", "R")

		assert.Contains(t, harness.readFrame(), "func Four()")
	})

	t.Run("a file with nothing more to render refuses and says so", func(t *testing.T) {
		harness := openReview(t, newReviewServer(t))

		harness.press("space", "E")

		assert.Contains(t, readEngineLine(harness.readFrame()), "no more of that file")
	})

	t.Run("a folded file refuses and says so", func(t *testing.T) {
		harness, _ := openTallReview(t)

		harness.press("space", "v", "space", "E")

		frame := harness.readFrame()
		assert.NotContains(t, frame, "func Four()")
		assert.Contains(t, readEngineLine(frame), "folded away")
	})

	t.Run("a line only the expansion renders takes a comment", func(t *testing.T) {
		harness, _ := openTallReview(t)

		harness.press("space", "E", "G")
		require.Equal(t, "tall.go:13", readReviewCursor(harness.readFrame()))
		harness.press("space", "c")
		typeText(harness, "unchanged and wrong")
		harness.press("enter")

		sent := harness.readSent(agentv1connect.AgentServiceCreateReviewThreadProcedure)
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

		harness.press("space", "E")
		harness.watch(server.buildSnapshot())
		harness.press("space", "E")

		frame := harness.readFrame()
		assert.Contains(t, frame, "outside what is on screen")
		assert.NotContains(t, frame, "has changed")
	})

	t.Run("a file folded while rendered whole stays folded after a reload", func(t *testing.T) {
		harness, _ := openTallReview(t)

		harness.press("space", "E", "space", "v", "space", "R")

		assert.Contains(t, readStageText(harness.readFrame()), "tall.go  +")
	})
}

// TestReviewSnapshots covers walking the snapshots of a review with the base keys.
func TestReviewSnapshots(t *testing.T) {
	t.Run("the base key narrows to the work since the last review and forward widens it back", func(t *testing.T) {
		harness := openReview(t, newEarlierReviewServer(t))
		frame := harness.readFrame()
		require.Contains(t, readSidebarProse(frame), "2 files, +4 -1")
		require.Contains(t, readSidebarProse(frame), "1 file changed since you last reviewed")

		harness.press("<")
		frame = harness.readFrame()
		assert.Contains(t, readStageText(frame), "snapshot 1 ▸ snapshot 2")
		assert.Contains(t, readSidebarProse(frame), "1 file, +3 -0")
		assert.NotRegexp(t, `\d+/\d+ viewed`, frame)
		assert.NotContains(t, readSidebarProse(frame), "since you last reviewed")

		harness.press(">")
		frame = harness.readFrame()
		assert.Contains(t, readStageText(frame), "where the task starts ▸ snapshot 2")
		assert.Contains(t, readSidebarProse(frame), "2 files, +4 -1")
	})

	t.Run("a range the reader has already stepped past never lands", func(t *testing.T) {
		harness := openReview(t, newEarlierReviewServer(t))
		harness.hold(agentv1connect.AgentServiceOpenReviewHistoryProcedure)

		harness.press("<", "<")
		harness.release(agentv1connect.AgentServiceOpenReviewHistoryProcedure)
		frame := harness.readFrame()
		assert.Contains(t, readStageText(frame), "where the task starts ▸ snapshot 1")
		assert.Contains(t, readSidebarProse(frame), "2 files, +4 -1")

		harness.release(agentv1connect.AgentServiceOpenReviewHistoryProcedure)
		assert.Contains(t, readSidebarProse(harness.readFrame()), "1 file, +1 -1")
	})

	t.Run("one load opens the history once", func(t *testing.T) {
		harness := openReview(t, newEarlierReviewServer(t))

		assert.Len(t, harness.readSent(agentv1connect.AgentServiceOpenReviewHistoryProcedure), 1)
	})

	t.Run("the line saying what changed survives a keypress and a notice", func(t *testing.T) {
		harness := openReview(t, newEarlierReviewServer(t))

		harness.press("j")
		require.Contains(t, readSidebarProse(harness.readFrame()), "1 file changed since you last reviewed")

		harness.press("n")
		frame := harness.readFrame()
		assert.Contains(t, readEngineLine(frame), "nothing to search for yet")
		assert.Contains(t, readSidebarProse(frame), "1 file changed since you last reviewed")
	})

	t.Run("narrowing onto a snapshot with nothing new keeps the whole change and says so", func(t *testing.T) {
		server := newEarlierReviewServer(t)
		harness := openReview(t, server)
		server.repo.Wrote(gittest.WroteParams{Tree: gittest.UnmovedTree, Whole: gittest.Patch})
		harness.press("esc", "esc", "space", "r")

		harness.press("<")

		frame := harness.readFrame()
		assert.True(t, harness.panel.IsBeside())
		assert.Contains(t, readStageText(frame), "greet.go")
		assert.Contains(t, readEngineLine(frame), "nothing written")
	})

	t.Run("the newest snapshot takes a comment measured from the snapshot reviewed before", func(t *testing.T) {
		harness := openReview(t, newEarlierReviewServer(t))

		harness.press("<", "space", "c")
		typeText(harness, "this line")
		harness.press("enter")

		harness.assertSentTo(agentv1connect.AgentServiceCreateReviewThreadProcedure, &agentv1.CreateReviewThreadRequest{
			Task:        xptr.Ptr("t1"),
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
		harness := openReviewAt(t, newEarlierReviewServer(t), panelHarnessParams{height: 50})
		harness.press("<", "<")
		require.Contains(t, readStageText(harness.readFrame()), "where the task starts ▸ snapshot 1")

		harness.press("space")
		assert.Contains(t, harness.readFrame(), "new comments go on snapshot 2")

		harness.press("c")
		assert.NotContains(t, harness.readFrame(), "  Comment")
		assert.Empty(t, harness.readSent(agentv1connect.AgentServiceCreateReviewThreadProcedure))
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

		harness.press("space", "v")
		require.Len(t, harness.readSent(agentv1connect.AgentServiceSetReviewFileViewedProcedure), 1)

		harness.press("<")
		require.Contains(t, readStageText(harness.readFrame()), "snapshot 1 ▸ snapshot 2")
		require.NotContains(t, readStageText(harness.readFrame()), "greet.go  +")
		harness.press("space", "v")
		frame := harness.readFrame()
		assert.Contains(t, frame, "viewed marks belong to the whole change")
		assert.NotContains(t, readStageText(frame), "greet.go  +")
		assert.Len(t, harness.readSent(agentv1connect.AgentServiceSetReviewFileViewedProcedure), 1)

		harness.press("esc", ">")
		assert.Contains(t, readStageText(harness.readFrame()), "greet.go  +")
	})

	t.Run("a comment being written goes with the snapshot a landing load takes away", func(t *testing.T) {
		server := newEarlierReviewServer(t)
		harness := openReview(t, server)
		harness.press("<")
		harness.hold(agentv1connect.AgentServiceOpenReviewHistoryProcedure)
		harness.press("space", "R")
		server.historyErr = errors.New("the server went away")

		harness.press("space", "c")
		typeText(harness, "hello")
		require.Contains(t, harness.readFrame(), "  Comment")
		harness.release(agentv1connect.AgentServiceOpenReviewHistoryProcedure)

		assert.NotContains(t, harness.readFrame(), "  Comment")
		harness.press("enter")
		assert.Empty(t, harness.readSent(agentv1connect.AgentServiceCreateReviewThreadProcedure))
	})

	t.Run("a range splits the replies written after the snapshot it ends on from the ones before", func(t *testing.T) {
		harness := openReviewAt(t, newLaterRepliesServer(t), panelHarnessParams{height: 50})

		harness.press("<", "<", "t")

		frame := harness.readFrame()
		require.Contains(t, readStageText(frame), "where the task starts ▸ snapshot 1")
		said := readPopupProse(frame)
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

		harness.press("<")

		frame := harness.readFrame()
		assert.Contains(t, readStageText(frame), "snapshot 1 ▸ snapshot 2, replayed across the rebase")
		assert.Contains(t, readSidebarProse(frame), "1 file, +3 -0")
	})

	t.Run("a snapshot the base rewrote under the agent is refused as conflicted", func(t *testing.T) {
		harness := openReview(t, newMovedBaseServer(t, "a-tree-with-markers-in-it", gittest.File, "second.go"))

		harness.press("<")

		assert.Contains(t, readStageText(harness.readFrame()), "snapshot 1 ▸ snapshot 2, conflicts in greet.go, second.go")
	})

	t.Run("a git that cannot replay says the base moved between the snapshots", func(t *testing.T) {
		harness := openReview(t, newMovedBaseServer(t, ""))

		harness.press("<")

		frame := harness.readFrame()
		assert.Contains(t, readStageText(frame), "snapshot 1 ▸ snapshot 2, the base moved between these snapshots")
		assert.NotContains(t, frame, "replayed")
	})

	t.Run("the first snapshot is measured from its own base", func(t *testing.T) {
		server := newMovedBaseServer(t, replayedTree)
		server.repo.Patches[gittest.NamePair(olderBase, gittest.SnapshotSHA)] = gittest.Patch
		server.repo.Patches[gittest.NamePair(gittest.BaseSHA, gittest.SnapshotSHA)] = gittest.PatchOfBothFiles
		harness := openReview(t, server)

		harness.press("<", "<")

		frame := harness.readFrame()
		assert.Contains(t, readStageText(frame), "where the task starts ▸ snapshot 1")
		assert.Contains(t, readSidebarProse(frame), "1 file, +1 -1")
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

		harness.press("<")
		assert.Contains(t, findLine(readStage(harness.readFrame()), `return "hello"`), "○")

		harness.press(">")
		frame := harness.readFrame()
		assert.NotContains(t, findLine(readStage(frame), `return "hello"`), "○")
		assert.Contains(t, readStageText(frame), "the code these were about has changed")
	})
}

// TestReviewComposer covers the field comments are typed into.
func TestReviewComposer(t *testing.T) {
	t.Run("ctrl+j makes a line rather than sending", func(t *testing.T) {
		harness := openReview(t, newReviewServer(t))

		harness.press("space", "c", "o", "n", "e", "ctrl+j", "t", "w", "o")

		assert.Contains(t, readPopupProse(harness.readFrame()), "one")
		assert.Contains(t, readPopupProse(harness.readFrame()), "two")
		assert.Empty(t, harness.readSent(agentv1connect.AgentServiceCreateReviewThreadProcedure))
	})

	t.Run("a row with no line of its own refuses the key and says why", func(t *testing.T) {
		harness := openReview(t, newReviewServer(t))

		harness.press("/")
		typeText(harness, "greet.go")
		harness.press("enter", "space", "c")

		frame := harness.readFrame()
		assert.NotContains(t, frame, "  Comment")
		assert.Contains(t, readOpenMessages(harness), "put the cursor on a line of the diff")
	})
}

// TestReviewHold covers the reader stopping the agent to edit its worktree.
func TestReviewHold(t *testing.T) {
	t.Run("the edit key stops the agent and opens the editor over the diff", func(t *testing.T) {
		server := newReviewServer(t)
		server.agent.State = agentv1.AgentState_AGENT_STATE_THINKING
		harness := openReview(t, server)

		harness.press("space", "e")

		frame := harness.readFrame()
		assert.Contains(t, frame, "editing greet.go")
		assert.NotContains(t, frame, "package greet")
		harness.assertSentTo(agentv1connect.AgentServiceHoldAgentProcedure, &agentv1.HoldAgentRequest{Id: "a1"})

		editor := harness.panel.ReadRespawnPanes()
		require.NotEmpty(t, editor)
		assert.Equal(t, ui.ColumnPane, editor[len(editor)-1], "the editor is split off the sidebar rather than put on the stage")
		assert.Equal(t, server.agent.GetPaneId(), harness.panel.ReadGuestPane())
		assert.Equal(t, ui.ColumnPane, harness.panel.ReadSelectedPanes()[len(harness.panel.ReadSelectedPanes())-1])
		assert.False(t, harness.panel.IsBeside())
	})

	t.Run("a waiting agent does not hold the edit key", func(t *testing.T) {
		server := newReviewServer(t)
		server.agent.State = agentv1.AgentState_AGENT_STATE_WAITING
		harness := openReview(t, server)

		harness.press("space", "e")

		assert.Contains(t, harness.readFrame(), "editing greet.go")
	})

	t.Run("an agent that will not stop opens no editor", func(t *testing.T) {
		server := newReviewServer(t)
		server.holdErr = errors.New("the agent did not stop")
		harness := openReview(t, server)

		harness.press("space", "e")

		assert.NotContains(t, harness.readFrame(), "editing")
	})

	t.Run("a removed worktree refuses the edit key and stops nothing", func(t *testing.T) {
		server := newReviewServer(t)
		server.task.WorktreeRemovalReason = taskv1.WorktreeRemovalReason_WORKTREE_REMOVAL_REASON_GONE
		server.task.Snapshots = 1
		server.workflow.Cwd = server.repo.Root
		harness := openReview(t, server)

		harness.press("space")
		require.Contains(t, harness.readFrame(), "the worktree was removed, so there is no checkout")
		harness.press("e")

		assert.NotContains(t, harness.readFrame(), "editing")
		assert.Empty(t, harness.readSent(agentv1connect.AgentServiceHoldAgentProcedure))
	})

	t.Run("an unreadable pane refuses the edit key and stops nothing", func(t *testing.T) {
		server := newReviewServer(t)
		server.agent.State = agentv1.AgentState_AGENT_STATE_UNCERTAIN
		harness := openReview(t, server)

		harness.press("space", "e")

		assert.Contains(t, readOpenMessages(harness), "cannot read this agent's pane")
		assert.Empty(t, harness.readSent(agentv1connect.AgentServiceHoldAgentProcedure))
	})

	t.Run("a dormant agent is edited without stopping anything", func(t *testing.T) {
		server := newReviewServer(t)
		server.agent.Alive = xptr.Ptr(false)
		harness := openReview(t, server)

		harness.press("space", "e")
		require.Contains(t, harness.readFrame(), "editing greet.go")
		harness.press("space", "e", "esc")

		assert.Empty(t, harness.readSent(agentv1connect.AgentServiceHoldAgentProcedure))
		assert.Empty(t, harness.readSent(agentv1connect.AgentServiceReleaseAgentProcedure))
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

		harness.press("/")
		typeText(harness, "Gone")
		harness.press("enter", "space", "e")

		frame := harness.readFrame()
		assert.Contains(t, readOpenMessages(harness), "deleted")
		assert.NotContains(t, frame, "editing ·")
	})

	t.Run("the compose key leaves the agent running", func(t *testing.T) {
		server := newReviewServer(t)
		server.agent.State = agentv1.AgentState_AGENT_STATE_THINKING
		harness := openReview(t, server)

		harness.press("space", "c")

		assert.Contains(t, harness.readFrame(), "  Comment")
		assert.Empty(t, harness.readSent(agentv1connect.AgentServiceHoldAgentProcedure))
	})

	t.Run("a second edit press waits for the agent the first is stopping", func(t *testing.T) {
		server := newReviewServer(t)
		server.agent.State = agentv1.AgentState_AGENT_STATE_THINKING
		harness := openReview(t, server)
		harness.hold(agentv1connect.AgentServiceHoldAgentProcedure)

		harness.press("space", "e")
		require.Contains(t, harness.readFrame(), "stopping the agent")

		harness.press("space", "e")
		frame := harness.readFrame()
		assert.Contains(t, readEngineLine(frame), "still stopping")
		assert.NotContains(t, frame, "editing greet.go")

		harness.release(agentv1connect.AgentServiceHoldAgentProcedure)
		assert.Contains(t, harness.readFrame(), "editing greet.go")
	})

	t.Run("an edit given up before the agent stops lets it go", func(t *testing.T) {
		server := newReviewServer(t)
		server.agent.State = agentv1.AgentState_AGENT_STATE_THINKING
		harness := openReview(t, server)
		harness.hold(agentv1connect.AgentServiceHoldAgentProcedure)
		harness.press("space", "e")

		harness.press("esc")
		harness.release(agentv1connect.AgentServiceHoldAgentProcedure)

		assert.NotContains(t, harness.readFrame(), "editing ·")
		assert.Contains(t, readReleaseSaid(t, harness), "without opening it")
	})

	t.Run("the editor quitting gives the stage back and reads the diff again", func(t *testing.T) {
		harness := openReview(t, newReviewServer(t))
		harness.press("space", "e")
		require.Contains(t, harness.readFrame(), "editing greet.go")
		loads := len(harness.readSent(agentv1connect.AgentServiceOpenReviewHistoryProcedure))

		editor := harness.panel.ReadRespawnPanes()
		require.NotEmpty(t, editor)
		harness.panel.SetPaneDead(editor[len(editor)-1])
		harness.fireTimers()

		assert.NotContains(t, harness.readFrame(), "editing ·")
		assert.Contains(t, readReleaseSaid(t, harness), "while I had your worktree open")
		assert.Len(t, harness.readSent(agentv1connect.AgentServiceOpenReviewHistoryProcedure), loads+1)
	})

	t.Run("the cursor resting while the editor is open leaves the editor on the stage", func(t *testing.T) {
		harness := openReview(t, newReviewServer(t))
		harness.press("space", "e")
		require.Contains(t, harness.readFrame(), "editing greet.go")
		swaps, respawns := harness.panel.CountSwaps(), harness.panel.CountRespawns()

		wheelOver(harness, defaultHarnessWidth-1)
		harness.press(rest)
		harness.fireTimers()

		assert.Equal(t, swaps, harness.panel.CountSwaps())
		assert.Equal(t, respawns, harness.panel.CountRespawns())
		assert.Empty(t, harness.readSent(agentv1connect.AgentServiceReleaseAgentProcedure))
	})

	t.Run("the editor opens at the repository root the paths are named from", func(t *testing.T) {
		server := newReviewServer(t)
		server.agent.Cwd = filepath.Join(server.repo.Root, "cmd", "greet")
		harness := openReviewAt(t, server, panelHarnessParams{settings: ui.PanelSettings{Editor: "vi"}})

		harness.press("space", "e")

		require.NotEmpty(t, harness.panel.ReadRespawnDirs())
		assert.Equal(t, server.repo.Root, harness.panel.ReadRespawnDirs()[0])
		assert.Contains(t, harness.panel.ReadRespawns()[0], "'greet.go'")
	})

	t.Run("on an engine elsewhere the editor runs there, through its ssh host", func(t *testing.T) {
		server := newReviewServer(t)
		harness := openReviewAt(t, server, panelHarnessParams{settings: remoteEditSettings(t, "/opt/homebrew/bin/nvim")})

		harness.press("space", "e")

		require.NotEmpty(t, harness.panel.ReadRespawns())
		assert.Contains(t, harness.panel.ReadRespawns()[0], "-t 'komrad-remote'")
		assert.Contains(t, harness.panel.ReadRespawns()[0], "exec nvim +")
		assert.Contains(t, harness.panel.ReadRespawns()[0], filepath.Join(server.repo.Root, "greet.go"))
	})

	t.Run("on an engine elsewhere an editor that cannot reach it opens nothing", func(t *testing.T) {
		harness := openReviewAt(t, newReviewServer(t), panelHarnessParams{settings: remoteEditSettings(t, "goland")})

		harness.press("space", "e")

		assert.Empty(t, harness.panel.ReadRespawns())
		assert.Empty(t, harness.readSent(agentv1connect.AgentServiceHoldAgentProcedure))
		assert.Contains(t, harness.readFrame(), "goland cannot open files")
	})

	t.Run("on an engine elsewhere an editor with a window of its own needs the ssh hosts included", func(t *testing.T) {
		settings := remoteEditSettings(t, "code")
		settings.IsSSHIncluded = false
		harness := openReviewAt(t, newReviewServer(t), panelHarnessParams{settings: settings})

		harness.press("space", "e")

		assert.Empty(t, harness.panel.ReadRespawns())
		assert.Contains(t, harness.readFrame(), "code cannot find the engine")
	})

	t.Run("on an engine elsewhere with no ssh host written the editor opens nothing", func(t *testing.T) {
		settings := remoteEditSettings(t, "nvim")
		settings.SSHConfig = filepath.Join(t.TempDir(), "missing.conf")
		harness := openReviewAt(t, newReviewServer(t), panelHarnessParams{settings: settings})

		harness.press("space", "e")

		assert.Empty(t, harness.panel.ReadRespawns())
		assert.Contains(t, harness.readFrame(), "this machine has no ssh host")
	})

	t.Run("the back key closes the editor before it leaves the screen", func(t *testing.T) {
		harness := openReview(t, newReviewServer(t))

		harness.press("space", "e", "esc")

		frame := harness.readFrame()
		assert.NotContains(t, frame, "editing ·")
		assert.Contains(t, frame, "auth › review")
	})

	t.Run("closing the editor lets the agent go and says where to look", func(t *testing.T) {
		server := newReviewServer(t)
		harness := openReview(t, server)

		harness.press("space", "e", "space", "e")

		said := readReleaseSaid(t, harness)
		assert.Contains(t, said, "let you go")
		assert.Contains(t, said, "greet.go")
		assert.Contains(t, harness.readFrame(), "auth › review")
		assert.Contains(t, harness.panel.ReadKilledPanes(), ui.ColumnPane)
		assert.NotContains(t, harness.panel.ReadKilledPanes(), server.agent.GetPaneId())
		assert.True(t, harness.panel.IsBeside())
	})

	t.Run("a diff that stops being readable lets the agent go with the files", func(t *testing.T) {
		server := newReviewServer(t)
		harness := openReview(t, server)
		harness.press("space", "e")

		server.repo.RootErr = gittest.ErrNoGit
		harness.press("space", "R")

		assert.Equal(t, agentsScreen, harness.readScreen())
		assert.Contains(t, readReleaseSaid(t, harness), "greet.go")
		assert.Contains(t, harness.panel.ReadKilledPanes(), ui.ColumnPane, "the editor goes with the review")
	})

	t.Run("an agent paused by somebody else opens no editor", func(t *testing.T) {
		server := newReviewServer(t)
		server.agent.HeldAt = heldAt
		harness := openReview(t, server)

		harness.press("space", "e")

		assert.Empty(t, harness.panel.ReadRespawns())
		assert.Empty(t, harness.readSent(agentv1connect.AgentServiceHoldAgentProcedure))
		assert.Contains(t, harness.readFrame(), "could not edit")
	})

	t.Run("a pause a refused release left standing is let go with the files on the next snapshot", func(t *testing.T) {
		server := newReviewServer(t)
		harness := openReview(t, server)
		refusals := 1
		harness.answer(agentv1connect.AgentServiceReleaseAgentProcedure, func(connect.AnyRequest) (connect.AnyResponse, error) {
			if refusals > 0 {
				refusals--

				return nil, connect.NewError(connect.CodeUnavailable, errors.New("the input box would not take the words"))
			}

			return connect.NewResponse(&agentv1.ReleaseAgentResponse{}), nil
		})

		harness.press("space", "e", "space", "e")
		server.agent.HeldAt = heldAt
		harness.watch(server.buildSnapshot())

		sent := harness.readSent(agentv1connect.AgentServiceReleaseAgentProcedure)
		require.Len(t, sent, 2)
		released, _ := sent[1].(*agentv1.ReleaseAgentRequest)
		assert.Contains(t, released.GetSaid(), "greet.go")
	})

	t.Run("closing the editor renders the reader's own edits over a pinned diff", func(t *testing.T) {
		server := newReviewServer(t)
		server.threads = []*agentv1.ReviewThread{newThreadOnFirstLine(newReleasedComment("m1"))}
		harness := openReview(t, server)

		harness.press("space", "e")
		server.repo.Wrote(gittest.WroteParams{Tree: "tree-the-reader-edited", Whole: gittest.PatchWrittenOver})
		harness.press("space", "e", "j")

		assert.Contains(t, readStageText(harness.readFrame()), "written by the agent")
	})

	t.Run("the gate reads the agent under review rather than the first row", func(t *testing.T) {
		server := newReviewServer(t)
		server.agent.State = agentv1.AgentState_AGENT_STATE_THINKING
		harness := newPanelHarness(t, panelHarnessParams{
			width:  reviewWidth,
			height: reviewHeight,
			git:    server.repo,
		})
		server.serve(harness)
		idle := newAgent("a0", "able", "w1")
		harness.watch(buildSnapshot(server.workflow, idle, server.agent, server.task))
		require.Contains(t, readHoveredRow(harness.readFrame()), "auth")

		harness.press("space", "r")
		server.repo.Wrote(newSnapshotWrittenOver())
		server.task.MovedSinceSnapshot = gittest.SnapshotSHA
		harness.watch(buildSnapshot(server.workflow, server.agent, server.task, idle))
		assert.NotContains(t, harness.readFrame(), "written by the agent")

		harness.press("space", "e")
		harness.assertSentTo(agentv1connect.AgentServiceHoldAgentProcedure, &agentv1.HoldAgentRequest{Id: "a1"})
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
	holdErr    error
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

func openReview(t *testing.T, server *reviewServer) *panelHarness {
	t.Helper()

	return openReviewAt(t, server, panelHarnessParams{})
}

func openLongThread(t *testing.T) *panelHarness {
	t.Helper()

	server := newReviewServer(t)
	server.threads = []*agentv1.ReviewThread{newLongThread()}

	return openReviewAt(t, server, panelHarnessParams{height: 24})
}

func openTallReview(t *testing.T) (*panelHarness, *reviewServer) {
	t.Helper()

	server := newReviewServerOver(gittest.NewTallRepo(t.TempDir()))

	return openReviewAt(t, server, panelHarnessParams{height: 40}), server
}

func openReviewAt(t *testing.T, server *reviewServer, params panelHarnessParams) *panelHarness {
	t.Helper()

	if params.width == 0 {
		params.width = reviewWidth
	}
	if params.height == 0 {
		params.height = reviewHeight
	}
	params.git = server.repo

	harness := newPanelHarness(t, params)
	server.serve(harness)
	harness.watch(server.buildSnapshot())
	harness.press("space", "r")

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

	return buildSnapshot(rows...)
}

func (s *reviewServer) serve(harness *panelHarness) {
	harness.answer(worktreev1connect.WorktreeServiceGetWorktreeProcedure, func(connect.AnyRequest) (connect.AnyResponse, error) {
		return connect.NewResponse(&worktreev1.GetWorktreeResponse{Worktree: &worktreev1.Worktree{Dir: s.repo.Root}}), nil
	})
	harness.answer(agentv1connect.AgentServiceOpenReviewHistoryProcedure, s.openHistory)
	harness.answer(agentv1connect.AgentServiceListReviewThreadsProcedure, s.listThreads)
	harness.answer(agentv1connect.AgentServiceListReviewViewedFilesProcedure, s.listViewed)
	harness.answer(agentv1connect.AgentServiceSetReviewFileViewedProcedure, s.markViewed)
	harness.answer(agentv1connect.AgentServiceAddReviewMessageProcedure, s.reply)
	harness.answer(agentv1connect.AgentServiceSetReviewThreadResolvedProcedure, s.settle)
	harness.answer(agentv1connect.AgentServiceCreateReviewThreadProcedure, s.createThread)
	harness.answer(agentv1connect.AgentServiceHoldAgentProcedure, s.hold)
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
		CreatedAt:  "2026-09-02T10:02:00Z",
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
		thread.ResolvedAt = ""
		if settle.GetResolved() {
			thread.ResolvedAt = "2026-08-27T12:00:00Z"
		}
	}
	for _, review := range s.reviews {
		var open int32
		for _, thread := range s.threads {
			if thread.GetReviewId() == review.GetId() && thread.GetResolvedAt() == "" {
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
	comment.CreatedAt = "2026-09-02T10:00:00Z"
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

func (s *reviewServer) hold(connect.AnyRequest) (connect.AnyResponse, error) {
	if s.holdErr != nil {
		return nil, s.holdErr
	}

	return connect.NewResponse(&agentv1.HoldAgentResponse{}), nil
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

func typeText(harness *panelHarness, text string) {
	for _, letter := range text {
		key := string(letter)
		if key == " " {
			key = "space"
		}
		harness.press(key)
	}
}

func readStageText(frame string) string {
	return strings.Join(readStage(frame), "\n")
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
	message.CreatedAt = "2026-09-02T10:00:00Z"

	return message
}

func newReadComment(id string) *agentv1.ReviewMessage {
	message := newReleasedComment(id)
	message.ReadAt = "2026-09-02T10:01:00Z"

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
	server.threads = []*agentv1.ReviewThread{newWholeChangeThread("snapshot-1", 1, "2026-09-08T10:00:00Z")}
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

func readReleaseSaid(t *testing.T, harness *panelHarness) string {
	t.Helper()

	sent := harness.readSent(agentv1connect.AgentServiceReleaseAgentProcedure)
	require.Len(t, sent, 1)
	released, _ := sent[0].(*agentv1.ReleaseAgentRequest)
	assert.Equal(t, "a1", released.GetId())

	return released.GetSaid()
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
	answer.Reviews = s.reviews
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
	answer.Widened = built.Widened
	answer.ChangedSinceEarlier = int32(built.ChangedSinceEarlier)
	answer.Traced = map[string]*agentv1.ReviewTracedPlace{}
	if built.Earlier != nil {
		answer.EarlierSnapshotId = built.Earlier.ID
	}
	if built.EarlierReleasedAt != nil {
		answer.EarlierReleasedAt = dates.Stamp(*built.EarlierReleasedAt)
	}
	for id, place := range built.Traced {
		answer.Traced[id] = &agentv1.ReviewTracedPlace{
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
			FromSnapshotID: req.GetSnapshots().GetFromSnapshotId(),
			ToSnapshotID:   req.GetSnapshots().GetToSnapshotId(),
		}}
	case req.GetAnswer() != nil:
		return history.Span{Answer: &history.AnswerSpan{
			FromSnapshotID: req.GetAnswer().GetFromSnapshotId(),
			ToSnapshotID:   req.GetAnswer().GetToSnapshotId(),
		}}
	case req.GetOrigin() != nil:
		return history.Span{Origin: &history.OriginSpan{
			SnapshotID: req.GetOrigin().GetSnapshotId(),
			BaseSHA:    req.GetOrigin().GetBaseSha(),
		}}
	}

	return history.Span{}
}

func historySnapshotOf(snapshot *agentv1.ReviewSnapshot) history.Snapshot {
	return history.Snapshot{
		ID:      snapshot.GetId(),
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
				released, err := time.Parse(time.RFC3339, message.GetCreatedAt())
				if err != nil {
					continue
				}
				comments = append(comments, history.Comment{
					SnapshotID:      snapshot.GetId(),
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
		ID:         thread.GetId(),
		SnapshotID: thread.GetSnapshotId(),
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

func newWholeChangeThread(snapshotID string, ordinal int32, at string) *agentv1.ReviewThread {
	remark := newHumanMessage("w1", "the shape is right")
	remark.CreatedAt = at

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
