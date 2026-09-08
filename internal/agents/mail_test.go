package agents

import (
	"context"
	"fmt"
	"testing"
	"time"

	"weave/config"
	"weave/internal/store"
	agentstore "weave/internal/store/agent"
	mailstore "weave/internal/store/mail"
	"weave/lib/xptr"
	"weave/lib/xslice"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSendMailRefusals pins which sentinel each way of addressing mail wrongly comes back
// with, from an unknown recipient to one whose mailbox is already full.
func TestSendMailRefusals(t *testing.T) {
	tests := []struct {
		name    string
		params  SendMailParams
		pending int64
		wantErr error
	}{
		{
			name: "an address nothing answers to",
			params: SendMailParams{
				From:      "agent-a",
				ToAgentID: "ghost",
				Body:      "hello",
			},
			wantErr: ErrRecipientNotFound,
		},
		{
			name: "a recipient that has been archived",
			params: SendMailParams{
				From:      "agent-a",
				ToAgentID: "agent-archived",
				Body:      "hello",
			},
			wantErr: ErrAgentArchived,
		},
		{
			name: "a sender naming an agent that does not exist",
			params: SendMailParams{
				From:      "agent-ghost",
				ToAgentID: "agent-b",
				Body:      "hello",
			},
			wantErr: ErrSenderUnknown,
		},
		{
			name: "an agent addressing itself",
			params: SendMailParams{
				From:      "agent-a",
				ToAgentID: "agent-a",
				Body:      "hello",
			},
			wantErr: ErrSelfAddressed,
		},
		{
			name: "a recipient already holding a full mailbox",
			params: SendMailParams{
				From:      "agent-a",
				ToAgentID: "agent-b",
				Body:      "hello",
			},
			pending: maxPendingMail,
			wantErr: ErrMailboxFull,
		},
		{
			name: "a reply_to that is not a well-formed id",
			params: SendMailParams{
				From:      "agent-a",
				ToAgentID: "agent-b",
				Body:      "hello",
				ReplyTo:   xptr.Ptr("not-an-id"),
			},
			wantErr: ErrReplyToNotFound,
		},
		{
			name: "a reply_to naming a well-formed id nothing holds",
			params: SendMailParams{
				From:      "agent-a",
				ToAgentID: "agent-b",
				Body:      "hello",
				ReplyTo:   xptr.Ptr(uuid.NewString()),
			},
			wantErr: ErrReplyToNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mailbox := &fakeMailbox{pending: tt.pending}
			svc := NewMailService(NewMailServiceParams{
				MailReadWriter: mailbox,
				Agents:         newFakeDirectory(),
				Config:         fakeMailConfig{maxExchange: config.DefaultMaxExchange},
			})

			sent, err := svc.SendMail(t.Context(), tt.params)
			require.ErrorIs(t, err, tt.wantErr)
			assert.Nil(t, sent)
			assert.Empty(t, mailbox.sent)
		})
	}
}

// TestSendMailStampsTheSender pins that mail records who sent it, which is what lets the
// recipient reply.
func TestSendMailStampsTheSender(t *testing.T) {
	mailbox := &fakeMailbox{}
	svc := NewMailService(NewMailServiceParams{
		MailReadWriter: mailbox,
		Agents:         newFakeDirectory(),
		Config:         fakeMailConfig{maxExchange: config.DefaultMaxExchange},
	})

	sent, err := svc.SendMail(t.Context(), SendMailParams{
		From:      "agent-a",
		ToAgentID: "agent-b",
		Body:      "which column holds the refund?",
	})
	require.NoError(t, err)
	require.NotNil(t, sent)
	require.Len(t, mailbox.sent, 1)

	wrote := mailbox.sent[0]
	require.NotNil(t, wrote.FromAgentID)
	assert.Equal(t, "agent-a", *wrote.FromAgentID)
	assert.Equal(t, "api", wrote.FromName)
	assert.Equal(t, "agent-b", wrote.ToAgentID)
	assert.Equal(t, "which column holds the refund?", wrote.Body)
}

// TestSendMailFromNobody pins that mail sent by no agent carries no sender rather than an
// invented one.
func TestSendMailFromNobody(t *testing.T) {
	mailbox := &fakeMailbox{}
	svc := NewMailService(NewMailServiceParams{
		MailReadWriter: mailbox,
		Agents:         newFakeDirectory(),
		Config:         fakeMailConfig{maxExchange: config.DefaultMaxExchange},
	})

	_, err := svc.SendMail(t.Context(), SendMailParams{ToAgentID: "agent-b", Body: "hello"})
	require.NoError(t, err)
	require.Len(t, mailbox.sent, 1)
	assert.Nil(t, mailbox.sent[0].FromAgentID)
	assert.Empty(t, mailbox.sent[0].FromName)
}

// TestSendMailRefusesPastTheExchangeCap pins that the cap counts the pair rather than a thread
// of it, so a send with no reply_to is refused exactly like a reply once the two agents have
// written each other as much as the limit allows.
func TestSendMailRefusesPastTheExchangeCap(t *testing.T) {
	mailbox := &fakeMailbox{}
	svc := NewMailService(NewMailServiceParams{
		MailReadWriter: mailbox,
		Agents:         newFakeDirectory(),
		Config:         fakeMailConfig{maxExchange: 2},
	})

	first, err := svc.SendMail(t.Context(), SendMailParams{
		From: "agent-a", ToAgentID: "agent-b", Body: "which column?",
	})
	require.NoError(t, err)

	_, err = svc.SendMail(t.Context(), SendMailParams{
		From: "agent-b", ToAgentID: "agent-a", Body: "refunded_cents", ReplyTo: &first.ID,
	})
	require.NoError(t, err)

	_, err = svc.SendMail(t.Context(), SendMailParams{
		From: "agent-a", ToAgentID: "agent-b", Body: "one more thing", ReplyTo: &first.ID,
	})
	require.ErrorIs(t, err, ErrExchangeCapped)

	_, err = svc.SendMail(t.Context(), SendMailParams{
		From: "agent-a", ToAgentID: "agent-b", Body: "a fresh subject",
	})
	require.ErrorIs(t, err, ErrExchangeCapped, "opening a new thread is not a way around the cap")
}

// exchange sends mail back and forth between two agents, every message opening a thread of its
// own the way a send with no reply_to does.
func exchange(t *testing.T, svc *MailService, one, other string, messages int) {
	t.Helper()

	for at := range messages {
		from, to := one, other
		if at%2 == 1 {
			from, to = other, one
		}

		_, err := svc.SendMail(t.Context(), SendMailParams{
			From:      from,
			ToAgentID: to,
			Body:      "round",
		})
		require.NoError(t, err)
	}
}

// cappedNotices is what a mailbox holds of the kind weave raises when it refuses a send, which
// is how a test tells one notice standing for every refusal after it from a pile of them.
func cappedNotices(mailbox *fakeMailbox) []mailstore.Mail {
	var held []mailstore.Mail
	for _, mail := range mailbox.byID {
		if mail.Kind == mailstore.KindExchangeCapped {
			held = append(held, mail)
		}
	}

	return held
}

// TestSendMailTellsBothSidesWhenTheCapRefusesASend covers what keeps a refusal from stranding
// the work: the sender gets the error, the recipient is told mail meant for it was stopped so it
// is not left waiting on a reply, and a row is marked so the person hears about it.
func TestSendMailTellsBothSidesWhenTheCapRefusesASend(t *testing.T) {
	mailbox := &fakeMailbox{}
	family := newFakeFamily()
	svc := NewMailService(NewMailServiceParams{
		MailReadWriter: mailbox,
		Agents:         family,
		Config:         fakeMailConfig{maxExchange: 4},
	})

	exchange(t, svc, "child-one", "parent", 3)
	assert.Empty(t, family.unviewed, "a pair inside the cap is nobody's business")
	assert.Empty(t, cappedNotices(mailbox))

	exchange(t, svc, "child-one", "parent", 1)

	_, err := svc.SendMail(t.Context(), SendMailParams{
		From: "child-one", ToAgentID: "parent", Body: "one more",
	})
	require.ErrorIs(t, err, ErrExchangeCapped)
	assert.Equal(t, 1, family.unviewed["parent"], "the parent's row is the one marked")

	told := cappedNotices(mailbox)
	require.Len(t, told, 1)
	assert.Equal(t, "parent", told[0].ToAgentID, "the side that would otherwise wait forever")
	assert.Contains(t, told[0].Body, "reviewer")
	assert.Contains(t, told[0].Body, "child-one", "the id, since two workflows may hold one name")
}

// TestSendMailKeepsOneNoticePerUnreadRefusal pins what a retry past the cap costs: the mark goes
// back up, an unread notice stands in for the refusals behind it, and one already read is
// replaced.
func TestSendMailKeepsOneNoticePerUnreadRefusal(t *testing.T) {
	mailbox := &fakeMailbox{}
	family := newFakeFamily()
	svc := NewMailService(NewMailServiceParams{
		MailReadWriter: mailbox,
		Agents:         family,
		Config:         fakeMailConfig{maxExchange: 2},
	})

	exchange(t, svc, "child-one", "parent", 2)

	for range 3 {
		_, err := svc.SendMail(t.Context(), SendMailParams{
			From: "child-one", ToAgentID: "parent", Body: "one more",
		})
		require.ErrorIs(t, err, ErrExchangeCapped)
	}

	assert.Equal(t, 3, family.unviewed["parent"], "every refusal puts the mark back")
	assert.Len(t, cappedNotices(mailbox), 1, "the unread notice stands for the refusals behind it")

	mailbox.claimHeld()

	_, err := svc.SendMail(t.Context(), SendMailParams{
		From: "child-one", ToAgentID: "parent", Body: "one more",
	})
	require.ErrorIs(t, err, ErrExchangeCapped)
	assert.Len(t, cappedNotices(mailbox), 2, "a recipient that has read the notice is told again")
}

// TestSendMailTellsAnAgentAboutEverySeveredPair covers the hole a per-kind notice leaves: an
// agent already holding an unread notice about one partner has to be told about a second one too,
// or the exchange it is still waiting on is severed without it ever hearing so.
func TestSendMailTellsAnAgentAboutEverySeveredPair(t *testing.T) {
	mailbox := &fakeMailbox{}
	family := newFakeFamily()
	svc := NewMailService(NewMailServiceParams{
		MailReadWriter: mailbox,
		Agents:         family,
		Config:         fakeMailConfig{maxExchange: 2},
	})

	exchange(t, svc, "child-one", "parent", 2)
	exchange(t, svc, "child-two", "parent", 2)

	_, err := svc.SendMail(t.Context(), SendMailParams{
		From: "child-one", ToAgentID: "parent", Body: "spent",
	})
	require.ErrorIs(t, err, ErrExchangeCapped)

	_, err = svc.SendMail(t.Context(), SendMailParams{
		From: "child-two", ToAgentID: "parent", Body: "spent too",
	})
	require.ErrorIs(t, err, ErrExchangeCapped)

	told := cappedNotices(mailbox)
	require.Len(t, told, 2, "one notice per severed pair, not one per kind")

	about := xslice.Map(told, func(notice mailstore.Mail) string {
		require.NotNil(t, notice.AboutAgentID)

		return *notice.AboutAgentID
	})
	assert.ElementsMatch(t, []string{"child-one", "child-two"}, about)
}

// TestSendMailCapsEachPairSeparately pins that the cap is spent per pair, so a reviewer that is
// replaced starts with the whole allowance and two children of one parent do not spend each
// other's.
func TestSendMailCapsEachPairSeparately(t *testing.T) {
	mailbox := &fakeMailbox{}
	family := newFakeFamily()
	svc := NewMailService(NewMailServiceParams{
		MailReadWriter: mailbox,
		Agents:         family,
		Config:         fakeMailConfig{maxExchange: 4},
	})

	exchange(t, svc, "child-one", "parent", 4)
	exchange(t, svc, "child-two", "parent", 3)

	_, err := svc.SendMail(t.Context(), SendMailParams{
		From: "child-one", ToAgentID: "parent", Body: "spent",
	})
	require.ErrorIs(t, err, ErrExchangeCapped)

	_, err = svc.SendMail(t.Context(), SendMailParams{
		From: "child-two", ToAgentID: "parent", Body: "still has room",
	})
	require.NoError(t, err)
}

// TestSendMailMarksTheParentOfTwoChildrenThatSpendTheCap pins that the mark reaches the parent
// of a pair it is not itself half of.
func TestSendMailMarksTheParentOfTwoChildrenThatSpendTheCap(t *testing.T) {
	mailbox := &fakeMailbox{}
	family := newFakeFamily()
	svc := NewMailService(NewMailServiceParams{
		MailReadWriter: mailbox,
		Agents:         family,
		Config:         fakeMailConfig{maxExchange: 2},
	})

	exchange(t, svc, "child-one", "child-two", 2)

	_, err := svc.SendMail(t.Context(), SendMailParams{
		From: "child-one", ToAgentID: "child-two", Body: "one more",
	})
	require.ErrorIs(t, err, ErrExchangeCapped)

	assert.Equal(t, 1, family.unviewed["parent"])

	told := cappedNotices(mailbox)
	require.Len(t, told, 1)
	assert.Equal(t, "child-two", told[0].ToAgentID, "the notice still goes to the side that was written to")
}

// TestSendMailCapsAPairThatIsNobodysChildren pins that a pair with no parent over it is capped
// too, marking whichever of the two sent.
func TestSendMailCapsAPairThatIsNobodysChildren(t *testing.T) {
	mailbox := &fakeMailbox{}
	family := newFakeFamily()
	svc := NewMailService(NewMailServiceParams{
		MailReadWriter: mailbox,
		Agents:         family,
		Config:         fakeMailConfig{maxExchange: 2},
	})

	exchange(t, svc, "parent", "other-parent", 2)

	_, err := svc.SendMail(t.Context(), SendMailParams{
		From: "parent", ToAgentID: "other-parent", Body: "one more",
	})
	require.ErrorIs(t, err, ErrExchangeCapped)

	assert.Equal(t, 1, family.unviewed["parent"], "the sender, since there is no parent over it")
}

// TestSendMailLeavesWeavesOwnMailOutOfTheCount pins that a lifecycle notice does not spend the
// pair's allowance.
func TestSendMailLeavesWeavesOwnMailOutOfTheCount(t *testing.T) {
	mailbox := &fakeMailbox{}
	family := newFakeFamily()
	svc := NewMailService(NewMailServiceParams{
		MailReadWriter: mailbox,
		Agents:         family,
		Config:         fakeMailConfig{maxExchange: 4},
	})

	for range 4 {
		_, err := svc.SendSystemMail(t.Context(), "parent", "your delegate was archived")
		require.NoError(t, err)
	}
	exchange(t, svc, "child-one", "parent", 3)

	_, err := svc.SendMail(t.Context(), SendMailParams{
		From: "child-one", ToAgentID: "parent", Body: "still inside the cap",
	})
	require.NoError(t, err)
	assert.Empty(t, family.unviewed)
}

// TestSendMailSystemNoticeIsNeverRefusedByTheCap proves weave can always reach an agent, a
// lifecycle notice carrying no sending agent and so being no half of a pair.
func TestSendMailSystemNoticeIsNeverRefusedByTheCap(t *testing.T) {
	mailbox := &fakeMailbox{}
	svc := NewMailService(NewMailServiceParams{
		MailReadWriter: mailbox,
		Agents:         newFakeDirectory(),
		Config:         fakeMailConfig{maxExchange: 1},
	})

	for range 3 {
		sent, err := svc.SendSystemMail(t.Context(), "agent-a", "your delegate was archived")
		require.NoError(t, err)
		require.NotNil(t, sent)
	}
}

// TestSendSystemMailStampsWeaveAsTheSender proves a lifecycle notice lands with weave's name and
// no agent id, which is what tells the stop hook this was not the panel operator talking.
func TestSendSystemMailStampsWeaveAsTheSender(t *testing.T) {
	mailbox := &fakeMailbox{}
	svc := NewMailService(NewMailServiceParams{
		MailReadWriter: mailbox,
		Agents:         newFakeDirectory(),
		Config:         fakeMailConfig{maxExchange: config.DefaultMaxExchange},
	})

	sent, err := svc.SendSystemMail(t.Context(), "agent-a", "your delegate was archived")
	require.NoError(t, err)
	assert.Equal(t, WeaveSenderName, sent.FromName)
	assert.Nil(t, sent.FromAgentID)
}

// TestClaimMail pins which claims are answered and which are refused, an id no agent holds
// included.
func TestClaimMail(t *testing.T) {
	tests := []struct {
		name    string
		agentID string
		wantErr error
		wantLen int
	}{
		{
			name:    "an id no agent holds",
			agentID: "agent-ghost",
			wantErr: ErrAgentNotFound,
		},
		{
			name:    "an agent reading its own",
			agentID: "agent-b",
			wantLen: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mailbox := &fakeMailbox{claimed: []mailstore.Mail{{ID: "mail-1", Body: "hello"}}}
			svc := NewMailService(NewMailServiceParams{
				MailReadWriter: mailbox,
				Agents:         newFakeDirectory(),
				Config:         fakeMailConfig{maxExchange: config.DefaultMaxExchange},
			})

			claimed, err := svc.ClaimMail(t.Context(), tt.agentID)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				assert.Empty(t, claimed)

				return
			}

			require.NoError(t, err)
			assert.Len(t, claimed, tt.wantLen)
			assert.Equal(t, int32(maxClaimedMail), mailbox.claimLimit)
		})
	}
}

type fakeMailConfig struct {
	maxExchange int
}

func (fakeMailConfig) GetDeliveryDeadline() (time.Duration, error) {
	return time.Hour, nil
}

func (f fakeMailConfig) GetMaxExchange() (int, error) {
	return f.maxExchange, nil
}

type fakeMailbox struct {
	sent       []mailstore.SendParams
	pending    int64
	claimed    []mailstore.Mail
	claimLimit int32
	byID       map[string]mailstore.Mail
	deleted    []string
}

func (f *fakeMailbox) Send(_ context.Context, params mailstore.SendParams) (*mailstore.Mail, error) {
	f.sent = append(f.sent, params)

	sent := mailstore.Mail{
		ID:           fmt.Sprintf("mail-%d", len(f.sent)),
		FromAgentID:  params.FromAgentID,
		FromName:     params.FromName,
		ToAgentID:    params.ToAgentID,
		Body:         params.Body,
		Kind:         params.Kind,
		ReplyTo:      params.ReplyTo,
		AboutAgentID: params.AboutAgentID,
	}

	if f.byID == nil {
		f.byID = map[string]mailstore.Mail{}
	}
	f.byID[sent.ID] = sent

	return &sent, nil
}

func (f *fakeMailbox) GetByID(_ context.Context, id string) (*mailstore.Mail, error) {
	found, ok := f.byID[id]
	if !ok {
		return nil, fmt.Errorf("%w: mail %q", store.ErrNotFound, id)
	}

	return &found, nil
}

func (f *fakeMailbox) Claim(_ context.Context, _ string, limit int32) ([]mailstore.Mail, error) {
	f.claimLimit = limit

	return f.claimed, nil
}

// claimHeld stamps everything the fake holds as read, which is what an agent reading its mailbox
// does to a notice and what stops that notice standing in for the next one.
func (f *fakeMailbox) claimHeld() {
	claimedAt := time.Now()
	for id, held := range f.byID {
		held.ClaimedAt = &claimedAt
		f.byID[id] = held
	}
}

func (f *fakeMailbox) CountPending(context.Context, string) (int64, error) {
	return f.pending, nil
}

func (f *fakeMailbox) GetPendingOfKind(_ context.Context, toAgentID string, kind mailstore.Kind, about *string) (*mailstore.Mail, error) {
	for _, held := range f.byID {
		sameSubject := held.AboutAgentID == nil && about == nil ||
			held.AboutAgentID != nil && about != nil && *held.AboutAgentID == *about
		if held.ToAgentID == toAgentID && held.Kind == kind && sameSubject && held.ClaimedAt == nil {
			return &held, nil
		}
	}

	return nil, fmt.Errorf("%w: an unread %q notice", store.ErrNotFound, kind)
}

func (f *fakeMailbox) ListPage(_ context.Context, toAgentID string, _ int32, _ *mailstore.Cursor) ([]mailstore.Mail, error) {
	var page []mailstore.Mail
	for _, m := range f.byID {
		if m.ToAgentID == toAgentID {
			page = append(page, m)
		}
	}

	return page, nil
}

// ListSentPage answers by sender rather than recipient, which is all this fake owes: the paging
// it ignores is pinned against a real database in internal/store/mail.
func (f *fakeMailbox) ListSentPage(_ context.Context, fromAgentID string, _ int32, _ *mailstore.Cursor) ([]mailstore.Mail, error) {
	var page []mailstore.Mail
	for _, m := range f.byID {
		if m.FromAgentID != nil && *m.FromAgentID == fromAgentID {
			page = append(page, m)
		}
	}

	return page, nil
}

func (f *fakeMailbox) ListByID(_ context.Context, ids []string) ([]mailstore.Mail, error) {
	var found []mailstore.Mail
	for _, id := range ids {
		if m, ok := f.byID[id]; ok {
			found = append(found, m)
		}
	}

	return found, nil
}

func (f *fakeMailbox) Delete(_ context.Context, ids []string) error {
	f.deleted = append(f.deleted, ids...)
	for _, id := range ids {
		delete(f.byID, id)
	}

	return nil
}

func (f *fakeMailbox) ListUnreadMailboxes(context.Context) ([]string, error) {
	var unread []string
	for _, mail := range f.byID {
		if mail.ClaimedAt == nil {
			unread = append(unread, mail.ToAgentID)
		}
	}

	return unread, nil
}

// CountExchanged counts off the mail the fake holds, in both directions and ignoring notices,
// the way the real store's query does.
func (f *fakeMailbox) CountExchanged(_ context.Context, one, other string) (int64, error) {
	var exchanged int64
	for _, mail := range f.byID {
		if mail.Kind != mailstore.KindPlain || mail.FromAgentID == nil {
			continue
		}

		from, to := *mail.FromAgentID, mail.ToAgentID
		if (from == one && to == other) || (from == other && to == one) {
			exchanged++
		}
	}

	return exchanged, nil
}

// TestListMailChecksTheAgentExists pins that listing an unknown mailbox is refused rather
// than answered with nothing.
func TestListMailChecksTheAgentExists(t *testing.T) {
	mailbox := &fakeMailbox{}
	svc := NewMailService(NewMailServiceParams{
		MailReadWriter: mailbox,
		Agents:         newFakeDirectory(),
		Config:         fakeMailConfig{maxExchange: config.DefaultMaxExchange},
	})

	_, err := svc.ListMail(t.Context(), ListMailParams{AgentID: "ghost", Limit: 10})
	require.ErrorIs(t, err, ErrAgentNotFound)
}

// TestListMailReturnsWhatIsAddressedToTheAgent pins that a listing is scoped to one mailbox.
func TestListMailReturnsWhatIsAddressedToTheAgent(t *testing.T) {
	mailbox := &fakeMailbox{byID: map[string]mailstore.Mail{
		"mail-1": {
			ID:        "mail-1",
			ToAgentID: "agent-a",
			Body:      "for alpha",
		},
		"mail-2": {
			ID:        "mail-2",
			ToAgentID: "agent-b",
			Body:      "for beta",
		},
	}}
	svc := NewMailService(NewMailServiceParams{
		MailReadWriter: mailbox,
		Agents:         newFakeDirectory(),
		Config:         fakeMailConfig{maxExchange: config.DefaultMaxExchange},
	})

	listed, err := svc.ListMail(t.Context(), ListMailParams{AgentID: "agent-a", Limit: 10})
	require.NoError(t, err)
	require.Len(t, listed, 1)
	assert.Equal(t, "for alpha", listed[0].Body)
}

// TestDeleteMailGoesWholeOrNotAtAll pins that a batch naming one unknown id deletes nothing
// at all.
func TestDeleteMailGoesWholeOrNotAtAll(t *testing.T) {
	mailbox := &fakeMailbox{byID: map[string]mailstore.Mail{
		"mail-1": {ID: "mail-1"},
	}}
	svc := NewMailService(NewMailServiceParams{
		MailReadWriter: mailbox,
		Agents:         newFakeDirectory(),
		Config:         fakeMailConfig{maxExchange: config.DefaultMaxExchange},
	})

	err := svc.DeleteMail(t.Context(), []string{"mail-1", "ghost"})
	require.ErrorIs(t, err, ErrMailNotFound)
	assert.Empty(t, mailbox.deleted, "a batch that is not whole deletes nothing")
	assert.Contains(t, mailbox.byID, "mail-1")
}

// TestDeleteMailDeletesEveryIDGiven pins that a batch every id of which is known goes whole.
func TestDeleteMailDeletesEveryIDGiven(t *testing.T) {
	mailbox := &fakeMailbox{byID: map[string]mailstore.Mail{
		"mail-1": {ID: "mail-1"},
		"mail-2": {ID: "mail-2"},
	}}
	svc := NewMailService(NewMailServiceParams{
		MailReadWriter: mailbox,
		Agents:         newFakeDirectory(),
		Config:         fakeMailConfig{maxExchange: config.DefaultMaxExchange},
	})

	err := svc.DeleteMail(t.Context(), []string{"mail-1", "mail-2"})
	require.NoError(t, err)
	assert.Empty(t, mailbox.byID)
}

type fakeDirectory struct {
	byID map[string]agentstore.Agent
	// unviewed is the agents whose attention mark has been raised, counted rather than set so
	// a test can tell one raise from several.
	unviewed map[string]int
}

func newFakeDirectory() *fakeDirectory {
	api := agentstore.Agent{ID: "agent-a", Name: "api"}
	migrations := agentstore.Agent{ID: "agent-b", Name: "migrations"}
	archivedAt := time.Date(2026, time.August, 4, 9, 0, 0, 0, time.UTC)
	archived := agentstore.Agent{
		ID:         "agent-archived",
		Name:       "old",
		ArchivedAt: &archivedAt,
	}

	return &fakeDirectory{
		byID: map[string]agentstore.Agent{
			api.ID:        api,
			migrations.ID: migrations,
			archived.ID:   archived,
		},
	}
}

func (f *fakeDirectory) GetByID(_ context.Context, id string) (*agentstore.Agent, error) {
	found, ok := f.byID[id]
	if !ok {
		return nil, fmt.Errorf("%w: agent %q", store.ErrNotFound, id)
	}

	return &found, nil
}

func (f *fakeDirectory) SetViewed(_ context.Context, id string, viewed bool) error {
	if viewed {
		return nil
	}

	if f.unviewed == nil {
		f.unviewed = map[string]int{}
	}
	f.unviewed[id]++

	return nil
}
