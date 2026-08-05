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

func TestDeliver(t *testing.T) {
	idleSince := time.Date(2026, time.August, 4, 12, 0, 0, 0, time.UTC)
	nudgedBefore := idleSince.Add(-time.Minute)
	nudgedAfter := idleSince.Add(time.Minute)
	archivedAt := idleSince

	tests := []struct {
		name      string
		agent     *agentstore.Agent
		pending   int64
		alive     bool
		claimed   bool
		want      Delivery
		wantNudge bool
	}{
		{
			name: "an id no agent holds",
			want: DeliveryDone,
		},
		{
			name:    "an agent archived since the message was sent",
			agent:   &agentstore.Agent{ID: "agent-a", Pane: "%1", State: agentstore.StateIdle, ArchivedAt: &archivedAt},
			pending: 1,
			alive:   true,
			want:    DeliveryDone,
		},
		{
			name:  "an empty mailbox",
			agent: &agentstore.Agent{ID: "agent-a", Pane: "%1", State: agentstore.StateIdle},
			want:  DeliveryDone,
		},
		{
			name:    "an agent mid-turn, which its own stop hook will catch",
			agent:   &agentstore.Agent{ID: "agent-a", Pane: "%1", State: agentstore.StateThinking},
			pending: 1,
			alive:   true,
			want:    DeliverySoon,
		},
		{
			name:    "an agent sitting on a permission prompt is never typed at",
			agent:   &agentstore.Agent{ID: "agent-a", Pane: "%1", State: agentstore.StateWaiting},
			pending: 1,
			alive:   true,
			want:    DeliveryLater,
		},
		{
			name:    "an agent whose pane is gone",
			agent:   &agentstore.Agent{ID: "agent-a", State: agentstore.StateIdle},
			pending: 1,
			want:    DeliveryLater,
		},
		{
			name: "an agent already told about this stretch of sitting idle",
			agent: &agentstore.Agent{
				ID: "agent-a", Pane: "%1", State: agentstore.StateIdle,
				StateChangedAt: idleSince, NudgedAt: &nudgedAfter,
			},
			pending: 1,
			alive:   true,
			want:    DeliveryLater,
		},
		{
			name: "an agent that has gone idle again since it was last told",
			agent: &agentstore.Agent{
				ID: "agent-a", Pane: "%1", State: agentstore.StateIdle,
				StateChangedAt: idleSince, NudgedAt: &nudgedBefore,
			},
			pending:   2,
			alive:     true,
			claimed:   true,
			want:      DeliverySoon,
			wantNudge: true,
		},
		{
			name:      "an idle agent never told before",
			agent:     &agentstore.Agent{ID: "agent-a", Pane: "%1", State: agentstore.StateIdle, StateChangedAt: idleSince},
			pending:   1,
			alive:     true,
			claimed:   true,
			want:      DeliverySoon,
			wantNudge: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agents := &fakeAgents{agent: tt.agent, claimed: tt.claimed}
			panes := &fakePanes{alive: tt.alive}
			svc := NewDeliveryService(NewDeliveryServiceParams{
				MailboxReader: &fakeMailboxReader{pending: tt.pending},
				NudgeClaimer:  agents,
				PaneReader:    panes,
				PaneWriter:    panes,
			})

			got, err := svc.Deliver(t.Context(), "agent-a")
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)

			if !tt.wantNudge {
				assert.Empty(t, panes.typed, "nothing is typed at the agent")

				return
			}

			require.Len(t, panes.typed, 2, "the line and the newline that submits it go separately")
			assert.Contains(t, panes.typed[0], "read_messages")
			assert.Equal(t, "Enter", panes.typed[1])
			assert.True(t, agents.taken, "the right to type is claimed before anything is typed")
		})
	}
}

func TestDeliverReportsExpiredMail(t *testing.T) {
	agents := &fakeAgents{agent: &agentstore.Agent{ID: "agent-a", Pane: "%1", State: agentstore.StateIdle}}
	mailbox := &fakeMailboxReader{expired: []messagestore.Message{{ID: "message-1"}}}
	panes := &fakePanes{alive: true}

	svc := NewDeliveryService(NewDeliveryServiceParams{
		MailboxReader: mailbox,
		NudgeClaimer:  agents,
		PaneReader:    panes,
		PaneWriter:    panes,
	})

	got, err := svc.Deliver(t.Context(), "agent-a")
	require.NoError(t, err)
	assert.Equal(t, DeliveryDone, got)
	assert.True(t, mailbox.readExpired, "an empty mailbox is checked for mail that ran out of time")
	assert.Empty(t, panes.typed)
}

type fakeMailboxReader struct {
	mailboxes   []string
	pending     int64
	expired     []messagestore.Message
	readExpired bool
}

func (f *fakeMailboxReader) ListPendingMailboxes(context.Context) ([]string, error) {
	return f.mailboxes, nil
}

func (f *fakeMailboxReader) CountPending(context.Context, string) (int64, error) {
	return f.pending, nil
}

func (f *fakeMailboxReader) ListExpired(context.Context, string) ([]messagestore.Message, error) {
	f.readExpired = true

	return f.expired, nil
}

type fakeAgents struct {
	agent   *agentstore.Agent
	claimed bool
	taken   bool
}

func (f *fakeAgents) GetByID(context.Context, string) (*agentstore.Agent, error) {
	return f.agent, nil
}

func (f *fakeAgents) ClaimNudge(context.Context, string) (bool, error) {
	f.taken = true

	return f.claimed, nil
}

type fakePanes struct {
	alive bool
	typed []string
}

func (f *fakePanes) IsAgentRunning(agentstore.Agent) bool { return f.alive }

func (f *fakePanes) SendKeys(_ string, keys ...string) error {
	f.typed = append(f.typed, keys...)

	return nil
}

func TestDeliverAllVisitsEveryMailboxHoldingMail(t *testing.T) {
	agents := &fakeAgents{}
	panes := &fakePanes{}
	mailbox := &fakeMailboxReader{mailboxes: []string{"agent-a", "agent-b"}}

	svc := NewDeliveryService(NewDeliveryServiceParams{
		MailboxReader: mailbox,
		NudgeClaimer:  agents,
		PaneReader:    panes,
		PaneWriter:    panes,
	})

	require.NoError(t, svc.DeliverAll(t.Context()))
	assert.Empty(t, panes.typed, "mailboxes belonging to no agent are left alone")
}

func TestDeliverAllDoesNothingWhenNoMailIsWaiting(t *testing.T) {
	agents := &fakeAgents{}
	panes := &fakePanes{}

	svc := NewDeliveryService(NewDeliveryServiceParams{
		MailboxReader: &fakeMailboxReader{},
		NudgeClaimer:  agents,
		PaneReader:    panes,
		PaneWriter:    panes,
	})

	require.NoError(t, svc.DeliverAll(t.Context()))
	assert.False(t, agents.taken)
}
