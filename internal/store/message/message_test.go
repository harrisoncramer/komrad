package message

import (
	"testing"
	"time"

	"weave/internal/pgtest"
	"weave/internal/store"
	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSendAndClaim(t *testing.T) {
	tests := []struct {
		name      string
		bodies    []string
		limit     int32
		wantFirst []string
		wantLeft  int64
	}{
		{
			name:     "an empty mailbox claims nothing",
			limit:    10,
			wantLeft: 0,
		},
		{
			name:      "mail comes back oldest first",
			bodies:    []string{"first", "second", "third"},
			limit:     10,
			wantFirst: []string{"first", "second", "third"},
			wantLeft:  0,
		},
		{
			name:      "a limit leaves the rest waiting",
			bodies:    []string{"first", "second", "third"},
			limit:     2,
			wantFirst: []string{"first", "second"},
			wantLeft:  1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := open(t)
			ctx := t.Context()
			sender := seedAgent(t, st, "sender")
			recipient := seedAgent(t, st, "recipient")

			for _, body := range tt.bodies {
				_, err := st.Send(ctx, SendParams{
					FromAgentID: &sender,
					FromName:    "sender",
					ToAgentID:   recipient,
					Body:        body,
					ExpiresAt:   time.Now().Add(time.Hour),
				})
				require.NoError(t, err)
			}

			claimed, err := st.Claim(ctx, recipient, tt.limit)
			require.NoError(t, err)

			var bodies []string
			for _, message := range claimed {
				bodies = append(bodies, message.Body)
				assert.NotNil(t, message.ClaimedAt)
				assert.Equal(t, "sender", message.FromName)
			}
			assert.Equal(t, tt.wantFirst, bodies)

			left, err := st.CountPending(ctx, recipient)
			require.NoError(t, err)
			assert.Equal(t, tt.wantLeft, left)
		})
	}
}

func TestClaimTakesEachMessageOnce(t *testing.T) {
	st := open(t)
	ctx := t.Context()
	recipient := seedAgent(t, st, "recipient")

	_, err := st.Send(ctx, SendParams{ToAgentID: recipient, Body: "only once", ExpiresAt: time.Now().Add(time.Hour)})
	require.NoError(t, err)

	first, err := st.Claim(ctx, recipient, 10)
	require.NoError(t, err)
	require.Len(t, first, 1)

	second, err := st.Claim(ctx, recipient, 10)
	require.NoError(t, err)
	assert.Empty(t, second)
}

func TestClaimIgnoresOtherMailboxes(t *testing.T) {
	st := open(t)
	ctx := t.Context()
	mine := seedAgent(t, st, "mine")
	theirs := seedAgent(t, st, "theirs")

	_, err := st.Send(ctx, SendParams{ToAgentID: theirs, Body: "not for you", ExpiresAt: time.Now().Add(time.Hour)})
	require.NoError(t, err)

	claimed, err := st.Claim(ctx, mine, 10)
	require.NoError(t, err)
	assert.Empty(t, claimed)

	left, err := st.CountPending(ctx, theirs)
	require.NoError(t, err)
	assert.Equal(t, int64(1), left)
}

func TestClaimToleratesAnIDNoColumnCouldHold(t *testing.T) {
	st := open(t)

	claimed, err := st.Claim(t.Context(), "not-an-id", 10)
	require.NoError(t, err)
	assert.Empty(t, claimed)

	left, err := st.CountPending(t.Context(), "not-an-id")
	require.NoError(t, err)
	assert.Zero(t, left)
}

func TestReplyToThreadsAMessage(t *testing.T) {
	st := open(t)
	ctx := t.Context()
	asker := seedAgent(t, st, "asker")
	answerer := seedAgent(t, st, "answerer")

	asked, err := st.Send(ctx, SendParams{FromAgentID: &asker, ToAgentID: answerer, Body: "which column?", ExpiresAt: time.Now().Add(time.Hour)})
	require.NoError(t, err)

	answered, err := st.Send(ctx, SendParams{
		FromAgentID: &answerer,
		ToAgentID:   asker,
		Body:        "refunded_cents",
		ReplyTo:     &asked.ID,
		ExpiresAt:   time.Now().Add(time.Hour),
	})
	require.NoError(t, err)
	require.NotNil(t, answered.ReplyTo)
	assert.Equal(t, asked.ID, *answered.ReplyTo)
}

func TestGetByID(t *testing.T) {
	st := open(t)
	ctx := t.Context()
	recipient := seedAgent(t, st, "recipient")

	tests := []struct {
		name   string
		lookup func(sent Message) string
		wantOK bool
	}{
		{
			name:   "an id that is not well-formed",
			lookup: func(Message) string { return "not-an-id" },
		},
		{
			name:   "a well-formed id nothing holds",
			lookup: func(Message) string { return uuid.NewString() },
		},
		{
			name:   "a message that was sent",
			lookup: func(sent Message) string { return sent.ID },
			wantOK: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sent, err := st.Send(ctx, SendParams{ToAgentID: recipient, Body: "which column?", Hops: 3, ExpiresAt: time.Now().Add(time.Hour)})
			require.NoError(t, err)

			got, err := st.GetByID(ctx, tt.lookup(*sent))
			require.NoError(t, err)
			if !tt.wantOK {
				assert.Nil(t, got)

				return
			}

			require.NotNil(t, got)
			assert.Equal(t, sent.ID, got.ID)
			assert.Equal(t, int32(3), got.Hops)
		})
	}
}

func TestExpiredMailIsNeitherDeliveredNorRead(t *testing.T) {
	st := open(t)
	ctx := t.Context()
	recipient := seedAgent(t, st, "recipient")

	_, err := st.Send(ctx, SendParams{ToAgentID: recipient, Body: "too late", ExpiresAt: time.Now().Add(-time.Minute)})
	require.NoError(t, err)

	claimed, err := st.Claim(ctx, recipient, 10)
	require.NoError(t, err)
	assert.Empty(t, claimed)

	pending, err := st.CountPending(ctx, recipient)
	require.NoError(t, err)
	assert.Zero(t, pending)

	expired, err := st.ListExpired(ctx, recipient)
	require.NoError(t, err)
	require.Len(t, expired, 1)
	assert.Equal(t, "too late", expired[0].Body)
}

func TestExpiredMailIsNotListedAgainOnceReported(t *testing.T) {
	st := open(t)
	ctx := t.Context()
	recipient := seedAgent(t, st, "recipient")

	_, err := st.Send(ctx, SendParams{ToAgentID: recipient, Body: "too late", ExpiresAt: time.Now().Add(-time.Minute)})
	require.NoError(t, err)

	first, err := st.ListExpired(ctx, recipient)
	require.NoError(t, err)
	require.Len(t, first, 1, "the message is expired and unclaimed, so it is due to be reported")

	second, err := st.ListExpired(ctx, recipient)
	require.NoError(t, err)
	assert.Empty(t, second, "a message already reported must not be reported again")
}

func TestListPendingMailboxesIncludesAMailboxWhoseOnlyMailHasExpired(t *testing.T) {
	st := open(t)
	ctx := t.Context()
	recipient := seedAgent(t, st, "recipient")

	_, err := st.Send(ctx, SendParams{ToAgentID: recipient, Body: "too late", ExpiresAt: time.Now().Add(-time.Minute)})
	require.NoError(t, err)

	mailboxes, err := st.ListPendingMailboxes(ctx)
	require.NoError(t, err)
	assert.Contains(t, mailboxes, recipient)
}

func TestListPendingMailboxesDropsAMailboxOnceItsExpiredMailIsReported(t *testing.T) {
	st := open(t)
	ctx := t.Context()
	recipient := seedAgent(t, st, "recipient")

	_, err := st.Send(ctx, SendParams{ToAgentID: recipient, Body: "too late", ExpiresAt: time.Now().Add(-time.Minute)})
	require.NoError(t, err)

	expired, err := st.ListExpired(ctx, recipient)
	require.NoError(t, err)
	require.Len(t, expired, 1)

	mailboxes, err := st.ListPendingMailboxes(ctx)
	require.NoError(t, err)
	assert.NotContains(t, mailboxes, recipient, "a mailbox whose only mail was already reported should not keep coming back to the sweep")
}

func TestListPendingMailboxesKeepsAMailboxWithLiveMailAfterItsExpiredMailIsReported(t *testing.T) {
	st := open(t)
	ctx := t.Context()
	recipient := seedAgent(t, st, "recipient")

	_, err := st.Send(ctx, SendParams{ToAgentID: recipient, Body: "too late", ExpiresAt: time.Now().Add(-time.Minute)})
	require.NoError(t, err)
	_, err = st.Send(ctx, SendParams{ToAgentID: recipient, Body: "still live", ExpiresAt: time.Now().Add(time.Hour)})
	require.NoError(t, err)

	expired, err := st.ListExpired(ctx, recipient)
	require.NoError(t, err)
	require.Len(t, expired, 1)

	mailboxes, err := st.ListPendingMailboxes(ctx)
	require.NoError(t, err)
	assert.Contains(t, mailboxes, recipient, "the mailbox still holds live mail, so it must stay in the sweep")
}

type fixture struct {
	*Store
	agents   *agentstore.Store
	workflow string
}

func open(t *testing.T) *fixture {
	t.Helper()
	p, err := store.Open(t.Context(), pgtest.URL(t))
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Close() })

	workflows := workflowstore.New(p.GetDB())
	created, err := workflows.Create(t.Context(), workflowstore.Config{Name: "default"})
	require.NoError(t, err)

	return &fixture{Store: New(p.GetDB()), agents: agentstore.New(p.GetDB()), workflow: created.ID}
}

func seedAgent(t *testing.T, st *fixture, name string) string {
	t.Helper()
	ctx := t.Context()
	require.NoError(t, st.agents.Create(ctx, agentstore.Agent{
		Name: name, State: agentstore.StateIdle, WorkflowID: st.workflow,
	}))
	got, err := st.agents.GetInWorkflow(ctx, st.workflow, name)
	require.NoError(t, err)
	require.NotNil(t, got)

	return got.ID
}
