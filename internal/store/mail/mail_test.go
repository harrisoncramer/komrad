package mail

import (
	"context"
	"testing"
	"time"

	"komrad/internal/dbtest"
	"komrad/internal/store"
	agentstore "komrad/internal/store/agent"
	"komrad/internal/store/task/tasktest"
	workflowstore "komrad/internal/store/workflow"
	"komrad/lib/xptr"
	"komrad/lib/xslice"
	eventv1 "komrad/proto/gen/event/v1"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
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

			claimed, err := st.claim(ctx, recipient, tt.limit)
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

	first, err := st.claim(ctx, recipient, 10)
	require.NoError(t, err)
	require.Len(t, first, 1)

	second, err := st.claim(ctx, recipient, 10)
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

	claimed, err := st.claim(ctx, mine, 10)
	require.NoError(t, err)
	assert.Empty(t, claimed)

	left, err := st.CountPending(ctx, theirs)
	require.NoError(t, err)
	assert.Equal(t, int64(1), left)
}

// TestGetByIdempotencyKey covers finding mail by the key it was sent with.
func TestGetByIdempotencyKey(t *testing.T) {
	st := open(t)
	ctx := t.Context()
	recipient := seedAgent(t, st, "recipient")

	sent, err := st.Send(ctx, SendParams{
		ToAgentID:      recipient,
		Body:           "check CI",
		IdempotencyKey: xptr.Ptr("schedule:s1:nine"),
		Deadline:       time.Hour,
	})
	require.NoError(t, err)

	t.Run("the key finds the mail sent with it", func(t *testing.T) {
		got, err := st.GetByIdempotencyKey(ctx, "schedule:s1:nine")
		require.NoError(t, err)
		assert.Equal(t, sent.ID, got.ID)
	})

	t.Run("a key nothing was sent with finds nothing", func(t *testing.T) {
		_, err := st.GetByIdempotencyKey(ctx, "schedule:s1:ten")
		require.ErrorIs(t, err, store.ErrNotFound)
	})

	t.Run("a second send with the same key is refused", func(t *testing.T) {
		_, err := st.Send(ctx, SendParams{
			ToAgentID:      recipient,
			Body:           "check CI",
			IdempotencyKey: xptr.Ptr("schedule:s1:nine"),
			Deadline:       time.Hour,
		})
		require.Error(t, err)
	})
}

// TestADeadlineIsMeasuredOnTheDatabaseClock pins that expires_at is compared against the
// database's NOW() rather than the caller's clock.
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

	claimed, err := st.claim(ctx, recipient, 10)
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

// TestListPendingMailboxesIncludesAMailboxWhoseOnlyMailHasExpired pins that expiry is what puts a
// mailbox into the sweep.
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

// TestListPendingMailboxesLeavesOutAnArchivedAgent pins that an archived agent's mailbox stays
// out of the sweep.
func TestListPendingMailboxesLeavesOutAnArchivedAgent(t *testing.T) {
	st := open(t)
	ctx := t.Context()
	recipient := seedAgent(t, st, "recipient")

	_, err := st.Send(ctx, SendParams{
		ToAgentID: recipient,
		Body:      "never read",
		Deadline:  time.Hour,
	})
	require.NoError(t, err)

	_, err = st.agents.Archive(ctx, []uuid.UUID{recipient})
	require.NoError(t, err)

	mailboxes, err := st.ListPendingMailboxes(ctx)
	require.NoError(t, err)
	assert.NotContains(t, mailboxes, recipient, "an archived agent's mailbox should not come back to the sweep every tick")
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

	_, err = st.claim(ctx, recipient, 10)
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

// TestListSentPage pins the other half of an agent's correspondence.
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

// TestListSentPageCursorTakesTheNextPage pins that the sent listing pages the way the received one
// does, on a keyset cursor of its own.
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

type fixture struct {
	*MailStore
	pool     *pgxpool.Pool
	agents   *agentstore.AgentStore
	workflow uuid.UUID
}

func open(t *testing.T) *fixture {
	t.Helper()
	p, err := store.Open(t.Context(), dbtest.URL(t))
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Close() })

	workflows := workflowstore.NewWorkflowStore(p.GetDB())
	created, err := workflows.Create(t.Context(), workflowstore.Config{Name: "default", Worktree: workflowstore.WorktreeConfig{BaseBranch: "main"}})
	require.NoError(t, err)

	return &fixture{
		MailStore: NewMailStore(p.GetDB()),
		pool:      p.GetRawPool(),
		agents:    agentstore.NewAgentStore(p.GetDB()),
		workflow:  created.ID,
	}
}

func seedAgent(t *testing.T, st *fixture, name string) uuid.UUID {
	t.Helper()
	ctx := t.Context()
	taskID, err := tasktest.Seed(ctx, st.pool, st.workflow)
	require.NoError(t, err)
	require.NoError(t, st.agents.Create(ctx, agentstore.Agent{
		TaskID:     taskID,
		Name:       name,
		State:      agentstore.StateIdle,
		WorkflowID: st.workflow,
	}))
	got, err := st.agents.GetInWorkflow(ctx, st.workflow, name)
	require.NoError(t, err)
	require.NotNil(t, got)

	return got.ID
}

// TestKomradMailIsOnePerSubject pins that a recipient holds one unread mail from komrad per kind and subject.
func TestKomradMailIsOnePerSubject(t *testing.T) {
	type write struct {
		kind    Kind
		subject string
		body    string
	}
	tests := []struct {
		name       string
		writes     []write
		claimFirst bool
		wantRows   int
		wantBodies []string
	}{
		{
			name:       "a second mail about the same subject rewrites the first",
			writes:     []write{{KindDocumentEdited, "one", "version 1"}, {KindDocumentEdited, "one", "version 2"}},
			wantRows:   1,
			wantBodies: []string{"version 2"},
		},
		{
			name:       "mail about another subject waits beside it",
			writes:     []write{{KindDocumentEdited, "one", "first"}, {KindDocumentEdited, "other", "second"}},
			wantRows:   2,
			wantBodies: []string{"first", "second"},
		},
		{
			name:       "mail of another kind about the same subject waits beside it",
			writes:     []write{{KindExchangeCapped, "one", "capped"}, {KindReviewActivity, "one", "reviewed"}},
			wantRows:   2,
			wantBodies: []string{"capped", "reviewed"},
		},
		{
			name:       "mail after the first was read is new",
			writes:     []write{{KindDocumentEdited, "one", "read"}, {KindDocumentEdited, "one", "unread"}},
			claimFirst: true,
			wantRows:   1,
			wantBodies: []string{"unread"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := open(t)
			ctx := t.Context()
			recipient := seedAgent(t, st, "recipient")
			subjects := map[string]uuid.UUID{"one": uuid.New(), "other": uuid.New()}

			for i, w := range tt.writes {
				_, err := st.UpsertFromKomrad(ctx, UpsertFromKomradParams{
					FromName:  "komrad",
					ToAgentID: recipient,
					Body:      w.body,
					Kind:      w.kind,
					SubjectID: subjects[w.subject],
					Deadline:  time.Hour,
				})
				require.NoError(t, err)
				if tt.claimFirst && i == 0 {
					_, err := st.claim(ctx, recipient, 10)
					require.NoError(t, err)
				}
			}

			pending, err := st.CountPending(ctx, recipient)
			require.NoError(t, err)
			assert.Equal(t, int64(tt.wantRows), pending)

			claimed, err := st.claim(ctx, recipient, 10)
			require.NoError(t, err)
			assert.Equal(t, tt.wantBodies, xslice.Map(claimed, func(m Mail) string { return m.Body }))
		})
	}
}

// TestGetUnreadAboutFindsOnlyUnreadMail pins that mail the agent read is no longer unread.
func TestGetUnreadAboutFindsOnlyUnreadMail(t *testing.T) {
	st := open(t)
	ctx := t.Context()
	recipient := seedAgent(t, st, "recipient")
	subject := uuid.New()

	_, err := st.GetUnreadAbout(ctx, recipient, KindPullRequestChanged, subject)
	require.ErrorIs(t, err, store.ErrNotFound)

	written, err := st.UpsertFromKomrad(ctx, UpsertFromKomradParams{
		FromName:  "komrad",
		ToAgentID: recipient,
		Body:      "checks failing",
		Kind:      KindPullRequestChanged,
		SubjectID: subject,
		Deadline:  time.Hour,
	})
	require.NoError(t, err)

	found, err := st.GetUnreadAbout(ctx, recipient, KindPullRequestChanged, subject)
	require.NoError(t, err)
	assert.Equal(t, written.ID, found.ID)
	require.NotNil(t, found.SubjectID)
	assert.Equal(t, subject, *found.SubjectID)

	_, err = st.claim(ctx, recipient, 10)
	require.NoError(t, err)
	_, err = st.GetUnreadAbout(ctx, recipient, KindPullRequestChanged, subject)
	require.ErrorIs(t, err, store.ErrNotFound, "read mail is not unread")
}

// TestUnreadMailHeldForRewriteIsNotClaimed pins that delivery skips mail a rewrite is reading until the rewrite commits.
func TestUnreadMailHeldForRewriteIsNotClaimed(t *testing.T) {
	st := open(t)
	ctx := t.Context()
	recipient := seedAgent(t, st, "recipient")
	subject := uuid.New()

	_, err := st.UpsertFromKomrad(ctx, UpsertFromKomradParams{
		FromName:  "komrad",
		ToAgentID: recipient,
		Body:      "checks failing",
		Kind:      KindPullRequestChanged,
		SubjectID: subject,
		Deadline:  time.Hour,
	})
	require.NoError(t, err)

	tx, err := st.pool.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }()

	_, err = NewMailStore(tx).GetUnreadAbout(ctx, recipient, KindPullRequestChanged, subject)
	require.NoError(t, err)

	claimed, err := st.claim(ctx, recipient, 10)
	require.NoError(t, err)
	assert.Empty(t, claimed)

	_, err = NewMailStore(tx).UpsertFromKomrad(ctx, UpsertFromKomradParams{
		FromName:  "komrad",
		ToAgentID: recipient,
		Body:      "checks passing",
		Kind:      KindPullRequestChanged,
		SubjectID: subject,
		Deadline:  time.Hour,
	})
	require.NoError(t, err)
	require.NoError(t, tx.Commit(ctx))

	claimed, err = st.claim(ctx, recipient, 10)
	require.NoError(t, err)
	assert.Equal(t, []string{"checks passing"}, xslice.Map(claimed, func(m Mail) string { return m.Body }))
}

// TestOrdinaryMailIsNeverDeduplicated pins that the dedupe covers only mail komrad writes.
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

// TestExpiredKomradMailComesBackWhenRewritten pins that rewriting expired mail gives it a new deadline.
func TestExpiredKomradMailComesBackWhenRewritten(t *testing.T) {
	st := open(t)
	ctx := t.Context()
	recipient := seedAgent(t, st, "recipient")
	subject := uuid.New()

	first, err := st.UpsertFromKomrad(ctx, UpsertFromKomradParams{
		FromName:  "komrad",
		ToAgentID: recipient,
		Body:      "one",
		Kind:      KindReviewActivity,
		SubjectID: subject,
		Deadline:  -time.Hour,
	})
	require.NoError(t, err)

	pending, err := st.CountPending(ctx, recipient)
	require.NoError(t, err)
	require.Equal(t, int64(0), pending, "mail that ran out of time is not waiting for anyone")

	for range 2 {
		expired, err := st.ListExpired(ctx, recipient)
		require.NoError(t, err)
		require.Len(t, expired, 1, "expired mail is reported, again after each revival")

		again, err := st.UpsertFromKomrad(ctx, UpsertFromKomradParams{
			FromName:  "komrad",
			ToAgentID: recipient,
			Body:      "two",
			Kind:      KindReviewActivity,
			SubjectID: subject,
			Deadline:  time.Hour,
		})
		require.NoError(t, err)
		assert.Equal(t, first.ID, again.ID, "expired mail is brought back rather than blocking new mail")

		_, err = st.UpsertFromKomrad(ctx, UpsertFromKomradParams{
			FromName:  "komrad",
			ToAgentID: recipient,
			Body:      "one",
			Kind:      KindReviewActivity,
			SubjectID: subject,
			Deadline:  -time.Hour,
		})
		require.NoError(t, err)
	}

	_, err = st.UpsertFromKomrad(ctx, UpsertFromKomradParams{
		FromName:  "komrad",
		ToAgentID: recipient,
		Body:      "last",
		Kind:      KindReviewActivity,
		SubjectID: subject,
		Deadline:  time.Hour,
	})
	require.NoError(t, err)
	claimed, err := st.claim(ctx, recipient, 10)
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	assert.Equal(t, "last", claimed[0].Body)
}

// TestCountExchangedSpansThreadsAndDirections pins what a send is refused by the size of: every
// plain message either agent wrote the other however it was addressed, leaving out komrad's own
// mail from komrad and anything belonging to another pair.
func TestCountExchangedSpansThreadsAndDirections(t *testing.T) {
	st := open(t)
	ctx := t.Context()
	author := seedAgent(t, st, "author")
	reviewer := seedAgent(t, st, "reviewer")
	stranger := seedAgent(t, st, "stranger")

	send := func(from *uuid.UUID, to uuid.UUID, kind Kind) *Mail {
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
	assert.EqualValues(t, 3, exchanged, "both directions, replies included, without komrad's mail or the stranger")

	reversed, err := st.CountExchanged(ctx, reviewer, author)
	require.NoError(t, err)
	assert.EqualValues(t, 3, reversed, "the pair is unordered")
}

// TestExistsReadAboutCountsOnlyReadMail pins which mail about a subject counts as the recipient having been told.
func TestExistsReadAboutCountsOnlyReadMail(t *testing.T) {
	st := open(t)
	ctx := t.Context()
	recipient := seedAgent(t, st, "recipient")
	subject := uuid.New()

	_, err := st.UpsertFromKomrad(ctx, UpsertFromKomradParams{
		FromName:  "komrad",
		ToAgentID: recipient,
		Body:      "written",
		Kind:      KindDocumentEdited,
		SubjectID: subject,
		Deadline:  time.Hour,
	})
	require.NoError(t, err)

	read, err := st.ExistsReadAbout(ctx, recipient, KindDocumentEdited, subject)
	require.NoError(t, err)
	assert.False(t, read, "unread mail is not read")

	_, err = st.claim(ctx, recipient, 10)
	require.NoError(t, err)

	read, err = st.ExistsReadAbout(ctx, recipient, KindDocumentEdited, subject)
	require.NoError(t, err)
	assert.True(t, read)

	read, err = st.ExistsReadAbout(ctx, recipient, KindPullRequestChanged, subject)
	require.NoError(t, err)
	assert.False(t, read, "mail of another kind is not about this")

	read, err = st.ExistsReadAbout(ctx, recipient, KindDocumentEdited, uuid.New())
	require.NoError(t, err)
	assert.False(t, read, "mail about another subject is not about this")
}

// TestUpsertFromKomradRewritesTheEvents pins that a rewrite replaces the events mail names.
func TestUpsertFromKomradRewritesTheEvents(t *testing.T) {
	st := open(t)
	ctx := t.Context()
	recipient := seedAgent(t, st, "recipient")
	subject := uuid.New()

	upsert := func(events []eventv1.Event) *Mail {
		t.Helper()
		written, err := st.UpsertFromKomrad(ctx, UpsertFromKomradParams{
			FromName:  "komrad",
			ToAgentID: recipient,
			Body:      "pull request changed",
			Kind:      KindPullRequestChanged,
			SubjectID: subject,
			Events:    events,
			Deadline:  time.Hour,
		})
		require.NoError(t, err)

		return written
	}

	assert.Empty(t, upsert(nil).Events, "mail naming no event stores none")

	upsert([]eventv1.Event{eventv1.Event_EVENT_PULL_REQUEST_CHECKS_FAILING, eventv1.Event_EVENT_PULL_REQUEST_BEHIND})
	found, err := st.GetUnreadAbout(ctx, recipient, KindPullRequestChanged, subject)
	require.NoError(t, err)
	assert.Equal(t, []eventv1.Event{eventv1.Event_EVENT_PULL_REQUEST_CHECKS_FAILING, eventv1.Event_EVENT_PULL_REQUEST_BEHIND}, found.Events)

	upsert([]eventv1.Event{eventv1.Event_EVENT_PULL_REQUEST_BEHIND})
	found, err = st.GetUnreadAbout(ctx, recipient, KindPullRequestChanged, subject)
	require.NoError(t, err)
	assert.Equal(t, []eventv1.Event{eventv1.Event_EVENT_PULL_REQUEST_BEHIND}, found.Events, "a rewrite replaces the events rather than adding to them")
}

// TestClaimNudge pins when an agent may be told about its mail: while it holds unread mail it has
// not been told about.
func TestClaimNudge(t *testing.T) {
	tests := []struct {
		name    string
		arrange func(t *testing.T, st *fixture, recipient uuid.UUID)
		want    bool
	}{
		{
			name: "an empty mailbox",
		},
		{
			name: "mail never told",
			arrange: func(t *testing.T, st *fixture, recipient uuid.UUID) {
				sendNudgeMail(t, st, recipient, time.Hour)
			},
			want: true,
		},
		{
			name: "mail told, with nothing sent since",
			arrange: func(t *testing.T, st *fixture, recipient uuid.UUID) {
				sendNudgeMail(t, st, recipient, time.Hour)
				claimNudge(t, st, recipient)
			},
		},
		{
			name: "mail told, with mail sent since",
			arrange: func(t *testing.T, st *fixture, recipient uuid.UUID) {
				sendNudgeMail(t, st, recipient, time.Hour)
				claimNudge(t, st, recipient)
				sendNudgeMail(t, st, recipient, time.Hour)
			},
			want: true,
		},
		{
			name: "mail told, whose mail sent since was read",
			arrange: func(t *testing.T, st *fixture, recipient uuid.UUID) {
				sendNudgeMail(t, st, recipient, time.Hour)
				claimNudge(t, st, recipient)
				sendNudgeMail(t, st, recipient, time.Hour)
				_, err := st.claim(t.Context(), recipient, 10)
				require.NoError(t, err)
			},
		},
		{
			name: "mail told, whose mail sent since has expired",
			arrange: func(t *testing.T, st *fixture, recipient uuid.UUID) {
				sendNudgeMail(t, st, recipient, time.Hour)
				claimNudge(t, st, recipient)
				sendNudgeMail(t, st, recipient, time.Nanosecond)
			},
		},
		{
			name: "mail whose claim was given back",
			arrange: func(t *testing.T, st *fixture, recipient uuid.UUID) {
				sendNudgeMail(t, st, recipient, time.Hour)
				claimNudge(t, st, recipient)
				require.NoError(t, st.ClearNudge(t.Context(), recipient))
			},
			want: true,
		},
		{
			name: "mail from komrad rewritten since it was told",
			arrange: func(t *testing.T, st *fixture, recipient uuid.UUID) {
				upsert := func() {
					_, err := st.UpsertFromKomrad(t.Context(), UpsertFromKomradParams{
						FromName:  "komrad",
						ToAgentID: recipient,
						Body:      "pull request changed",
						Kind:      KindPullRequestChanged,
						SubjectID: uuid.New(),
						Deadline:  time.Hour,
					})
					require.NoError(t, err)
				}
				upsert()
				claimNudge(t, st, recipient)
				upsert()
			},
			want: true,
		},
		{
			name: "mail told before the recipient launched again",
			arrange: func(t *testing.T, st *fixture, recipient uuid.UUID) {
				sendNudgeMail(t, st, recipient, time.Hour)
				claimNudge(t, st, recipient)
				agent, err := st.agents.GetByID(t.Context(), recipient)
				require.NoError(t, err)
				live, err := st.agents.UpdateIfLive(t.Context(), *agent)
				require.NoError(t, err)
				require.True(t, live)
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := open(t)
			recipient := seedAgent(t, st, "recipient")
			if tt.arrange != nil {
				tt.arrange(t, st, recipient)
			}

			claimed, err := st.ClaimNudge(t.Context(), recipient)
			require.NoError(t, err)
			assert.Equal(t, tt.want, claimed)
		})
	}
}

func claimNudge(t *testing.T, st *fixture, recipient uuid.UUID) {
	t.Helper()

	claimed, err := st.ClaimNudge(t.Context(), recipient)
	require.NoError(t, err)
	require.True(t, claimed)
}

func sendNudgeMail(t *testing.T, st *fixture, recipient uuid.UUID, deadline time.Duration) {
	t.Helper()

	_, err := st.Send(t.Context(), SendParams{
		ToAgentID: recipient,
		Body:      "please read this",
		Deadline:  deadline,
	})
	require.NoError(t, err)
}

// claim takes the waiting mail the way a read of the mailbox does: listed, then claimed by id.
func (st *fixture) claim(ctx context.Context, toAgentID uuid.UUID, limit int32) ([]Mail, error) {
	waiting, err := st.ListWaiting(ctx, toAgentID, limit)
	if err != nil {
		return nil, err
	}

	return st.ClaimByID(ctx, toAgentID, xslice.Map(waiting, func(m Mail) uuid.UUID { return m.ID }))
}

// TestListWaitingLeavesMailForTheClaim pins that a listing claims nothing, that the claim takes only
// the ids it is given, and that mail already claimed is skipped.
func TestListWaitingLeavesMailForTheClaim(t *testing.T) {
	st := open(t)
	ctx := t.Context()
	recipient := seedAgent(t, st, "recipient")

	var ids []uuid.UUID
	for _, body := range []string{"first", "second", "third"} {
		sent, err := st.Send(ctx, SendParams{
			ToAgentID: recipient,
			Body:      body,
			Deadline:  time.Hour,
		})
		require.NoError(t, err)
		ids = append(ids, sent.ID)
	}

	waiting, err := st.ListWaiting(ctx, recipient, 2)
	require.NoError(t, err)
	assert.Equal(t, ids[:2], xslice.Map(waiting, func(m Mail) uuid.UUID { return m.ID }))
	for _, item := range waiting {
		assert.Nil(t, item.ClaimedAt)
	}

	pending, err := st.CountPending(ctx, recipient)
	require.NoError(t, err)
	assert.Equal(t, int64(3), pending, "listing claims nothing")

	claimed, err := st.ClaimByID(ctx, recipient, ids[:2])
	require.NoError(t, err)
	assert.Equal(t, ids[:2], xslice.Map(claimed, func(m Mail) uuid.UUID { return m.ID }))
	for _, item := range claimed {
		assert.NotNil(t, item.ClaimedAt)
	}

	again, err := st.ClaimByID(ctx, recipient, ids)
	require.NoError(t, err)
	assert.Equal(t, ids[2:], xslice.Map(again, func(m Mail) uuid.UUID { return m.ID }), "mail already claimed is skipped")

	none, err := st.ClaimByID(ctx, seedAgent(t, st, "other"), ids)
	require.NoError(t, err)
	assert.Empty(t, none, "another mailbox's ids claim nothing")
}
