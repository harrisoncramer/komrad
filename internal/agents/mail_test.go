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

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSendMailRefusals(t *testing.T) {
	tests := []struct {
		name    string
		params  SendMailParams
		pending int64
		wantErr error
	}{
		{
			name:    "an address nothing answers to",
			params:  SendMailParams{From: "agent-a", ToAgentID: "ghost", Body: "hello"},
			wantErr: ErrRecipientNotFound,
		},
		{
			name:    "a recipient that has been archived",
			params:  SendMailParams{From: "agent-a", ToAgentID: "agent-archived", Body: "hello"},
			wantErr: ErrAgentArchived,
		},
		{
			name:    "a sender naming an agent that does not exist",
			params:  SendMailParams{From: "agent-ghost", ToAgentID: "agent-b", Body: "hello"},
			wantErr: ErrSenderUnknown,
		},
		{
			name:    "an agent addressing itself",
			params:  SendMailParams{From: "agent-a", ToAgentID: "agent-a", Body: "hello"},
			wantErr: ErrSelfAddressed,
		},
		{
			name:    "a recipient already holding a full mailbox",
			params:  SendMailParams{From: "agent-a", ToAgentID: "agent-b", Body: "hello"},
			pending: maxPendingMail,
			wantErr: ErrMailboxFull,
		},
		{
			name:    "a reply_to that is not a well-formed id",
			params:  SendMailParams{From: "agent-a", ToAgentID: "agent-b", Body: "hello", ReplyTo: xptr.Ptr("not-an-id")},
			wantErr: ErrReplyToNotFound,
		},
		{
			name:    "a reply_to naming a well-formed id nothing holds",
			params:  SendMailParams{From: "agent-a", ToAgentID: "agent-b", Body: "hello", ReplyTo: xptr.Ptr(uuid.NewString())},
			wantErr: ErrReplyToNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mailbox := &fakeMailbox{pending: tt.pending}
			svc := NewMailService(NewMailServiceParams{
				MailReadWriter: mailbox,
				AgentFinder:    newFakeDirectory(),
				Config:         fakeMailConfig{maxHops: config.DefaultMaxHops},
			})

			sent, err := svc.SendMail(t.Context(), tt.params)
			require.ErrorIs(t, err, tt.wantErr)
			assert.Nil(t, sent)
			assert.Empty(t, mailbox.sent)
		})
	}
}

func TestSendMailStampsTheSender(t *testing.T) {
	mailbox := &fakeMailbox{}
	svc := NewMailService(NewMailServiceParams{
		MailReadWriter: mailbox,
		AgentFinder:    newFakeDirectory(),
		Config:         fakeMailConfig{maxHops: config.DefaultMaxHops},
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

func TestSendMailFromNobody(t *testing.T) {
	mailbox := &fakeMailbox{}
	svc := NewMailService(NewMailServiceParams{
		MailReadWriter: mailbox,
		AgentFinder:    newFakeDirectory(),
		Config:         fakeMailConfig{maxHops: config.DefaultMaxHops},
	})

	_, err := svc.SendMail(t.Context(), SendMailParams{ToAgentID: "agent-b", Body: "hello"})
	require.NoError(t, err)
	require.Len(t, mailbox.sent, 1)
	assert.Nil(t, mailbox.sent[0].FromAgentID)
	assert.Empty(t, mailbox.sent[0].FromName)
}

func TestSendMailRefusesPastTheHopLimit(t *testing.T) {
	mailbox := &fakeMailbox{}
	svc := NewMailService(NewMailServiceParams{
		MailReadWriter: mailbox,
		AgentFinder:    newFakeDirectory(),
		Config:         fakeMailConfig{maxHops: 2},
	})

	first, err := svc.SendMail(t.Context(), SendMailParams{From: "agent-a", ToAgentID: "agent-b", Body: "which column?"})
	require.NoError(t, err)
	assert.Equal(t, int32(1), first.Hops)

	second, err := svc.SendMail(t.Context(), SendMailParams{
		From: "agent-b", ToAgentID: "agent-a", Body: "refunded_cents", ReplyTo: &first.ID,
	})
	require.NoError(t, err)
	assert.Equal(t, int32(2), second.Hops)

	_, err = svc.SendMail(t.Context(), SendMailParams{
		From: "agent-a", ToAgentID: "agent-b", Body: "one more thing", ReplyTo: &second.ID,
	})
	require.ErrorIs(t, err, ErrThreadCapped)

	unrelated, err := svc.SendMail(t.Context(), SendMailParams{From: "agent-a", ToAgentID: "agent-b", Body: "unrelated ping"})
	require.NoError(t, err)
	assert.Equal(t, int32(1), unrelated.Hops)
}

// TestSendMailSystemNoticeIsAlwaysAFreshThread proves, rather than assumes, that a
// lifecycle notice cannot be refused by the hop cap: it carries no reply_to, so it always
// takes hops 1, which clears even the tightest limit a panel could configure.
func TestSendMailSystemNoticeIsAlwaysAFreshThread(t *testing.T) {
	mailbox := &fakeMailbox{}
	svc := NewMailService(NewMailServiceParams{
		MailReadWriter: mailbox,
		AgentFinder:    newFakeDirectory(),
		Config:         fakeMailConfig{maxHops: 1},
	})

	sent, err := svc.SendSystemMail(t.Context(), "agent-a", "your delegate was archived")
	require.NoError(t, err)
	assert.EqualValues(t, 1, sent.Hops)
}

// TestSendSystemMailStampsWeaveAsTheSender proves the join the framing in hook.go relies
// on: a lifecycle notice lands with weave's name and no agent id, which is what tells the
// stop hook this was not the panel operator talking.
func TestSendSystemMailStampsWeaveAsTheSender(t *testing.T) {
	mailbox := &fakeMailbox{}
	svc := NewMailService(NewMailServiceParams{
		MailReadWriter: mailbox,
		AgentFinder:    newFakeDirectory(),
		Config:         fakeMailConfig{maxHops: config.DefaultMaxHops},
	})

	sent, err := svc.SendSystemMail(t.Context(), "agent-a", "your delegate was archived")
	require.NoError(t, err)
	assert.Equal(t, WeaveSenderName, sent.FromName)
	assert.Nil(t, sent.FromAgentID)
}

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
				AgentFinder:    newFakeDirectory(),
				Config:         fakeMailConfig{maxHops: config.DefaultMaxHops},
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
	maxHops int
}

func (fakeMailConfig) GetDeliveryDeadline() (time.Duration, error) {
	return time.Hour, nil
}

func (f fakeMailConfig) GetMaxHops() (int, error) {
	return f.maxHops, nil
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
		ID:          fmt.Sprintf("mail-%d", len(f.sent)),
		FromAgentID: params.FromAgentID,
		FromName:    params.FromName,
		ToAgentID:   params.ToAgentID,
		Body:        params.Body,
		ReplyTo:     params.ReplyTo,
		Hops:        params.Hops,
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

func (f *fakeMailbox) CountPending(context.Context, string) (int64, error) {
	return f.pending, nil
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

func TestListMailChecksTheAgentExists(t *testing.T) {
	mailbox := &fakeMailbox{}
	svc := NewMailService(NewMailServiceParams{
		MailReadWriter: mailbox,
		AgentFinder:    newFakeDirectory(),
		Config:         fakeMailConfig{maxHops: config.DefaultMaxHops},
	})

	_, err := svc.ListMail(t.Context(), "ghost", 10, nil)
	require.ErrorIs(t, err, ErrAgentNotFound)
}

func TestListMailReturnsWhatIsAddressedToTheAgent(t *testing.T) {
	mailbox := &fakeMailbox{byID: map[string]mailstore.Mail{
		"mail-1": {ID: "mail-1", ToAgentID: "agent-a", Body: "for alpha"},
		"mail-2": {ID: "mail-2", ToAgentID: "agent-b", Body: "for beta"},
	}}
	svc := NewMailService(NewMailServiceParams{
		MailReadWriter: mailbox,
		AgentFinder:    newFakeDirectory(),
		Config:         fakeMailConfig{maxHops: config.DefaultMaxHops},
	})

	listed, err := svc.ListMail(t.Context(), "agent-a", 10, nil)
	require.NoError(t, err)
	require.Len(t, listed, 1)
	assert.Equal(t, "for alpha", listed[0].Body)
}

func TestDeleteMailGoesWholeOrNotAtAll(t *testing.T) {
	mailbox := &fakeMailbox{byID: map[string]mailstore.Mail{
		"mail-1": {ID: "mail-1"},
	}}
	svc := NewMailService(NewMailServiceParams{
		MailReadWriter: mailbox,
		AgentFinder:    newFakeDirectory(),
		Config:         fakeMailConfig{maxHops: config.DefaultMaxHops},
	})

	err := svc.DeleteMail(t.Context(), []string{"mail-1", "ghost"})
	require.ErrorIs(t, err, ErrMailNotFound)
	assert.Empty(t, mailbox.deleted, "a batch that is not whole deletes nothing")
	assert.Contains(t, mailbox.byID, "mail-1")
}

func TestDeleteMailDeletesEveryIDGiven(t *testing.T) {
	mailbox := &fakeMailbox{byID: map[string]mailstore.Mail{
		"mail-1": {ID: "mail-1"},
		"mail-2": {ID: "mail-2"},
	}}
	svc := NewMailService(NewMailServiceParams{
		MailReadWriter: mailbox,
		AgentFinder:    newFakeDirectory(),
		Config:         fakeMailConfig{maxHops: config.DefaultMaxHops},
	})

	err := svc.DeleteMail(t.Context(), []string{"mail-1", "mail-2"})
	require.NoError(t, err)
	assert.Empty(t, mailbox.byID)
}

type fakeDirectory struct {
	byID map[string]agentstore.Agent
}

func newFakeDirectory() *fakeDirectory {
	api := agentstore.Agent{ID: "agent-a", Name: "api"}
	migrations := agentstore.Agent{ID: "agent-b", Name: "migrations"}
	archivedAt := time.Date(2026, time.August, 4, 9, 0, 0, 0, time.UTC)
	archived := agentstore.Agent{ID: "agent-archived", Name: "old", ArchivedAt: &archivedAt}

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
