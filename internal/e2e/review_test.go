//go:build integration

package e2e

import (
	"testing"
	"time"

	"komrad/internal/harness"
	"komrad/internal/komradtest"
	"komrad/internal/mail"
	agentstore "komrad/internal/store/agent"
	"komrad/lib/xslice"
	agentv1 "komrad/proto/gen/agent/v1"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

// TestIntegrationReviews walks one review loop, from the worker asking for a reviewer through the release and its doorbell to the answer that rings the reviewer back.
func TestIntegrationReviews(t *testing.T) {
	isolateGit(t)

	instance := komradtest.Start(t, komradtest.Params{WorktreesDir: t.TempDir()})
	ctx := t.Context()

	workflow, err := instance.Workflows.CreateWorkflow(ctx, connect.NewRequest(buildWorktreeWorkflow(t)))
	require.NoError(t, err)

	worker := createAgent(t, instance, workflow.Msg.GetWorkflow().GetId(), "worker")
	instance.Settle(worker.GetId(), harness.ActivityIdle, agentstore.StateIdle)
	writeIn(t, worker.GetCwd(), "greet.go", "package greet\n\nfunc Hello() {}\n")
	runGitIn(t, worker.GetCwd(), "add", "greet.go")
	runGitIn(t, worker.GetCwd(), "commit", "--quiet", "-m", "greeting")

	var reviewer *agentv1.Agent

	t.Run("the worker asks for a review and a reviewer is attached to it", func(t *testing.T) {
		req := connect.NewRequest(&agentv1.RequestReviewRequest{Prompt: "read greet.go"})
		callAs(req, worker.GetId())

		requested, err := instance.Agents.RequestReview(ctx, req)
		require.NoError(t, err)

		reviewer = requested.Msg.GetReviewer()
		assert.Equal(t, worker.GetId(), reviewer.GetParentAgentId())
		assert.Equal(t, agentv1.AgentRole_AGENT_ROLE_REVIEWER, reviewer.GetRole())
		assert.Equal(t, worker.GetCwd(), reviewer.GetCwd(), "the reviewer reads the worker's own checkout")
		instance.WaitForHarness(reviewer.GetId())
	})

	t.Run("the reviewer is refused a document of its own", func(t *testing.T) {
		req := connect.NewRequest(&agentv1.WriteDocumentRequest{
			Name:   "findings",
			Kind:   agentv1.DocumentKind_DOCUMENT_KIND_REPORT,
			Source: &agentv1.WriteDocumentRequest_Body{Body: "greet.go is fine"},
		})
		callAs(req, reviewer.GetId())

		_, err := instance.Agents.WriteDocument(ctx, req)
		assert.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
	})

	var threadID string

	t.Run("the reviewer takes a round of the committed branch and comments on it", func(t *testing.T) {
		opened := connect.NewRequest(&agentv1.OpenReviewRoundRequest{AgentId: worker.GetId()})
		callAs(opened, reviewer.GetId())

		round, err := instance.Agents.OpenReviewRound(ctx, opened)
		require.NoError(t, err)
		assert.Equal(t, int32(1), round.Msg.GetRound().GetOrdinal())

		written := connect.NewRequest(&agentv1.CreateReviewThreadRequest{
			AgentId:     worker.GetId(),
			SnapshotSha: round.Msg.GetRound().GetSnapshotSha(),
			FilePath:    "greet.go",
			NewStart:    3,
			NewEnd:      3,
			QuotedLines: []string{"+func Hello() {}"},
			Body:        "say who it greets",
		})
		callAs(written, reviewer.GetId())

		thread, err := instance.Agents.CreateReviewThread(ctx, written)
		require.NoError(t, err)

		threadID = thread.Msg.GetThread().GetId()
	})

	t.Run("the worker is shown nothing of a review still being written", func(t *testing.T) {
		assert.Empty(t, listThreadBodies(t, instance, listThreadBodiesParams{
			AgentID:  worker.GetId(),
			ReaderID: worker.GetId(),
		}))
		assert.Equal(t, mail.DeliveryDone, instance.Deliver(worker.GetId()))
	})

	var reviewID string

	t.Run("releasing it rings the worker's doorbell once", func(t *testing.T) {
		req := connect.NewRequest(&agentv1.ReleaseReviewRequest{
			AgentId: worker.GetId(),
			Verdict: agentv1.ReviewVerdict_REVIEW_VERDICT_CHANGES_REQUESTED,
		})
		callAs(req, reviewer.GetId())

		released, err := instance.Agents.ReleaseReview(ctx, req)
		require.NoError(t, err)

		reviewID = released.Msg.GetReview().GetId()

		instance.WorkQueue()
		assert.Equal(t, mail.DeliverySoon, instance.Deliver(worker.GetId()))
		typed := instance.WaitForTyped(worker.GetId(), 1)
		require.Len(t, typed, 1)
		assert.Contains(t, typed[0], "read_mail")

		doorbell := instance.EndTurn(worker.GetId())
		require.Len(t, doorbell, 1)
		assert.Equal(t, agentv1.MailKind_MAIL_KIND_REVIEW_ACTIVITY, doorbell[0].GetKind())
		assert.Contains(t, doorbell[0].GetBody(), "get_review with review id "+reviewID)
		assert.NotContains(t, doorbell[0].GetBody(), "say who it greets", "the mail carries no feedback")
	})

	t.Run("the worker reads the review whole, around the code as the round froze it", func(t *testing.T) {
		req := connect.NewRequest(&agentv1.GetReviewRequest{ReviewId: reviewID})
		callAs(req, worker.GetId())

		whole, err := instance.Agents.GetReview(ctx, req)
		require.NoError(t, err)

		assert.Equal(t, agentv1.ReviewVerdict_REVIEW_VERDICT_CHANGES_REQUESTED, whole.Msg.GetReview().GetVerdict())
		assert.Empty(t, whole.Msg.GetReview().GetPath(), "the agent is not told where the file is")
		require.Len(t, whole.Msg.GetThreads(), 1)
		assert.Contains(t, whole.Msg.GetThreads()[0].GetContext(), ">> 3  func Hello() {}")
		assert.Equal(t, []string{"say who it greets"}, listThreadBodies(t, instance, listThreadBodiesParams{
			AgentID:  worker.GetId(),
			ReaderID: worker.GetId(),
		}))
	})

	t.Run("the worker's row counts the round the review was given on", func(t *testing.T) {
		listed, err := instance.Agents.ListAgents(ctx, connect.NewRequest(&agentv1.ListAgentsRequest{}))
		require.NoError(t, err)

		rows := map[string]*agentv1.AgentRow{}
		for _, row := range listed.Msg.GetAgents() {
			rows[row.GetAgent().GetId()] = row
		}
		assert.Equal(t, int32(1), rows[worker.GetId()].GetLocalReviewRounds())
		assert.Zero(t, rows[reviewer.GetId()].GetLocalReviewRounds(), "the reviewer holds no work of its own")
	})

	t.Run("the worker's answer rings the reviewer", func(t *testing.T) {
		req := connect.NewRequest(&agentv1.AddReviewMessageRequest{
			ThreadId: threadID,
			Body:     "it greets the caller now",
		})
		callAs(req, worker.GetId())

		_, err := instance.Agents.AddReviewMessage(ctx, req)
		require.NoError(t, err)

		var rung []*agentv1.Mail
		require.Eventually(t, func() bool {
			rung = append(rung, readMail(t, instance, reviewer.GetId())...)

			return len(rung) > 0
		}, 15*time.Second, 100*time.Millisecond, "the mail waits out the debounce and no longer")
		require.Len(t, rung, 1)
		assert.Equal(t, agentv1.MailKind_MAIL_KIND_REVIEW_ACTIVITY, rung[0].GetKind())
		assert.Contains(t, rung[0].GetBody(), "list_review_threads")

		assert.Equal(t, []string{"say who it greets", "it greets the caller now"}, listThreadBodies(t, instance, listThreadBodiesParams{
			AgentID:  worker.GetId(),
			ReaderID: reviewer.GetId(),
		}))
	})

	t.Run("the worker's addressed answer carries the round that made the change and leaves the reviewer to settle it", func(t *testing.T) {
		writeIn(t, worker.GetCwd(), "greet.go", "package greet\n\nfunc Hello(name string) {}\n")

		req := connect.NewRequest(&agentv1.AddReviewMessageRequest{
			ThreadId:  threadID,
			Body:      "takes the name now",
			Addressed: true,
		})
		callAs(req, worker.GetId())

		added, err := instance.Agents.AddReviewMessage(ctx, req)
		require.NoError(t, err)
		assert.NotEmpty(t, added.Msg.GetMessage().GetRoundId())

		listed := connect.NewRequest(&agentv1.ListReviewThreadsRequest{AgentId: proto.String(worker.GetId())})
		callAs(listed, worker.GetId())

		threads, err := instance.Agents.ListReviewThreads(ctx, listed)
		require.NoError(t, err)
		require.Len(t, threads.Msg.GetThreads(), 1)
		assert.Empty(t, threads.Msg.GetThreads()[0].GetResolvedAt())
	})
}

// listThreadBodiesParams names whose diff to read and who is reading it.
type listThreadBodiesParams struct {
	AgentID  string
	ReaderID string
}

// listThreadBodies is every message the reader is shown on an agent's diff.
func listThreadBodies(t *testing.T, instance *komradtest.Instance, params listThreadBodiesParams) []string {
	t.Helper()

	req := connect.NewRequest(&agentv1.ListReviewThreadsRequest{AgentId: &params.AgentID})
	callAs(req, params.ReaderID)

	threads, err := instance.Agents.ListReviewThreads(t.Context(), req)
	require.NoError(t, err)

	var bodies []string
	for _, thread := range threads.Msg.GetThreads() {
		bodies = append(bodies, xslice.Map(thread.GetMessages(), func(message *agentv1.ReviewMessage) string {
			return message.GetBody()
		})...)
	}

	return bodies
}
