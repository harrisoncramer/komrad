package agents

import (
	"context"
	"testing"
	"time"

	agentstore "weave/internal/store/agent"
	messagestore "weave/internal/store/message"

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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mailbox := &fakeMailbox{pending: tt.pending}
			svc := NewMessageService(NewMessageServiceParams{
				MessageReadWriter: mailbox,
				AgentFinder:       newFakeDirectory(),
				DeadlineReader:    fakeDeadline{},
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
		AgentFinder:       newFakeDirectory(),
		DeadlineReader:    fakeDeadline{},
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
		AgentFinder:       newFakeDirectory(),
		DeadlineReader:    fakeDeadline{},
	})

	_, err := svc.SendMessage(t.Context(), SendMessageParams{ToAgentID: "agent-b", Body: "hello"})
	require.NoError(t, err)
	require.Len(t, mailbox.sent, 1)
	assert.Nil(t, mailbox.sent[0].FromAgentID)
	assert.Empty(t, mailbox.sent[0].FromName)
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
				AgentFinder:       newFakeDirectory(),
				DeadlineReader:    fakeDeadline{},
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

type fakeDeadline struct{}

func (fakeDeadline) GetDeliveryDeadline() (time.Duration, error) {
	return time.Hour, nil
}

type fakeMailbox struct {
	sent       []messagestore.SendParams
	pending    int64
	claimed    []messagestore.Message
	claimLimit int32
}

func (f *fakeMailbox) Send(_ context.Context, params messagestore.SendParams) (*messagestore.Message, error) {
	f.sent = append(f.sent, params)

	return &messagestore.Message{
		ID:          "message-sent",
		FromAgentID: params.FromAgentID,
		FromName:    params.FromName,
		ToAgentID:   params.ToAgentID,
		Body:        params.Body,
		ReplyTo:     params.ReplyTo,
	}, nil
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
		return nil, nil
	}

	return &found, nil
}
