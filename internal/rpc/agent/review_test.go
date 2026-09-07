package agent

import (
	"context"
	"testing"

	"weave/internal/git"

	"weave/config"
	"weave/internal/agents"
	"weave/internal/caller"
	agentstore "weave/internal/store/agent"
	reviewstore "weave/internal/store/review"
	"weave/internal/storetest"
	agentv1 "weave/proto/gen/agent/v1"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestListReviewThreadsAnswersTheCallerItHas covers the identity being read off the call rather
// than assumed, since the tool an agent has is the same one the panel reads its own drafts back
// through, and a handler that forgot who was asking would hand a review over as it was typed.
func TestListReviewThreadsAnswersTheCallerItHas(t *testing.T) {
	svc, st := reviewService(t)
	worker := st.SeedAgent(t, agentstore.Agent{
		Name:  "worker",
		Cwd:   "/repo",
		State: agentstore.StateIdle,
	})

	asHuman := caller.With(t.Context(), caller.Caller{})
	_, err := svc.OpenReviewRound(asHuman, connect.NewRequest(&agentv1.OpenReviewRoundRequest{
		AgentId: worker.ID,
	}))
	require.NoError(t, err)

	released, err := svc.CreateReviewThread(asHuman, connect.NewRequest(&agentv1.CreateReviewThreadRequest{
		AgentId:     worker.ID,
		FilePath:    "greet.go",
		NewStart:    41,
		NewEnd:      41,
		QuotedLines: []string{"+\tgreet(nil)"},
		Body:        "this needs a nil check",
	}))
	require.NoError(t, err)

	_, err = svc.ReleaseReview(asHuman, connect.NewRequest(&agentv1.ReleaseReviewRequest{
		AgentId: worker.ID,
		Verdict: agentv1.ReviewVerdict_REVIEW_VERDICT_CHANGES_REQUESTED,
	}))
	require.NoError(t, err)

	_, err = svc.CreateReviewThread(asHuman, connect.NewRequest(&agentv1.CreateReviewThreadRequest{
		AgentId:     worker.ID,
		FilePath:    "secret.go",
		NewStart:    7,
		NewEnd:      7,
		QuotedLines: []string{"+\tdoThing()"},
		Body:        "rip this out, I have not decided yet",
	}))
	require.NoError(t, err)

	forHuman, err := svc.ListReviewThreads(asHuman, connect.NewRequest(&agentv1.ListReviewThreadsRequest{
		AgentId: &worker.ID,
	}))
	require.NoError(t, err)
	assert.Len(t, forHuman.Msg.GetThreads(), 2, "the panel reads its own unfinished review back")

	asAgent := caller.With(t.Context(), caller.Caller{AgentID: worker.ID})
	forAgent, err := svc.ListReviewThreads(asAgent, connect.NewRequest(&agentv1.ListReviewThreadsRequest{}))
	require.NoError(t, err)

	require.Len(t, forAgent.Msg.GetThreads(), 1)
	thread := forAgent.Msg.GetThreads()[0]
	assert.Equal(t, released.Msg.GetThread().GetId(), thread.GetId())
	assert.NotContains(t, forAgent.Msg.String(), "I have not decided yet")
	assert.NotContains(t, forAgent.Msg.String(), "secret.go")

	require.Len(t, thread.GetMessages(), 1)
	message := thread.GetMessages()[0]
	assert.Equal(t, "this needs a nil check", message.GetBody())
	assert.Empty(t, message.GetSentAt(), "when the person let the comment go is their own record")
	assert.Equal(t, agentv1.ReviewDeliveryState_REVIEW_DELIVERY_STATE_UNSPECIFIED, message.GetDelivery())
}

func reviewService(t *testing.T) (*Service, *storetest.Stores) {
	t.Helper()

	st := storetest.Open(t)
	reviews := reviewstore.New(st.Pool.GetDB())
	mail := agents.NewMailService(agents.NewMailServiceParams{
		MailReadWriter: st.Mail,
		Agents:         st.Agents,
		Config:         stubMailConfig(config.DefaultMaxExchange),
	})
	review := agents.NewReviewService(agents.ReviewServiceParams{
		Threads:   reviews,
		Messages:  reviews,
		Viewed:    reviews,
		Rounds:    reviews,
		Reviews:   reviews,
		Snapshots: stubSnapshotter{},
		Workflows: st.Workflows,
		StateDir:  t.TempDir(),
		Agents:    st.Agents,
		Mailer:    mail,
	})

	return NewService(NewServiceParams{
		Agents:   st.Agents,
		Mail:     mail,
		Review:   review,
		Runs:     st.Runs,
		Marks:    markService(st),
		Liveness: &fakeLauncher{},
	}), st
}

// stubSnapshotter stands in for git, since these tests are about who the handler thinks is
// calling rather than about what a worktree holds.
type stubSnapshotter struct{}

func (stubSnapshotter) Take(context.Context, git.TakeSnapshotParams) (*git.Snapshot, error) {
	return &git.Snapshot{BaseSHA: "base-sha", SHA: "snapshot-sha"}, nil
}

func (stubSnapshotter) Hold(context.Context, string, string, string) error { return nil }

func (stubSnapshotter) FileAt(context.Context, string, string, string) ([]string, error) {
	return nil, nil
}

// TestListedAgentsCarryTheirReviewRounds is the wiring the panel draws its loop mark from: a
// released review is counted against the agent whose work it was given on and against the agent
// that gave it, and a review still being written is counted against neither.
func TestListedAgentsCarryTheirReviewRounds(t *testing.T) {
	svc, st := reviewService(t)
	worker := st.SeedAgent(t, agentstore.Agent{Name: "worker", Cwd: "/repo", State: agentstore.StateIdle})
	reviewer := st.SeedAgent(t, agentstore.Agent{
		Name: "reviewer", Cwd: "/repo", State: agentstore.StateIdle,
		Role: agentstore.RoleReviewer, ParentAgentID: &worker.ID,
	})

	asReviewer := caller.With(t.Context(), caller.Caller{AgentID: reviewer.ID})
	_, err := svc.OpenReviewRound(asReviewer, connect.NewRequest(&agentv1.OpenReviewRoundRequest{
		AgentId: worker.ID,
	}))
	require.NoError(t, err)

	_, err = svc.CreateReviewThread(asReviewer, connect.NewRequest(&agentv1.CreateReviewThreadRequest{
		AgentId:     worker.ID,
		FilePath:    "greet.go",
		NewStart:    41,
		NewEnd:      41,
		QuotedLines: []string{"+\tgreet(nil)"},
		Body:        "this needs a nil check",
	}))
	require.NoError(t, err)

	counts := func() map[string][2]int32 {
		listed, err := svc.ListAgents(t.Context(), connect.NewRequest(&agentv1.ListAgentsRequest{}))
		require.NoError(t, err)

		rounds := make(map[string][2]int32, len(listed.Msg.GetAgents()))
		for _, agent := range listed.Msg.GetAgents() {
			rounds[agent.GetName()] = [2]int32{agent.GetReviewRoundsReceived(), agent.GetReviewRoundsSent()}
		}

		return rounds
	}

	assert.Equal(t, [2]int32{0, 0}, counts()["worker"], "a review nobody has released is nothing yet")

	_, err = svc.ReleaseReview(asReviewer, connect.NewRequest(&agentv1.ReleaseReviewRequest{
		AgentId: worker.ID,
		Verdict: agentv1.ReviewVerdict_REVIEW_VERDICT_CHANGES_REQUESTED,
	}))
	require.NoError(t, err)

	assert.Equal(t, [2]int32{1, 0}, counts()["worker"])
	assert.Equal(t, [2]int32{0, 1}, counts()["reviewer"])
}
