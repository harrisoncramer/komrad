package mail

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

// TestSendAndClaim pins what a claim returns and what it leaves behind for each shape of
// mailbox, from an empty one to one holding more than the limit asks for.
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
					Deadline:    time.Hour,
				})
				require.NoError(t, err)
			}

			claimed, err := st.Claim(ctx, recipient, tt.limit)
			require.NoError(t, err)

			var bodies []string
			for _, item := range claimed {
				bodies = append(bodies, item.Body)
				assert.NotNil(t, item.ClaimedAt)
				assert.Equal(t, "sender", item.FromName)
			}
			assert.Equal(t, tt.wantFirst, bodies)

			left, err := st.CountPending(ctx, recipient)
			require.NoError(t, err)
			assert.Equal(t, tt.wantLeft, left)
		})
	}
}

// TestClaimTakesEachMailOnce pins that claiming empties the mailbox, so a second call is
// not a second delivery.
func TestClaimTakesEachMailOnce(t *testing.T) {
	st := open(t)
	ctx := t.Context()
	recipient := seedAgent(t, st, "recipient")

	_, err := st.Send(ctx, SendParams{
		ToAgentID: recipient,
		Body:      "only once",
		Deadline:  time.Hour,
	})
	require.NoError(t, err)

	first, err := st.Claim(ctx, recipient, 10)
	require.NoError(t, err)
	require.Len(t, first, 1)

	second, err := st.Claim(ctx, recipient, 10)
	require.NoError(t, err)
	assert.Empty(t, second)
}

// TestClaimIgnoresOtherMailboxes pins that a claim reaches only the mail addressed to that
// agent, leaving anybody else's where it is.
func TestClaimIgnoresOtherMailboxes(t *testing.T) {
	st := open(t)
	ctx := t.Context()
	mine := seedAgent(t, st, "mine")
	theirs := seedAgent(t, st, "theirs")

	_, err := st.Send(ctx, SendParams{
		ToAgentID: theirs,
		Body:      "not for you",
		Deadline:  time.Hour,
	})
	require.NoError(t, err)

	claimed, err := st.Claim(ctx, mine, 10)
	require.NoError(t, err)
	assert.Empty(t, claimed)

	left, err := st.CountPending(ctx, theirs)
	require.NoError(t, err)
	assert.Equal(t, int64(1), left)
}

// TestClaimToleratesAnIDNoColumnCouldHold pins that an id the uuid column could not hold
// reads as an empty mailbox rather than as a database error.
func TestClaimToleratesAnIDNoColumnCouldHold(t *testing.T) {
	st := open(t)

	claimed, err := st.Claim(t.Context(), "not-an-id", 10)
	require.NoError(t, err)
	assert.Empty(t, claimed)

	left, err := st.CountPending(t.Context(), "not-an-id")
	require.NoError(t, err)
	assert.Zero(t, left)
}

// TestReplyToThreadsMail pins that a reply records the mail it answers, which is what makes
// the two read as one thread.
func TestReplyToThreadsMail(t *testing.T) {
	st := open(t)
	ctx := t.Context()
	asker := seedAgent(t, st, "asker")
	answerer := seedAgent(t, st, "answerer")

	asked, err := st.Send(ctx, SendParams{
		FromAgentID: &asker,
		ToAgentID:   answerer,
		Body:        "which column?",
		Deadline:    time.Hour,
	})
	require.NoError(t, err)

	answered, err := st.Send(ctx, SendParams{
		FromAgentID: &answerer,
		ToAgentID:   asker,
		Body:        "refunded_cents",
		ReplyTo:     &asked.ID,
		Deadline:    time.Hour,
	})
	require.NoError(t, err)
	require.NotNil(t, answered.ReplyTo)
	assert.Equal(t, asked.ID, *answered.ReplyTo)
}

// TestGetByID pins which lookups find mail and which come back as not found, an id the
// column could not hold included.
func TestGetByID(t *testing.T) {
	st := open(t)
	ctx := t.Context()
	recipient := seedAgent(t, st, "recipient")

	tests := []struct {
		name   string
		lookup func(sent Mail) string
		wantOK bool
	}{
		{
			name:   "an id that is not well-formed",
			lookup: func(Mail) string { return "not-an-id" },
		},
		{
			name:   "a well-formed id nothing holds",
			lookup: func(Mail) string { return uuid.NewString() },
		},
		{
			name:   "mail that was sent",
			lookup: func(sent Mail) string { return sent.ID },
			wantOK: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sent, err := st.Send(ctx, SendParams{
				ToAgentID: recipient,
				Body:      "which column?",
				Deadline:  time.Hour,
			})
			require.NoError(t, err)

			got, err := st.GetByID(ctx, tt.lookup(*sent))
			if !tt.wantOK {
				require.ErrorIs(t, err, store.ErrNotFound)

				return
			}

			require.NoError(t, err)
			require.NotNil(t, got)
			assert.Equal(t, sent.ID, got.ID)
		})
	}
}

// TestADeadlineIsMeasuredOnTheDatabaseClock pins that expires_at is compared against the
// database's NOW(), since one counted from the caller's clock holds only to within the drift.
func TestADeadlineIsMeasuredOnTheDatabaseClock(t *testing.T) {
	st := open(t)
	ctx := t.Context()
	recipient := seedAgent(t, st, "recipient")

	sent, err := st.Send(ctx, SendParams{
		ToAgentID: recipient,
		Body:      "still live",
		Deadline:  time.Hour,
	})
	require.NoError(t, err)

	assert.Equal(t, time.Hour, sent.ExpiresAt.Sub(sent.CreatedAt))
}

// TestMailPastItsDeadlineTheInstantItIsSentIsAlreadyExpired pins that a deadline below the
// database's microsecond resolution is nothing rather than almost nothing.
func TestMailPastItsDeadlineTheInstantItIsSentIsAlreadyExpired(t *testing.T) {
	st := open(t)
	ctx := t.Context()
	recipient := seedAgent(t, st, "recipient")

	_, err := st.Send(ctx, SendParams{
		ToAgentID: recipient,
		Body:      "too late",
		Deadline:  time.Nanosecond,
	})
	require.NoError(t, err)

	pending, err := st.CountPending(ctx, recipient)
	require.NoError(t, err)
	assert.Zero(t, pending)
}

// TestExpiredMailIsNeitherDeliveredNorRead pins that mail past its deadline is neither
// claimed nor counted as waiting.
func TestExpiredMailIsNeitherDeliveredNorRead(t *testing.T) {
	st := open(t)
	ctx := t.Context()
	recipient := seedAgent(t, st, "recipient")

	_, err := st.Send(ctx, SendParams{
		ToAgentID: recipient,
		Body:      "too late",
		Deadline:  -time.Minute,
	})
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

// TestExpiredMailIsNotListedAgainOnceReported pins that listing expired mail marks it, so
// the sender is told once rather than on every sweep.
func TestExpiredMailIsNotListedAgainOnceReported(t *testing.T) {
	st := open(t)
	ctx := t.Context()
	recipient := seedAgent(t, st, "recipient")

	_, err := st.Send(ctx, SendParams{
		ToAgentID: recipient,
		Body:      "too late",
		Deadline:  -time.Minute,
	})
	require.NoError(t, err)

	first, err := st.ListExpired(ctx, recipient)
	require.NoError(t, err)
	require.Len(t, first, 1, "the mail is expired and unclaimed, so it is due to be reported")

	second, err := st.ListExpired(ctx, recipient)
	require.NoError(t, err)
	assert.Empty(t, second, "mail already reported must not be reported again")
}

// TestListPendingMailboxesIncludesAMailboxWhoseOnlyMailHasExpired pins that expiry is what
// puts a mailbox into the sweep, since the expiry is the thing to report.
func TestListPendingMailboxesIncludesAMailboxWhoseOnlyMailHasExpired(t *testing.T) {
	st := open(t)
	ctx := t.Context()
	recipient := seedAgent(t, st, "recipient")

	_, err := st.Send(ctx, SendParams{
		ToAgentID: recipient,
		Body:      "too late",
		Deadline:  -time.Minute,
	})
	require.NoError(t, err)

	mailboxes, err := st.ListPendingMailboxes(ctx)
	require.NoError(t, err)
	assert.Contains(t, mailboxes, recipient)
}

// TestListPendingMailboxesDropsAMailboxOnceItsExpiredMailIsReported pins that a mailbox
// leaves the sweep once there is nothing left in it to report.
func TestListPendingMailboxesDropsAMailboxOnceItsExpiredMailIsReported(t *testing.T) {
	st := open(t)
	ctx := t.Context()
	recipient := seedAgent(t, st, "recipient")

	_, err := st.Send(ctx, SendParams{
		ToAgentID: recipient,
		Body:      "too late",
		Deadline:  -time.Minute,
	})
	require.NoError(t, err)

	expired, err := st.ListExpired(ctx, recipient)
	require.NoError(t, err)
	require.Len(t, expired, 1)

	mailboxes, err := st.ListPendingMailboxes(ctx)
	require.NoError(t, err)
	assert.NotContains(t, mailboxes, recipient, "a mailbox whose only mail was already reported should not keep coming back to the sweep")
}

// TestListPendingMailboxesKeepsAMailboxWithLiveMailAfterItsExpiredMailIsReported pins that
// reporting one expiry does not take a mailbox still holding live mail out of the sweep.
func TestListPendingMailboxesKeepsAMailboxWithLiveMailAfterItsExpiredMailIsReported(t *testing.T) {
	st := open(t)
	ctx := t.Context()
	recipient := seedAgent(t, st, "recipient")

	_, err := st.Send(ctx, SendParams{
		ToAgentID: recipient,
		Body:      "too late",
		Deadline:  -time.Minute,
	})
	require.NoError(t, err)
	_, err = st.Send(ctx, SendParams{
		ToAgentID: recipient,
		Body:      "still live",
		Deadline:  time.Hour,
	})
	require.NoError(t, err)

	expired, err := st.ListExpired(ctx, recipient)
	require.NoError(t, err)
	require.Len(t, expired, 1)

	mailboxes, err := st.ListPendingMailboxes(ctx)
	require.NoError(t, err)
	assert.Contains(t, mailboxes, recipient, "the mailbox still holds live mail, so it must stay in the sweep")
}

// TestListUnreadMailboxesExcludesExpiredMail pins that mail nobody can read any more does
// not make a mailbox look unread.
func TestListUnreadMailboxesExcludesExpiredMail(t *testing.T) {
	st := open(t)
	ctx := t.Context()
	recipient := seedAgent(t, st, "recipient")

	_, err := st.Send(ctx, SendParams{
		ToAgentID: recipient,
		Body:      "too late",
		Deadline:  -time.Minute,
	})
	require.NoError(t, err)

	mailboxes, err := st.ListUnreadMailboxes(ctx)
	require.NoError(t, err)
	assert.NotContains(t, mailboxes, recipient, "expired mail cannot be read, so it does not count as unread")
}

// TestListUnreadMailboxesExcludesClaimedMail pins that mail already taken stops counting as
// unread.
func TestListUnreadMailboxesExcludesClaimedMail(t *testing.T) {
	st := open(t)
	ctx := t.Context()
	recipient := seedAgent(t, st, "recipient")

	_, err := st.Send(ctx, SendParams{
		ToAgentID: recipient,
		Body:      "already read",
		Deadline:  time.Hour,
	})
	require.NoError(t, err)
	_, err = st.Claim(ctx, recipient, 10)
	require.NoError(t, err)

	mailboxes, err := st.ListUnreadMailboxes(ctx)
	require.NoError(t, err)
	assert.NotContains(t, mailboxes, recipient)
}

// TestListUnreadMailboxesIncludesLiveUnclaimedMail pins that mail still waiting to be taken
// is what makes a mailbox unread.
func TestListUnreadMailboxesIncludesLiveUnclaimedMail(t *testing.T) {
	st := open(t)
	ctx := t.Context()
	recipient := seedAgent(t, st, "recipient")

	_, err := st.Send(ctx, SendParams{
		ToAgentID: recipient,
		Body:      "still live",
		Deadline:  time.Hour,
	})
	require.NoError(t, err)

	mailboxes, err := st.ListUnreadMailboxes(ctx)
	require.NoError(t, err)
	assert.Contains(t, mailboxes, recipient)
}

// TestListPage pins that a listing is newest first, is scoped to one mailbox, and shows
// claimed mail rather than dropping it.
func TestListPage(t *testing.T) {
	st := open(t)
	ctx := t.Context()
	recipient := seedAgent(t, st, "recipient")
	other := seedAgent(t, st, "other")

	_, err := st.Send(ctx, SendParams{
		ToAgentID: other,
		Body:      "not for you",
		Deadline:  time.Hour,
	})
	require.NoError(t, err)

	first, err := st.Send(ctx, SendParams{
		ToAgentID: recipient,
		Body:      "first",
		Deadline:  time.Hour,
	})
	require.NoError(t, err)
	second, err := st.Send(ctx, SendParams{
		ToAgentID: recipient,
		Body:      "second",
		Deadline:  time.Hour,
	})
	require.NoError(t, err)

	_, err = st.Claim(ctx, recipient, 10)
	require.NoError(t, err)

	page, err := st.ListPage(ctx, recipient, 10, nil)
	require.NoError(t, err)
	require.Len(t, page, 2, "claiming does not remove mail from the listing")
	assert.Equal(t, second.ID, page[0].ID, "newest first")
	assert.Equal(t, first.ID, page[1].ID)
	assert.NotNil(t, page[0].ClaimedAt)
	assert.NotNil(t, page[1].ClaimedAt)
}

// TestListPageCursorTakesTheNextPage pins that the cursor from one page starts the next
// where the first left off.
func TestListPageCursorTakesTheNextPage(t *testing.T) {
	st := open(t)
	ctx := t.Context()
	recipient := seedAgent(t, st, "recipient")

	var sent []*Mail
	for _, body := range []string{"first", "second", "third"} {
		mail, err := st.Send(ctx, SendParams{
			ToAgentID: recipient,
			Body:      body,
			Deadline:  time.Hour,
		})
		require.NoError(t, err)
		sent = append(sent, mail)
	}

	firstPage, err := st.ListPage(ctx, recipient, 2, nil)
	require.NoError(t, err)
	require.Len(t, firstPage, 2)
	assert.Equal(t, sent[2].ID, firstPage[0].ID)
	assert.Equal(t, sent[1].ID, firstPage[1].ID)

	secondPage, err := st.ListPage(ctx, recipient, 2, &Cursor{ID: firstPage[1].ID})
	require.NoError(t, err)
	require.Len(t, secondPage, 1)
	assert.Equal(t, sent[0].ID, secondPage[0].ID)
}

// TestListPageIgnoresOtherMailboxes pins that a listing reaches only the mail addressed to
// that agent.
func TestListPageIgnoresOtherMailboxes(t *testing.T) {
	st := open(t)
	ctx := t.Context()
	mine := seedAgent(t, st, "mine")
	theirs := seedAgent(t, st, "theirs")

	_, err := st.Send(ctx, SendParams{
		ToAgentID: theirs,
		Body:      "not for you",
		Deadline:  time.Hour,
	})
	require.NoError(t, err)

	page, err := st.ListPage(ctx, mine, 10, nil)
	require.NoError(t, err)
	assert.Empty(t, page)
}

// TestListSentPage pins the other half of an agent's correspondence, which is read by sender
// rather than by recipient and is what the panel draws a thread's outgoing side from.
func TestListSentPage(t *testing.T) {
	st := open(t)
	ctx := t.Context()
	sender := seedAgent(t, st, "sender")
	recipient := seedAgent(t, st, "recipient")
	other := seedAgent(t, st, "other")

	_, err := st.Send(ctx, SendParams{
		FromAgentID: &other,
		FromName:    "other",
		ToAgentID:   recipient,
		Body:        "not from you",
		Deadline:    time.Hour,
	})
	require.NoError(t, err)

	_, err = st.Send(ctx, SendParams{
		ToAgentID: sender,
		Body:      "addressed to you rather than from you",
		Deadline:  time.Hour,
	})
	require.NoError(t, err)

	var sent []*Mail
	for _, body := range []string{"first", "second", "third"} {
		mail, err := st.Send(ctx, SendParams{
			FromAgentID: &sender,
			FromName:    "sender",
			ToAgentID:   recipient,
			Body:        body,
			Deadline:    time.Hour,
		})
		require.NoError(t, err)
		sent = append(sent, mail)
	}

	page, err := st.ListSentPage(ctx, sender, 10, nil)
	require.NoError(t, err)
	require.Len(t, page, 3, "the mail this agent sent, and nothing addressed to it")
	assert.Equal(t, sent[2].ID, page[0].ID, "newest first")
	assert.Equal(t, sent[0].ID, page[2].ID)
}

// TestListSentPageCursorTakesTheNextPage pins that the sent listing pages the way the received
// one does, since it carries a keyset cursor of its own.
func TestListSentPageCursorTakesTheNextPage(t *testing.T) {
	st := open(t)
	ctx := t.Context()
	sender := seedAgent(t, st, "sender")
	recipient := seedAgent(t, st, "recipient")

	var sent []*Mail
	for _, body := range []string{"first", "second", "third"} {
		mail, err := st.Send(ctx, SendParams{
			FromAgentID: &sender,
			FromName:    "sender",
			ToAgentID:   recipient,
			Body:        body,
			Deadline:    time.Hour,
		})
		require.NoError(t, err)
		sent = append(sent, mail)
	}

	firstPage, err := st.ListSentPage(ctx, sender, 2, nil)
	require.NoError(t, err)
	require.Len(t, firstPage, 2, "the limit is what it asked for")
	assert.Equal(t, sent[2].ID, firstPage[0].ID)
	assert.Equal(t, sent[1].ID, firstPage[1].ID)

	secondPage, err := st.ListSentPage(ctx, sender, 2, &Cursor{ID: firstPage[1].ID})
	require.NoError(t, err)
	require.Len(t, secondPage, 1)
	assert.Equal(t, sent[0].ID, secondPage[0].ID)
}

// TestListByID pins that only the ids actually holding mail come back, so an id that is
// unknown or malformed is skipped rather than failing the call.
func TestListByID(t *testing.T) {
	st := open(t)
	ctx := t.Context()
	recipient := seedAgent(t, st, "recipient")

	sent, err := st.Send(ctx, SendParams{
		ToAgentID: recipient,
		Body:      "which column?",
		Deadline:  time.Hour,
	})
	require.NoError(t, err)

	found, err := st.ListByID(ctx, []string{sent.ID, uuid.NewString(), "not-an-id"})
	require.NoError(t, err)
	require.Len(t, found, 1, "only the id that holds mail comes back")
	assert.Equal(t, sent.ID, found[0].ID)
}

// TestDelete pins that deleted mail is gone rather than merely marked read.
func TestDelete(t *testing.T) {
	st := open(t)
	ctx := t.Context()
	recipient := seedAgent(t, st, "recipient")

	sent, err := st.Send(ctx, SendParams{
		ToAgentID: recipient,
		Body:      "which column?",
		Deadline:  time.Hour,
	})
	require.NoError(t, err)

	require.NoError(t, st.Delete(ctx, []string{sent.ID}))

	_, err = st.GetByID(ctx, sent.ID)
	require.ErrorIs(t, err, store.ErrNotFound, "deleted mail is gone, not just claimed")
}

// doorbell is what a review doorbell says, which is that there is review to read and nothing
// of the review itself.
const doorbell = "weave: there is review on your diff you have not read, call list_review_threads"

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

	return &fixture{
		Store:    New(p.GetDB()),
		agents:   agentstore.New(p.GetDB()),
		workflow: created.ID,
	}
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

// TestPendingNoticeOfKindIsToldApartByWhatItIsAbout pins the discrimination the exchange cap
// rests on: a notice already waiting about one agent must not stand in for a notice about a
// different one, or a second severed exchange is never reported and the agent waiting on it is
// never told.
func TestPendingNoticeOfKindIsToldApartByWhatItIsAbout(t *testing.T) {
	st := open(t)
	ctx := t.Context()
	recipient := seedAgent(t, st, "recipient")
	one := seedAgent(t, st, "partner-one")
	other := seedAgent(t, st, "partner-other")

	sent, err := st.Send(ctx, SendParams{
		ToAgentID:    recipient,
		Body:         "your exchange with partner-one was cut off",
		Kind:         KindExchangeCapped,
		AboutAgentID: &one,
		Deadline:     time.Hour,
	})
	require.NoError(t, err)
	require.NotNil(t, sent.AboutAgentID)
	assert.Equal(t, one, *sent.AboutAgentID, "the subject round-trips")

	found, err := st.GetPendingOfKind(ctx, recipient, KindExchangeCapped, &one)
	require.NoError(t, err)
	assert.Equal(t, sent.ID, found.ID, "a lookup naming that partner finds it")

	_, err = st.GetPendingOfKind(ctx, recipient, KindExchangeCapped, &other)
	require.ErrorIs(t, err, store.ErrNotFound, "a notice about one partner does not stand in for another")

	_, err = st.GetPendingOfKind(ctx, recipient, KindExchangeCapped, nil)
	require.ErrorIs(t, err, store.ErrNotFound, "nor does it answer a lookup for a notice about nobody")

	doorbell, err := st.Send(ctx, SendParams{
		ToAgentID: recipient,
		Body:      "review activity",
		Kind:      KindReviewActivity,
		Deadline:  time.Hour,
	})
	require.NoError(t, err)

	reused, err := st.GetPendingOfKind(ctx, recipient, KindReviewActivity, nil)
	require.NoError(t, err)
	assert.Equal(t, doorbell.ID, reused.ID, "a notice about nobody is still found by a lookup for one")
}

// TestPendingNoticeOfKindIsTheOneWaiting pins the lookup a caller reuses a doorbell through,
// which finds only a notice of that kind the agent can still read, so a claimed or expired one
// does not stand in for a doorbell that needs raising.
func TestPendingNoticeOfKindIsTheOneWaiting(t *testing.T) {
	st := open(t)
	ctx := t.Context()
	recipient := seedAgent(t, st, "recipient")

	_, err := st.GetPendingOfKind(ctx, recipient, KindReviewActivity, nil)
	require.ErrorIs(t, err, store.ErrNotFound, "an agent holding nothing has no doorbell to reuse")

	_, err = st.Send(ctx, SendParams{
		ToAgentID: recipient,
		Body:      "from a peer",
		Deadline:  time.Hour,
	})
	require.NoError(t, err)

	stale, err := st.Send(ctx, SendParams{
		ToAgentID: recipient,
		Body:      doorbell,
		Kind:      KindReviewActivity,
		Deadline:  -time.Hour,
	})
	require.NoError(t, err)

	_, err = st.GetPendingOfKind(ctx, recipient, KindReviewActivity, nil)
	require.ErrorIs(t, err, store.ErrNotFound, "a doorbell nobody claimed in time is not one the agent can still read")

	live, err := st.Send(ctx, SendParams{
		ToAgentID: recipient,
		Body:      doorbell,
		Kind:      KindReviewActivity,
		Deadline:  time.Hour,
	})
	require.NoError(t, err)

	waiting, err := st.GetPendingOfKind(ctx, recipient, KindReviewActivity, nil)
	require.NoError(t, err)
	require.NotNil(t, waiting)
	assert.Equal(t, live.ID, waiting.ID, "the doorbell to reuse is the deliverable one")
	assert.NotEqual(t, stale.ID, waiting.ID)

	_, err = st.Claim(ctx, recipient, 10)
	require.NoError(t, err)

	_, err = st.GetPendingOfKind(ctx, recipient, KindReviewActivity, nil)
	require.ErrorIs(t, err, store.ErrNotFound, "once the agent has taken it, the next release raises its own")
}

// TestOrdinaryMailIsNeverDeduplicated pins that the dedupe covers only notices weave raises,
// since two agents saying the same thing twice are two things to read.
func TestOrdinaryMailIsNeverDeduplicated(t *testing.T) {
	st := open(t)
	ctx := t.Context()
	recipient := seedAgent(t, st, "recipient")

	for range 3 {
		sent, err := st.Send(ctx, SendParams{
			ToAgentID: recipient,
			Body:      "same thing",
			Deadline:  time.Hour,
		})
		require.NoError(t, err)
		require.NotNil(t, sent)
	}

	pending, err := st.CountPending(ctx, recipient)
	require.NoError(t, err)
	assert.Equal(t, int64(3), pending)
}

// TestNoticeRingsAgainOnceItHasExpired pins that a notice nobody claimed before it ran out
// of time frees its slot, where it used to hold one for the life of the database.
func TestNoticeRingsAgainOnceItHasExpired(t *testing.T) {
	st := open(t)
	ctx := t.Context()
	recipient := seedAgent(t, st, "recipient")

	first, err := st.Send(ctx, SendParams{
		ToAgentID: recipient,
		Body:      "weave: 1 review thread(s) have activity",
		Kind:      KindReviewActivity,
		Deadline:  -time.Hour,
	})
	require.NoError(t, err)
	require.NotNil(t, first)

	pending, err := st.CountPending(ctx, recipient)
	require.NoError(t, err)
	require.Equal(t, int64(0), pending, "mail that ran out of time is not waiting for anyone")

	again, err := st.Send(ctx, SendParams{
		ToAgentID: recipient,
		Body:      "weave: 2 review thread(s) have activity",
		Kind:      KindReviewActivity,
		Deadline:  time.Hour,
	})
	require.NoError(t, err)
	require.NotNil(t, again, "a notice nobody could claim does not stand in for one that can be")

	claimed, err := st.Claim(ctx, recipient, 10)
	require.NoError(t, err)
	require.Len(t, claimed, 1, "the notice that replaced it is the only one, and it is deliverable")
	assert.Equal(t, "weave: 2 review thread(s) have activity", claimed[0].Body)
}

// TestExpiredNoticeIsReportableAgain proves a revived notice can expire a second time and
// still be reported, since ListExpired hands back only what it has not reported before.
func TestExpiredNoticeIsReportableAgain(t *testing.T) {
	st := open(t)
	ctx := t.Context()
	recipient := seedAgent(t, st, "recipient")

	for range 2 {
		sent, err := st.Send(ctx, SendParams{
			ToAgentID: recipient,
			Body:      "weave: review thread(s) have activity",
			Kind:      KindReviewActivity,
			Deadline:  -time.Hour,
		})
		require.NoError(t, err)
		require.NotNil(t, sent)

		expired, err := st.ListExpired(ctx, recipient)
		require.NoError(t, err)
		assert.Len(t, expired, 1)
	}
}

// TestCountExchangedSpansThreadsAndDirections pins what a send is refused by the size of: every
// plain message either agent wrote the other however it was addressed, leaving out weave's own
// notices and anything belonging to another pair.
func TestCountExchangedSpansThreadsAndDirections(t *testing.T) {
	st := open(t)
	ctx := t.Context()
	author := seedAgent(t, st, "author")
	reviewer := seedAgent(t, st, "reviewer")
	stranger := seedAgent(t, st, "stranger")

	send := func(from *string, to string, kind Kind) *Mail {
		t.Helper()
		sent, err := st.Send(ctx, SendParams{
			FromAgentID: from,
			ToAgentID:   to,
			Body:        "hello",
			Kind:        kind,
			Deadline:    time.Hour,
		})
		require.NoError(t, err)

		return sent
	}

	opening := send(&reviewer, author, KindPlain)
	reply, err := st.Send(ctx, SendParams{
		FromAgentID: &author,
		ToAgentID:   reviewer,
		Body:        "fixed",
		Kind:        KindPlain,
		ReplyTo:     &opening.ID,
		Deadline:    time.Hour,
	})
	require.NoError(t, err)
	require.NotNil(t, reply)

	send(&reviewer, author, KindPlain)
	send(nil, author, KindExchangeCapped)
	send(&stranger, author, KindPlain)

	exchanged, err := st.CountExchanged(ctx, author, reviewer)
	require.NoError(t, err)
	assert.EqualValues(t, 3, exchanged, "both directions, replies included, without the notice or the stranger")

	reversed, err := st.CountExchanged(ctx, reviewer, author)
	require.NoError(t, err)
	assert.EqualValues(t, 3, reversed, "the pair is unordered")

	quiet, err := st.CountExchanged(ctx, author, "not-an-id")
	require.NoError(t, err)
	assert.Zero(t, quiet, "an id nothing holds counts nothing rather than failing")
}
