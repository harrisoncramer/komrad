package agent

import (
	"testing"

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
	released, err := svc.CreateReviewThread(asHuman, connect.NewRequest(&agentv1.CreateReviewThreadRequest{
		AgentId:     worker.ID,
		FilePath:    "greet.go",
		NewStart:    41,
		NewEnd:      41,
		QuotedLines: []string{"+\tgreet(nil)"},
		Body:        "this needs a nil check",
	}))
	require.NoError(t, err)

	_, err = svc.ReleaseReviewMessages(asHuman, connect.NewRequest(&agentv1.ReleaseReviewMessagesRequest{
		AgentId: worker.ID,
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
		Threads:  reviews,
		Messages: reviews,
		Viewed:   reviews,
		Agents:   st.Agents,
		Mailer:   mail,
	})

	return NewService(NewServiceParams{
		Agents: st.Agents,
		Mail:   mail,
		Review: review,
		Runs:   st.Runs,
	}), st
}
