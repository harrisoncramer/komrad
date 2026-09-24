package ui_test

import (
	"fmt"
	"strings"
	"testing"

	"weave/config"
	"weave/internal/gittest"
	agentv1 "weave/proto/gen/agent/v1"
	"weave/proto/gen/agent/v1/agentv1connect"
	watchv1 "weave/proto/gen/watch/v1"
	workflowv1 "weave/proto/gen/workflow/v1"
	worktreev1 "weave/proto/gen/worktree/v1"
	"weave/proto/gen/worktree/v1/worktreev1connect"

	"connectrpc.com/connect"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/testing/protocmp"
)

const (
	reviewWidth  = 200
	reviewHeight = 30
)

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
			Cwd:        repo.Root,
			PaneId:     "%9",
			Alive:      proto.Bool(true),
			State:      agentv1.AgentState_AGENT_STATE_IDLE,
		},
	}
}

func openReview(t *testing.T, server *reviewServer) *panelHarness {
	t.Helper()

	return openReviewAt(t, server, panelHarnessParams{})
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
	harness.watch(server.snapshot())
	harness.press("d")

	return harness
}

func (s *reviewServer) snapshot() *watchv1.WatchEvent {
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

func newThreadOnFirstLine(id string, messages ...*agentv1.ReviewMessage) *agentv1.ReviewThread {
	return &agentv1.ReviewThread{
		Id:          id,
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

// TestReviewOpen covers which agents the review key opens a diff for and what it draws.
func TestReviewOpen(t *testing.T) {
	t.Run("the diff is drawn beside the sidebar over the whole window", func(t *testing.T) {
		harness := openReview(t, newReviewServer(t))

		frame := harness.readFrame()
		assert.True(t, harness.panel.IsZoomed())
		assert.Contains(t, readStage(frame), "greet.go")
		assert.Contains(t, readStage(frame), "Review · auth")
		assert.Contains(t, readStage(frame), "1 file, +1 -1")
		assert.Len(t, splitLines(frame), reviewHeight)
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

	t.Run("an agent that has written nothing is refused", func(t *testing.T) {
		repo := gittest.NewRepo(t.TempDir())
		repo.Wrote(gittest.WroteParams{Tree: "tree-of-the-base"})

		harness := openReview(t, newReviewServerOver(repo))

		frame := harness.readFrame()
		assert.False(t, harness.panel.IsZoomed())
		assert.Contains(t, frame, "Nothing to review in auth")
		assert.Contains(t, frame, "no change against the base branch")
	})

	t.Run("an agent whose directory has gone is not opened", func(t *testing.T) {
		server := newReviewServer(t)
		server.agent.WorktreeRemoved = true

		harness := openReview(t, server)

		assert.False(t, harness.panel.IsZoomed())
		assert.Empty(t, harness.readSent(agentv1connect.AgentServiceOpenReviewRoundProcedure))
	})

	t.Run("a diff git cannot read is refused with the reason", func(t *testing.T) {
		repo := gittest.NewRepo(t.TempDir())
		repo.RootErr = gittest.ErrNoGit

		harness := openReview(t, newReviewServerOver(repo))

		frame := harness.readFrame()
		assert.False(t, harness.panel.IsZoomed())
		assert.Contains(t, frame, "Could not read the diff of")
		assert.Contains(t, frame, gittest.ErrNoGit.Error())
	})
}

// TestReviewCursor covers the keys that move through the diff.
func TestReviewCursor(t *testing.T) {
	t.Run("the movement keys walk the lines", func(t *testing.T) {
		harness := openReview(t, newReviewServer(t))
		require.Equal(t, "greet.go:1", readReviewCursor(harness.readFrame()))

		harness.press("j")

		assert.Equal(t, "greet.go:2", readReviewCursor(harness.readFrame()))
	})

	t.Run("the scroll keys turn the cursor a few rows and back", func(t *testing.T) {
		harness := openReview(t, newReviewServer(t))

		harness.press("ctrl+n")
		assert.Equal(t, "greet.go:4 (removed)", readReviewCursor(harness.readFrame()))

		harness.press("ctrl+p")
		assert.Equal(t, "greet.go:1", readReviewCursor(harness.readFrame()))
	})

	t.Run("the page keys settle on a line and come back", func(t *testing.T) {
		harness := openReview(t, newReviewServer(t))

		harness.press("ctrl+d")
		assert.Equal(t, "greet.go:5", readReviewCursor(harness.readFrame()))

		harness.press("ctrl+u")
		assert.Equal(t, "greet.go:1", readReviewCursor(harness.readFrame()))
	})

	t.Run("panning moves the text a third of the diff's width", func(t *testing.T) {
		harness := openReview(t, newReviewServer(t))
		require.Contains(t, harness.readFrame(), "func Hello() string {")

		harness.press("l")

		frame := harness.readFrame()
		assert.NotContains(t, frame, "func Hello() string {")
		harness.press("0")
		assert.Contains(t, harness.readFrame(), "func Hello() string {")
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

		harness.watch(server.snapshot())
		assert.Equal(t, jumped, readStage(harness.readFrame()))
	})

	t.Run("the keys come from the keymap", func(t *testing.T) {
		keys := config.DefaultKeybindings()
		keys.Review.Compose = []string{"i"}
		harness := openReviewAt(t, newReviewServer(t), panelHarnessParams{height: 40, keys: &keys})

		harness.press("?")
		assert.Contains(t, readLegend(harness.readFrame()), "i       start a review here")

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

		assertSentTo(t, harness, agentv1connect.AgentServiceCreateReviewThreadProcedure, &agentv1.CreateReviewThreadRequest{
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

	t.Run("the thread under the cursor is drawn in the sidebar", func(t *testing.T) {
		server := newReviewServer(t)
		server.threads = []*agentv1.ReviewThread{newThreadOnFirstLine("t1", newHumanMessage("m1", "wrong greeting"))}

		harness := openReview(t, server)

		sidebar := readSidebarProse(harness.readFrame())
		assert.Contains(t, sidebar, "you wrong greeting")
	})

	t.Run("the compose key on a line with a thread replies in it", func(t *testing.T) {
		server := newReviewServer(t)
		server.threads = []*agentv1.ReviewThread{newThreadOnFirstLine("t1", newAgentMessage("m1", "renamed it, is that what you meant?"))}
		harness := openReviewAt(t, server, panelHarnessParams{height: 40})

		harness.press("?")
		require.Contains(t, readLegend(harness.readFrame()), "reply in this thread")

		harness.press("c")
		typeText(harness, "yes, that is it")
		harness.press("ctrl+s")

		assertSentTo(t, harness, agentv1connect.AgentServiceAddReviewMessageProcedure, &agentv1.AddReviewMessageRequest{
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

		assertSentTo(t, harness, agentv1connect.AgentServiceAddReviewMessageProcedure, &agentv1.AddReviewMessageRequest{
			ThreadId: "t1",
			Body:     "because",
		})
	})

	t.Run("the settle key settles a thread and presses again to reopen it", func(t *testing.T) {
		server := newReviewServer(t)
		server.threads = []*agentv1.ReviewThread{newThreadOnFirstLine("t1", newAgentMessage("m1", "why did this change?"))}
		harness := openReview(t, server)

		harness.press("r")
		harness.watch(server.snapshot())
		assert.Contains(t, findLine(readStage(harness.readFrame()), "package greet"), "✓")

		harness.press("r")
		harness.watch(server.snapshot())
		assert.NotContains(t, findLine(readStage(harness.readFrame()), "package greet"), "✓")

		assertSentTo(t, harness, agentv1connect.AgentServiceSetReviewThreadResolvedProcedure,
			&agentv1.SetReviewThreadResolvedRequest{Id: "t1", Resolved: true},
			&agentv1.SetReviewThreadResolvedRequest{Id: "t1", Resolved: false},
		)
	})

	t.Run("a thread whose file left the diff settles like any other", func(t *testing.T) {
		server := newReviewServer(t)
		server.threads = []*agentv1.ReviewThread{newAdriftThread("t1", newAgentMessage("m1", "why did this go?"))}
		harness := openReview(t, server)

		harness.press("t", "r")
		harness.watch(server.snapshot())

		assert.Contains(t, findLine(readStage(harness.readFrame()), "gone.go:12"), "✓")
		assertSentTo(t, harness, agentv1connect.AgentServiceSetReviewThreadResolvedProcedure,
			&agentv1.SetReviewThreadResolvedRequest{Id: "t1", Resolved: true})
	})

	t.Run("a thread deleted from under the cursor leaves it on a row that exists", func(t *testing.T) {
		server := newReviewServer(t)
		server.threads = []*agentv1.ReviewThread{newAdriftThread("t1", newAgentMessage("m1", "why did this go?"))}
		harness := openReview(t, server)
		harness.press("t")

		server.threads = nil
		harness.watch(server.snapshot())

		assert.Equal(t, "greet.go:5", readReviewCursor(harness.readFrame()))
	})

	t.Run("the composer takes every key but escape as a character", func(t *testing.T) {
		harness := openReview(t, newReviewServer(t))

		harness.press("c", "?", "l", "n")
		frame := harness.readFrame()
		assert.Contains(t, readSidebar(frame), "┃ ?ln")
		assert.Contains(t, frame, "k/j move …")

		harness.press("esc")
		assert.NotContains(t, harness.readFrame(), "┃ Comment")
	})
}

// TestReviewThreadFocus covers the thread taking the keys to read a conversation taller than the sidebar.
func TestReviewThreadFocus(t *testing.T) {
	openLongThread := func(t *testing.T) *panelHarness {
		t.Helper()

		server := newReviewServer(t)
		server.threads = []*agentv1.ReviewThread{newLongThread()}

		return openReviewAt(t, server, panelHarnessParams{height: 14})
	}

	t.Run("the thread scrolls back to its start and escape hands the keys back", func(t *testing.T) {
		harness := openLongThread(t)
		require.NotContains(t, readSidebarProse(harness.readFrame()), "note 0")

		harness.press("ctrl+f")
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
		harness.press("ctrl+f")

		harness.press("ctrl+p")
		assert.NotContains(t, readSidebarProse(harness.readFrame()), "note 5")
		assert.Equal(t, "greet.go:1", readReviewCursor(harness.readFrame()))

		harness.press("ctrl+n")
		assert.Contains(t, readSidebarProse(harness.readFrame()), "note 5")
	})

	t.Run("moving the diff cursor drops the scroll and the keys", func(t *testing.T) {
		harness := openLongThread(t)
		harness.press("ctrl+f", "k")
		require.Contains(t, readLegend(harness.readFrame()), "back to the diff")

		harness.press("ctrl+d")

		frame := harness.readFrame()
		assert.Equal(t, "greet.go:5", readReviewCursor(frame))
		assert.NotContains(t, readLegend(frame), "back to the diff")
	})

	t.Run("a line with no thread refuses the key and says so", func(t *testing.T) {
		harness := openReview(t, newReviewServer(t))

		harness.press("ctrl+f")

		assert.Contains(t, readSidebarProse(harness.readFrame()), "no thread here to read")
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

			harness.press("ctrl+f", "k", "k", "k", "k", "k")
			assert.Len(t, splitLines(harness.readFrame()), height)

			harness.press("esc", "c")
			assert.Len(t, splitLines(harness.readFrame()), height)
		})
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
	thread := newThreadOnFirstLine("t1")
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

func assertSentTo(t *testing.T, harness *panelHarness, procedure string, want ...proto.Message) {
	t.Helper()

	assert.Empty(t, cmp.Diff(want, harness.readSent(procedure), protocmp.Transform(), cmpopts.EquateEmpty()), "the requests sent (-want +got)")
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
		assert.Contains(t, frame, "k/j move …")
	})

	t.Run("the search is the review's and leaves the agents list alone", func(t *testing.T) {
		server := newReviewServer(t)
		harness := newPanelHarness(t, panelHarnessParams{width: reviewWidth, height: reviewHeight, git: server.repo})
		server.serve(harness)
		second := newAgent("a2", "billing", "w1")
		second.Cwd = server.repo.Root
		harness.watch(buildSnapshot(server.workflow, server.agent, second))
		harness.press("j")
		require.Contains(t, readHoveredRow(harness.readFrame()), "auth")

		harness.press("d", "/")
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

		harness.press("v", "esc", "d")
		require.NotContains(t, harness.readFrame(), "func Hello() string {")

		server.repo.Wrote(gittest.WroteParams{
			Tree:  "tree-written-again",
			Whole: gittest.PatchWrittenOver,
			Sources: map[string]string{
				gittest.File: "package greet\n\nfunc Hello() string {\n\treturn \"written by the agent\"\n}\n",
			},
		})
		harness.press("esc", "d")

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
			name:     "a comment never let go of is unsent",
			messages: []*agentv1.ReviewMessage{newUnsentComment("m1")},
			want:     "1 unsent",
			absent:   []string{"delivered"},
		},
		{
			name:     "each comment is counted by how far it got",
			messages: []*agentv1.ReviewMessage{newUnsentComment("m1"), newSentComment("m2"), newDeliveredComment("m3")},
			want:     "1 unsent · 1 waiting · 1 delivered",
		},
		{
			name:     "what the agent said is nobody's delivery",
			messages: []*agentv1.ReviewMessage{newAgentMessage("m9", "done")},
			absent:   []string{"unsent", "waiting", "delivered"},
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

// TestReviewRelease covers sending the review to the agent.
func TestReviewRelease(t *testing.T) {
	t.Run("the submit key asks what to send and the same key sends it", func(t *testing.T) {
		harness := openReview(t, newReviewServer(t))

		harness.press("ctrl+s")
		assert.NotContains(t, harness.readFrame(), "┃ Comment")
		assert.Empty(t, harness.readSent(agentv1connect.AgentServiceReleaseReviewProcedure))

		harness.press("ctrl+s")

		sent := harness.readSent(agentv1connect.AgentServiceReleaseReviewProcedure)
		require.Len(t, sent, 1)
		released, _ := sent[0].(*agentv1.ReleaseReviewRequest)
		assert.Equal(t, "a1", released.GetAgentId())
	})
}

func newUnsentComment(id string) *agentv1.ReviewMessage {
	message := newHumanMessage(id, "")
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
		harness.watch(server.snapshot())

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

		server.threads = []*agentv1.ReviewThread{newThreadOnFirstLine("t1", newAgentMessage("m1", "done"))}
		harness.watch(server.snapshot())
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
		harness.watch(server.snapshot())

		assert.Contains(t, harness.readFrame(), "written by the agent")
		assert.Len(t, harness.readSent(agentv1connect.AgentServiceOpenReviewRoundProcedure), 2)

		harness.watch(server.snapshot())
		assert.Len(t, harness.readSent(agentv1connect.AgentServiceOpenReviewRoundProcedure), 2)
	})

	t.Run("a move from a round other than the one on screen is not followed", func(t *testing.T) {
		server := newReviewServer(t)
		harness := openReview(t, server)

		server.agent.MovedSinceRound = "snapshot-of-another-round"
		harness.watch(server.snapshot())

		assert.Len(t, harness.readSent(agentv1connect.AgentServiceOpenReviewRoundProcedure), 1)
	})

	t.Run("a move waits while a search is typed and is read once it is put away", func(t *testing.T) {
		server := newReviewServer(t)
		harness := openReview(t, server)
		harness.press("/", "a")

		server.repo.Wrote(newRoundWrittenOver())
		server.agent.MovedSinceRound = gittest.SnapshotSHA
		harness.watch(server.snapshot())
		assert.NotContains(t, harness.readFrame(), "written by the agent")

		harness.press("esc")
		harness.watch(server.snapshot())
		assert.Contains(t, harness.readFrame(), "written by the agent")
	})

	t.Run("a move while the reader writes is offered rather than read", func(t *testing.T) {
		server := newReviewServer(t)
		harness := openReview(t, server)
		harness.press("c", "a")

		server.repo.Wrote(newRoundWrittenOver())
		server.agent.MovedSinceRound = gittest.SnapshotSHA
		harness.watch(server.snapshot())
		harness.press("esc")
		harness.watch(server.snapshot())

		frame := harness.readFrame()
		assert.NotContains(t, frame, "written by the agent")
		assert.Contains(t, readStageText(frame), "the worktree has moved · R to catch up")
	})
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

// TestReviewOwed covers the reader's released review holding comments nobody has settled.
func TestReviewOwed(t *testing.T) {
	t.Run("the list names each open comment and whether it was answered", func(t *testing.T) {
		harness := openReview(t, newOwedServer(t, newOwedThread("t1", "a.txt", 2), newOwedThread("t2", "b.txt", 5, newAgentMessage("m9", "rewrote the header"))))

		sidebar := readSidebarProse(harness.readFrame())
		assert.Contains(t, sidebar, "Open in your review 2")
		assert.Contains(t, sidebar, "a.txt:2 no reply")
		assert.Contains(t, sidebar, "b.txt:5 answered")
	})

	t.Run("the list counts the comments it draws and goes with the last one settled", func(t *testing.T) {
		server := newOwedServer(t, newOwedThread("t1", "a.txt", 2), newOwedThread("t2", "b.txt", 5))
		harness := openReview(t, server)

		server.threads[0].ResolvedAt = "2026-09-13T11:00:00Z"
		harness.watch(server.snapshot())
		sidebar := readSidebarProse(harness.readFrame())
		assert.Contains(t, sidebar, "Open in your review 1")
		assert.NotContains(t, sidebar, "a.txt:2")

		server.threads[1].ResolvedAt = "2026-09-13T11:00:00Z"
		harness.watch(server.snapshot())
		assert.NotContains(t, readSidebarProse(harness.readFrame()), "Open in your review")
	})

	t.Run("the list counts what it has no room to draw", func(t *testing.T) {
		threads := make([]*agentv1.ReviewThread, 0, 6)
		for i := range 6 {
			threads = append(threads, newOwedThread(fmt.Sprintf("t%d", i), fmt.Sprintf("f%d.txt", i), int32(i+1)))
		}

		harness := openReview(t, newOwedServer(t, threads...))

		sidebar := readSidebarProse(harness.readFrame())
		assert.Contains(t, sidebar, "Open in your review 6")
		assert.Contains(t, sidebar, "+ 3 more")
	})

	t.Run("the list is not drawn over an agent that will never answer", func(t *testing.T) {
		server := newOwedServer(t, newOwedThread("t1", "a.txt", 2))
		server.agent.Alive = proto.Bool(false)

		harness := openReview(t, server)

		assert.NotContains(t, readSidebarProse(harness.readFrame()), "Open in your review")
	})

	t.Run("the list says when a comment is folded away", func(t *testing.T) {
		harness := openReview(t, newOwedServer(t, newOwedThread("t1", gittest.File, 2)))

		harness.press("v")

		assert.Contains(t, readSidebarProse(harness.readFrame()), "folded away")
	})

	t.Run("the compose key refuses a new comment and says why", func(t *testing.T) {
		harness := openReview(t, newOwedServer(t, newOwedThread("t1", "a.txt", 2)))

		harness.press("c")

		frame := harness.readFrame()
		assert.NotContains(t, frame, "┃ Comment")
		assert.Contains(t, readSidebarProse(frame), "comment of yours is still open")
	})

	t.Run("the compose key still replies in a comment the review holds", func(t *testing.T) {
		harness := openReview(t, newOwedServer(t, newOwedThread("t1", gittest.File, 1)))

		harness.press("c")
		typeText(harness, "still reads like a changelog")
		harness.press("ctrl+s")

		assertSentTo(t, harness, agentv1connect.AgentServiceAddReviewMessageProcedure, &agentv1.AddReviewMessageRequest{
			ThreadId: "t1",
			Body:     "still reads like a changelog",
		})
	})

	t.Run("the submit key refuses with nothing written and opens once a reply is", func(t *testing.T) {
		server := newOwedServer(t, newOwedThread("t1", gittest.File, 1))
		harness := openReview(t, server)

		harness.press("ctrl+s", "ctrl+s")
		assert.Empty(t, harness.readSent(agentv1connect.AgentServiceReleaseReviewProcedure))
		assert.Contains(t, readSidebarProse(harness.readFrame()), "still open")

		harness.press("c")
		typeText(harness, "again")
		harness.press("ctrl+s")
		harness.watch(server.snapshot())
		harness.press("ctrl+s", "ctrl+s")

		assert.Len(t, harness.readSent(agentv1connect.AgentServiceReleaseReviewProcedure), 1)
	})

	t.Run("settling the last comment lets the next review go", func(t *testing.T) {
		server := newOwedServer(t, newOwedThread("t1", gittest.File, 1))
		harness := openReview(t, server)

		harness.press("r")
		harness.watch(server.snapshot())
		harness.press("ctrl+s", "ctrl+s")

		assert.Len(t, harness.readSent(agentv1connect.AgentServiceReleaseReviewProcedure), 1)
		assert.NotContains(t, readSidebarProse(harness.readFrame()), "Open in your review")
	})

	t.Run("the edit key refuses and names the key that settles", func(t *testing.T) {
		harness := openReview(t, newOwedServer(t, newOwedThread("t1", "a.txt", 2)))

		harness.press("e")

		assert.Contains(t, readSidebarProse(harness.readFrame()), "r settles")
		assert.NotContains(t, harness.readFrame(), "editing")
	})

	t.Run("the edit key opens over an agent that will never answer", func(t *testing.T) {
		server := newOwedServer(t, newOwedThread("t1", "a.txt", 2))
		server.agent.Alive = proto.Bool(false)
		harness := openReview(t, server)

		harness.press("e")

		assert.Contains(t, harness.readFrame(), "editing · greet.go")
	})

	t.Run("the open comment keys walk both ways and stop at the ends", func(t *testing.T) {
		harness := openReview(t, newOwedServer(t,
			newOwedThread("t1", gittest.File, 2),
			newOwedThread("t2", gittest.File, 3),
			newOwedThread("t3", gittest.File, 5),
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

	t.Run("the legend follows the keys onto the thread and back", func(t *testing.T) {
		harness := openReviewAt(t, newOwedServer(t, newOwedThread("t1", gittest.File, 2)), panelHarnessParams{height: 50})
		harness.press("?", "u")
		diff := readLegend(harness.readFrame())
		require.NotContains(t, diff, "scroll the thread")

		harness.press("ctrl+f")
		thread := readLegend(harness.readFrame())
		assert.Contains(t, thread, "scroll the thread")
		assert.Contains(t, thread, "back to the diff")

		harness.press("ctrl+f")
		assert.Equal(t, diff, readLegend(harness.readFrame()))
	})
}

func newOwedServer(t *testing.T, threads ...*agentv1.ReviewThread) *reviewServer {
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

func newOwedThread(id, path string, line int32, replies ...*agentv1.ReviewMessage) *agentv1.ReviewThread {
	source := map[int32]string{1: " package greet", 2: " ", 3: " func Hello() string {", 5: " }"}

	return &agentv1.ReviewThread{
		Id:          id,
		ReviewId:    "r1",
		FilePath:    path,
		OldStart:    line,
		NewStart:    line,
		QuotedLines: []string{source[line]},
		Messages:    append([]*agentv1.ReviewMessage{newDeliveredComment(id + "-m1")}, replies...),
	}
}
