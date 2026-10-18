package ui_test

import (
	"testing"

	"komrad/internal/gittest"
	"komrad/lib/xptr"
	agentv1 "komrad/proto/gen/agent/v1"
	pullrequestv1 "komrad/proto/gen/pullrequest/v1"
	taskv1 "komrad/proto/gen/task/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestReviewSidebar covers the rows over the file tree: the stack, the forge and what needs the
// person.
func TestReviewSidebar(t *testing.T) {
	t.Run("a task on the base branch with nothing standing on it has no stack", func(t *testing.T) {
		harness := openReviewAt(t, newReviewServer(t), panelHarnessParams{height: 40})

		assert.NotContains(t, readSidebar(harness.readFrame()), "stack")
	})

	t.Run("the stack lists the tasks under the task and the tasks standing on it", func(t *testing.T) {
		harness := openReviewAt(t, newStackedReviewServer(t), panelHarnessParams{height: 40})

		sidebar := readSidebar(harness.readFrame())
		assert.Subset(t, sidebar, []string{"stack", "  base-work", "  └ auth", "    └ top-work  conflicts"})
	})

	t.Run("choosing a task in the stack opens its review", func(t *testing.T) {
		harness := openReviewAt(t, newStackedReviewServer(t), panelHarnessParams{height: 40})
		require.Contains(t, harness.readFrame(), "Review · root")

		harness.press("ctrl+space", "ctrl+space", "k", "k", "o")

		assert.Contains(t, harness.readFrame(), "Review · auth")
	})

	t.Run("choosing a task whose worktree is gone says so and stays", func(t *testing.T) {
		harness := openReviewAt(t, newStackedReviewServer(t), panelHarnessParams{height: 40})

		harness.press("ctrl+space", "ctrl+space", "k", "o")

		assert.Contains(t, harness.readFrame(), "Review · root")
		assert.Contains(t, readOpenMessages(harness), "its worktree is gone")
	})

	t.Run("the forge lists its decision and who commented", func(t *testing.T) {
		server := newDescribedReviewServer(t, "Why it changed.")
		server.forge[0].(*pullrequestv1.PullRequest).MergeReadiness = &pullrequestv1.MergeReadiness{
			Decision: pullrequestv1.ReviewDecision_REVIEW_DECISION_CHANGES_REQUESTED,
		}
		server.threads = []*agentv1.ReviewThread{moveToLastLine(newForgeThread())}
		harness := openReviewAt(t, server, panelHarnessParams{height: 40})

		sidebar := readSidebar(harness.readFrame())
		assert.Subset(t, sidebar, []string{"the forge", "  changes requested", "  @dana  1 thread"})
	})

	t.Run("needs you lists an unread reply, a held forge comment and a draft", func(t *testing.T) {
		harness := openReviewAt(t, newNeedsServer(t), panelHarnessParams{height: 40})

		sidebar := readSidebar(harness.readFrame())
		assert.Subset(t, sidebar, []string{
			"needs you",
			"  draft on greet.go:3",
			"  forge comment on greet.go:5",
			"  reply on greet.go:5",
		})
	})

	t.Run("needs you counts the person's threads waiting on the writer", func(t *testing.T) {
		server := newReviewServer(t)
		server.threads = []*agentv1.ReviewThread{newThreadOnLastLine(newSentComment("m1"))}
		harness := openReviewAt(t, server, panelHarnessParams{height: 40})

		assert.Contains(t, readSidebar(harness.readFrame()), "  1 thread of yours waiting on the writer")
	})

	t.Run("choosing what needs the person lands on its thread", func(t *testing.T) {
		harness := openReviewAt(t, newNeedsServer(t), panelHarnessParams{height: 40})

		harness.press("ctrl+space", "ctrl+space", "k", "o")

		frame := harness.readFrame()
		assert.Contains(t, readPopupProse(frame), "because")
		assert.NotContains(t, readPopupProse(frame), "use a map")
	})
}

func newStackedReviewServer(t *testing.T) *reviewServer {
	t.Helper()

	server := newReviewServer(t)
	server.task.Stand = &taskv1.Stand{TaskId: "t0"}
	server.forge = append(server.forge,
		&taskv1.Task{
			Id:         "t0",
			WorkflowId: "w1",
			Slug:       "base-work",
			Cwd:        xptr.Ptr(server.repo.Root),
		},
		&agentv1.Agent{
			Id:         "a0",
			Name:       "root",
			WorkflowId: "w1",
			Role:       agentv1.AgentRole_AGENT_ROLE_WRITER,
			TaskId:     xptr.Ptr("t0"),
			Cwd:        server.repo.Root,
			PaneId:     "%8",
			Alive:      xptr.Ptr(true),
			State:      agentv1.AgentState_AGENT_STATE_IDLE,
		},
		&taskv1.Task{
			Id:         "t2",
			WorkflowId: "w1",
			Slug:       "top-work",
			Stand:      &taskv1.Stand{TaskId: "t1", Blocked: "conflicts"},
		},
	)

	return server
}

func newNeedsServer(t *testing.T) *reviewServer {
	t.Helper()

	reply := newThreadOnLastLine(newHumanMessage("m1", "why"), newAgentMessage("m2", "because"))
	reply.Id = "r1"
	reply.UnreadByHuman = true
	draft := &agentv1.ReviewThread{
		Id:          "d1",
		FilePath:    gittest.File,
		OldStart:    3,
		NewStart:    3,
		QuotedLines: []string{" func Hello() string {"},
		Messages:    []*agentv1.ReviewMessage{newUnsentComment()},
	}
	forge := moveToLastLine(newForgeThread())
	forge.Id = "f1"
	server := newReviewServer(t)
	server.threads = []*agentv1.ReviewThread{reply, draft, forge}

	return server
}

// moveToLastLine puts a thread on the last line of the diff, away from the cursor's first line.
func moveToLastLine(thread *agentv1.ReviewThread) *agentv1.ReviewThread {
	last := newThreadOnLastLine()
	thread.OldStart = last.GetOldStart()
	thread.NewStart = last.GetNewStart()
	thread.QuotedLines = last.GetQuotedLines()

	return thread
}
