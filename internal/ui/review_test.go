package ui_test

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"komrad/config"
	"komrad/internal/errui"
	"komrad/internal/git"
	"komrad/internal/gittest"
	"komrad/internal/ui"
	"komrad/internal/ui/paint"
	"komrad/lib/xptr"
	agentv1 "komrad/proto/gen/agent/v1"
	"komrad/proto/gen/agent/v1/agentv1connect"
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
	threads    []*agentv1.ReviewThread
	viewed     []*agentv1.ReviewViewedFile
	rounds     []*agentv1.ReviewRound
	reviews    []*agentv1.Review
	reviewsErr error
	roundsErr  error
	openErr    error
	holdErr    error
}

// TestReviewOpen covers which agents the review key opens a diff for and what it draws.
func TestReviewOpen(t *testing.T) {
	t.Run("an agent that has written nothing is refused", func(t *testing.T) {
		repo := gittest.NewRepo(t.TempDir())
		repo.Wrote(gittest.WroteParams{Tree: "tree-of-the-base"})

		harness := openReview(t, newReviewServerOver(repo))

		frame := harness.readFrame()
		assert.False(t, harness.panel.IsZoomed())
		assert.Contains(t, frame, "Nothing to review in auth")
		assert.Contains(t, readOpenMessages(harness), "no change against the base branch")
	})

	t.Run("an agent whose directory has gone is not opened", func(t *testing.T) {
		server := newReviewServer(t)
		server.agent.WorktreeRemoved = true

		harness := openReview(t, server)

		assert.False(t, harness.panel.IsZoomed())
		assert.Empty(t, harness.readSent(agentv1connect.AgentServiceOpenReviewRoundProcedure))
	})

	t.Run("an agent part way through a rebase opens on its newest round", func(t *testing.T) {
		server := newEarlierReviewServer(t)
		server.openErr = connect.NewError(connect.CodeFailedPrecondition, errors.New("a rebase is part way through"))

		harness := openReview(t, server)

		frame := harness.readFrame()
		assert.True(t, harness.panel.IsZoomed())
		assert.Contains(t, readSidebarProse(frame), "a rebase or merge is part way through, so this is the work as it stood before it")
		assert.Contains(t, readStageText(frame), "1 file")
	})

	t.Run("a diff git cannot read is refused with the reason", func(t *testing.T) {
		repo := gittest.NewRepo(t.TempDir())
		repo.RootErr = gittest.ErrNoGit

		harness := openReview(t, newReviewServerOver(repo))

		frame := harness.readFrame()
		assert.False(t, harness.panel.IsZoomed())
		assert.Contains(t, frame, "Could not read the diff of")
		assert.Contains(t, readOpenMessages(harness), gittest.ErrNoGit.Error())
	})

	t.Run("the diff is drawn beside the sidebar over the whole window", func(t *testing.T) {
		harness := openReview(t, newReviewServer(t))

		frame := harness.readFrame()
		assert.True(t, harness.panel.IsZoomed())
		assert.Contains(t, readStage(frame), "greet.go")
		assert.Contains(t, readStage(frame), "Review · auth")
		assert.Contains(t, readStage(frame), "1 file, +1 -1")
		assert.Len(t, splitLines(frame), reviewHeight)
	})

	t.Run("the frame draws while the diff loads", func(t *testing.T) {
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
		assert.Contains(t, frame, "comment on this line")
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

		wheelOver(harness, defaultHarnessWidth-1, false)

		assert.Equal(t, "greet.go:4 (removed)", readReviewCursor(harness.readFrame()))
	})

	t.Run("the wheel over the diff leaves a comment being written on its line", func(t *testing.T) {
		harness := openReview(t, newReviewServer(t))
		harness.press("c")
		typeText(harness, "still here")

		wheelOver(harness, defaultHarnessWidth-1, false)
		harness.press("ctrl+s")

		harness.assertSentTo(agentv1connect.AgentServiceCreateReviewThreadProcedure, &agentv1.CreateReviewThreadRequest{
			AgentId:     "a1",
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

		harness.press("ctrl+d")
		assert.Equal(t, "greet.go:5", readReviewCursor(harness.readFrame()))

		harness.press("ctrl+u")
		assert.Contains(t, readSidebar(harness.readFrame()), "the change as a whole")
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

		harness.press("?")
		assert.Contains(t, readLegend(harness.readFrame()), "i       comment on this line")

		harness.press("c")
		assert.NotContains(t, harness.readFrame(), "┃ Comment")

		harness.press("i")
		assert.Contains(t, harness.readFrame(), "┃ Comment")
	})
}

// TestReviewThreads covers writing, reading and settling the conversations pinned to the diff.
func TestReviewThreads(t *testing.T) {
	t.Run("the compose key opens a thread on the line under the cursor", func(t *testing.T) {
		harness := openReview(t, newReviewServer(t))

		harness.press("c")
		typeText(harness, "this should say hello")
		harness.press("ctrl+s")

		harness.assertSentTo(agentv1connect.AgentServiceCreateReviewThreadProcedure, &agentv1.CreateReviewThreadRequest{
			AgentId:     "a1",
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
		assert.NotContains(t, harness.readFrame(), "┃ Comment")
	})

	t.Run("a comment the service refuses stays in the composer", func(t *testing.T) {
		harness := openReview(t, newReviewServer(t))
		harness.refuse(agentv1connect.AgentServiceCreateReviewThreadProcedure,
			connect.NewError(connect.CodeFailedPrecondition, errors.New("not now")))

		harness.press("c")
		typeText(harness, "this should say hello")
		harness.press("ctrl+s")

		frame := harness.readFrame()
		assert.Contains(t, frame, "┃ Comment")
		assert.Contains(t, frame, "this should say hello")
	})

	t.Run("a refused comment is sent again on the line it was written on", func(t *testing.T) {
		harness := openReview(t, newReviewServer(t))
		harness.refuse(agentv1connect.AgentServiceCreateReviewThreadProcedure,
			connect.NewError(connect.CodeFailedPrecondition, errors.New("not now")))
		harness.hold(agentv1connect.AgentServiceCreateReviewThreadProcedure)

		harness.press("c")
		typeText(harness, "this should say hello")
		harness.press("ctrl+s")
		harness.press("j", "j", "j")
		harness.release(agentv1connect.AgentServiceCreateReviewThreadProcedure)
		harness.press("ctrl+s")

		sent := harness.readSent(agentv1connect.AgentServiceCreateReviewThreadProcedure)
		require.Len(t, sent, 2)
		first, _ := sent[0].(*agentv1.CreateReviewThreadRequest)
		again, _ := sent[1].(*agentv1.CreateReviewThreadRequest)
		assert.Equal(t, first.GetNewStart(), again.GetNewStart())
		assert.Equal(t, first.GetQuotedLines(), again.GetQuotedLines())
		assert.Equal(t, "this should say hello", again.GetBody())
	})

	t.Run("the thread under the cursor is drawn in the sidebar", func(t *testing.T) {
		server := newReviewServer(t)
		server.threads = []*agentv1.ReviewThread{newThreadOnFirstLine(newHumanMessage("m1", "wrong greeting"))}

		harness := openReview(t, server)

		sidebar := readSidebarProse(harness.readFrame())
		assert.Contains(t, sidebar, "you wrong greeting")
	})

	t.Run("the compose key on a line with a thread replies in it", func(t *testing.T) {
		server := newReviewServer(t)
		server.threads = []*agentv1.ReviewThread{newThreadOnFirstLine(newAgentMessage("m1", "renamed it, is that what you meant?"))}
		harness := openReviewAt(t, server, panelHarnessParams{height: 40})

		harness.press("?")
		require.Contains(t, readLegend(harness.readFrame()), "reply in this thread")

		harness.press("c")
		typeText(harness, "yes, that is it")
		harness.press("ctrl+s")

		harness.assertSentTo(agentv1connect.AgentServiceAddReviewMessageProcedure, &agentv1.AddReviewMessageRequest{
			ThreadId: "t1",
			Body:     "yes, that is it",
		})
		assert.Empty(t, harness.readSent(agentv1connect.AgentServiceCreateReviewThreadProcedure))
	})

	t.Run("the thread key reaches the only thread and stays on it", func(t *testing.T) {
		server := newReviewServer(t)
		server.threads = []*agentv1.ReviewThread{newThreadOnLastLine("t1")}
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
		assert.Equal(t, "gone.go:12", readReviewCursor(frame))
		assert.Contains(t, readSidebarProse(frame), "still worth reading")
		assert.Contains(t, readSidebarProse(frame), "+what this used to say")
	})

	t.Run("a thread whose file left the diff takes a reply", func(t *testing.T) {
		server := newReviewServer(t)
		server.threads = []*agentv1.ReviewThread{newAdriftThread("t1", newAgentMessage("m1", "why did this go?"))}
		harness := openReview(t, server)

		harness.press("t", "c")
		typeText(harness, "because")
		harness.press("ctrl+s")

		harness.assertSentTo(agentv1connect.AgentServiceAddReviewMessageProcedure, &agentv1.AddReviewMessageRequest{
			ThreadId: "t1",
			Body:     "because",
		})
	})

	t.Run("the settle key settles a thread and presses again to reopen it", func(t *testing.T) {
		server := newReviewServer(t)
		server.threads = []*agentv1.ReviewThread{newThreadOnFirstLine(newAgentMessage("m1", "why did this change?"))}
		harness := openReview(t, server)

		harness.press("r")
		harness.watch(server.buildSnapshot())
		assert.Contains(t, findLine(readStage(harness.readFrame()), "package greet"), "✓")

		harness.press("r")
		harness.watch(server.buildSnapshot())
		assert.NotContains(t, findLine(readStage(harness.readFrame()), "package greet"), "✓")

		harness.assertSentTo(agentv1connect.AgentServiceSetReviewThreadResolvedProcedure,
			&agentv1.SetReviewThreadResolvedRequest{Id: "t1", Resolved: true},
			&agentv1.SetReviewThreadResolvedRequest{Id: "t1", Resolved: false},
		)
	})

	t.Run("a thread whose file left the diff settles like any other", func(t *testing.T) {
		server := newReviewServer(t)
		server.threads = []*agentv1.ReviewThread{newAdriftThread("t1", newAgentMessage("m1", "why did this go?"))}
		harness := openReview(t, server)

		harness.press("t", "r")
		harness.watch(server.buildSnapshot())

		assert.Contains(t, findLine(readStage(harness.readFrame()), "gone.go:12"), "✓")
		harness.assertSentTo(agentv1connect.AgentServiceSetReviewThreadResolvedProcedure,
			&agentv1.SetReviewThreadResolvedRequest{Id: "t1", Resolved: true})
	})

	t.Run("a thread deleted from under the cursor leaves it on a row that exists", func(t *testing.T) {
		server := newReviewServer(t)
		server.threads = []*agentv1.ReviewThread{newAdriftThread("t1", newAgentMessage("m1", "why did this go?"))}
		harness := openReview(t, server)
		harness.press("t")

		server.threads = nil
		harness.watch(server.buildSnapshot())

		assert.Equal(t, "greet.go:5", readReviewCursor(harness.readFrame()))
	})

	t.Run("the composer takes every key but escape as a character", func(t *testing.T) {
		harness := openReview(t, newReviewServer(t))

		harness.press("c", "?", "l", "n")
		frame := harness.readFrame()
		assert.Contains(t, readSidebar(frame), "┃ ?ln")
		assert.Contains(t, frame, "c comment on this line")

		harness.press("esc")
		assert.NotContains(t, harness.readFrame(), "┃ Comment")
	})
}

// TestReviewThreadFocus covers the thread taking the keys to read a conversation taller than the sidebar.
func TestReviewThreadFocus(t *testing.T) {
	t.Run("the thread scrolls back to its start and escape hands the keys back", func(t *testing.T) {
		harness := openLongThread(t)
		require.NotContains(t, readSidebarProse(harness.readFrame()), "note 0")

		harness.press("ctrl+a")
		for range 20 {
			harness.press("k")
		}

		frame := harness.readFrame()
		assert.Contains(t, readSidebarProse(frame), "note 0")
		assert.NotContains(t, readSidebarProse(frame), "note 5")
		assert.Equal(t, "greet.go:1", readReviewCursor(frame))

		harness.press("esc")

		frame = harness.readFrame()
		assert.True(t, harness.panel.IsZoomed())
		assert.NotContains(t, readSidebarProse(frame), "note 0")
	})

	t.Run("the scroll keys move the thread while it holds the keys", func(t *testing.T) {
		harness := openLongThread(t)
		harness.press("ctrl+a")

		harness.press("ctrl+p")
		assert.NotContains(t, readSidebarProse(harness.readFrame()), "note 5")
		assert.Equal(t, "greet.go:1", readReviewCursor(harness.readFrame()))

		harness.press("ctrl+n")
		assert.Contains(t, readSidebarProse(harness.readFrame()), "note 5")
	})

	t.Run("the wheel over the sidebar scrolls the thread and leaves the diff", func(t *testing.T) {
		harness := openLongThread(t)

		wheelOver(harness, 1, true)

		frame := harness.readFrame()
		assert.NotContains(t, readSidebarProse(frame), "note 5")
		assert.Equal(t, "greet.go:1", readReviewCursor(frame))
	})

	t.Run("moving the diff cursor drops the scroll and the keys", func(t *testing.T) {
		harness := openLongThread(t)
		harness.press("ctrl+a", "k")
		require.Contains(t, readLegend(harness.readFrame()), "back to the diff")

		harness.press("ctrl+d")

		frame := harness.readFrame()
		assert.Equal(t, "greet.go:5", readReviewCursor(frame))
		assert.NotContains(t, readLegend(frame), "back to the diff")
	})

	t.Run("a line with no thread is passed over for the record", func(t *testing.T) {
		harness := openReview(t, newReviewServer(t))

		harness.press("ctrl+a")

		assert.NotEmpty(t, readRecordCursor(harness.readFrame()))
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

			harness.press("ctrl+a", "k", "k", "k", "k", "k")
			assert.Len(t, splitLines(harness.readFrame()), height)

			harness.press("esc", "c")
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
		assert.Contains(t, readSidebarProse(harness.readFrame()), "nothing to search for yet")

		harness.press("/")
		typeText(harness, "wallaby")
		harness.press("enter", "n")

		frame := harness.readFrame()
		assert.Contains(t, readStage(frame), "/ wallaby")
		assert.Contains(t, readSidebarProse(frame), "no matches")
	})

	t.Run("the prompt takes every key typed", func(t *testing.T) {
		harness := openReview(t, newReviewServer(t))

		harness.press("/", "g", "o", "?")

		frame := harness.readFrame()
		assert.Contains(t, readStage(frame), "/ go?▏")
		assert.Contains(t, frame, "c comment on this line")
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
		harness.watch(buildSnapshot(server.workflow, server.agent, second))
		harness.press("j")
		require.Contains(t, readHoveredRow(harness.readFrame()), "auth")

		harness.press("space", "r", "/")
		typeText(harness, "goodbye")
		harness.press("enter", "esc")

		frame := harness.readFrame()
		assert.Len(t, readListRows(frame), 3)
		assert.Contains(t, readHoveredRow(frame), "auth")
	})
}

// TestReviewViewed covers folding a file away once it has been read.
func TestReviewViewed(t *testing.T) {
	t.Run("the viewed key folds the file to its header and again opens it", func(t *testing.T) {
		harness := openReview(t, newReviewServer(t))

		harness.press("v")

		frame := harness.readFrame()
		assert.NotContains(t, frame, "func Hello() string {")
		assert.Contains(t, readStageText(frame), "greet.go  ✓ viewed")

		harness.press("v")
		assert.Contains(t, harness.readFrame(), "func Hello() string {")

		sent := harness.readSent(agentv1connect.AgentServiceSetReviewFileViewedProcedure)
		require.Len(t, sent, 2)
		marked, _ := sent[0].(*agentv1.SetReviewFileViewedRequest)
		assert.Equal(t, "a1", marked.GetAgentId())
		assert.Equal(t, gittest.File, marked.GetFilePath())
		assert.True(t, marked.GetViewed())
		assert.NotEmpty(t, marked.GetFingerprint())
		unmarked, _ := sent[1].(*agentv1.SetReviewFileViewedRequest)
		assert.False(t, unmarked.GetViewed())
	})

	t.Run("a mark holds while the tree is unmoved and a new round has read nothing", func(t *testing.T) {
		server := newReviewServer(t)
		harness := openReview(t, server)

		harness.press("v", "esc", "space", "r")
		require.NotContains(t, harness.readFrame(), "func Hello() string {")

		server.repo.Wrote(gittest.WroteParams{
			Tree:  "tree-written-again",
			Whole: gittest.PatchWrittenOver,
			Sources: map[string]string{
				gittest.File: "package greet\n\nfunc Hello() string {\n\treturn \"written by the agent\"\n}\n",
			},
		})
		harness.press("esc", "space", "r")

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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := newReviewServer(t)
			if tt.messages != nil {
				server.threads = []*agentv1.ReviewThread{newThreadOnLastLine("t1", tt.messages...)}
			}

			harness := openReview(t, server)

			stage := strings.Join(readStage(harness.readFrame()), "\n")
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := newReviewServer(t)
			server.threads = []*agentv1.ReviewThread{newThreadOnFirstLine(tt.message)}

			harness := openReview(t, server)

			sidebar := readSidebarProse(harness.readFrame())
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			thread := newThreadOnFirstLine(tt.message)
			thread.UnreadByHuman = tt.unread
			thread.ResolvedAt = tt.resolved
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
	t.Run("the panel sends with no verdict and says how many comments went", func(t *testing.T) {
		harness := openReview(t, newReviewServer(t))

		harness.press("ctrl+s")
		assert.Empty(t, harness.readSent(agentv1connect.AgentServiceReleaseReviewProcedure))
		harness.press("ctrl+s")

		harness.assertSentTo(agentv1connect.AgentServiceReleaseReviewProcedure, &agentv1.ReleaseReviewRequest{
			AgentId: "a1",
		})
		assert.Contains(t, readSidebarProse(harness.readFrame()), "0 comments sent")
	})

	t.Run("the panel carries a word about the whole change", func(t *testing.T) {
		harness := openReview(t, newReviewServer(t))

		harness.press("ctrl+s")
		typeText(harness, "the shape is right")
		harness.press("ctrl+s")

		harness.assertSentTo(agentv1connect.AgentServiceReleaseReviewProcedure, &agentv1.ReleaseReviewRequest{
			AgentId: "a1",
			Body:    "the shape is right",
		})
	})

	t.Run("escape puts the panel away and sends nothing", func(t *testing.T) {
		harness := openReview(t, newReviewServer(t))

		harness.press("ctrl+s", "esc", "ctrl+s")

		assert.Empty(t, harness.readSent(agentv1connect.AgentServiceReleaseReviewProcedure))
	})
}

// TestReviewWatch covers the conversation following the watch without a keypress.
func TestReviewWatch(t *testing.T) {
	t.Run("an answer the watch puts is drawn", func(t *testing.T) {
		harness := openReview(t, newReviewServer(t))

		thread := newThreadOnFirstLine(newAgentMessage("m1", "fixed it"))
		thread.AgentId = "a1"
		harness.watch(buildPuts(thread))

		assert.Contains(t, readSidebarProse(harness.readFrame()), "fixed it")
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
		require.Contains(t, readSidebarProse(harness.readFrame()), "waiting")

		server.reviews[0].DeliveredAt = "2026-09-02T10:05:00Z"
		harness.watch(server.buildSnapshot())

		assert.Contains(t, readSidebarProse(harness.readFrame()), "delivered")
	})

	t.Run("an answer arriving while the reader types waits for them to stop", func(t *testing.T) {
		server := newReviewServer(t)
		harness := openReview(t, server)
		harness.press("j", "c")
		typeText(harness, "half a thought")

		server.threads = []*agentv1.ReviewThread{newThreadOnFirstLine(newAgentMessage("m1", "fixed it"))}
		harness.watch(server.buildSnapshot())

		frame := harness.readFrame()
		assert.Contains(t, readSidebar(frame), "┃ half a thought")
		assert.Equal(t, "greet.go:2", readReviewCursor(frame))
		assert.NotContains(t, readSidebarProse(frame), "fixed it")

		harness.press("esc", "k")
		assert.Contains(t, readSidebarProse(harness.readFrame()), "fixed it")
	})

	t.Run("an archived agent's threads stay drawn and a settle reads them back", func(t *testing.T) {
		server := newReviewServer(t)
		server.threads = []*agentv1.ReviewThread{newThreadOnFirstLine(newDeliveredComment("m1"))}
		harness := openReview(t, server)

		server.agent.ArchivedAt = xptr.Ptr("2026-09-02T11:00:00Z")
		harness.watch(buildSnapshot(server.workflow, server.agent))
		require.Contains(t, findLine(readStage(harness.readFrame()), "package greet"), "○")

		harness.press("R")
		require.Len(t, harness.readSent(agentv1connect.AgentServiceListReviewThreadsProcedure), 1)
		require.Contains(t, findLine(readStage(harness.readFrame()), "package greet"), "○")

		harness.press("r")

		assert.Contains(t, findLine(readStage(harness.readFrame()), "package greet"), "✓")
		assert.Len(t, harness.readSent(agentv1connect.AgentServiceListReviewThreadsProcedure), 2)
	})
}

// TestReviewOfAnAgentThatIsGone covers the diff of a dormant or archived agent opening read only.
func TestReviewOfAnAgentThatIsGone(t *testing.T) {
	tests := []struct {
		name  string
		leave func(*agentv1.Agent)
		want  string
	}{
		{
			name:  "a dormant agent",
			leave: func(agent *agentv1.Agent) { agent.Alive = xptr.Ptr(false) },
			want:  "this agent is dormant",
		},
		{
			name:  "an archived agent",
			leave: func(agent *agentv1.Agent) { agent.ArchivedAt = xptr.Ptr("2026-09-02T11:00:00Z") },
			want:  "this agent is archived",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := newReviewServer(t)
			server.threads = []*agentv1.ReviewThread{newThreadOnFirstLine(newUnsentComment())}
			harness := openReviewAt(t, server, panelHarnessParams{height: 50})
			tt.leave(server.agent)
			harness.watch(server.buildSnapshot())

			frame := harness.readFrame()
			assert.Contains(t, readStageText(frame), tt.want)
			assert.NotContains(t, frame, "reply in this thread")

			harness.press("?")
			legend := readLegend(harness.readFrame())
			assert.NotContains(t, legend, "send to the agent")
			assert.Contains(t, legend, "settle the thread")
			harness.press("?")

			harness.press("c")
			assert.NotContains(t, harness.readFrame(), "┃ Comment")
			harness.press("ctrl+s", "ctrl+s")
			assert.Empty(t, harness.readSent(agentv1connect.AgentServiceReleaseReviewProcedure))
			assert.Empty(t, harness.readSent(agentv1connect.AgentServiceAddReviewMessageProcedure))

			harness.press("r", "v")
			harness.assertSentTo(agentv1connect.AgentServiceSetReviewThreadResolvedProcedure,
				&agentv1.SetReviewThreadResolvedRequest{Id: "t1", Resolved: true})
			assert.Len(t, harness.readSent(agentv1connect.AgentServiceSetReviewFileViewedProcedure), 1)
		})
	}
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

		server.threads = []*agentv1.ReviewThread{newThreadOnLastLine("t1", newAgentMessage("m1", "done"))}
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

		harness.press("R")
		assert.Equal(t, header, readReviewCursor(harness.readFrame()))
		assert.Contains(t, readStageText(harness.readFrame()), "1 of 1")
	})

	t.Run("the reload key keeps the search and its matches", func(t *testing.T) {
		harness := openReview(t, newReviewServer(t))
		harness.press("/")
		typeText(harness, "return")
		harness.press("enter", "R")

		assert.Contains(t, readStageText(harness.readFrame()), "1 of 2")
		harness.press("n")
		assert.Contains(t, readStageText(harness.readFrame()), "2 of 2")
	})

	t.Run("the reload key reads again with nothing moved", func(t *testing.T) {
		harness := openReview(t, newReviewServer(t))

		harness.press("R")

		assert.Len(t, harness.readSent(agentv1connect.AgentServiceOpenReviewRoundProcedure), 2)
		assert.True(t, harness.panel.IsZoomed())
	})

	t.Run("the reload key opens the next round where the reader stood", func(t *testing.T) {
		server := newReviewServer(t)
		harness := openReview(t, server)
		harness.press("j", "j")

		server.repo.Wrote(newRoundWrittenOver())
		harness.press("R")

		frame := harness.readFrame()
		assert.Equal(t, "greet.go:3", readReviewCursor(frame))
		assert.Contains(t, frame, "written by the agent")
	})

	t.Run("a reload finding nothing written backs out to the listing", func(t *testing.T) {
		server := newReviewServer(t)
		harness := openReview(t, server)

		server.repo.Wrote(gittest.WroteParams{Tree: "tree-of-the-base"})
		harness.press("R")

		assert.False(t, harness.panel.IsZoomed())
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
		assert.False(t, harness.panel.IsZoomed())
		assert.Contains(t, frame, "excluded from the diff")
		assert.NotContains(t, frame, "Nothing to review")
	})

	t.Run("a move the watch reports of an idle agent is read again", func(t *testing.T) {
		server := newReviewServer(t)
		harness := openReview(t, server)

		server.repo.Wrote(newRoundWrittenOver())
		server.agent.MovedSinceRound = gittest.SnapshotSHA
		harness.watch(server.buildSnapshot())

		assert.Contains(t, harness.readFrame(), "written by the agent")
		assert.Len(t, harness.readSent(agentv1connect.AgentServiceOpenReviewRoundProcedure), 2)

		harness.watch(server.buildSnapshot())
		assert.Len(t, harness.readSent(agentv1connect.AgentServiceOpenReviewRoundProcedure), 2)
	})

	t.Run("a move from a round other than the one on screen is not followed", func(t *testing.T) {
		server := newReviewServer(t)
		harness := openReview(t, server)

		server.agent.MovedSinceRound = "snapshot-of-another-round"
		harness.watch(server.buildSnapshot())

		assert.Len(t, harness.readSent(agentv1connect.AgentServiceOpenReviewRoundProcedure), 1)
	})

	t.Run("a move waits while a search is typed and is read once it is put away", func(t *testing.T) {
		server := newReviewServer(t)
		harness := openReview(t, server)
		harness.press("/", "a")

		server.repo.Wrote(newRoundWrittenOver())
		server.agent.MovedSinceRound = gittest.SnapshotSHA
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
		server.agent.MovedSinceRound = gittest.SnapshotSHA
		harness.watch(server.buildSnapshot())

		frame := harness.readFrame()
		assert.NotContains(t, frame, "written by the agent")
		assert.Contains(t, readStageText(frame), "the worktree has moved · R to catch up")
	})
}

// TestReviewYours covers the list of comments the reader started, open and resolved.
func TestReviewYours(t *testing.T) {
	t.Run("the list names each comment and what it is waiting for", func(t *testing.T) {
		answered := newYourThread("b.txt", 5)
		answered.Messages = append(answered.Messages, newAgentMessage("m9", "rewrote the header"))
		reopened := newYourThread("c.txt", 3)
		reopened.Messages = append(reopened.Messages,
			newAgentMessage("m10", "renamed it"),
			newHumanMessage("m11", "the caller still uses the old name"),
		)
		harness := openReview(t, newYoursServer(t, newYourThread("a.txt", 2), answered, reopened))

		sidebar := readSidebarProse(harness.readFrame())
		assert.Contains(t, sidebar, "Yours 3 open, 0 resolved")
		assert.Contains(t, sidebar, "a.txt:2 no reply")
		assert.Contains(t, sidebar, "b.txt:5 answered")
		assert.Contains(t, sidebar, "c.txt:3 no reply", "the person's word after the answer waits for another")
	})

	t.Run("a resolved comment stays on the list and is counted apart", func(t *testing.T) {
		server := newYoursServer(t, newYourThread("a.txt", 2), newYourThread("b.txt", 5))
		harness := openReview(t, server)

		server.threads[0].ResolvedAt = "2026-09-13T11:00:00Z"
		harness.watch(server.buildSnapshot())

		sidebar := readSidebarProse(harness.readFrame())
		assert.Contains(t, sidebar, "Yours 1 open, 1 resolved")
		assert.Contains(t, sidebar, "a.txt:2 resolved")
		assert.Less(t, strings.Index(sidebar, "b.txt:5"), strings.Index(sidebar, "a.txt:2"))
	})

	t.Run("a comment with something unread goes first", func(t *testing.T) {
		unread := newYourThread("b.txt", 5)
		unread.UnreadByHuman = true
		harness := openReview(t, newYoursServer(t, newYourThread("a.txt", 2), unread))

		sidebar := readSidebarProse(harness.readFrame())
		assert.Contains(t, sidebar, "b.txt:5 unread")
		assert.Less(t, strings.Index(sidebar, "b.txt:5"), strings.Index(sidebar, "a.txt:2"))
	})

	t.Run("the list counts what it has no room to draw", func(t *testing.T) {
		threads := make([]*agentv1.ReviewThread, 0, 6)
		for i := range 6 {
			threads = append(threads, newYourThread(fmt.Sprintf("f%d.txt", i), int32(i+1)))
		}

		harness := openReview(t, newYoursServer(t, threads...))

		sidebar := readSidebarProse(harness.readFrame())
		assert.Contains(t, sidebar, "Yours 6 open, 0 resolved")
		assert.Contains(t, sidebar, "+ 3 more")
	})

	t.Run("the list leaves out the comments an agent started", func(t *testing.T) {
		reviewer := newYourThread("a.txt", 2)
		reviewer.Messages = []*agentv1.ReviewMessage{newAgentMessage("m1", "rename this")}
		harness := openReview(t, newYoursServer(t, reviewer))

		assert.NotRegexp(t, `Yours \d+ open`, readSidebarProse(harness.readFrame()))
	})

	t.Run("the list says when a comment is folded away", func(t *testing.T) {
		harness := openReview(t, newYoursServer(t, newYourThread(gittest.File, 2)))

		harness.press("v")

		assert.Contains(t, readSidebarProse(harness.readFrame()), "folded away")
	})

	t.Run("an open comment leaves the reader free to write another", func(t *testing.T) {
		harness := openReview(t, newYoursServer(t, newYourThread("a.txt", 2)))

		harness.press("c")

		assert.Contains(t, harness.readFrame(), "┃ Comment")
	})

	t.Run("an open comment leaves the editor free to open", func(t *testing.T) {
		harness := openReview(t, newYoursServer(t, newYourThread("a.txt", 2)))

		harness.press("e")

		assert.Contains(t, harness.readFrame(), "editing · greet.go")
	})

	t.Run("the compose key replies in a comment the cursor stands on", func(t *testing.T) {
		harness := openReview(t, newYoursServer(t, newYourThread(gittest.File, 1)))

		harness.press("c")
		typeText(harness, "still reads like a changelog")
		harness.press("ctrl+s")

		harness.assertSentTo(agentv1connect.AgentServiceAddReviewMessageProcedure, &agentv1.AddReviewMessageRequest{
			ThreadId: "greet.go:1",
			Body:     "still reads like a changelog",
		})
	})

	t.Run("the open comment keys walk both ways and stop at the ends", func(t *testing.T) {
		harness := openReview(t, newYoursServer(t,
			newYourThread(gittest.File, 2),
			newYourThread(gittest.File, 3),
			newYourThread(gittest.File, 5),
		))

		harness.press("u")
		assert.Equal(t, "greet.go:2  1 of 3", readReviewCursor(harness.readFrame()))
		harness.press("U")
		assert.Equal(t, "greet.go:2  1 of 3", readReviewCursor(harness.readFrame()))

		harness.press("u", "u")
		assert.Equal(t, "greet.go:5  3 of 3", readReviewCursor(harness.readFrame()))
		harness.press("U")
		assert.Equal(t, "greet.go:3  2 of 3", readReviewCursor(harness.readFrame()))

		harness.press("u", "u")
		frame := harness.readFrame()
		assert.Equal(t, "greet.go:5  3 of 3", readReviewCursor(frame))
		assert.Contains(t, readSidebarProse(frame), "comment")
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
				threads: []*agentv1.ReviewThread{settled, newYourThread(gittest.File, 3)},
				keys:    []string{"u", "u"},
				want:    "only comment left open",
			},
			{
				name:    "the only one left is folded away",
				threads: []*agentv1.ReviewThread{newYourThread(gittest.File, 3)},
				keys:    []string{"v", "u"},
				want:    "folded away",
				absent:  "settled",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				harness := openReview(t, newYoursServer(t, tt.threads...))

				harness.press(tt.keys...)

				sidebar := readSidebarProse(harness.readFrame())
				assert.Contains(t, sidebar, tt.want)
				if tt.absent != "" {
					assert.NotContains(t, sidebar, tt.absent)
				}
			})
		}
	})

	t.Run("the legend follows the keys onto the thread and back", func(t *testing.T) {
		harness := openReviewAt(t, newYoursServer(t, newYourThread(gittest.File, 2)), panelHarnessParams{height: 50})
		harness.press("?", "u")
		diff := readLegend(harness.readFrame())
		require.NotContains(t, diff, "scroll the thread")

		harness.press("ctrl+a")
		thread := readLegend(harness.readFrame())
		assert.Contains(t, thread, "scroll the thread")
		assert.Contains(t, thread, "back to the diff")

		harness.press("ctrl+q")
		assert.Equal(t, diff, readLegend(harness.readFrame()))
	})
}

// TestReviewRecord covers the agent's record drawn beside the diff.
func TestReviewRecord(t *testing.T) {
	t.Run("the record names the agent and the two commits the diff is between", func(t *testing.T) {
		harness := openReviewAt(t, newReviewServer(t), panelHarnessParams{height: 50})
		harness.press("ctrl+q")

		frame := harness.readFrame()
		assert.Equal(t, "auth", readRecordField(frame, "name"))
		assert.Equal(t, gittest.BaseSHA[:12], readRecordField(frame, "base"))
		assert.Equal(t, gittest.SnapshotSHA[:12], readRecordField(frame, "snapshot"))
		assert.Empty(t, readRecordField(frame, "round"))
	})

	t.Run("the focus key hands the keys to the record and back", func(t *testing.T) {
		harness := openReviewAt(t, newReviewServer(t), panelHarnessParams{height: 50})
		respawns, sentKeys := harness.panel.CountRespawns(), harness.panel.CountSentKeys()

		harness.press("ctrl+q", "j")
		frame := harness.readFrame()
		assert.Equal(t, respawns, harness.panel.CountRespawns())
		assert.Equal(t, sentKeys, harness.panel.CountSentKeys())
		assert.Equal(t, "greet.go:1", readReviewCursor(frame))
		assert.True(t, strings.HasPrefix(readRecordCursor(frame), "agent"))

		harness.press("ctrl+a", "j")
		assert.Equal(t, "greet.go:2", readReviewCursor(harness.readFrame()))
	})

	t.Run("the record says nothing of an agent that is gone", func(t *testing.T) {
		server := newReviewServer(t)
		harness := openReview(t, server)

		server.agent.ArchivedAt = xptr.Ptr("2026-09-02T11:00:00Z")
		harness.watch(server.buildSnapshot())

		assert.NotContains(t, harness.readFrame(), "agent_id")
	})

	t.Run("the record takes the keys only at a height it is drawn at", func(t *testing.T) {
		tests := []struct {
			name     string
			query    string
			expanded bool
		}{
			{
				name: "a bare diff",
			},
			{
				name:     "an expanded legend",
				expanded: true,
			},
			{
				name:  "a standing search",
				query: "hello",
			},
			{
				name:     "a standing search under an expanded legend",
				query:    "hello",
				expanded: true,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				harness := openReview(t, newReviewServer(t))
				if tt.query != "" {
					harness.press("/")
					typeText(harness, tt.query)
					harness.press("enter")
				}
				if tt.expanded {
					harness.press("?")
				}

				taken := 0
				for height := range 60 {
					harness.send(tea.WindowSizeMsg{Width: reviewWidth, Height: height})
					harness.press("ctrl+q")
					if readRecordCursor(harness.readFrame()) == "" {
						harness.press("j")
						assert.NotEqual(t, "greet.go:1", readReviewCursor(harness.readFrame()),
							"the record took the keys at height %d without being drawn", height)
						harness.press("g", "g")

						continue
					}

					taken++
					harness.press("ctrl+a")
				}

				assert.Positive(t, taken)
			})
		}
	})

	t.Run("a shrink under a focused record gives the keys back to the diff", func(t *testing.T) {
		harness := openReview(t, newReviewServer(t))
		harness.press("ctrl+q")
		require.NotEmpty(t, readRecordCursor(harness.readFrame()))

		harness.press("?")
		harness.send(tea.WindowSizeMsg{Width: reviewWidth, Height: 12})
		require.Empty(t, readRecordCursor(harness.readFrame()))
		harness.press("j")

		assert.Equal(t, "greet.go:2", readReviewCursor(harness.readFrame()))
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

	t.Run("the switcher and the trail give way before the head does", func(t *testing.T) {
		harness := openReviewAt(t, newReviewServer(t), panelHarnessParams{height: 22})
		require.Contains(t, splitLines(harness.readFrame())[0], "komrad")

		harness.press("?")

		assert.NotContains(t, harness.readFrame(), "komrad")
		assert.Equal(t, "greet.go:1", readReviewCursor(harness.readFrame()), "the head is whole")
	})

	t.Run("a window too narrow for both keeps the sidebar and drops the diff", func(t *testing.T) {
		harness := openReviewAt(t, newReviewServer(t), panelHarnessParams{width: 120})
		assert.Contains(t, harness.readFrame(), "package greet")

		harness.send(tea.WindowSizeMsg{Width: 45, Height: reviewHeight})

		frame := harness.readFrame()
		assert.NotContains(t, frame, "package greet")
		assert.Contains(t, frame, "greet.go:1")
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

// TestReviewExpand covers drawing a file whole rather than as its hunks.
func TestReviewExpand(t *testing.T) {
	t.Run("the expand key draws the lines the patch left out and gives the hunks back", func(t *testing.T) {
		harness, _ := openTallReview(t)
		before := harness.readFrame()
		require.Contains(t, before, "func Two()")
		require.Contains(t, before, "@@")
		require.NotContains(t, before, "func Four()")

		harness.press("E")
		expanded := harness.readFrame()
		assert.Contains(t, expanded, "func Four()")
		assert.Contains(t, expanded, `import "fmt"`)
		assert.NotContains(t, expanded, "@@")

		harness.press("E")
		tight := harness.readFrame()
		assert.NotContains(t, tight, "func Four()")
		assert.Contains(t, tight, "func Two()")
	})

	t.Run("the cursor stays on the line it stood on", func(t *testing.T) {
		harness, _ := openTallReview(t)
		harness.press("j", "j")
		before := readReviewCursor(harness.readFrame())

		harness.press("E")

		assert.Equal(t, before, readReviewCursor(harness.readFrame()))
	})

	t.Run("a reload keeps the file drawn whole", func(t *testing.T) {
		harness, _ := openTallReview(t)

		harness.press("E", "R")

		assert.Contains(t, harness.readFrame(), "func Four()")
	})

	t.Run("a file with nothing more to draw refuses and says so", func(t *testing.T) {
		harness := openReview(t, newReviewServer(t))

		harness.press("E")

		assert.Contains(t, readSidebarProse(harness.readFrame()), "no more of that file")
	})

	t.Run("a folded file refuses and says so", func(t *testing.T) {
		harness, _ := openTallReview(t)

		harness.press("v", "E")

		frame := harness.readFrame()
		assert.NotContains(t, frame, "func Four()")
		assert.Contains(t, readSidebarProse(frame), "folded away")
	})

	t.Run("a line only the expansion draws takes a comment", func(t *testing.T) {
		harness, _ := openTallReview(t)

		harness.press("E", "G", "c")
		require.Equal(t, "tall.go:13", readReviewCursor(harness.readFrame()))
		typeText(harness, "unchanged and wrong")
		harness.press("ctrl+s")

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

		harness.press("E")
		harness.watch(server.buildSnapshot())
		harness.press("E")

		frame := harness.readFrame()
		assert.Contains(t, frame, "outside what is on screen")
		assert.NotContains(t, frame, "has changed")
	})

	t.Run("a file folded while drawn whole stays folded after a reload", func(t *testing.T) {
		harness, _ := openTallReview(t)

		harness.press("E", "v", "R")

		assert.Contains(t, readStageText(harness.readFrame()), "tall.go  ✓ viewed")
	})
}

// TestReviewRounds covers walking the rounds of a review with the base keys.
func TestReviewRounds(t *testing.T) {
	t.Run("the base key narrows to the work since the last review and forward widens it back", func(t *testing.T) {
		harness := openReview(t, newEarlierReviewServer(t))
		frame := harness.readFrame()
		require.Contains(t, readStageText(frame), "2 files, +4 -1")
		require.Contains(t, readSidebarProse(frame), "1 file changed since you last reviewed")

		harness.press("<")
		frame = harness.readFrame()
		assert.Contains(t, readStageText(frame), "chapter 2 of 2")
		assert.Contains(t, readStageText(frame), "1 file, +3 -0")
		assert.NotRegexp(t, `\d+/\d+ viewed`, frame)
		assert.NotContains(t, readSidebarProse(frame), "since you last reviewed")

		harness.press(">")
		frame = harness.readFrame()
		assert.Contains(t, readStageText(frame), "all changes")
		assert.Contains(t, readStageText(frame), "2 files, +4 -1")
	})

	t.Run("one load reads the reviews and the rounds once", func(t *testing.T) {
		harness := openReview(t, newEarlierReviewServer(t))

		assert.Len(t, harness.readSent(agentv1connect.AgentServiceListReviewsProcedure), 1)
		assert.Len(t, harness.readSent(agentv1connect.AgentServiceListReviewRoundsProcedure), 1)
	})

	t.Run("the line saying what changed survives a keypress and yields to a notice", func(t *testing.T) {
		harness := openReview(t, newEarlierReviewServer(t))

		harness.press("j")
		require.Contains(t, readSidebarProse(harness.readFrame()), "1 file changed since you last reviewed")

		harness.press("n")
		frame := readSidebarProse(harness.readFrame())
		assert.Contains(t, frame, "nothing to search for yet")
		assert.NotContains(t, frame, "since you last reviewed")

		harness.press("j")
		assert.Contains(t, readSidebarProse(harness.readFrame()), "1 file changed since you last reviewed")
	})

	t.Run("narrowing onto a round with nothing new keeps the whole change and says so", func(t *testing.T) {
		server := newEarlierReviewServer(t)
		harness := openReview(t, server)
		server.repo.Wrote(gittest.WroteParams{Tree: gittest.UnmovedTree, Whole: gittest.Patch})
		harness.press("esc", "space", "r")

		harness.press("<")

		frame := harness.readFrame()
		assert.True(t, harness.panel.IsZoomed())
		assert.Contains(t, readStageText(frame), "greet.go")
		assert.Contains(t, readSidebarProse(frame), "nothing written")
	})

	t.Run("a round measured from another base is walked but not compared", func(t *testing.T) {
		server := newEarlierReviewServer(t)
		server.rounds[0].BaseSha = "0000000000000000000000000000000000000000"
		harness := openReview(t, server)

		harness.press("<")

		assert.Contains(t, readStageText(harness.readFrame()), "not comparable, the base moved")
	})

	t.Run("the newest round takes a comment measured from the round reviewed before", func(t *testing.T) {
		harness := openReview(t, newEarlierReviewServer(t))

		harness.press("<", "c")
		typeText(harness, "this line")
		harness.press("ctrl+s")

		harness.assertSentTo(agentv1connect.AgentServiceCreateReviewThreadProcedure, &agentv1.CreateReviewThreadRequest{
			AgentId:     "a1",
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
		require.Contains(t, readStageText(harness.readFrame()), "chapter 1 of 2")

		harness.press("?")
		assert.NotContains(t, readLegend(harness.readFrame()), "send to the agent")
		harness.press("?")

		for _, key := range []string{"c", "ctrl+s"} {
			harness.press(key)

			frame := harness.readFrame()
			assert.NotContains(t, frame, "┃ Comment")
			assert.Contains(t, readSidebarProse(frame), "chapter 1 of 2 is finished")
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

		harness.press("v")
		require.Len(t, harness.readSent(agentv1connect.AgentServiceSetReviewFileViewedProcedure), 1)

		harness.press("<")
		require.Contains(t, readStageText(harness.readFrame()), "chapter 2 of 2")
		require.NotContains(t, readStageText(harness.readFrame()), "✓ viewed")
		harness.press("v")
		frame := harness.readFrame()
		assert.Contains(t, readSidebarProse(frame), "whole change")
		assert.NotContains(t, readStageText(frame), "✓ viewed")
		assert.Len(t, harness.readSent(agentv1connect.AgentServiceSetReviewFileViewedProcedure), 1)

		harness.press(">")
		assert.Contains(t, readStageText(harness.readFrame()), "greet.go  ✓ viewed")
	})

	t.Run("a comment being written goes with the round a landing load takes away", func(t *testing.T) {
		server := newEarlierReviewServer(t)
		harness := openReview(t, server)
		harness.press("<")
		harness.hold(agentv1connect.AgentServiceListReviewRoundsProcedure)
		harness.press("R")
		server.roundsErr = errors.New("the server went away")

		harness.press("c")
		typeText(harness, "hello")
		require.Contains(t, harness.readFrame(), "┃ Comment")
		harness.release(agentv1connect.AgentServiceListReviewRoundsProcedure)

		assert.NotContains(t, harness.readFrame(), "┃ Comment")
		harness.press("ctrl+s")
		assert.Empty(t, harness.readSent(agentv1connect.AgentServiceCreateReviewThreadProcedure))
	})

	t.Run("a round listing that fails takes the reader back to the whole change", func(t *testing.T) {
		server := newEarlierReviewServer(t)
		harness := openReview(t, server)
		harness.press("<")
		require.Contains(t, readStageText(harness.readFrame()), "chapter 2 of 2")

		server.roundsErr = errors.New("the server went away")
		harness.press("R")

		frame := readStageText(harness.readFrame())
		assert.Contains(t, frame, "all changes")
		assert.Contains(t, frame, "2 files, +4 -1")
	})

	t.Run("a chapter splits the replies written after the next round from the ones before", func(t *testing.T) {
		harness := openReviewAt(t, newLaterRepliesServer(t), panelHarnessParams{height: 50})

		harness.press("<", "<", "t")

		frame := harness.readFrame()
		require.Contains(t, readStageText(frame), "chapter 1 of 2")
		said := readSidebarProse(frame)
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
		harness.press("esc", "space", "r")

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
		harness.press("esc", "space", "r")
		require.NotContains(t, harness.readFrame(), "written by the agent")

		harness.press("R")

		assert.Contains(t, harness.readFrame(), "written by the agent")
	})

	t.Run("the reviews failing to read fails the load rather than moving the pin", func(t *testing.T) {
		server := newReviewServer(t)
		server.reviewsErr = errors.New("the server went away")

		harness := openReview(t, server)

		assert.False(t, harness.panel.IsZoomed())
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
		assert.False(t, harness.panel.IsZoomed())

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

		frame := readStageText(harness.readFrame())
		assert.Contains(t, frame, "chapter 2 of 2, not reviewed yet, the base moved, replayed")
		assert.Contains(t, frame, "1 file, +3 -0")
		assert.NotContains(t, frame, "not comparable")
	})

	t.Run("a round the base rewrote under the agent is refused and names the paths", func(t *testing.T) {
		harness := openReview(t, newMovedBaseServer(t, &git.Replayed{
			Tree:      "a-tree-with-markers-in-it",
			Conflicts: []string{gittest.File, "second.go"},
		}))

		harness.press("<")

		assert.Contains(t, readStageText(harness.readFrame()), "chapter 2 of 2, not comparable, the base moved, conflicts in greet.go, second.go")
	})

	t.Run("a git that cannot replay leaves the refusal standing", func(t *testing.T) {
		harness := openReview(t, newMovedBaseServer(t, nil))

		harness.press("<")

		frame := readStageText(harness.readFrame())
		assert.Contains(t, frame, "chapter 2 of 2, not comparable, the base moved")
		assert.NotContains(t, frame, "rebased")
	})

	t.Run("a base the branch no longer descends from is said to be rewritten", func(t *testing.T) {
		server := newMovedBaseServer(t, &git.Replayed{Tree: replayedTree})
		server.repo.Rewritten = map[string]bool{gittest.NamePair(olderBase, gittest.BaseSHA): true}
		harness := openReview(t, server)

		harness.press("<")

		assert.Contains(t, readStageText(harness.readFrame()), "chapter 2 of 2, not reviewed yet, history was rewritten, replayed")
	})

	t.Run("the first round is measured from its own base", func(t *testing.T) {
		server := newMovedBaseServer(t, &git.Replayed{Tree: replayedTree})
		server.repo.Patches[gittest.NamePair(olderBase, gittest.SnapshotSHA)] = gittest.Patch
		server.repo.Patches[gittest.NamePair(gittest.BaseSHA, gittest.SnapshotSHA)] = gittest.PatchOfBothFiles
		harness := openReview(t, server)

		harness.press("<", "<")

		frame := readStageText(harness.readFrame())
		assert.Contains(t, frame, "chapter 1 of 2, reviewed at ")
		assert.Contains(t, frame, ", on an earlier base")
		assert.Contains(t, frame, "1 file, +1 -1")
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
	t.Run("the composer is drawn as an ordinary field", func(t *testing.T) {
		harness := openReview(t, newReviewServer(t))

		harness.press("c")

		sidebar := readSidebarProse(harness.readFrame())
		assert.Contains(t, sidebar, "┃ Comment")
		assert.Contains(t, sidebar, "opens $EDITOR")
	})

	t.Run("return makes a line rather than sending", func(t *testing.T) {
		harness := openReview(t, newReviewServer(t))

		harness.press("c", "o", "n", "e", "enter", "t", "w", "o")

		assert.Contains(t, readSidebar(harness.readFrame()), "┃ one")
		assert.Contains(t, readSidebar(harness.readFrame()), "┃ two")
		assert.Empty(t, harness.readSent(agentv1connect.AgentServiceCreateReviewThreadProcedure))
	})

	t.Run("the field takes a width it was not built at", func(t *testing.T) {
		harness := openReviewAt(t, newReviewServer(t), panelHarnessParams{width: 24})
		harness.press("c")
		built := findWidestFieldRow(harness.readFrame())

		harness.send(tea.WindowSizeMsg{Width: reviewWidth, Height: reviewHeight})

		widened := findWidestFieldRow(harness.readFrame())
		assert.Greater(t, widened, built)
		assert.LessOrEqual(t, widened, ui.SidebarCols)
	})

	t.Run("every line typed is drawn at a narrow sidebar", func(t *testing.T) {
		harness := openReviewAt(t, newReviewServer(t), panelHarnessParams{width: 22, height: 60})

		harness.press("ctrl+s")
		typeText(harness, "one")
		for _, line := range []string{"two", "three", "four"} {
			harness.press("enter")
			typeText(harness, line)
		}

		frame := harness.readFrame()
		for _, word := range []string{"one", "two", "three", "four"} {
			assert.Contains(t, frame, "┃ "+word)
		}
	})

	t.Run("a row with no line of its own refuses the key and says why", func(t *testing.T) {
		harness := openReview(t, newReviewServer(t))

		harness.press("/")
		typeText(harness, "greet.go")
		harness.press("enter", "c")

		frame := harness.readFrame()
		assert.NotContains(t, frame, "┃ Comment")
		assert.Contains(t, readSidebarProse(frame), "put the cursor on a line of the diff")
	})
}

// TestReviewHold covers the reader stopping the agent to edit its worktree.
func TestReviewHold(t *testing.T) {
	t.Run("the edit key stops the agent and opens the editor over the diff", func(t *testing.T) {
		server := newReviewServer(t)
		server.agent.State = agentv1.AgentState_AGENT_STATE_THINKING
		harness := openReview(t, server)

		harness.press("e")

		frame := harness.readFrame()
		assert.Contains(t, frame, "editing · greet.go")
		assert.NotContains(t, frame, "package greet")
		harness.assertSentTo(agentv1connect.AgentServiceHoldAgentProcedure, &agentv1.HoldAgentRequest{Id: "a1"})
	})

	t.Run("a waiting agent does not hold the edit key", func(t *testing.T) {
		server := newReviewServer(t)
		server.agent.State = agentv1.AgentState_AGENT_STATE_WAITING
		harness := openReview(t, server)

		harness.press("e")

		assert.Contains(t, harness.readFrame(), "editing · greet.go")
	})

	t.Run("an agent that will not stop opens no editor", func(t *testing.T) {
		server := newReviewServer(t)
		server.holdErr = errors.New("the agent did not stop")
		harness := openReview(t, server)

		harness.press("e")

		assert.NotContains(t, harness.readFrame(), "editing")
	})

	t.Run("an unreadable pane refuses the edit key and stops nothing", func(t *testing.T) {
		server := newReviewServer(t)
		server.agent.State = agentv1.AgentState_AGENT_STATE_UNCERTAIN
		harness := openReview(t, server)

		harness.press("e")

		assert.Contains(t, readSidebarProse(harness.readFrame()), "cannot read this agent's pane")
		assert.Empty(t, harness.readSent(agentv1connect.AgentServiceHoldAgentProcedure))

		server.repo.Wrote(newRoundWrittenOver())
		server.agent.MovedSinceRound = gittest.SnapshotSHA
		harness.watch(server.buildSnapshot())
		assert.Contains(t, harness.readFrame(), "written by the agent")
	})

	t.Run("a dormant agent is edited without stopping anything", func(t *testing.T) {
		server := newReviewServer(t)
		server.agent.Alive = xptr.Ptr(false)
		harness := openReview(t, server)

		harness.press("e")
		require.Contains(t, harness.readFrame(), "editing · greet.go")
		harness.press("e", "esc")

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
		harness.press("enter", "e")

		frame := harness.readFrame()
		assert.Contains(t, readSidebarProse(frame), "deleted")
		assert.NotContains(t, frame, "editing ·")
	})

	t.Run("the compose key leaves the agent running", func(t *testing.T) {
		server := newReviewServer(t)
		server.agent.State = agentv1.AgentState_AGENT_STATE_THINKING
		harness := openReview(t, server)

		harness.press("c")

		assert.Contains(t, harness.readFrame(), "┃ Comment")
		assert.Empty(t, harness.readSent(agentv1connect.AgentServiceHoldAgentProcedure))
	})

	t.Run("a second edit press waits for the agent the first is stopping", func(t *testing.T) {
		server := newReviewServer(t)
		server.agent.State = agentv1.AgentState_AGENT_STATE_THINKING
		harness := openReview(t, server)
		harness.hold(agentv1connect.AgentServiceHoldAgentProcedure)

		harness.press("e")
		require.Contains(t, harness.readFrame(), "editing · stopping the agent")

		harness.press("e")
		frame := harness.readFrame()
		assert.Contains(t, readSidebarProse(frame), "still stopping")
		assert.NotContains(t, frame, "editing · greet.go")

		harness.release(agentv1connect.AgentServiceHoldAgentProcedure)
		assert.Contains(t, harness.readFrame(), "editing · greet.go")
	})

	t.Run("an edit given up before the agent stops lets it go", func(t *testing.T) {
		server := newReviewServer(t)
		server.agent.State = agentv1.AgentState_AGENT_STATE_THINKING
		harness := openReview(t, server)
		harness.hold(agentv1connect.AgentServiceHoldAgentProcedure)
		harness.press("e")

		harness.press("esc")
		harness.release(agentv1connect.AgentServiceHoldAgentProcedure)

		assert.NotContains(t, harness.readFrame(), "editing ·")
		assert.Contains(t, readReleaseSaid(t, harness), "without opening it")
	})

	t.Run("the editor quitting gives the stage back and reads the diff again", func(t *testing.T) {
		harness := openReview(t, newReviewServer(t))
		harness.press("e")
		require.Contains(t, harness.readFrame(), "editing · greet.go")
		loads := len(harness.readSent(agentv1connect.AgentServiceOpenReviewRoundProcedure))

		editor := harness.panel.ReadRespawnPanes()
		require.NotEmpty(t, editor)
		harness.panel.SetPaneDead(editor[len(editor)-1])
		harness.fireTimers()

		assert.NotContains(t, harness.readFrame(), "editing ·")
		assert.Contains(t, readReleaseSaid(t, harness), "while I had your worktree open")
		assert.Len(t, harness.readSent(agentv1connect.AgentServiceOpenReviewRoundProcedure), loads+1)
	})

	t.Run("the editor opens at the repository root the paths are named from", func(t *testing.T) {
		server := newReviewServer(t)
		server.agent.Cwd = filepath.Join(server.repo.Root, "cmd", "greet")
		harness := openReviewAt(t, server, panelHarnessParams{settings: ui.PanelSettings{Editor: "vi"}})

		harness.press("e")

		require.NotEmpty(t, harness.panel.ReadRespawnDirs())
		assert.Equal(t, server.repo.Root, harness.panel.ReadRespawnDirs()[0])
		assert.Contains(t, harness.panel.ReadRespawns()[0], "'greet.go'")
	})

	t.Run("the back key closes the editor before it leaves the screen", func(t *testing.T) {
		harness := openReview(t, newReviewServer(t))

		harness.press("e", "esc")

		frame := harness.readFrame()
		assert.NotContains(t, frame, "editing ·")
		assert.Contains(t, frame, "Review · auth")
	})

	t.Run("closing the editor lets the agent go and says where to look", func(t *testing.T) {
		harness := openReview(t, newReviewServer(t))

		harness.press("e", "e")

		said := readReleaseSaid(t, harness)
		assert.Contains(t, said, "let you go")
		assert.Contains(t, said, "greet.go")
		assert.Contains(t, harness.readFrame(), "Review · auth")
	})

	t.Run("a diff that stops being readable lets the agent go with the files", func(t *testing.T) {
		server := newReviewServer(t)
		harness := openReview(t, server)
		harness.press("e")

		server.repo.RootErr = gittest.ErrNoGit
		harness.press("R")

		assert.False(t, harness.panel.IsZoomed())
		assert.Contains(t, readReleaseSaid(t, harness), "greet.go")
	})

	t.Run("a pause the reader took is theirs to let go and keeps the files", func(t *testing.T) {
		tests := []struct {
			name       string
			keys       []string
			unreadable bool
		}{
			{
				name:       "the diff becoming unreadable leaves the pause standing",
				keys:       []string{"R"},
				unreadable: true,
			},
			{
				name: "closing the editor and leaving the screen leaves the pause standing",
				keys: []string{"e", "esc"},
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				server := newReviewServer(t)
				server.agent.HeldAt = heldAt
				server.agent.State = agentv1.AgentState_AGENT_STATE_THINKING
				harness := openReview(t, server)
				harness.press("e")

				if tt.unreadable {
					server.repo.RootErr = gittest.ErrNoGit
				}
				harness.press(tt.keys...)
				assert.Empty(t, harness.readSent(agentv1connect.AgentServiceHoldAgentProcedure))
				assert.Empty(t, harness.readSent(agentv1connect.AgentServiceReleaseAgentProcedure))

				harness.press("p")
				assert.Contains(t, readReleaseSaid(t, harness), "greet.go")
			})
		}
	})

	t.Run("an unpause forgets the files it carried", func(t *testing.T) {
		tests := []struct {
			name    string
			unpause []string
		}{
			{
				name:    "one that told the agent about them",
				unpause: []string{"p"},
			},
			{
				name:    "one let go in silence",
				unpause: []string{"P", "y"},
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				harness := openPausedReview(t, newReviewServer(t))
				harness.press("e", "e", "esc")

				harness.press(tt.unpause...)
				harness.press("p")

				sent := harness.readSent(agentv1connect.AgentServiceReleaseAgentProcedure)
				require.Len(t, sent, 2)
				again, _ := sent[1].(*agentv1.ReleaseAgentRequest)
				assert.NotContains(t, again.GetSaid(), "greet.go")
			})
		}
	})

	t.Run("an unpause in silence says which files the agent was not told about", func(t *testing.T) {
		harness := openPausedReview(t, newReviewServer(t))
		harness.press("e", "e", "esc")

		harness.press("P", "y", "ctrl+g")

		prose := joinProse(splitLines(harness.readFrame()))
		assert.Contains(t, prose, "auth: let go in s")
		assert.Contains(t, prose, "tell it yourself about greet.go")
	})

	t.Run("a file opened while an unpause flew is kept for the next", func(t *testing.T) {
		server := newReviewServer(t)
		server.repo.Wrote(gittest.WroteParams{
			Tree:  secondTree,
			Whole: gittest.PatchOfBothFiles,
			Sources: map[string]string{
				gittest.File: gittest.HeadSource,
				"second.go":  "package greet\n\nfunc Second() {}\n",
			},
		})
		harness := openPausedReview(t, server)
		harness.press("e", "e", "esc")
		harness.hold(agentv1connect.AgentServiceReleaseAgentProcedure)
		harness.press("p")

		harness.press("space", "r", "/")
		typeText(harness, "Second")
		harness.press("enter", "e", "e", "esc")
		harness.release(agentv1connect.AgentServiceReleaseAgentProcedure)
		harness.press("p")

		sent := harness.readSent(agentv1connect.AgentServiceReleaseAgentProcedure)
		require.Len(t, sent, 2)
		again, _ := sent[1].(*agentv1.ReleaseAgentRequest)
		assert.Contains(t, again.GetSaid(), "second.go")
		assert.NotContains(t, again.GetSaid(), "greet.go")
	})

	t.Run("a refused release at the end of a pass keeps the files for the next", func(t *testing.T) {
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

		harness.press("e", "e")
		server.agent.HeldAt = heldAt
		harness.watch(server.buildSnapshot())
		harness.press("esc", "p")

		sent := harness.readSent(agentv1connect.AgentServiceReleaseAgentProcedure)
		require.Len(t, sent, 2)
		released, _ := sent[1].(*agentv1.ReleaseAgentRequest)
		assert.Contains(t, released.GetSaid(), "greet.go")
	})

	t.Run("closing the editor draws the reader's own edits over a pinned diff", func(t *testing.T) {
		server := newReviewServer(t)
		server.threads = []*agentv1.ReviewThread{newThreadOnFirstLine(newUnsentComment())}
		harness := openReview(t, server)

		harness.press("e")
		server.repo.Wrote(gittest.WroteParams{Tree: "tree-the-reader-edited", Whole: gittest.PatchWrittenOver})
		harness.press("e")

		assert.Contains(t, harness.readFrame(), "written by the agent")
	})

	t.Run("a diff is held while the agent writes and read again when it stops", func(t *testing.T) {
		server := newReviewServer(t)
		server.agent.State = agentv1.AgentState_AGENT_STATE_THINKING
		harness := openReview(t, server)

		server.repo.Wrote(newRoundWrittenOver())
		server.agent.MovedSinceRound = gittest.SnapshotSHA
		harness.watch(server.buildSnapshot())
		frame := harness.readFrame()
		assert.NotContains(t, frame, "written by the agent")
		assert.Contains(t, readStageText(frame), "catches up when it stops")

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
		harness.watch(buildSnapshot(server.workflow, idle, server.agent))
		require.Contains(t, readHoveredRow(harness.readFrame()), "auth")

		harness.press("space", "r")
		server.repo.Wrote(newRoundWrittenOver())
		server.agent.MovedSinceRound = gittest.SnapshotSHA
		harness.watch(buildSnapshot(server.workflow, server.agent, idle))
		assert.NotContains(t, harness.readFrame(), "written by the agent")

		harness.press("e")
		harness.assertSentTo(agentv1connect.AgentServiceHoldAgentProcedure, &agentv1.HoldAgentRequest{Id: "a1"})
	})
}

// TestReviewEmphasis covers what the review says in colour and weight rather than in text.
func TestReviewEmphasis(t *testing.T) {
	t.Run("the owed list brightens a comment only while its line is on screen", func(t *testing.T) {
		server := newReviewServerOver(gittest.NewTallRepo(t.TempDir()))
		thread := newYourThread(gittest.TallFile, 5)
		thread.QuotedLines = []string{" func One() {}"}
		server.threads = []*agentv1.ReviewThread{thread}
		server.reviews = []*agentv1.Review{{
			Id:          "r1",
			AuthorKind:  agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_HUMAN,
			ReleasedAt:  "2026-09-13T10:00:00Z",
			OpenThreads: 1,
		}}
		harness := openReviewAt(t, server, panelHarnessParams{
			height:        12,
			detailsHeight: 0.01,
		})
		pin := gittest.TallFile + ":5"
		require.Contains(t, harness.readRawFrame(), paint.Panel.Style(paint.ToneAccent).Bold(true).Render(pin))

		harness.press("G")

		frame := harness.readRawFrame()
		assert.NotContains(t, frame, paint.Panel.Style(paint.ToneAccent).Bold(true).Render(pin))
		assert.Contains(t, frame, errui.Dim.Render(pin))
	})

	t.Run("the cursor row dims while the record has the keys", func(t *testing.T) {
		harness := openReview(t, newReviewServer(t))
		live := readRawCursorRow(harness)

		harness.press("ctrl+q")
		dimmed := readRawCursorRow(harness)
		require.NotEmpty(t, ansi.Strip(live))
		assert.Equal(t, ansi.Strip(live), ansi.Strip(dimmed), "the same row, restyled")
		assert.NotEqual(t, live, dimmed)

		harness.press("ctrl+a")
		assert.Equal(t, live, readRawCursorRow(harness))
	})
}

func readRawCursorRow(harness *panelHarness) string {
	line := findRawLine(harness.readRawFrame(), cursorRowLine)

	return ansi.Cut(line, config.DefaultSidebarWidth, reviewWidth)
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
			Cwd:        repo.Root,
			PaneId:     "%9",
			Alive:      xptr.Ptr(true),
			State:      agentv1.AgentState_AGENT_STATE_IDLE,
		},
	}
}

func openReview(t *testing.T, server *reviewServer) *panelHarness {
	t.Helper()

	return openReviewAt(t, server, panelHarnessParams{})
}

func openPausedReview(t *testing.T, server *reviewServer) *panelHarness {
	t.Helper()

	server.agent.HeldAt = heldAt
	server.agent.State = agentv1.AgentState_AGENT_STATE_THINKING

	return openReview(t, server)
}

func openLongThread(t *testing.T) *panelHarness {
	t.Helper()

	server := newReviewServer(t)
	server.threads = []*agentv1.ReviewThread{newLongThread()}

	return openReviewAt(t, server, panelHarnessParams{height: 16})
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
	rows := []proto.Message{s.workflow, s.agent}
	for _, thread := range s.threads {
		if thread.GetAgentId() == "" {
			thread.AgentId = s.agent.GetId()
		}
		rows = append(rows, thread)
	}
	for _, review := range s.reviews {
		agentID := review.GetAgentId()
		if agentID == "" {
			agentID = s.agent.GetId()
		}
		rows = append(rows, &agentv1.WatchedReview{
			Id:           review.GetId(),
			AgentId:      agentID,
			RoundId:      review.GetRoundId(),
			RoundOrdinal: review.GetRoundOrdinal(),
			AuthorKind:   review.GetAuthorKind(),
			Verdict:      review.GetVerdict(),
			ReleasedAt:   review.GetReleasedAt(),
			DeliveredAt:  review.GetDeliveredAt(),
		})
	}

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

func (s *reviewServer) openRound(req connect.AnyRequest) (connect.AnyResponse, error) {
	if s.openErr != nil {
		return nil, s.openErr
	}

	open, _ := req.Any().(*agentv1.OpenReviewRoundRequest)
	sha := gittest.NameSnapshot(s.repo.Tree)
	if len(s.rounds) == 0 || s.rounds[len(s.rounds)-1].GetSnapshotSha() != sha {
		s.rounds = append(s.rounds, &agentv1.ReviewRound{
			Id:          fmt.Sprintf("round-%d", len(s.rounds)+1),
			AgentId:     open.GetAgentId(),
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
			AgentId:     mark.GetAgentId(),
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

func newThreadOnLastLine(id string, messages ...*agentv1.ReviewMessage) *agentv1.ReviewThread {
	return &agentv1.ReviewThread{
		Id:          id,
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
		Id:         id,
		AuthorKind: agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_AGENT,
		Body:       body,
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
		AgentId:     "a1",
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

func findWidestFieldRow(frame string) int {
	widest := 0
	for _, line := range readSidebar(frame) {
		if strings.HasPrefix(line, "┃") {
			widest = max(widest, ansi.StringWidth(line))
		}
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

// TestReviewChange covers the row above the first file, where a comment is about the change as a
// whole rather than any line of it.
func TestReviewChange(t *testing.T) {
	t.Run("a comment written on the row names no file and quotes no line", func(t *testing.T) {
		harness := openReview(t, newReviewServer(t))

		harness.press("k")
		frame := harness.readFrame()
		assert.Contains(t, readSidebar(frame), "the change as a whole")
		assert.Contains(t, frame, "comment on the change")

		harness.press("c")
		typeText(harness, "split this in two")
		harness.press("ctrl+s")

		harness.assertSentTo(agentv1connect.AgentServiceCreateReviewThreadProcedure, &agentv1.CreateReviewThreadRequest{
			AgentId:     "a1",
			Body:        "split this in two",
			BaseSha:     gittest.BaseSHA,
			SnapshotSha: gittest.SnapshotSHA,
		})
	})

	t.Run("a comment on the change waits at the end of the diff and the row still starts another", func(t *testing.T) {
		server := newReviewServer(t)
		server.threads = []*agentv1.ReviewThread{{
			Id:       "t1",
			Messages: []*agentv1.ReviewMessage{newHumanMessage("m1", "split this in two")},
		}}
		harness := openReview(t, server)

		assert.Contains(t, findLine(readStage(harness.readFrame()), "the change as a whole"), "1 at the end of the diff")

		harness.press("k")
		assert.Contains(t, harness.readFrame(), "comment on the change")

		harness.press("G")
		frame := harness.readFrame()
		assert.NotEmpty(t, findLine(readStage(frame), "on the change as a whole ──"))
		assert.Contains(t, readSidebarProse(frame), "split this in two")
	})
}
