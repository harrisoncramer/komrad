package agents

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"weave/config"
	agentstore "weave/internal/store/agent"
	messagestore "weave/internal/store/message"
	"weave/lib/xptr"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSendMessageRefusals(t *testing.T) {
	tests := []struct {
		name    string
		params  SendMessageParams
		pending int64
		wantErr error
	}{
		{
			name:    "an address nothing answers to",
			params:  SendMessageParams{From: "agent-a", ToAgentID: "ghost", Body: "hello"},
			wantErr: ErrRecipientNotFound,
		},
		{
			name:    "a recipient that has been archived",
			params:  SendMessageParams{From: "agent-a", ToAgentID: "agent-archived", Body: "hello"},
			wantErr: ErrAgentArchived,
		},
		{
			name:    "a sender naming an agent that does not exist",
			params:  SendMessageParams{From: "agent-ghost", ToAgentID: "agent-b", Body: "hello"},
			wantErr: ErrSenderUnknown,
		},
		{
			name:    "an agent addressing itself",
			params:  SendMessageParams{From: "agent-a", ToAgentID: "agent-a", Body: "hello"},
			wantErr: ErrSelfAddressed,
		},
		{
			name:    "a recipient already holding a full mailbox",
			params:  SendMessageParams{From: "agent-a", ToAgentID: "agent-b", Body: "hello"},
			pending: maxPendingMessages,
			wantErr: ErrMailboxFull,
		},
		{
			name:    "a reply_to that is not a well-formed id",
			params:  SendMessageParams{From: "agent-a", ToAgentID: "agent-b", Body: "hello", ReplyTo: xptr.Ptr("not-an-id")},
			wantErr: ErrReplyToNotFound,
		},
		{
			name:    "a reply_to naming a well-formed id nothing holds",
			params:  SendMessageParams{From: "agent-a", ToAgentID: "agent-b", Body: "hello", ReplyTo: xptr.Ptr(uuid.NewString())},
			wantErr: ErrReplyToNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mailbox := &fakeMailbox{pending: tt.pending}
			svc := NewMessageService(NewMessageServiceParams{
				MessageReadWriter: mailbox,
				Agents:            newFakeDirectory(),
				Config:            fakeMessageConfig{maxHops: config.DefaultMaxHops},
			})

			sent, err := svc.SendMessage(t.Context(), tt.params)
			require.ErrorIs(t, err, tt.wantErr)
			assert.Nil(t, sent)
			assert.Empty(t, mailbox.sent)
		})
	}
}

func TestSendMessageStampsTheSender(t *testing.T) {
	mailbox := &fakeMailbox{}
	svc := NewMessageService(NewMessageServiceParams{
		MessageReadWriter: mailbox,
		Agents:            newFakeDirectory(),
		Config:            fakeMessageConfig{maxHops: config.DefaultMaxHops},
	})

	sent, err := svc.SendMessage(t.Context(), SendMessageParams{
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

func TestSendMessageFromNobody(t *testing.T) {
	mailbox := &fakeMailbox{}
	svc := NewMessageService(NewMessageServiceParams{
		MessageReadWriter: mailbox,
		Agents:            newFakeDirectory(),
		Config:            fakeMessageConfig{maxHops: config.DefaultMaxHops},
	})

	_, err := svc.SendMessage(t.Context(), SendMessageParams{ToAgentID: "agent-b", Body: "hello"})
	require.NoError(t, err)
	require.Len(t, mailbox.sent, 1)
	assert.Nil(t, mailbox.sent[0].FromAgentID)
	assert.Empty(t, mailbox.sent[0].FromName)
}

func TestSendMessageRefusesPastTheHopLimit(t *testing.T) {
	mailbox := &fakeMailbox{}
	svc := NewMessageService(NewMessageServiceParams{
		MessageReadWriter: mailbox,
		Agents:            newFakeDirectory(),
		Config:            fakeMessageConfig{maxHops: 2},
	})

	first, err := svc.SendMessage(t.Context(), SendMessageParams{From: "agent-a", ToAgentID: "agent-b", Body: "which column?"})
	require.NoError(t, err)
	assert.Equal(t, int32(1), first.Hops)

	second, err := svc.SendMessage(t.Context(), SendMessageParams{
		From: "agent-b", ToAgentID: "agent-a", Body: "refunded_cents", ReplyTo: &first.ID,
	})
	require.NoError(t, err)
	assert.Equal(t, int32(2), second.Hops)

	_, err = svc.SendMessage(t.Context(), SendMessageParams{
		From: "agent-a", ToAgentID: "agent-b", Body: "one more thing", ReplyTo: &second.ID,
	})
	require.ErrorIs(t, err, ErrThreadCapped)

	unrelated, err := svc.SendMessage(t.Context(), SendMessageParams{From: "agent-a", ToAgentID: "agent-b", Body: "unrelated ping"})
	require.NoError(t, err)
	assert.Equal(t, int32(1), unrelated.Hops)
}

// TestSendMessageSystemNoticeIsAlwaysAFreshThread proves, rather than assumes, that a
// lifecycle notice cannot be refused by the hop cap: it carries no reply_to, so it always
// takes hops 1, which clears even the tightest limit a panel could configure.
func TestSendMessageSystemNoticeIsAlwaysAFreshThread(t *testing.T) {
	mailbox := &fakeMailbox{}
	svc := NewMessageService(NewMessageServiceParams{
		MessageReadWriter: mailbox,
		Agents:            newFakeDirectory(),
		Config:            fakeMessageConfig{maxHops: 1},
	})

	sent, err := svc.SendSystemMessage(t.Context(), "agent-a", "your delegate was archived")
	require.NoError(t, err)
	assert.EqualValues(t, 1, sent.Hops)
}

// TestSendSystemMessageStampsWeaveAsTheSender proves the join the framing in hook.go relies
// on: a lifecycle notice lands with weave's name and no agent id, which is what tells the
// stop hook this was not the panel operator talking.
func TestSendSystemMessageStampsWeaveAsTheSender(t *testing.T) {
	mailbox := &fakeMailbox{}
	svc := NewMessageService(NewMessageServiceParams{
		MessageReadWriter: mailbox,
		Agents:            newFakeDirectory(),
		Config:            fakeMessageConfig{maxHops: config.DefaultMaxHops},
	})

	sent, err := svc.SendSystemMessage(t.Context(), "agent-a", "your delegate was archived")
	require.NoError(t, err)
	assert.Equal(t, WeaveSenderName, sent.FromName)
	assert.Nil(t, sent.FromAgentID)
}

func TestClaimMessages(t *testing.T) {
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
			mailbox := &fakeMailbox{claimed: []messagestore.Message{{ID: "message-1", Body: "hello"}}}
			svc := NewMessageService(NewMessageServiceParams{
				MessageReadWriter: mailbox,
				Agents:            newFakeDirectory(),
				Config:            fakeMessageConfig{maxHops: config.DefaultMaxHops},
			})

			claimed, err := svc.ClaimMessages(t.Context(), tt.agentID)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				assert.Empty(t, claimed)

				return
			}

			require.NoError(t, err)
			assert.Len(t, claimed, tt.wantLen)
			assert.Equal(t, int32(maxClaimedMessages), mailbox.claimLimit)
		})
	}
}

type fakeMessageConfig struct {
	maxHops int
}

func (fakeMessageConfig) GetDeliveryDeadline() (time.Duration, error) {
	return time.Hour, nil
}

func (f fakeMessageConfig) GetMaxHops() (int, error) {
	return f.maxHops, nil
}

type fakeMailbox struct {
	sent       []messagestore.SendParams
	pending    int64
	claimed    []messagestore.Message
	claimLimit int32
	byID       map[string]messagestore.Message
}

func (f *fakeMailbox) Send(_ context.Context, params messagestore.SendParams) (*messagestore.Message, error) {
	f.sent = append(f.sent, params)

	sent := messagestore.Message{
		ID:          fmt.Sprintf("message-%d", len(f.sent)),
		FromAgentID: params.FromAgentID,
		FromName:    params.FromName,
		ToAgentID:   params.ToAgentID,
		Body:        params.Body,
		ReplyTo:     params.ReplyTo,
		Hops:        params.Hops,
	}

	if f.byID == nil {
		f.byID = map[string]messagestore.Message{}
	}
	f.byID[sent.ID] = sent

	return &sent, nil
}

func (f *fakeMailbox) GetByID(_ context.Context, id string) (*messagestore.Message, error) {
	found, ok := f.byID[id]
	if !ok {
		return nil, nil
	}

	return &found, nil
}

func (f *fakeMailbox) Claim(_ context.Context, _ string, limit int32) ([]messagestore.Message, error) {
	f.claimLimit = limit

	return f.claimed, nil
}

func (f *fakeMailbox) CountPending(context.Context, string) (int64, error) {
	return f.pending, nil
}

type fakeDirectory struct {
	byID map[string]agentstore.Agent
}

func newFakeDirectory() *fakeDirectory {
	api := agentstore.Agent{ID: "agent-a", Name: "api", Ref: "AAA111"}
	migrations := agentstore.Agent{ID: "agent-b", Name: "migrations", Ref: "BBB222"}
	archivedAt := time.Date(2026, time.August, 4, 9, 0, 0, 0, time.UTC)
	archived := agentstore.Agent{ID: "agent-archived", Name: "old", Ref: "CCC333", ArchivedAt: &archivedAt}

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
		return nil, nil
	}

	return &found, nil
}

func (f *fakeDirectory) Resolve(ctx context.Context, ref string) (*agentstore.Agent, error) {
	if found, err := f.GetByID(ctx, ref); found != nil || err != nil {
		return found, err
	}

	for _, agent := range f.byID {
		if agent.Ref != "" && strings.HasSuffix(agent.Ref, strings.ToUpper(ref)) {
			return &agent, nil
		}
	}

	return nil, nil
}
