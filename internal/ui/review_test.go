package ui_test

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"komrad/config"
	"komrad/internal/git"
	"komrad/internal/gittest"
	"komrad/internal/ui"
	"komrad/lib/xptr"
	agentv1 "komrad/proto/gen/agent/v1"
	"komrad/proto/gen/agent/v1/agentv1connect"
	taskv1 "komrad/proto/gen/task/v1"
	watchv1 "komrad/proto/gen/watch/v1"
	workflowv1 "komrad/proto/gen/workflow/v1"
	worktreev1 "komrad/proto/gen/worktree/v1"
	"komrad/proto/gen/worktree/v1/worktreev1connect"

	tea "charm.land/bubbletea/v2"
	"connectrpc.com/connect"
	"github.com/charmbracelet/x/ansi"
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

	t.Run("a task's row opens the review through its newest agent", func(t *testing.T) {
		server := newReviewServer(t)
		server.task.Slug = "login-fix"
		server.agent.CreatedAt = "2026-09-20T12:05:00Z"
		server.onTask = []*agentv1.Agent{{
			Id:         "a0",
			Name:       "first-try",
			WorkflowId: "w1",
			TaskId:     xptr.Ptr("t1"),
			State:      agentv1.AgentState_AGENT_STATE_IDLE,
			CreatedAt:  "2026-09-20T12:00:00Z",
		}}
		harness := newPanelHarness(t, panelHarnessParams{
			width:  reviewWidth,
			height: reviewHeight,
			git:    server.repo,
		})
		server.serve(harness)
		harness.watch(server.buildSnapshot())
		require.Contains(t, readHoveredRow(harness.readFrame()), "login-fix")

		harness.press("space", "r")

		assert.True(t, harness.panel.IsZoomed())
		harness.assertSentTo(agentv1connect.AgentServiceOpenReviewRoundProcedure, &agentv1.OpenReviewRoundRequest{Task: xptr.Ptr("t1")})
	})

	t.Run("an agent whose directory has gone is not opened", func(t *testing.T) {
		server := newReviewServer(t)
		server.task.WorktreeRemoved = true

		harness := openReview(t, server)

		assert.Equal(t, agentsScreen, harness.readScreen())
		assert.Empty(t, harness.readSent(agentv1connect.AgentServiceOpenReviewRoundProcedure))
	})

	t.Run("an agent part way through a rebase opens on its newest round", func(t *testing.T) {
		server := newEarlierReviewServer(t)
		server.openErr = connect.NewError(connect.CodeFailedPrecondition, errors.New("a rebase is part way through"))

		harness := openReview(t, server)

		frame := harness.readFrame()
		assert.True(t, harness.panel.IsZoomed())
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

	t.Run("the diff is rendered beside the sidebar over the whole window", func(t *testing.T) {
		harness := openReview(t, newReviewServer(t))

		frame := harness.readFrame()
		assert.True(t, harness.panel.IsZoomed())
		assert.Contains(t, readStage(frame), "greet.go")
		assert.Contains(t, readStage(frame), "Review · auth")
		assert.Contains(t, readSidebarProse(frame), "1 file, +1 -1")
		assert.Len(t, splitLines(frame), reviewHeight)
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
		harness.press("space", "b")

		sidebar := readPopupProse(harness.readFrame())
		assert.Contains(t, sidebar, "reviewer this drops the error")
		assert.Contains(t, sidebar, "writer wrapped it")
	})

	t.Run("a conversation an agent opened is hidden until the menu shows it", func(t *testing.T) {
		server := newReviewServer(t)
		server.threads = []*agentv1.ReviewThread{newThreadOnFirstLine(newAgentMessage("m1", "renamed it, is that what you meant?"))}
		harness := openReview(t, server)
		require.NotContains(t, harness.readFrame(), "renamed it")

		harness.press("space")
		require.Contains(t, harness.readFrame(), "show agents' conversations")
		harness.press("b")
		assert.Contains(t, readPopupProse(harness.readFrame()), "renamed it")

		harness.press("space", "b")
		assert.NotContains(t, harness.readFrame(), "renamed it")
	})

	t.Run("a conversation an agent opened stays rendered while it holds an unsent reply", func(t *testing.T) {
		server := newReviewServer(t)
		reply := newHumanMessage("m2", "yes, keep it")
		reply.Delivery = agentv1.ReviewDeliveryState_REVIEW_DELIVERY_STATE_UNSENT
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

// TestReviewPopupPlacement covers the popup standing over the line it is about, beside the code
// and across the divider.
func TestReviewPopupPlacement(t *testing.T) {
	for _, side := range []string{config.SidebarSideLeft, config.SidebarSideRight} {
		t.Run("with the sidebar on the "+side, func(t *testing.T) {
			server := newReviewServerOver(newWideLineRepo(t))
			server.threads = []*agentv1.ReviewThread{newThreadOnWideLine()}
			harness := openReviewAt(t, server, panelHarnessParams{width: 160, sidebarSide: side})
			harness.press("j", "j", "j", "j")
			lines := splitLines(ansi.Strip(harness.readFrame()))

			top := slices.IndexFunc(lines, func(line string) bool { return strings.Contains(line, "╭") })
			bottom := slices.IndexFunc(lines, func(line string) bool { return strings.Contains(line, "╰") })
			wide := slices.IndexFunc(lines, func(line string) bool { return strings.Contains(line, wideLine) })
			require.GreaterOrEqual(t, top, 0)
			require.GreaterOrEqual(t, wide, 0)
			divider := readColumnOf(t, lines[0], "│")
			left, right := readColumnOf(t, lines[top], "╭"), readColumnOf(t, lines[top], "╮")

			assert.True(t, bottom < wide || top > wide)
			assert.Less(t, left, divider)
			assert.Greater(t, right, divider)
			assert.True(t, slices.ContainsFunc(lines, func(line string) bool { return strings.Contains(line, "func Hello() string {") }))
		})
	}
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
		assert.Len(t, readListRows(frame), 4)
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

	t.Run("a mark holds while the tree is unmoved and a new round has read nothing", func(t *testing.T) {
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

// TestReviewDeliveryCount covers the line counting the reader's own comments by how far each has got.
func TestReviewDeliveryCount(t *testing.T) {
	tests := []struct {
		name     string
		messages []*agentv1.ReviewMessage
		want     string
		absent   []string
	}{
		{
			name:   "a review with nothing in it counts nothing",
			absent: []string{"unsent", "waiting", "delivered"},
		},
		{
			name:     "what the agent said is nobody's delivery",
			messages: []*agentv1.ReviewMessage{newAgentMessage("m9", "done")},
			absent:   []string{"unsent", "waiting", "delivered"},
		},
		{
			name:     "a comment never let go of is unsent",
			messages: []*agentv1.ReviewMessage{newUnsentComment()},
			want:     "1 unsent",
			absent:   []string{"delivered"},
		},
		{
			name:     "each comment is counted by how far it got",
			messages: []*agentv1.ReviewMessage{newUnsentComment(), newSentComment("m2"), newDeliveredComment("m3")},
			want:     "1 unsent · 1 waiting · 1 delivered",
		},
		{
			name:     "a forge comment its author could not push is counted as held",
			messages: []*agentv1.ReviewMessage{newHeldForgeComment("f1", "use a map"), newUnsentComment()},
			want:     "1 held · 1 unsent",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := newReviewServer(t)
			if tt.messages != nil {
				server.threads = []*agentv1.ReviewThread{newThreadOnLastLine(tt.messages...)}
			}

			harness := openReview(t, server)

			stage := readSidebarProse(harness.readFrame())
			assert.Contains(t, stage, tt.want)
			for _, absent := range tt.absent {
				assert.NotContains(t, stage, absent)
			}
		})
	}
}

// TestReviewDeliveryBadge covers the sidebar saying how far each of the reader's comments got.
func TestReviewDeliveryBadge(t *testing.T) {
	tests := []struct {
		name    string
		message *agentv1.ReviewMessage
		want    string
		absent  string
	}{
		{
			name:    "nothing is said of what the agent wrote",
			message: newAgentMessage("m9", "renamed it"),
			want:    "renamed it",
			absent:  "unsent",
		},
		{
			name:    "a comment not let go of is unsent",
			message: newUnsentComment(),
			want:    "◇ unsent",
			absent:  "delivered",
		},
		{
			name:    "a released comment whose notice is waiting says so",
			message: newSentComment("m1"),
			want:    "→ waiting",
			absent:  "unsent",
		},
		{
			name:    "a comment the agent picked up is delivered",
			message: newDeliveredComment("m1"),
			want:    "✓ delivered",
			absent:  "waiting",
		},
		{
			name:    "a forge comment its author could not push is held from the agent",
			message: newHeldForgeComment("f1", "use a map"),
			want:    "! held",
			absent:  "delivered",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := newReviewServer(t)
			server.threads = []*agentv1.ReviewThread{newThreadOnFirstLine(tt.message)}

			harness := openReview(t, server)
			harness.press("space", "b")

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
			name:    "a comment only written down is marked hollow",
			message: newUnsentComment(),
			want:    "◇",
			absent:  "●",
		},
		{
			name:    "a draft outranks the answer waiting in it",
			unread:  true,
			message: newUnsentComment(),
			want:    "◇",
			absent:  "●",
		},
		{
			name:    "an answer nobody has read is filled in",
			unread:  true,
			message: newDeliveredComment("m1"),
			want:    "●",
			absent:  "○",
		},
		{
			name:     "a settled thread says so",
			resolved: "2026-09-02T11:00:00Z",
			message:  newDeliveredComment("m1"),
			want:     "✓",
			absent:   "●",
		},
		{
			name:     "a settled thread the agent spoke in again is filled in",
			resolved: "2026-09-02T11:00:00Z",
			unread:   true,
			message:  newDeliveredComment("m1"),
			want:     "●",
			absent:   "✓",
		},
		{
			name:    "a comment that landed and was read is hollow",
			message: newDeliveredComment("m1"),
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

// TestReviewRelease covers sending the review to the agent.
func TestReviewRelease(t *testing.T) {
	t.Run("the panel sends with no verdict and says the review went", func(t *testing.T) {
		harness := openReview(t, newReviewServer(t))

		harness.press("space", "S")
		assert.Empty(t, harness.readSent(agentv1connect.AgentServiceReleaseReviewProcedure))
		harness.press("enter")

		harness.assertSentTo(agentv1connect.AgentServiceReleaseReviewProcedure, &agentv1.ReleaseReviewRequest{
			Task: xptr.Ptr("t1"),
		})
		assert.Contains(t, readEngineLine(harness.readFrame()), "review sent")
	})

	t.Run("the panel carries a word about the whole change", func(t *testing.T) {
		harness := openReview(t, newReviewServer(t))

		harness.press("space", "S")
		typeText(harness, "the shape is right")
		harness.press("enter")

		harness.assertSentTo(agentv1connect.AgentServiceReleaseReviewProcedure, &agentv1.ReleaseReviewRequest{
			Task: xptr.Ptr("t1"),
			Body: "the shape is right",
		})
	})

	t.Run("escape puts the panel away and sends nothing", func(t *testing.T) {
		harness := openReview(t, newReviewServer(t))

		harness.press("space", "S", "esc", "space", "S")

		assert.Empty(t, harness.readSent(agentv1connect.AgentServiceReleaseReviewProcedure))
	})

	t.Run("a dormant agent takes the review and the panel says it is queued", func(t *testing.T) {
		server := newReviewServer(t)
		server.agent.Alive = xptr.Ptr(false)
		harness := openReview(t, server)

		harness.press("space", "S", "enter")

		harness.assertSentTo(agentv1connect.AgentServiceReleaseReviewProcedure, &agentv1.ReleaseReviewRequest{
			Task: xptr.Ptr("t1"),
		})
		assert.Contains(t, readEngineLine(harness.readFrame()), "review queued for resume")
	})
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

	t.Run("a comment's delivery follows its review", func(t *testing.T) {
		server := newReviewServer(t)
		comment := newHumanMessage("m1", "wrong greeting")
		comment.ReviewId = xptr.Ptr("r1")
		server.threads = []*agentv1.ReviewThread{newThreadOnFirstLine(comment)}
		server.reviews = []*agentv1.Review{{
			Id:         "r1",
			AuthorKind: agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_HUMAN,
			ReleasedAt: "2026-09-02T10:00:00Z",
		}}
		harness := openReview(t, server)
		require.Contains(t, readPopupProse(harness.readFrame()), "waiting")

		server.reviews[0].DeliveredAt = "2026-09-02T10:05:00Z"
		harness.watch(server.buildSnapshot())

		assert.Contains(t, readPopupProse(harness.readFrame()), "delivered")
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
		server.threads = []*agentv1.ReviewThread{newThreadOnFirstLine(newDeliveredComment("m1"))}
		harness := openReview(t, server)

		server.agent.ArchivedAt = xptr.Ptr("2026-09-02T11:00:00Z")
		harness.watch(buildSnapshot(server.workflow, server.agent, server.task))
		require.Contains(t, findLine(readStage(harness.readFrame()), "package greet"), "○")

		harness.press("space", "R")
		require.Len(t, harness.readSent(agentv1connect.AgentServiceListReviewThreadsProcedure), 1)
		require.Contains(t, findLine(readStage(harness.readFrame()), "package greet"), "○")

		harness.press("space", "r")

		assert.Contains(t, findLine(readStage(harness.readFrame()), "package greet"), "✓")
		assert.Len(t, harness.readSent(agentv1connect.AgentServiceListReviewThreadsProcedure), 2)
	})
}

// TestReviewOfAnArchivedAgent covers the diff of an archived agent opening read only.
func TestReviewOfAnArchivedAgent(t *testing.T) {
	server := newReviewServer(t)
	server.threads = []*agentv1.ReviewThread{newThreadOnFirstLine(newUnsentComment())}
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
	assert.NotContains(t, menu, "send to the agent")
	assert.Contains(t, menu, "settle the thread")
	harness.press("esc")

	harness.press("space", "c")
	assert.NotContains(t, harness.readFrame(), "  Comment")
	harness.press("esc", "space", "S", "esc")
	assert.Empty(t, harness.readSent(agentv1connect.AgentServiceReleaseReviewProcedure))
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

		assert.Len(t, harness.readSent(agentv1connect.AgentServiceOpenReviewRoundProcedure), 2)
		assert.True(t, harness.panel.IsZoomed())
	})

	t.Run("the reload key opens the next round where the reader stood", func(t *testing.T) {
		server := newReviewServer(t)
		harness := openReview(t, server)
		harness.press("j", "j")

		server.repo.Wrote(newRoundWrittenOver())
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

	t.Run("a move the watch reports of an idle agent is read again", func(t *testing.T) {
		server := newReviewServer(t)
		harness := openReview(t, server)

		server.repo.Wrote(newRoundWrittenOver())
		server.task.MovedSinceRound = gittest.SnapshotSHA
		harness.watch(server.buildSnapshot())

		frame := harness.readFrame()
		assert.Contains(t, frame, "written by the agent")
		assert.NotContains(t, readEngineLine(frame), "the worktree has moved")
		assert.Len(t, harness.readSent(agentv1connect.AgentServiceOpenReviewRoundProcedure), 2)

		harness.watch(server.buildSnapshot())
		assert.Len(t, harness.readSent(agentv1connect.AgentServiceOpenReviewRoundProcedure), 2)
	})

	t.Run("a move from a round other than the one on screen is not followed", func(t *testing.T) {
		server := newReviewServer(t)
		harness := openReview(t, server)

		server.task.MovedSinceRound = "snapshot-of-another-round"
		harness.watch(server.buildSnapshot())

		assert.Len(t, harness.readSent(agentv1connect.AgentServiceOpenReviewRoundProcedure), 1)
	})

	t.Run("a move waits while a search is typed and is read once it is put away", func(t *testing.T) {
		server := newReviewServer(t)
		harness := openReview(t, server)
		harness.press("/", "a")

		server.repo.Wrote(newRoundWrittenOver())
		server.task.MovedSinceRound = gittest.SnapshotSHA
		harness.watch(server.buildSnapshot())
		assert.NotContains(t, harness.readFrame(), "written by the agent")

		harness.press("esc")
		harness.watch(server.buildSnapshot())
		assert.Contains(t, harness.readFrame(), "written by the agent")
	})

	t.Run("a move under an unsent comment is offered rather than read", func(t *testing.T) {
		server := newReviewServer(t)
		server.threads = []*agentv1.ReviewThread{newThreadOnFirstLine(newUnsentComment())}
		harness := openReview(t, server)

		server.repo.Wrote(newRoundWrittenOver())
		server.task.MovedSinceRound = gittest.SnapshotSHA
		harness.watch(server.buildSnapshot())

		frame := harness.readFrame()
		assert.NotContains(t, frame, "written by the agent")
		assert.Contains(t, readEngineLine(frame), "the worktree has moved")

		harness.watch(server.buildSnapshot())
		assert.Equal(t, 1, strings.Count(readOpenMessages(harness), "the worktree has moved"))
	})

	t.Run("a writer stopping under an unsent comment turns its notice into the reload hint", func(t *testing.T) {
		server := newReviewServer(t)
		server.agent.State = agentv1.AgentState_AGENT_STATE_THINKING
		server.threads = []*agentv1.ReviewThread{newThreadOnFirstLine(newUnsentComment())}
		harness := openReview(t, server)

		server.repo.Wrote(newRoundWrittenOver())
		server.task.MovedSinceRound = gittest.SnapshotSHA
		harness.watch(server.buildSnapshot())
		require.Contains(t, readEngineLine(harness.readFrame()), "the agent is writing")

		server.agent.State = agentv1.AgentState_AGENT_STATE_IDLE
		harness.watch(server.buildSnapshot())

		said := readOpenMessages(harness)
		assert.Contains(t, said, "the worktree has moved")
		assert.NotContains(t, said, "the agent is writing")
	})

	t.Run("a writer stopping a second time under an unsent comment raises the reload hint again", func(t *testing.T) {
		server := newReviewServer(t)
		server.threads = []*agentv1.ReviewThread{newThreadOnFirstLine(newUnsentComment())}
		harness := openReview(t, server)

		server.repo.Wrote(newRoundWrittenOver())
		server.task.MovedSinceRound = gittest.SnapshotSHA
		harness.watch(server.buildSnapshot())
		require.Contains(t, readEngineLine(harness.readFrame()), "the worktree has moved")

		server.agent.State = agentv1.AgentState_AGENT_STATE_THINKING
		harness.watch(server.buildSnapshot())
		require.NotContains(t, readOpenMessages(harness), "the worktree has moved")

		server.agent.State = agentv1.AgentState_AGENT_STATE_IDLE
		harness.watch(server.buildSnapshot())

		assert.Contains(t, readOpenMessages(harness), "the worktree has moved")
	})

	t.Run("a move's notice comes down once the diff catches up", func(t *testing.T) {
		server := newReviewServer(t)
		server.threads = []*agentv1.ReviewThread{newThreadOnFirstLine(newUnsentComment())}
		harness := openReview(t, server)

		server.repo.Wrote(newRoundWrittenOver())
		server.task.MovedSinceRound = gittest.SnapshotSHA
		harness.watch(server.buildSnapshot())
		require.Contains(t, readEngineLine(harness.readFrame()), "the worktree has moved")

		harness.press("space", "R")

		assert.NotContains(t, readOpenMessages(harness), "the worktree has moved")
	})

	t.Run("a move while the editor is open raises nothing", func(t *testing.T) {
		server := newReviewServer(t)
		server.threads = []*agentv1.ReviewThread{newThreadOnFirstLine(newUnsentComment())}
		harness := openReview(t, server)
		harness.press("space", "e")

		server.repo.Wrote(newRoundWrittenOver())
		server.task.MovedSinceRound = gittest.SnapshotSHA
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

// TestReviewLayout covers where the diff and the sidebar stand and that the frame fits the pane.
func TestReviewLayout(t *testing.T) {
	t.Run("the sidebar stands on the side the layout names", func(t *testing.T) {
		tests := []struct {
			name string
			side string
			want string
		}{
			{
				name: "on the left the top row opens with the sidebar",
				side: config.SidebarSideLeft,
				want: "\U000F050C komrad",
			},
			{
				name: "on the right the top row opens with the band over the diff",
				side: config.SidebarSideRight,
				want: "Review · auth",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				harness := openReviewAt(t, newReviewServer(t), panelHarnessParams{sidebarSide: tt.side})

				assert.True(t, strings.HasPrefix(splitLines(harness.readFrame())[0], tt.want))
			})
		}
	})

	t.Run("the logo stands over the sidebar without the row of views", func(t *testing.T) {
		harness := openReview(t, newReviewServer(t))

		frame := harness.readFrame()
		require.Contains(t, splitLines(frame)[0], "komrad")
		assert.NotContains(t, frame, "agents  schedules")
	})

	t.Run("the logo gives way before the legend does and the engine row stays", func(t *testing.T) {
		harness := openReviewAt(t, newReviewServer(t), panelHarnessParams{height: 21})
		require.Contains(t, splitLines(harness.readFrame())[0], "komrad")

		harness.press("?")

		assert.NotContains(t, harness.readFrame(), "komrad")
		assert.Contains(t, readEngineRow(harness.readFrame(), 0), "local")
		assert.Equal(t, "greet.go:1", readReviewCursor(harness.readFrame()))
	})

	t.Run("a window too narrow for both keeps the sidebar and drops the diff", func(t *testing.T) {
		harness := openReviewAt(t, newReviewServer(t), panelHarnessParams{width: 120})
		assert.Contains(t, harness.readFrame(), "package greet")

		harness.send(tea.WindowSizeMsg{Width: 45, Height: reviewHeight})

		frame := harness.readFrame()
		assert.NotContains(t, frame, "package greet")
		assert.Contains(t, frame, "greet.go:1")
	})

	t.Run("the menu stands in a popup over the diff", func(t *testing.T) {
		harness := openReview(t, newReviewServer(t))

		harness.press("space")

		frame := harness.readFrame()
		assert.Contains(t, frame, "wrap lines")
		assert.Contains(t, frame, "package greet")
	})

	t.Run("the popup on a thread renders the conversation alone until the menu opens under it", func(t *testing.T) {
		server := newReviewServer(t)
		server.threads = []*agentv1.ReviewThread{newThreadOnFirstLine(newUnsentComment())}
		harness := openReview(t, server)

		frame := harness.readFrame()
		require.Contains(t, frame, "◇ unsent")
		assert.NotContains(t, frame, "wrap lines")

		harness.press("space")

		frame = harness.readFrame()
		assert.Contains(t, frame, "◇ unsent")
		assert.Contains(t, frame, "wrap lines")
		assert.Less(t, strings.Index(frame, "◇ unsent"), strings.Index(frame, "wrap lines"))
	})

	t.Run("the menu fits the room beside the cursor on a short pane", func(t *testing.T) {
		harness, _ := openTallReview(t)
		harness.send(tea.WindowSizeMsg{Width: reviewWidth, Height: 16})
		harness.press("j", "j", "j", "j", "j", "space")

		frame := harness.readFrame()
		assert.Contains(t, frame, "open in your editor")
		assert.Contains(t, frame, "wrap lines")
		assert.Len(t, splitLines(frame), 16)
	})

	t.Run("an expanded legend stays inside every pane height", func(t *testing.T) {
		harness := openReview(t, newEarlierReviewServer(t))
		harness.press("?")

		for height := 8; height <= 40; height++ {
			harness.send(tea.WindowSizeMsg{Width: reviewWidth, Height: height})

			assert.Len(t, splitLines(harness.readFrame()), height)
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

// TestReviewRounds covers walking the rounds of a review with the base keys.
func TestReviewRounds(t *testing.T) {
	t.Run("the base key narrows to the work since the last review and forward widens it back", func(t *testing.T) {
		harness := openReview(t, newEarlierReviewServer(t))
		frame := harness.readFrame()
		require.Contains(t, readSidebarProse(frame), "2 files, +4 -1")
		require.Contains(t, readSidebarProse(frame), "1 file changed since you last reviewed")

		harness.press("<")
		frame = harness.readFrame()
		assert.Contains(t, readSidebarProse(frame), "chapter 2 of 2")
		assert.Contains(t, readSidebarProse(frame), "1 file, +3 -0")
		assert.NotRegexp(t, `\d+/\d+ viewed`, frame)
		assert.NotContains(t, readSidebarProse(frame), "since you last reviewed")

		harness.press(">")
		frame = harness.readFrame()
		assert.Contains(t, readSidebarProse(frame), "all changes")
		assert.Contains(t, readSidebarProse(frame), "2 files, +4 -1")
	})

	t.Run("one load reads the reviews and the rounds once", func(t *testing.T) {
		harness := openReview(t, newEarlierReviewServer(t))

		assert.Len(t, harness.readSent(agentv1connect.AgentServiceListReviewsProcedure), 1)
		assert.Len(t, harness.readSent(agentv1connect.AgentServiceListReviewRoundsProcedure), 1)
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

	t.Run("narrowing onto a round with nothing new keeps the whole change and says so", func(t *testing.T) {
		server := newEarlierReviewServer(t)
		harness := openReview(t, server)
		server.repo.Wrote(gittest.WroteParams{Tree: gittest.UnmovedTree, Whole: gittest.Patch})
		harness.press("esc", "esc", "space", "r")

		harness.press("<")

		frame := harness.readFrame()
		assert.True(t, harness.panel.IsZoomed())
		assert.Contains(t, readStageText(frame), "greet.go")
		assert.Contains(t, readEngineLine(frame), "nothing written")
	})

	t.Run("a round measured from another base is walked but not compared", func(t *testing.T) {
		server := newEarlierReviewServer(t)
		server.rounds[0].BaseSha = "0000000000000000000000000000000000000000"
		harness := openReview(t, server)

		harness.press("<")

		assert.Contains(t, readSidebarProse(harness.readFrame()), "not comparable, the base moved")
	})

	t.Run("the newest round takes a comment measured from the round reviewed before", func(t *testing.T) {
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

	t.Run("a finished round refuses every key that writes", func(t *testing.T) {
		harness := openReviewAt(t, newEarlierReviewServer(t), panelHarnessParams{height: 50})
		harness.press("<", "<")
		require.Contains(t, readSidebarProse(harness.readFrame()), "chapter 1 of 2")
		harness.press("space")
		assert.NotContains(t, harness.readFrame(), "send to the agent")
		harness.press("esc")

		for _, key := range []string{"c", "S"} {
			harness.press("space", key)

			frame := harness.readFrame()
			assert.NotContains(t, frame, "  Comment")
			assert.Contains(t, readEngineLine(frame), "chapter 1 of 2 is finished")
		}
		assert.Empty(t, harness.readSent(agentv1connect.AgentServiceCreateReviewThreadProcedure))
		assert.Empty(t, harness.readSent(agentv1connect.AgentServiceReleaseReviewProcedure))
		assert.Empty(t, harness.readSent(agentv1connect.AgentServiceHoldAgentProcedure))
	})

	t.Run("a viewed mark taken in a round leaves the whole change's mark alone", func(t *testing.T) {
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
		require.Contains(t, readSidebarProse(harness.readFrame()), "chapter 2 of 2")
		require.NotContains(t, readStageText(harness.readFrame()), "greet.go  +")
		harness.press("space", "v")
		frame := harness.readFrame()
		assert.Contains(t, readOpenMessages(harness), "whole change")
		assert.NotContains(t, readStageText(frame), "greet.go  +")
		assert.Len(t, harness.readSent(agentv1connect.AgentServiceSetReviewFileViewedProcedure), 1)

		harness.press("esc", ">")
		assert.Contains(t, readStageText(harness.readFrame()), "greet.go  +")
	})

	t.Run("a comment being written goes with the round a landing load takes away", func(t *testing.T) {
		server := newEarlierReviewServer(t)
		harness := openReview(t, server)
		harness.press("<")
		harness.hold(agentv1connect.AgentServiceListReviewRoundsProcedure)
		harness.press("space", "R")
		server.roundsErr = errors.New("the server went away")

		harness.press("space", "c")
		typeText(harness, "hello")
		require.Contains(t, harness.readFrame(), "  Comment")
		harness.release(agentv1connect.AgentServiceListReviewRoundsProcedure)

		assert.NotContains(t, harness.readFrame(), "  Comment")
		harness.press("enter")
		assert.Empty(t, harness.readSent(agentv1connect.AgentServiceCreateReviewThreadProcedure))
	})

	t.Run("a round listing that fails takes the reader back to the whole change", func(t *testing.T) {
		server := newEarlierReviewServer(t)
		harness := openReview(t, server)
		harness.press("<")
		require.Contains(t, readSidebarProse(harness.readFrame()), "chapter 2 of 2")

		server.roundsErr = errors.New("the server went away")
		harness.press("space", "R")

		frame := harness.readFrame()
		assert.Contains(t, frame, "all changes")
		assert.Contains(t, frame, "2 files, +4 -1")
	})

	t.Run("a chapter splits the replies written after the next round from the ones before", func(t *testing.T) {
		harness := openReviewAt(t, newLaterRepliesServer(t), panelHarnessParams{height: 50})

		harness.press("<", "<", "t")

		frame := harness.readFrame()
		require.Contains(t, readSidebarProse(frame), "chapter 1 of 2")
		said := readPopupProse(frame)
		assert.Contains(t, said, "thanks")
		assert.Less(t, strings.Index(said, "looking"), strings.Index(said, laterRule))
		assert.Less(t, strings.Index(said, laterRule), strings.Index(said, "added it"))
		assert.Contains(t, said, "addressed in round 2")
	})
}

// TestReviewPin covers an open batch of comments holding the diff to the round it was written on.
func TestReviewPin(t *testing.T) {
	t.Run("an open batch holds the diff to its round while the agent writes on", func(t *testing.T) {
		server := newReviewServer(t)
		harness := openReview(t, server)
		server.reviews = []*agentv1.Review{{
			Id:         "v1",
			RoundId:    "round-1",
			AuthorKind: agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_HUMAN,
		}}

		server.repo.Wrote(newRoundWrittenOver())
		harness.press("esc", "esc", "space", "r")

		assert.NotContains(t, harness.readFrame(), "written by the agent")
	})

	t.Run("the reload key catches up past an open batch", func(t *testing.T) {
		server := newReviewServer(t)
		harness := openReview(t, server)
		server.reviews = []*agentv1.Review{{
			Id:         "v1",
			RoundId:    "round-1",
			AuthorKind: agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_HUMAN,
		}}

		server.repo.Wrote(newRoundWrittenOver())
		harness.press("esc", "esc", "space", "r")
		require.NotContains(t, harness.readFrame(), "written by the agent")

		harness.press("space", "R")

		assert.Contains(t, harness.readFrame(), "written by the agent")
	})

	t.Run("a held review says the diff is frozen and how to see the latest", func(t *testing.T) {
		taken := time.Date(2026, 10, 9, 15, 4, 0, 0, time.UTC)
		server := newReviewServer(t)
		server.rounds = []*agentv1.ReviewRound{{
			Id:          "round-1",
			TaskId:      "t1",
			Ordinal:     1,
			BaseSha:     gittest.BaseSHA,
			SnapshotSha: gittest.SnapshotSHA,
			CreatedAt:   taken.Format(time.RFC3339),
		}}
		server.reviews = []*agentv1.Review{{
			Id:         "v1",
			RoundId:    "round-1",
			AuthorKind: agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_HUMAN,
		}}
		harness := openReview(t, server)

		assert.Contains(t, joinProse(readStage(harness.readFrame())), "Frozen on round 1 from "+
			taken.Local().Format("Jan 2 at 3:04pm")+" until you send your review · space R shows the latest")

		harness.press("space", "R")

		assert.NotContains(t, harness.readFrame(), "Frozen on round")
	})

	t.Run("the reviews failing to read fails the load rather than moving the pin", func(t *testing.T) {
		server := newReviewServer(t)
		server.reviewsErr = errors.New("the server went away")

		harness := openReview(t, server)

		assert.Equal(t, agentsScreen, harness.readScreen())
		assert.Contains(t, readOpenMessages(harness), "the server went away")
	})

	t.Run("the rounds failing to read fails the load only while a batch is open", func(t *testing.T) {
		server := newReviewServer(t)
		server.roundsErr = errors.New("the server went away")
		server.reviews = []*agentv1.Review{{
			Id:         "v1",
			RoundId:    "round-1",
			AuthorKind: agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_HUMAN,
		}}
		harness := openReview(t, server)
		assert.Equal(t, agentsScreen, harness.readScreen())

		server.reviews = nil
		harness.press("space", "r")
		assert.True(t, harness.panel.IsZoomed())
		assert.Contains(t, harness.readFrame(), "package greet")
	})
}

// TestReviewReplay covers a round across a moved base carried onto it before it is compared.
func TestReviewReplay(t *testing.T) {
	t.Run("a round across a moved base is replayed onto it", func(t *testing.T) {
		harness := openReview(t, newMovedBaseServer(t, &git.Replayed{Tree: replayedTree}))

		harness.press("<")

		frame := harness.readFrame()
		assert.Contains(t, readSidebarProse(frame), "chapter 2 of 2, not reviewed yet, the base moved, replayed")
		assert.Contains(t, readSidebarProse(frame), "1 file, +3 -0")
		assert.NotContains(t, frame, "not comparable")
	})

	t.Run("a round the base rewrote under the agent is refused and names the paths", func(t *testing.T) {
		harness := openReview(t, newMovedBaseServer(t, &git.Replayed{
			Tree:      "a-tree-with-markers-in-it",
			Conflicts: []string{gittest.File, "second.go"},
		}))

		harness.press("<")

		assert.Contains(t, readSidebarProse(harness.readFrame()), "chapter 2 of 2, not comparable, the base moved, conflicts in greet.go, second.go")
	})

	t.Run("a git that cannot replay leaves the refusal standing", func(t *testing.T) {
		harness := openReview(t, newMovedBaseServer(t, nil))

		harness.press("<")

		frame := harness.readFrame()
		assert.Contains(t, readSidebarProse(frame), "chapter 2 of 2, not comparable, the base moved")
		assert.NotContains(t, frame, "rebased")
	})

	t.Run("a base the branch no longer descends from is said to be rewritten", func(t *testing.T) {
		server := newMovedBaseServer(t, &git.Replayed{Tree: replayedTree})
		server.repo.Rewritten = map[string]bool{gittest.NamePair(olderBase, gittest.BaseSHA): true}
		harness := openReview(t, server)

		harness.press("<")

		assert.Contains(t, readSidebarProse(harness.readFrame()), "chapter 2 of 2, not reviewed yet, history was rewritten, replayed")
	})

	t.Run("the first round is measured from its own base", func(t *testing.T) {
		server := newMovedBaseServer(t, &git.Replayed{Tree: replayedTree})
		server.repo.Patches[gittest.NamePair(olderBase, gittest.SnapshotSHA)] = gittest.Patch
		server.repo.Patches[gittest.NamePair(gittest.BaseSHA, gittest.SnapshotSHA)] = gittest.PatchOfBothFiles
		harness := openReview(t, server)

		harness.press("<", "<")

		frame := harness.readFrame()
		assert.Contains(t, readSidebarProse(frame), "chapter 1 of 2, reviewed at ")
		assert.Contains(t, readSidebarProse(frame), ", on an earlier base")
		assert.Contains(t, readSidebarProse(frame), "1 file, +1 -1")
	})

	t.Run("an old side counted in the replayed tree pins only there", func(t *testing.T) {
		server := newMovedBaseServer(t, &git.Replayed{Tree: replayedTree})
		server.repo.Patches[gittest.NamePair(replayedTree, gittest.NameSnapshot(secondTree))] = gittest.Patch
		server.threads = []*agentv1.ReviewThread{{
			Id:          "t1",
			RoundId:     "round-2",
			FilePath:    gittest.File,
			BaseSha:     replayedTree,
			OldStart:    4,
			OldEnd:      4,
			QuotedLines: []string{"-\treturn \"nowhere\""},
			Messages:    []*agentv1.ReviewMessage{newHumanMessage("m1", "written on the replayed chapter")},
		}}
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
	t.Run("the composer is rendered as an ordinary field", func(t *testing.T) {
		harness := openReview(t, newReviewServer(t))

		harness.press("space", "c")

		sidebar := readPopupProse(harness.readFrame())
		assert.Contains(t, sidebar, "Comment")
		assert.Contains(t, sidebar, "opens $EDITOR")
	})

	t.Run("ctrl+j makes a line rather than sending", func(t *testing.T) {
		harness := openReview(t, newReviewServer(t))

		harness.press("space", "c", "o", "n", "e", "ctrl+j", "t", "w", "o")

		assert.Contains(t, readPopupProse(harness.readFrame()), "one")
		assert.Contains(t, readPopupProse(harness.readFrame()), "two")
		assert.Empty(t, harness.readSent(agentv1connect.AgentServiceCreateReviewThreadProcedure))
	})

	t.Run("the field takes a width it was not built at", func(t *testing.T) {
		harness := openReviewAt(t, newReviewServer(t), panelHarnessParams{width: reviewWidth, sidebarWidth: 100})
		harness.press("space", "c")
		typeText(harness, strings.Repeat("word ", 30))
		built := findWidestPopupRow(harness.readFrame())

		harness.send(tea.WindowSizeMsg{Width: 70, Height: reviewHeight})

		frame := harness.readFrame()
		assert.Less(t, findWidestPopupRow(frame), built)
		assert.Equal(t, 30, strings.Count(readPopupProse(frame), "word"))
	})

	t.Run("a note wraps inside a popup narrower than its widest", func(t *testing.T) {
		const note = "a note long enough to wrap once the field it is typed in has been narrowed"
		harness := openReviewAt(t, newReviewServer(t), panelHarnessParams{width: 90})
		harness.press("space", "c")
		typeText(harness, note)

		assert.Contains(t, readPopupProse(harness.readFrame()), note)
	})

	t.Run("every line typed is rendered at a narrow sidebar", func(t *testing.T) {
		harness := openReviewAt(t, newReviewServer(t), panelHarnessParams{width: 22, height: 60})

		harness.press("space", "S")
		typeText(harness, "one")
		for _, line := range []string{"two", "three", "four"} {
			harness.press("ctrl+j")
			typeText(harness, line)
		}

		frame := harness.readFrame()
		for _, word := range []string{"one", "two", "three", "four"} {
			assert.Contains(t, frame, "  "+word)
		}
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

	t.Run("an unreadable pane refuses the edit key and stops nothing", func(t *testing.T) {
		server := newReviewServer(t)
		server.agent.State = agentv1.AgentState_AGENT_STATE_UNCERTAIN
		harness := openReview(t, server)

		harness.press("space", "e")

		assert.Contains(t, readOpenMessages(harness), "cannot read this agent's pane")
		assert.Empty(t, harness.readSent(agentv1connect.AgentServiceHoldAgentProcedure))
		harness.press("esc")

		server.repo.Wrote(newRoundWrittenOver())
		server.task.MovedSinceRound = gittest.SnapshotSHA
		harness.watch(server.buildSnapshot())
		assert.Contains(t, harness.readFrame(), "written by the agent")
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
		loads := len(harness.readSent(agentv1connect.AgentServiceOpenReviewRoundProcedure))

		editor := harness.panel.ReadRespawnPanes()
		require.NotEmpty(t, editor)
		harness.panel.SetPaneDead(editor[len(editor)-1])
		harness.fireTimers()

		assert.NotContains(t, harness.readFrame(), "editing ·")
		assert.Contains(t, readReleaseSaid(t, harness), "while I had your worktree open")
		assert.Len(t, harness.readSent(agentv1connect.AgentServiceOpenReviewRoundProcedure), loads+1)
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

	t.Run("the back key closes the editor before it leaves the screen", func(t *testing.T) {
		harness := openReview(t, newReviewServer(t))

		harness.press("space", "e", "esc")

		frame := harness.readFrame()
		assert.NotContains(t, frame, "editing ·")
		assert.Contains(t, frame, "Review · auth")
	})

	t.Run("closing the editor lets the agent go and says where to look", func(t *testing.T) {
		harness := openReview(t, newReviewServer(t))

		harness.press("space", "e", "space", "e")

		said := readReleaseSaid(t, harness)
		assert.Contains(t, said, "let you go")
		assert.Contains(t, said, "greet.go")
		assert.Contains(t, harness.readFrame(), "Review · auth")
	})

	t.Run("a diff that stops being readable lets the agent go with the files", func(t *testing.T) {
		server := newReviewServer(t)
		harness := openReview(t, server)
		harness.press("space", "e")

		server.repo.RootErr = gittest.ErrNoGit
		harness.press("space", "R")

		assert.Equal(t, agentsScreen, harness.readScreen())
		assert.Contains(t, readReleaseSaid(t, harness), "greet.go")
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
		server.threads = []*agentv1.ReviewThread{newThreadOnFirstLine(newUnsentComment())}
		harness := openReview(t, server)

		harness.press("space", "e")
		server.repo.Wrote(gittest.WroteParams{Tree: "tree-the-reader-edited", Whole: gittest.PatchWrittenOver})
		harness.press("space", "e", "j")

		assert.Contains(t, readStageText(harness.readFrame()), "written by the agent")
	})

	t.Run("a diff is held while the agent writes and read again when it stops", func(t *testing.T) {
		server := newReviewServer(t)
		server.agent.State = agentv1.AgentState_AGENT_STATE_THINKING
		harness := openReview(t, server)

		server.repo.Wrote(newRoundWrittenOver())
		server.task.MovedSinceRound = gittest.SnapshotSHA
		harness.watch(server.buildSnapshot())
		frame := harness.readFrame()
		assert.NotContains(t, frame, "written by the agent")
		assert.Contains(t, readEngineLine(frame), "the agent is writing")

		server.agent.State = agentv1.AgentState_AGENT_STATE_IDLE
		harness.watch(server.buildSnapshot())
		assert.Contains(t, harness.readFrame(), "written by the agent")
		assert.Len(t, harness.readSent(agentv1connect.AgentServiceOpenReviewRoundProcedure), 2)
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
		server.repo.Wrote(newRoundWrittenOver())
		server.task.MovedSinceRound = gittest.SnapshotSHA
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
	replayedTree = "replayed-round-one"
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
	rounds     []*agentv1.ReviewRound
	reviews    []*agentv1.Review
	forge      []proto.Message
	reviewsErr error
	roundsErr  error
	openErr    error
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
		rows = append(rows, thread)
	}
	for _, review := range s.reviews {
		taskID := review.GetTaskId()
		if taskID == "" {
			taskID = s.agent.GetTaskId()
		}
		rows = append(rows, &agentv1.WatchedReview{
			Id:           review.GetId(),
			TaskId:       taskID,
			RoundId:      review.GetRoundId(),
			RoundOrdinal: review.GetRoundOrdinal(),
			AuthorKind:   review.GetAuthorKind(),
			Verdict:      review.GetVerdict(),
			ReleasedAt:   review.GetReleasedAt(),
			DeliveredAt:  review.GetDeliveredAt(),
		})
	}
	rows = append(rows, s.forge...)

	return buildSnapshot(rows...)
}

func (s *reviewServer) serve(harness *panelHarness) {
	harness.answer(worktreev1connect.WorktreeServiceGetWorktreeProcedure, func(connect.AnyRequest) (connect.AnyResponse, error) {
		return connect.NewResponse(&worktreev1.GetWorktreeResponse{Worktree: &worktreev1.Worktree{Dir: s.repo.Root}}), nil
	})
	harness.answer(agentv1connect.AgentServiceOpenReviewRoundProcedure, s.openRound)
	harness.answer(agentv1connect.AgentServiceListReviewRoundsProcedure, s.listRounds)
	harness.answer(agentv1connect.AgentServiceListReviewsProcedure, s.listReviews)
	harness.answer(agentv1connect.AgentServiceListReviewThreadsProcedure, s.listThreads)
	harness.answer(agentv1connect.AgentServiceListReviewViewedFilesProcedure, s.listViewed)
	harness.answer(agentv1connect.AgentServiceSetReviewFileViewedProcedure, s.markViewed)
	harness.answer(agentv1connect.AgentServiceReleaseReviewProcedure, s.release)
	harness.answer(agentv1connect.AgentServiceAddReviewMessageProcedure, s.reply)
	harness.answer(agentv1connect.AgentServiceSetReviewThreadResolvedProcedure, s.settle)
	harness.answer(agentv1connect.AgentServiceCreateReviewThreadProcedure, s.createThread)
	harness.answer(agentv1connect.AgentServiceHoldAgentProcedure, s.hold)
}

func (s *reviewServer) openRound(connect.AnyRequest) (connect.AnyResponse, error) {
	if s.openErr != nil {
		return nil, s.openErr
	}

	sha := gittest.NameSnapshot(s.repo.Tree)
	if len(s.rounds) == 0 || s.rounds[len(s.rounds)-1].GetSnapshotSha() != sha {
		s.rounds = append(s.rounds, &agentv1.ReviewRound{
			Id:          fmt.Sprintf("round-%d", len(s.rounds)+1),
			TaskId:      s.agent.GetTaskId(),
			Ordinal:     int32(len(s.rounds) + 1),
			BaseSha:     gittest.BaseSHA,
			SnapshotSha: sha,
		})
	}

	return connect.NewResponse(&agentv1.OpenReviewRoundResponse{Round: s.rounds[len(s.rounds)-1]}), nil
}

func (s *reviewServer) listRounds(connect.AnyRequest) (connect.AnyResponse, error) {
	if s.roundsErr != nil {
		return nil, s.roundsErr
	}

	return connect.NewResponse(&agentv1.ListReviewRoundsResponse{Rounds: s.rounds}), nil
}

func (s *reviewServer) listReviews(connect.AnyRequest) (connect.AnyResponse, error) {
	if s.reviewsErr != nil {
		return nil, s.reviewsErr
	}

	return connect.NewResponse(&agentv1.ListReviewsResponse{Reviews: s.reviews}), nil
}

func (s *reviewServer) listThreads(connect.AnyRequest) (connect.AnyResponse, error) {
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

func (s *reviewServer) release(connect.AnyRequest) (connect.AnyResponse, error) {
	var sent int32
	for _, thread := range s.threads {
		for _, message := range thread.GetMessages() {
			if message.GetDelivery() == agentv1.ReviewDeliveryState_REVIEW_DELIVERY_STATE_UNSENT {
				message.Delivery = agentv1.ReviewDeliveryState_REVIEW_DELIVERY_STATE_SENT
				sent++
			}
		}
	}

	return connect.NewResponse(&agentv1.ReleaseReviewResponse{Comments: sent}), nil
}

func (s *reviewServer) reply(req connect.AnyRequest) (connect.AnyResponse, error) {
	reply, _ := req.Any().(*agentv1.AddReviewMessageRequest)
	message := &agentv1.ReviewMessage{
		Id:         "m2",
		ThreadId:   reply.GetThreadId(),
		Body:       reply.GetBody(),
		AuthorKind: agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_HUMAN,
		Delivery:   agentv1.ReviewDeliveryState_REVIEW_DELIVERY_STATE_UNSENT,
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

	return connect.NewResponse(&agentv1.CreateReviewThreadResponse{
		Thread: &agentv1.ReviewThread{Id: "t1", FilePath: create.GetFilePath()},
	}), nil
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

func newUnsentComment() *agentv1.ReviewMessage {
	message := newHumanMessage("m1", "")
	message.Delivery = agentv1.ReviewDeliveryState_REVIEW_DELIVERY_STATE_UNSENT

	return message
}

func newSentComment(id string) *agentv1.ReviewMessage {
	message := newHumanMessage(id, "")
	message.Delivery = agentv1.ReviewDeliveryState_REVIEW_DELIVERY_STATE_SENT
	message.SentAt = "2026-09-02T10:00:00Z"

	return message
}

func newDeliveredComment(id string) *agentv1.ReviewMessage {
	message := newSentComment(id)
	message.Delivery = agentv1.ReviewDeliveryState_REVIEW_DELIVERY_STATE_DELIVERED
	message.DeliveredAt = "2026-09-02T10:01:00Z"

	return message
}

func newHeldForgeComment(id, body string) *agentv1.ReviewMessage {
	return &agentv1.ReviewMessage{
		Id:         id,
		AuthorKind: agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_FORGE,
		Body:       body,
		Delivery:   agentv1.ReviewDeliveryState_REVIEW_DELIVERY_STATE_HELD,
	}
}

func newRoundWrittenOver() gittest.WroteParams {
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
	server.reviews = []*agentv1.Review{{
		Id:          "r1",
		AuthorKind:  agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_HUMAN,
		ReleasedAt:  "2026-09-13T10:00:00Z",
		OpenThreads: int32(len(threads)),
	}}

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
		ReviewId:    "r1",
		FilePath:    path,
		OldStart:    line,
		NewStart:    line,
		QuotedLines: []string{source[line]},
		Messages:    []*agentv1.ReviewMessage{newDeliveredComment(id + "-m1")},
	}
}

func newEarlierReviewServer(t *testing.T) *reviewServer {
	t.Helper()

	server := newReviewServer(t)
	server.rounds = []*agentv1.ReviewRound{{
		Id:          "round-1",
		TaskId:      "t1",
		Ordinal:     1,
		BaseSha:     gittest.BaseSHA,
		SnapshotSha: gittest.SnapshotSHA,
	}}
	server.reviews = []*agentv1.Review{{
		Id:           "v1",
		RoundId:      "round-1",
		RoundOrdinal: 1,
		AuthorKind:   agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_HUMAN,
		ReleasedAt:   "2026-09-08T10:00:00Z",
	}}
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

func newMovedBaseServer(t *testing.T, replayed *git.Replayed) *reviewServer {
	t.Helper()

	server := newEarlierReviewServer(t)
	server.rounds[0].BaseSha = olderBase
	server.repo.Patches[gittest.NamePair(gittest.SnapshotSHA, gittest.NameSnapshot(secondTree))] = gittest.PatchOfBothFiles
	server.repo.Patches[gittest.NamePair(replayedTree, gittest.NameSnapshot(secondTree))] = gittest.PatchOfTheSecondFile
	if replayed != nil {
		server.repo.Replays = map[string]*git.Replayed{
			gittest.NameReplay(olderBase, gittest.BaseSHA, gittest.SnapshotSHA): replayed,
		}
	}

	return server
}

func findWidestPopupRow(frame string) int {
	widest := 0
	for _, row := range readPopup(frame) {
		widest = max(widest, ansi.StringWidth(row))
	}

	return widest
}

func readReleaseSaid(t *testing.T, harness *panelHarness) string {
	t.Helper()

	sent := harness.readSent(agentv1connect.AgentServiceReleaseAgentProcedure)
	require.Len(t, sent, 1)
	released, _ := sent[0].(*agentv1.ReleaseAgentRequest)
	assert.Equal(t, "a1", released.GetId())

	return released.GetSaid()
}

// wideLine is a line of code long enough to reach past the room beside it.
const wideLine = `return "a farewell long enough to crowd the popup"`

// readColumnOf is the column a glyph first stands at in a row.
func readColumnOf(t *testing.T, line, glyph string) int {
	t.Helper()

	at := strings.Index(line, glyph)
	require.GreaterOrEqual(t, at, 0, "no %s in %q", glyph, line)

	return ansi.StringWidth(line[:at])
}

func newWideLineRepo(t *testing.T) *gittest.Repo {
	t.Helper()

	repo := gittest.NewRepo(t.TempDir())
	repo.Patches[gittest.NamePair(gittest.BaseSHA, gittest.SnapshotSHA)] = strings.Replace(gittest.Patch, `return "goodbye"`, wideLine, 1)
	repo.Sources[gittest.NameSource(gittest.SnapshotSHA, gittest.File)] = strings.Replace(gittest.HeadSource, `return "goodbye"`, wideLine, 1)

	return repo
}

func newThreadOnWideLine() *agentv1.ReviewThread {
	return &agentv1.ReviewThread{
		Id:          "t1",
		FilePath:    gittest.File,
		NewStart:    4,
		QuotedLines: []string{"+\t" + wideLine},
		Messages:    []*agentv1.ReviewMessage{newHumanMessage("m1", "tighten this")},
	}
}

// TestReviewChangeBlock covers what the sidebar says about the change under review.
func TestReviewChangeBlock(t *testing.T) {
	t.Run("the band over the diff holds only the title and the filter", func(t *testing.T) {
		harness := openReviewAt(t, newReviewServer(t), panelHarnessParams{})

		frame := harness.readFrame()
		assert.NotContains(t, readStageText(frame), "1 file, +1 -1")
		assert.Contains(t, readSidebarProse(frame), "1 file, +1 -1")
	})

	t.Run("the change comes before the pull request and the files", func(t *testing.T) {
		harness := openReviewAt(t, newDescribedReviewServer(t, "Why it changed."), panelHarnessParams{})

		sidebar := readSidebarProse(harness.readFrame())
		scope, pullRequest, files := strings.Index(sidebar, "all changes"), strings.Index(sidebar, "#7"), strings.Index(sidebar, "▸ greet.go")
		require.NotEqual(t, -1, scope)
		assert.Less(t, scope, pullRequest)
		assert.Less(t, pullRequest, files)
	})

	t.Run("an unsent comment names the key that sends it", func(t *testing.T) {
		server := newReviewServer(t)
		server.threads = []*agentv1.ReviewThread{newThreadOnLastLine(newUnsentComment())}
		harness := openReviewAt(t, server, panelHarnessParams{})

		assert.Contains(t, readSidebarProse(harness.readFrame()), "space S sends 1 comment to the agent")
	})

	t.Run("the narrow layout puts the change at the top of the sidebar", func(t *testing.T) {
		harness := openReviewAt(t, newReviewServer(t), panelHarnessParams{width: 45})

		frame := harness.readFrame()
		band, scope, files := strings.Index(frame, "Review · auth"), strings.Index(frame, "all changes"), strings.Index(frame, "▸ greet.go")
		require.NotEqual(t, -1, scope)
		assert.Less(t, band, scope)
		assert.Less(t, scope, files)
	})
}

// TestReviewChangeFolds covers the change block giving up rows before the pull request does.
func TestReviewChangeFolds(t *testing.T) {
	tests := []struct {
		name    string
		height  int
		threads []*agentv1.ReviewThread
		rows    []string
	}{
		{
			name:    "a tall pane gives each part a row of its own",
			height:  40,
			threads: []*agentv1.ReviewThread{newThreadOnLastLine(newUnsentComment(), newSentComment("m2"))},
			rows:    []string{"all changes", "1 file, +1 -1", "1 unsent · 1 waiting", "space S sends 1 comment to the agent"},
		},
		{
			name:    "a shorter pane joins the delivery to the stat",
			height:  24,
			threads: []*agentv1.ReviewThread{newThreadOnLastLine(newUnsentComment(), newSentComment("m2"))},
			rows:    []string{"all changes", "1 file, +1 -1 · 1 unsent · 1 waiting", "space S sends 1 comment to the agent"},
		},
		{
			name:   "a short pane joins the scope, the stat and the delivery",
			height: 16,
			rows:   []string{"all changes · 1 file, +1 -1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := newDescribedReviewServer(t, "Why it changed.\n\nline two\n\nline three\n\nline four")
			server.threads = tt.threads
			harness := openReviewAt(t, server, panelHarnessParams{height: tt.height})

			sidebar := readSidebar(harness.readFrame())
			top := slices.Index(sidebar, tt.rows[0])
			require.NotEqual(t, -1, top)
			assert.Equal(t, append(slices.Clone(tt.rows), ""), sidebar[top:min(top+len(tt.rows)+1, len(sidebar))])
			assert.Contains(t, sidebar, "#7 fix the greeting")
		})
	}
}
