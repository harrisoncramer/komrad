package agents

import (
	"context"
	"fmt"
	"testing"
	"time"

	"weave/internal/harness"
	"weave/internal/harness/claude"
	"weave/internal/harness/codex"
	"weave/internal/store"
	agentstore "weave/internal/store/agent"
	mailstore "weave/internal/store/mail"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeliver(t *testing.T) {
	idleSince := time.Date(2026, time.August, 4, 12, 0, 0, 0, time.UTC)
	nudgedBefore := idleSince.Add(-time.Minute)
	nudgedAfter := idleSince.Add(time.Minute)
	archivedAt := idleSince

	seenNow := time.Now()
	seenLongAgo := seenNow.Add(-stateSeenBound - time.Minute)

	tests := []struct {
		name      string
		agent     *agentstore.Agent
		pending   int64
		alive     bool
		inMode    bool
		frame     string
		claimed   bool
		want      Delivery
		wantClaim bool
		wantNudge bool
	}{
		{
			name: "an id no agent holds",
			want: DeliveryDone,
		},
		{
			name: "an agent archived since the mail was sent",
			agent: &agentstore.Agent{
				ID:         "agent-a",
				Pane:       "%1",
				State:      agentstore.StateIdle,
				ArchivedAt: &archivedAt,
			},
			pending: 1,
			alive:   true,
			want:    DeliveryDone,
		},
		{
			name: "an empty mailbox",
			agent: &agentstore.Agent{
				ID:    "agent-a",
				Pane:  "%1",
				State: agentstore.StateIdle,
			},
			want: DeliveryDone,
		},
		{
			name: "an agent mid-turn, which its own stop hook will catch",
			agent: &agentstore.Agent{
				ID:          "agent-a",
				Pane:        "%1",
				State:       agentstore.StateThinking,
				StateSeenAt: seenNow,
			},
			pending: 1,
			alive:   true,
			want:    DeliverySoon,
		},
		{
			name: "an agent mid-turn that no pass has confirmed since",
			agent: &agentstore.Agent{
				ID:          "agent-a",
				Pane:        "%1",
				State:       agentstore.StateThinking,
				StateSeenAt: seenLongAgo,
			},
			pending:   1,
			alive:     true,
			claimed:   true,
			want:      DeliverySoon,
			wantNudge: true,
		},
		{
			name: "an agent sitting on a permission prompt is never typed at",
			agent: &agentstore.Agent{
				ID:          "agent-a",
				Pane:        "%1",
				State:       agentstore.StateWaiting,
				StateSeenAt: seenNow,
			},
			pending: 1,
			alive:   true,
			want:    DeliveryLater,
		},
		{
			name: "a prompt nothing has confirmed is still left alone, since keys typed at one answer it",
			agent: &agentstore.Agent{
				ID:          "agent-a",
				Pane:        "%1",
				State:       agentstore.StateWaiting,
				StateSeenAt: seenLongAgo,
			},
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
			name: "an agent whose pane the user is scrolled back in",
			agent: &agentstore.Agent{
				ID:    "agent-a",
				Pane:  "%1",
				State: agentstore.StateIdle,
			},
			pending: 1,
			alive:   true,
			inMode:  true,
			claimed: true,
			want:    DeliveryLater,
		},
		{
			name: "an agent told about the mail recently enough that the line may yet be answered",
			agent: &agentstore.Agent{
				ID: "agent-a", Pane: "%1", State: agentstore.StateIdle,
				StateChangedAt: idleSince, NudgedAt: &nudgedAfter,
			},
			pending:   1,
			alive:     true,
			want:      DeliveryLater,
			wantClaim: true,
		},
		{
			name: "an agent whose last nudge has stood long enough to have been lost",
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
			name: "an agent whose pane has opened but whose CLI has not finished coming up",
			agent: &agentstore.Agent{
				ID:             "agent-a",
				Pane:           "%1",
				State:          agentstore.StateIdle,
				StateChangedAt: idleSince,
			},
			pending: 1,
			alive:   true,
			frame:   unrecognizedFrame,
			want:    DeliveryLater,
		},
		{
			name: "an idle agent never told before",
			agent: &agentstore.Agent{
				ID:             "agent-a",
				Pane:           "%1",
				State:          agentstore.StateIdle,
				StateChangedAt: idleSince,
			},
			pending:   1,
			alive:     true,
			claimed:   true,
			want:      DeliverySoon,
			wantNudge: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			frame := tt.frame
			if frame == "" {
				frame = idleFrame
			}

			agents := &fakeAgents{agent: tt.agent, claimed: tt.claimed}
			panes := &fakePanes{
				alive:  tt.alive,
				inMode: tt.inMode,
				frame:  frame,
			}
			svc := NewDeliveryService(NewDeliveryServiceParams{
				MailboxReader: &fakeMailboxReader{pending: tt.pending},
				NudgeClaimer:  agents,
				PaneReader:    panes,
				PaneTyper:     panes,
				Harnesses:     fakeHarnesses{harness: &fakeHarness{}},
			})

			got, err := svc.Deliver(t.Context(), "agent-a")
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)

			if !tt.wantNudge {
				assert.Empty(t, panes.text, "nothing is typed at the agent")
				assert.Empty(t, panes.keys, "no key is sent to the agent")
				assert.Equal(t, tt.wantClaim, agents.taken,
					"the right to type is only spent on an agent that could have taken the keys")

				return
			}

			require.Len(t, panes.text, 1, "the line goes as text, so tmux does not read it as keys")
			assert.Contains(t, panes.text[0], "read_mail")
			assert.Equal(t, []string{"Enter"}, panes.keys, "only the newline that submits it goes as a key")
			assert.True(t, agents.taken, "the right to type is claimed before anything is typed")
		})
	}
}

func TestDeliverReportsExpiredMail(t *testing.T) {
	agents := &fakeAgents{agent: &agentstore.Agent{
		ID:    "agent-a",
		Pane:  "%1",
		State: agentstore.StateIdle,
	}}
	mailbox := &fakeMailboxReader{expired: []mailstore.Mail{{ID: "mail-1"}}}
	panes := &fakePanes{alive: true, frame: idleFrame}

	svc := NewDeliveryService(NewDeliveryServiceParams{
		MailboxReader: mailbox,
		NudgeClaimer:  agents,
		PaneReader:    panes,
		PaneTyper:     panes,
		Harnesses:     fakeHarnesses{harness: &fakeHarness{}},
	})

	got, err := svc.Deliver(t.Context(), "agent-a")
	require.NoError(t, err)
	assert.Equal(t, DeliveryDone, got)
	assert.True(t, mailbox.readExpired, "an empty mailbox is checked for mail that ran out of time")
	assert.Empty(t, panes.text)
}

type fakeMailboxReader struct {
	mailboxes   []string
	pending     int64
	expired     []mailstore.Mail
	readExpired bool
}

func (f *fakeMailboxReader) ListPendingMailboxes(context.Context) ([]string, error) {
	return f.mailboxes, nil
}

func (f *fakeMailboxReader) CountPending(context.Context, string) (int64, error) {
	return f.pending, nil
}

func (f *fakeMailboxReader) ListExpired(context.Context, string) ([]mailstore.Mail, error) {
	f.readExpired = true

	return f.expired, nil
}

type fakeAgents struct {
	agent   *agentstore.Agent
	claimed bool
	taken   bool
}

func (f *fakeAgents) GetByID(_ context.Context, id string) (*agentstore.Agent, error) {
	if f.agent == nil {
		return nil, fmt.Errorf("%w: agent %q", store.ErrNotFound, id)
	}

	return f.agent, nil
}

func (f *fakeAgents) ClaimNudge(context.Context, string, time.Duration) (bool, error) {
	f.taken = true

	return f.claimed, nil
}

type fakePanes struct {
	alive  bool
	inMode bool
	frame  string
	text   []string
	keys   []string
}

func (f *fakePanes) IsAgentRunning(agentstore.Agent) bool { return f.alive }

func (f *fakePanes) IsPaneInMode(string) bool { return f.inMode }

func (f *fakePanes) CapturePane(string) string { return f.frame }

func (f *fakePanes) SendText(_, text string) error {
	f.text = append(f.text, text)

	return nil
}

func (f *fakePanes) SendKeys(_ string, keys ...string) error {
	f.keys = append(f.keys, keys...)

	return nil
}

func TestDeliverAllVisitsEveryMailboxHoldingMail(t *testing.T) {
	agents := &fakeAgents{}
	panes := &fakePanes{frame: idleFrame}
	mailbox := &fakeMailboxReader{mailboxes: []string{"agent-a", "agent-b"}}

	svc := NewDeliveryService(NewDeliveryServiceParams{
		MailboxReader: mailbox,
		NudgeClaimer:  agents,
		PaneReader:    panes,
		PaneTyper:     panes,
		Harnesses:     fakeHarnesses{harness: &fakeHarness{}},
	})

	require.NoError(t, svc.DeliverAll(t.Context()))
	assert.Empty(t, panes.text, "mailboxes belonging to no agent are left alone")
}

func TestDeliverAllDoesNothingWhenNoMailIsWaiting(t *testing.T) {
	agents := &fakeAgents{}
	panes := &fakePanes{}

	svc := NewDeliveryService(NewDeliveryServiceParams{
		MailboxReader: &fakeMailboxReader{},
		NudgeClaimer:  agents,
		PaneReader:    panes,
		PaneTyper:     panes,
		Harnesses:     fakeHarnesses{harness: &fakeHarness{}},
	})

	require.NoError(t, svc.DeliverAll(t.Context()))
	assert.False(t, agents.taken)
}

// TestDeliverToAnAgentNoPassCanRead pins that an agent whose harness this binary cannot run
// is told once, rather than deferred every pass until the mail runs out of time.
func TestDeliverToAnAgentNoPassCanRead(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()

	require.NoError(t, st.Create(ctx, agentstore.Agent{
		Name:    "orphan",
		Pane:    "%1",
		State:   agentstore.StateThinking,
		Harness: "gemini",
	}))
	agent, err := st.Get(ctx, "orphan")
	require.NoError(t, err)

	tmux := newFakeTmux()
	tmux.owners["%1"] = agent.ID
	tmux.captures["%1"] = idleFrame

	rec := NewReconciler(tmux, fakeHarnesses{harness: &fakeHarness{}, only: []string{testHarnessID}}, st.Agents)
	for range 30 {
		require.NoError(t, rec.Reconcile(ctx))
	}

	pinned := agentByID(t, st, agent.ID)
	assert.Equal(t, agentstore.StateThinking, pinned.State, "thirty passes read nothing, so the state stands")
	assert.Equal(t, agent.StateSeenAt, pinned.StateSeenAt, "and none of them confirmed it")

	_, err = st.Mail.Send(ctx, mailstore.SendParams{
		FromName:  "somebody",
		ToAgentID: agent.ID,
		Body:      "please read this",
		Deadline:  time.Hour,
	})
	require.NoError(t, err)

	panes := &fakePanes{alive: true, frame: idleFrame}
	svc := NewDeliveryService(NewDeliveryServiceParams{
		MailboxReader: st.Mail,
		NudgeClaimer:  st.Agents,
		PaneReader:    panes,
		PaneTyper:     panes,
		Harnesses:     fakeHarnesses{harness: &fakeHarness{}, only: []string{testHarnessID}},
	})

	got, err := svc.Deliver(ctx, agent.ID)
	require.NoError(t, err)
	assert.Equal(t, DeliverySoon, got, "a state confirmed a moment ago is still worth waiting on")
	assert.Empty(t, panes.text)

	backdateStateSeen(t, st, agent.ID, stateSeenBound+time.Minute)

	got, err = svc.Deliver(ctx, agent.ID)
	require.NoError(t, err)
	assert.Equal(t, DeliverySoon, got)
	require.Len(t, panes.text, 1, "an unconfirmed state is not live work, so the agent is told")
	assert.Contains(t, panes.text[0], "read_mail")

	got, err = svc.Deliver(ctx, agent.ID)
	require.NoError(t, err)
	assert.Equal(t, DeliveryLater, got)
	assert.Len(t, panes.text, 1, "and told once, not once a pass")
}

// TestDeliverRetriesANudgeTheAgentNeverActedOn pins that unread mail puts the nudge back,
// since a line tmux took and the agent never saw leaves nothing about the agent changed.
func TestDeliverRetriesANudgeTheAgentNeverActedOn(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()

	require.NoError(t, st.Create(ctx, agentstore.Agent{
		Name:  "idler",
		Pane:  "%1",
		State: agentstore.StateIdle,
	}))
	agent, err := st.Get(ctx, "idler")
	require.NoError(t, err)
	backdateStateChanged(t, st, agent.ID, time.Hour)

	_, err = st.Mail.Send(ctx, mailstore.SendParams{
		FromName:  "somebody",
		ToAgentID: agent.ID,
		Body:      "please read this",
		Deadline:  time.Hour,
	})
	require.NoError(t, err)

	panes := &fakePanes{alive: true, frame: idleFrame}
	svc := NewDeliveryService(NewDeliveryServiceParams{
		MailboxReader: st.Mail,
		NudgeClaimer:  st.Agents,
		PaneReader:    panes,
		PaneTyper:     panes,
		Harnesses:     fakeHarnesses{harness: &fakeHarness{}},
	})

	got, err := svc.Deliver(ctx, agent.ID)
	require.NoError(t, err)
	assert.Equal(t, DeliverySoon, got)
	require.Len(t, panes.text, 1, "an idle agent holding mail is told about it")

	for range 30 {
		got, err = svc.Deliver(ctx, agent.ID)
		require.NoError(t, err)
		assert.Equal(t, DeliveryLater, got)
	}
	assert.Len(t, panes.text, 1, "and not told again once a pass")

	backdateNudge(t, st, agent.ID, nudgeRetryAfter+time.Minute)

	got, err = svc.Deliver(ctx, agent.ID)
	require.NoError(t, err)
	assert.Equal(t, DeliverySoon, got)
	assert.Len(t, panes.text, 2, "a nudge that did not get the mail read is sent again")

	stranded := agentByID(t, st, agent.ID)
	assert.Equal(t, agentstore.StateIdle, stranded.State, "nothing about the agent moved to earn the retry")
	require.NotNil(t, stranded.NudgedAt)
	assert.True(t, stranded.NudgedAt.After(stranded.StateChangedAt),
		"the agent was told about this stretch of idling, which is all the agent itself can say")

	pending, err := st.Mail.CountPending(ctx, agent.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(1), pending, "and the mail is still waiting")
}

// TestDeliverRetriesANudgeAcrossAnArchive pins the same retry across an archive, which leaves
// the agent's state and its spent nudge exactly as they were.
func TestDeliverRetriesANudgeAcrossAnArchive(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()

	require.NoError(t, st.Create(ctx, agentstore.Agent{
		Name:  "sleeper",
		Pane:  "%1",
		State: agentstore.StateIdle,
	}))
	agent, err := st.Get(ctx, "sleeper")
	require.NoError(t, err)
	backdateStateChanged(t, st, agent.ID, time.Hour)

	_, err = st.Mail.Send(ctx, mailstore.SendParams{
		FromName:  "somebody",
		ToAgentID: agent.ID,
		Body:      "please read this",
		Deadline:  time.Hour,
	})
	require.NoError(t, err)

	panes := &fakePanes{alive: true, frame: idleFrame}
	svc := NewDeliveryService(NewDeliveryServiceParams{
		MailboxReader: st.Mail,
		NudgeClaimer:  st.Agents,
		PaneReader:    panes,
		PaneTyper:     panes,
		Harnesses:     fakeHarnesses{harness: &fakeHarness{}},
	})

	got, err := svc.Deliver(ctx, agent.ID)
	require.NoError(t, err)
	assert.Equal(t, DeliverySoon, got)
	require.Len(t, panes.text, 1)

	_, archiveErr := st.Agents.Archive(ctx, []string{agent.ID})

	require.NoError(t, archiveErr)

	got, err = svc.Deliver(ctx, agent.ID)
	require.NoError(t, err)
	assert.Equal(t, DeliveryDone, got, "an archived agent is not typed at")
	assert.Len(t, panes.text, 1)

	require.NoError(t, st.Agents.Restore(ctx, []string{agent.ID}))
	backdateNudge(t, st, agent.ID, nudgeRetryAfter+time.Minute)

	got, err = svc.Deliver(ctx, agent.ID)
	require.NoError(t, err)
	assert.Equal(t, DeliverySoon, got)
	assert.Len(t, panes.text, 2, "an agent back from the archive is told about the mail it never read")
}

type fixedHarnesses struct{ harness harness.Harness }

func (f fixedHarnesses) GetByID(string) (harness.Harness, error) { return f.harness, nil }

// TestDeliverDefersToARealHarnessStillComingUp wires the registered harnesses into the gate,
// since the fake answers correctly by construction and so proves nothing about a real one.
func TestDeliverDefersToARealHarnessStillComingUp(t *testing.T) {
	shellEcho := "harry@mac ~/weave % WEAVE_AGENT_ID=01a0 codex resume 019abc"

	harnesses := []struct {
		name    string
		harness harness.Harness
	}{
		{name: "claude", harness: claude.NewHarness(claude.NewHarnessParams{})},
		{name: "codex", harness: codex.NewHarness(codex.NewHarnessParams{SessionsDir: t.TempDir()})},
	}

	for _, hn := range harnesses {
		t.Run(hn.name, func(t *testing.T) {
			agents := &fakeAgents{
				agent: &agentstore.Agent{
					ID:    "agent-a",
					Pane:  "%1",
					State: agentstore.StateIdle,
				},
				claimed: true,
			}
			panes := &fakePanes{alive: true, frame: shellEcho}
			svc := NewDeliveryService(NewDeliveryServiceParams{
				MailboxReader: &fakeMailboxReader{pending: 1},
				NudgeClaimer:  agents,
				PaneReader:    panes,
				PaneTyper:     panes,
				Harnesses:     fixedHarnesses{harness: hn.harness},
			})

			got, err := svc.Deliver(t.Context(), "agent-a")
			require.NoError(t, err)
			assert.Equal(t, DeliveryLater, got)
			assert.Empty(t, panes.text, "nothing is typed at a pane that is not listening")
			assert.Empty(t, panes.keys)
		})
	}
}
