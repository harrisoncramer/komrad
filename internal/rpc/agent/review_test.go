package agent

import (
	"context"
	"testing"

	agentv1weave "weave/proto/gen/agent/v1/agentv1weave"

	"weave/internal/git"
	"weave/internal/mail"

	"weave/config"
	"weave/internal/caller"
	"weave/internal/reviews"
	agentstore "weave/internal/store/agent"
	reviewstore "weave/internal/store/review"
	"weave/internal/storetest"
	agentv1 "weave/proto/gen/agent/v1"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestListReviewThreadsAnswersTheCallerItHas covers the identity being read off the call rather
// than assumed.
func TestListReviewThreadsAnswersTheCallerItHas(t *testing.T) {
	svc, st, rounds := reviewService(t)
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

	// A second review may only be started once the first is done with.
	_, err = svc.SetReviewThreadResolved(asHuman, connect.NewRequest(&agentv1.SetReviewThreadResolvedRequest{
		Id:       released.Msg.GetThread().GetId(),
		Resolved: true,
	}))
	require.NoError(t, err)

	_, err = rounds.CreateRound(asHuman, reviewstore.CreateRoundParams{
		AgentID:     worker.ID,
		BaseSHA:     "base-sha",
		SnapshotSHA: "moved-on",
	})
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

// TestGetReviewHandsTheAgentOneReleaseWhole pins that the agent reads a release in one call and
// sees only what went out with it, cut by the review each message carries.
func TestGetReviewHandsTheAgentOneReleaseWhole(t *testing.T) {
	svc, st, _ := reviewService(t)
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

	opened, err := svc.CreateReviewThread(asHuman, connect.NewRequest(&agentv1.CreateReviewThreadRequest{
		AgentId:     worker.ID,
		FilePath:    "greet.go",
		NewStart:    41,
		NewEnd:      41,
		QuotedLines: []string{"+\tgreet(nil)"},
		Body:        "this needs a nil check",
	}))
	require.NoError(t, err)

	first, err := svc.ReleaseReview(asHuman, connect.NewRequest(&agentv1.ReleaseReviewRequest{
		AgentId: worker.ID,
		Verdict: agentv1.ReviewVerdict_REVIEW_VERDICT_CHANGES_REQUESTED,
		Body:    "one thing before it goes",
	}))
	require.NoError(t, err)

	asAgent := caller.With(t.Context(), caller.Caller{AgentID: worker.ID})
	_, err = svc.AddReviewMessage(asAgent, connect.NewRequest(&agentv1.AddReviewMessageRequest{
		ThreadId: opened.Msg.GetThread().GetId(),
		Body:     "done",
	}))
	require.NoError(t, err)

	_, err = svc.AddReviewMessage(asHuman, connect.NewRequest(&agentv1.AddReviewMessageRequest{
		ThreadId: opened.Msg.GetThread().GetId(),
		Body:     "not quite, the other branch too",
	}))
	require.NoError(t, err)

	second, err := svc.ReleaseReview(asHuman, connect.NewRequest(&agentv1.ReleaseReviewRequest{
		AgentId: worker.ID,
		Verdict: agentv1.ReviewVerdict_REVIEW_VERDICT_CHANGES_REQUESTED,
	}))
	require.NoError(t, err)

	whole, err := svc.GetReview(asAgent, connect.NewRequest(&agentv1.GetReviewRequest{
		ReviewId: first.Msg.GetReview().GetId(),
	}))
	require.NoError(t, err)

	assert.Equal(t, agentv1.ReviewVerdict_REVIEW_VERDICT_CHANGES_REQUESTED, whole.Msg.GetReview().GetVerdict())
	assert.Equal(t, "one thing before it goes", whole.Msg.GetReview().GetBody())
	assert.Empty(t, whole.Msg.GetReview().GetPath(), "the agent is not told where the file is")
	assert.Equal(t, int32(1), whole.Msg.GetRound().GetOrdinal())

	require.Len(t, whole.Msg.GetThreads(), 1)
	thread := whole.Msg.GetThreads()[0].GetThread()
	assert.Equal(t, opened.Msg.GetThread().GetId(), thread.GetId())
	assert.Equal(t, "greet.go", thread.GetFilePath())
	require.Len(t, thread.GetMessages(), 1, "the agent's answer and the second release belong elsewhere")
	assert.Equal(t, "this needs a nil check", thread.GetMessages()[0].GetBody())
	assert.Equal(t, first.Msg.GetReview().GetId(), thread.GetMessages()[0].GetReviewId())

	again, err := svc.GetReview(asAgent, connect.NewRequest(&agentv1.GetReviewRequest{
		ReviewId: second.Msg.GetReview().GetId(),
	}))
	require.NoError(t, err)
	require.Len(t, again.Msg.GetThreads(), 1)
	require.Len(t, again.Msg.GetThreads()[0].GetThread().GetMessages(), 1)
	assert.Equal(t, "not quite, the other branch too", again.Msg.GetThreads()[0].GetThread().GetMessages()[0].GetBody())

	threads, err := svc.ListReviewThreads(asAgent, connect.NewRequest(&agentv1.ListReviewThreadsRequest{}))
	require.NoError(t, err)
	require.Len(t, threads.Msg.GetThreads(), 1)
	require.Len(t, threads.Msg.GetThreads()[0].GetMessages(), 3)
	assert.Nil(t, threads.Msg.GetThreads()[0].GetMessages()[1].ReviewId, "an answer belongs to no batch")
}

func reviewService(t *testing.T) (agentv1weave.AgentServiceAdapter, *storetest.Stores, *reviewstore.ReviewStore) {
	t.Helper()

	st := storetest.Open(t)
	rounds := reviewstore.NewReviewStore(st.Pool.GetDB())
	mailer := mail.NewMailService(mail.NewMailServiceParams{
		MailReadWriter: st.Mail,
		Agents:         st.Agents,
		Config:         config.Mail{},
	})
	review := reviews.NewReviewService(reviews.NewReviewServiceParams{
		Threads:   rounds,
		Messages:  rounds,
		Viewed:    rounds,
		Rounds:    rounds,
		Reviews:   rounds,
		Snapshots: fakeSnapshotter{},
		Workflows: st.Workflows,
		StateDir:  t.TempDir(),
		Agents:    st.Agents,
		Mailer:    mailer,
	})

	liveness := &fakeLauncher{}

	return agentv1weave.AgentServiceAdapter{Impl: NewHandler(NewHandlerParams{
		Agents: st.Agents,
		Rows:   st.Rows,
		Mail:   mailer,
		Review: review,
		Marks:  markService(st),
		Watched: WatchedListers{
			Marks:        st.Marks,
			Mail:         st.Mail,
			PullRequests: st.PullRequests,
			Repositories: st.PullRequestSyncs,
			Reviews:      rounds,
			Workflows:    st.Workflows,
		},
		Liveness: liveness,
	})}, st, rounds
}

// fakeSnapshotter stands in for git, these tests being about who the handler thinks is calling
// rather than about what a worktree holds.
type fakeSnapshotter struct{}

func (fakeSnapshotter) TakeSnapshot(context.Context, git.TakeSnapshotParams) (*git.Snapshot, error) {
	return &git.Snapshot{BaseSHA: "base-sha", SHA: "snapshot-sha"}, nil
}

func (fakeSnapshotter) HoldRef(context.Context, string, string, string) error { return nil }

func (fakeSnapshotter) ReleaseRef(context.Context, string, string) error { return nil }

func (fakeSnapshotter) ReadFileLines(context.Context, string, string, string) ([]string, error) {
	return nil, nil
}

// TestListedAgentsCarryWhereTheirTreeStands pins that the standing the cwd pass wrote against
// the round of the last verdict rides the row.
func TestListedAgentsCarryWhereTheirTreeStands(t *testing.T) {
	svc, st, _ := reviewService(t)
	worker := st.SeedAgent(t, agentstore.Agent{
		Name:  "worker",
		Cwd:   "/repo",
		State: agentstore.StateIdle,
	})
	reviewer := st.SeedAgent(t, agentstore.Agent{
		Name:          "reviewer",
		Cwd:           "/repo",
		State:         agentstore.StateIdle,
		Role:          agentstore.RoleReviewer,
		ParentAgentID: &worker.ID,
	})
	require.NoError(t, st.Agents.SetTreeStanding(t.Context(), worker.ID, git.StandingLandedMoved, "snapshot-sha"))

	asReviewer := caller.With(t.Context(), caller.Caller{AgentID: reviewer.ID})
	_, err := svc.OpenReviewRound(asReviewer, connect.NewRequest(&agentv1.OpenReviewRoundRequest{
		AgentId: worker.ID,
	}))
	require.NoError(t, err)

	standing := func() agentv1.ReviewTreeStanding {
		listed, err := svc.ListAgents(t.Context(), connect.NewRequest(&agentv1.ListAgentsRequest{}))
		require.NoError(t, err)

		for _, row := range listed.Msg.GetAgents() {
			if agent := row.GetAgent(); agent.GetName() == "worker" {
				return row.GetLocalReviewTree()
			}
		}

		return agentv1.ReviewTreeStanding_REVIEW_TREE_STANDING_UNSPECIFIED
	}

	assert.Equal(t, agentv1.ReviewTreeStanding_REVIEW_TREE_STANDING_UNSPECIFIED, standing(),
		"a round nobody has concluded on is nothing to stand against")

	_, err = svc.ReleaseReview(asReviewer, connect.NewRequest(&agentv1.ReleaseReviewRequest{
		AgentId: worker.ID,
		Verdict: agentv1.ReviewVerdict_REVIEW_VERDICT_APPROVED,
	}))
	require.NoError(t, err)

	assert.Equal(t, agentv1.ReviewTreeStanding_REVIEW_TREE_STANDING_LANDED_MOVED, standing())
}

// TestListedAgentsCarryTheirReviewRounds is the wiring the panel draws its loop mark from: a
// released review is counted against the agent whose work it was given on, and a review still
// being written is counted against nobody.
func TestListedAgentsCarryTheirReviewRounds(t *testing.T) {
	svc, st, _ := reviewService(t)
	worker := st.SeedAgent(t, agentstore.Agent{
		Name:  "worker",
		Cwd:   "/repo",
		State: agentstore.StateIdle,
	})
	reviewer := st.SeedAgent(t, agentstore.Agent{
		Name:          "reviewer",
		Cwd:           "/repo",
		State:         agentstore.StateIdle,
		Role:          agentstore.RoleReviewer,
		ParentAgentID: &worker.ID,
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

	counts := func() map[string]int32 {
		listed, err := svc.ListAgents(t.Context(), connect.NewRequest(&agentv1.ListAgentsRequest{}))
		require.NoError(t, err)

		rounds := make(map[string]int32, len(listed.Msg.GetAgents()))
		for _, row := range listed.Msg.GetAgents() {
			rounds[row.GetAgent().GetName()] = row.GetLocalReviewRounds()
		}

		return rounds
	}

	assert.Zero(t, counts()["worker"], "a review nobody has released is nothing yet")

	_, err = svc.ReleaseReview(asReviewer, connect.NewRequest(&agentv1.ReleaseReviewRequest{
		AgentId: worker.ID,
		Verdict: agentv1.ReviewVerdict_REVIEW_VERDICT_CHANGES_REQUESTED,
	}))
	require.NoError(t, err)

	assert.Equal(t, int32(1), counts()["worker"])
	assert.Zero(t, counts()["reviewer"], "the reviewer holds no diff of its own to have gone round")
}
