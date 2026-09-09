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

// TestDeliver pins which agents mail is handed to and which are passed over, covering the
// pane, the state, and how long ago it was last seen.
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
			name: "an agent on a pane weave could not read, where the keys would land nowhere known",
			agent: &agentstore.Agent{
				ID:             "agent-a",
				Pane:           "%1",
				State:          agentstore.StateUncertain,
				StateChangedAt: idleSince,
			},
			pending: 1,
			alive:   true,
			frame:   unrecognizedFrame,
			want:    DeliveryLater,
		},
		{
			name: "an agent whose prompt somebody is halfway through typing into",
			agent: &agentstore.Agent{
				ID:             "agent-a",
				Pane:           "%1",
				State:          agentstore.StateIdle,
				StateChangedAt: idleSince,
			},
			pending: 1,
			alive:   true,
			frame:   composingFrame,
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
				PaneWriter:    panes,
				Harnesses:     fakeHarnesses{harness: &fakeHarness{}},
			})

			got, err := svc.Deliver(t.Context(), "agent-a")
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)

			if !tt.wantNudge {
				assert.Empty(t, panes.actions, "nothing is sent to the agent")
				assert.Equal(t, tt.wantClaim, agents.taken,
					"the right to type is only spent on an agent that could have taken the keys")

				return
			}

			require.Len(t, panes.pasted, 1, "the line goes as one paste rather than as typed keys")
			assert.Contains(t, panes.pasted[0], "read_mail")
			assert.Equal(t, []string{"paste"}, panes.actions,
				"an empty box is pasted into and nothing else, the newline riding with the paste")
			assert.True(t, agents.taken, "the right to type is claimed before anything is typed")
		})
	}
}

// TestDeliverSaysWhenANudgeWentNowhere pins which second nudge is worth a warning.
func TestDeliverSaysWhenANudgeWentNowhere(t *testing.T) {
	nudgedAt := time.Date(2026, time.August, 4, 12, 0, 0, 0, time.UTC)
	before := nudgedAt.Add(-time.Minute)
	after := nudgedAt.Add(time.Minute)

	tests := []struct {
		name        string
		nudgedAt    *time.Time
		turnEndedAt *time.Time
		want        string
	}{
		{
			name: "an agent nobody has told before",
			want: "INFO",
		},
		{
			name:        "an agent that ended a turn after the last nudge",
			nudgedAt:    &nudgedAt,
			turnEndedAt: &after,
			want:        "INFO",
		},
		{
			name:        "an agent that has ended no turn since the last nudge",
			nudgedAt:    &nudgedAt,
			turnEndedAt: &before,
			want:        "WARN",
		},
		{
			name:     "an agent that has never reported a turn at all",
			nudgedAt: &nudgedAt,
			want:     "WARN",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agents := &fakeAgents{claimed: true, agent: &agentstore.Agent{
				ID:          "agent-a",
				Pane:        "%1",
				State:       agentstore.StateIdle,
				NudgedAt:    tt.nudgedAt,
				TurnEndedAt: tt.turnEndedAt,
			}}
			panes := &fakePanes{alive: true, frame: idleFrame}

			svc := NewDeliveryService(NewDeliveryServiceParams{
				MailboxReader: &fakeMailboxReader{pending: 1},
				NudgeClaimer:  agents,
				PaneReader:    panes,
				PaneWriter:    panes,
				Harnesses:     fakeHarnesses{harness: &fakeHarness{}},
			})

			ctx, records := loggedContext(t)

			got, err := svc.Deliver(ctx, "agent-a")
			require.NoError(t, err)
			require.Equal(t, DeliverySoon, got)

			written := records()
			require.Len(t, written, 1)
			assert.Equal(t, tt.want, written[0]["severity_text"])
		})
	}
}

// TestDeliverReportsExpiredMail pins that mail nobody read before its deadline is reported
// rather than dropped in silence.
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
		PaneWriter:    panes,
		Harnesses:     fakeHarnesses{harness: &fakeHarness{}},
	})

	got, err := svc.Deliver(t.Context(), "agent-a")
	require.NoError(t, err)
	assert.Equal(t, DeliveryDone, got)
	assert.True(t, mailbox.readExpired, "an empty mailbox is checked for mail that ran out of time")
	assert.Empty(t, panes.pasted)
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

// fakePanes stands in for tmux, showing one frame until a cut of its input box has landed and
// the frame after it once it has.
type fakePanes struct {
	alive    bool
	inMode   bool
	clientAt bool
	frame    string

	// afterCut is the frame the pane shows once its line has been cut, an empty box unless a
	// test says otherwise.
	afterCut string

	// staleReads is how many reads after the cut still show the frame from before it, which is
	// a CLI that has yet to repaint.
	staleReads int

	// actions is what was done to the pane, in the order it was done.
	actions  []string
	pasted   []string
	captures int
	cutAt    int
	cut      bool
}

func (f *fakePanes) IsAgentRunning(agentstore.Agent) bool { return f.alive }

func (f *fakePanes) IsPaneInMode(string) bool { return f.inMode }

func (f *fakePanes) CapturePane(string) string {
	f.captures++

	if f.cut && f.captures > f.cutAt+f.staleReads {
		if f.afterCut != "" {
			return f.afterCut
		}

		return idleFrame
	}

	return f.frame
}

func (f *fakePanes) IsClientAtPane(string, time.Duration) bool { return f.clientAt }

func (f *fakePanes) PasteAndSubmit(_, text string) error {
	f.actions = append(f.actions, "paste")
	f.pasted = append(f.pasted, text)

	return nil
}

func (f *fakePanes) CutLine(string) error {
	f.actions = append(f.actions, "cut")
	f.cut = true
	f.cutAt = f.captures

	return nil
}

func (f *fakePanes) YankLine(string) error {
	f.actions = append(f.actions, "yank")

	return nil
}

// TestDeliverAllVisitsEveryMailboxHoldingMail pins that a sweep reaches every mailbox with
// something waiting in it.
func TestDeliverAllVisitsEveryMailboxHoldingMail(t *testing.T) {
	agents := &fakeAgents{}
	panes := &fakePanes{frame: idleFrame}
	mailbox := &fakeMailboxReader{mailboxes: []string{"agent-a", "agent-b"}}

	svc := NewDeliveryService(NewDeliveryServiceParams{
		MailboxReader: mailbox,
		NudgeClaimer:  agents,
		PaneReader:    panes,
		PaneWriter:    panes,
		Harnesses:     fakeHarnesses{harness: &fakeHarness{}},
	})

	require.NoError(t, svc.DeliverAll(t.Context()))
	assert.Empty(t, panes.pasted, "mailboxes belonging to no agent are left alone")
}

// TestDeliverAllDoesNothingWhenNoMailIsWaiting pins that an empty sweep touches no pane.
func TestDeliverAllDoesNothingWhenNoMailIsWaiting(t *testing.T) {
	agents := &fakeAgents{}
	panes := &fakePanes{}

	svc := NewDeliveryService(NewDeliveryServiceParams{
		MailboxReader: &fakeMailboxReader{},
		NudgeClaimer:  agents,
		PaneReader:    panes,
		PaneWriter:    panes,
		Harnesses:     fakeHarnesses{harness: &fakeHarness{}},
	})

	require.NoError(t, svc.DeliverAll(t.Context()))
	assert.False(t, agents.taken)
}

// TestDeliverToAnAgentNoPassCanRead pins that an agent whose harness this binary cannot run is
// told once rather than deferred every pass.
func TestDeliverToAnAgentNoPassCanRead(t *testing.T) {
	st := newStore(t)
	ctx := t.Context()

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

	rec := NewReconciler(NewReconcilerParams{
		Panes:     tmux,
		Harnesses: fakeHarnesses{harness: &fakeHarness{}, only: []string{testHarnessID}},
		States:    st.Agents,
	})
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
		PaneWriter:    panes,
		Harnesses:     fakeHarnesses{harness: &fakeHarness{}, only: []string{testHarnessID}},
	})

	got, err := svc.Deliver(ctx, agent.ID)
	require.NoError(t, err)
	assert.Equal(t, DeliverySoon, got, "a state confirmed a moment ago is still worth waiting on")
	assert.Empty(t, panes.pasted)

	backdateStateSeen(t, st, agent.ID, stateSeenBound+time.Minute)

	got, err = svc.Deliver(ctx, agent.ID)
	require.NoError(t, err)
	assert.Equal(t, DeliverySoon, got)
	require.Len(t, panes.pasted, 1, "an unconfirmed state is not live work, so the agent is told")
	assert.Contains(t, panes.pasted[0], "read_mail")

	got, err = svc.Deliver(ctx, agent.ID)
	require.NoError(t, err)
	assert.Equal(t, DeliveryLater, got)
	assert.Len(t, panes.pasted, 1, "and told once, not once a pass")
}

// TestDeliverRetriesANudgeTheAgentNeverActedOn pins that unread mail puts the nudge back.
func TestDeliverRetriesANudgeTheAgentNeverActedOn(t *testing.T) {
	st := newStore(t)
	ctx := t.Context()

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
		PaneWriter:    panes,
		Harnesses:     fakeHarnesses{harness: &fakeHarness{}},
	})

	got, err := svc.Deliver(ctx, agent.ID)
	require.NoError(t, err)
	assert.Equal(t, DeliverySoon, got)
	require.Len(t, panes.pasted, 1, "an idle agent holding mail is told about it")

	for range 30 {
		got, err = svc.Deliver(ctx, agent.ID)
		require.NoError(t, err)
		assert.Equal(t, DeliveryLater, got)
	}
	assert.Len(t, panes.pasted, 1, "and not told again once a pass")

	backdateNudge(t, st, agent.ID, nudgeRetryAfter+time.Minute)

	got, err = svc.Deliver(ctx, agent.ID)
	require.NoError(t, err)
	assert.Equal(t, DeliverySoon, got)
	assert.Len(t, panes.pasted, 2, "a nudge that did not get the mail read is sent again")

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
	ctx := t.Context()

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
		PaneWriter:    panes,
		Harnesses:     fakeHarnesses{harness: &fakeHarness{}},
	})

	got, err := svc.Deliver(ctx, agent.ID)
	require.NoError(t, err)
	assert.Equal(t, DeliverySoon, got)
	require.Len(t, panes.pasted, 1)

	_, archiveErr := st.Agents.Archive(ctx, []string{agent.ID})

	require.NoError(t, archiveErr)

	got, err = svc.Deliver(ctx, agent.ID)
	require.NoError(t, err)
	assert.Equal(t, DeliveryDone, got, "an archived agent is not typed at")
	assert.Len(t, panes.pasted, 1)

	require.NoError(t, st.Agents.Restore(ctx, []string{agent.ID}))
	backdateNudge(t, st, agent.ID, nudgeRetryAfter+time.Minute)

	got, err = svc.Deliver(ctx, agent.ID)
	require.NoError(t, err)
	assert.Equal(t, DeliverySoon, got)
	assert.Len(t, panes.pasted, 2, "an agent back from the archive is told about the mail it never read")
}

type fixedHarnesses struct{ harness harness.Harness }

func (f fixedHarnesses) GetByID(string) (harness.Harness, error) { return f.harness, nil }

// TestDeliverDefersToARealHarnessStillComingUp wires the registered harnesses into the gate,
// which the fake answers correctly by construction.
func TestDeliverDefersToARealHarnessStillComingUp(t *testing.T) {
	shellEcho := "harry@mac ~/weave % WEAVE_AGENT_ID=01a0 codex resume 019abc"

	tests := []struct {
		name    string
		harness harness.Harness
	}{
		{name: "claude", harness: claude.NewHarness(claude.NewHarnessParams{})},
		{name: "codex", harness: codex.NewHarness(codex.NewHarnessParams{SessionsDir: t.TempDir()})},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
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
				PaneWriter:    panes,
				Harnesses:     fixedHarnesses{harness: tt.harness},
			})

			got, err := svc.Deliver(t.Context(), "agent-a")
			require.NoError(t, err)
			assert.Equal(t, DeliveryLater, got)
			assert.Empty(t, panes.actions, "nothing is sent to a pane that is not listening")
		})
	}
}

// composingService is a delivery service over one composing agent, whose clock a test moves by
// writing to the time it returns.
func composingService(t *testing.T, panes *fakePanes, now *time.Time) *DeliveryService {
	t.Helper()

	return NewDeliveryService(NewDeliveryServiceParams{
		MailboxReader: &fakeMailboxReader{pending: 1},
		NudgeClaimer: &fakeAgents{claimed: true, agent: &agentstore.Agent{
			ID:    "agent-a",
			Pane:  "%1",
			State: agentstore.StateIdle,
		}},
		PaneReader: panes,
		PaneWriter: panes,
		Harnesses:  fakeHarnesses{harness: &fakeHarness{}},
		Now:        func() time.Time { return *now },
	})
}

// TestDeliverToAComposingPane pins which draft weave cuts to get a nudge past and which it
// leaves where it is.
func TestDeliverToAComposingPane(t *testing.T) {
	tests := []struct {
		name        string
		frame       string
		afterCut    string
		clientAt    bool
		staleReads  int
		held        time.Duration
		want        Delivery
		wantActions []string
	}{
		{
			name: "a draft somebody may still be in the middle of typing",
			held: draftStaleAfter - time.Second,
			want: DeliveryLater,
		},
		{
			name:        "a draft nobody has moved for long enough, at a pane nobody is at",
			held:        draftStaleAfter,
			want:        DeliverySoon,
			wantActions: []string{"cut", "paste", "yank"},
		},
		{
			name:     "a still draft at a pane somebody is sitting at",
			held:     draftStaleAfter,
			clientAt: true,
			want:     DeliveryLater,
		},
		{
			name:  "a still draft spread over more than one line",
			frame: composingFrame + ": first|second",
			held:  draftStaleAfter,
			want:  DeliveryLater,
		},
		{
			name:        "a cut whose frame the CLI has yet to repaint",
			held:        draftStaleAfter,
			staleReads:  cutSettleReads - 1,
			want:        DeliverySoon,
			wantActions: []string{"cut", "paste", "yank"},
		},
		{
			name:        "a cut the frame never shows, which is a box that held more than the cut took",
			held:        draftStaleAfter,
			staleReads:  cutSettleReads,
			want:        DeliveryLater,
			wantActions: []string{"cut", "yank"},
		},
		{
			name:        "a cut that left text in the box",
			held:        draftStaleAfter,
			afterCut:    composingFrame,
			want:        DeliveryLater,
			wantActions: []string{"cut", "yank"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			frame := tt.frame
			if frame == "" {
				frame = composingFrame
			}

			now := time.Date(2026, time.September, 9, 12, 0, 0, 0, time.UTC)
			panes := &fakePanes{
				alive:      true,
				frame:      frame,
				afterCut:   tt.afterCut,
				clientAt:   tt.clientAt,
				staleReads: tt.staleReads,
			}
			svc := composingService(t, panes, &now)

			got, err := svc.Deliver(t.Context(), "agent-a")
			require.NoError(t, err)
			require.Equal(t, DeliveryLater, got, "the first look at a draft only starts its clock")
			require.Empty(t, panes.actions)

			now = now.Add(tt.held)

			got, err = svc.Deliver(t.Context(), "agent-a")
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.wantActions, panes.actions)

			if tt.want == DeliverySoon {
				require.Len(t, panes.pasted, 1)
				assert.Contains(t, panes.pasted[0], "read_mail")
			} else {
				assert.Empty(t, panes.pasted, "a draft weave will not cut is never pasted over")
			}
		})
	}
}

// TestDeliverTimesADraftFromWhenItLastMoved pins that a box somebody is editing is never still,
// however long weave has been watching it.
func TestDeliverTimesADraftFromWhenItLastMoved(t *testing.T) {
	now := time.Date(2026, time.September, 9, 12, 0, 0, 0, time.UTC)
	panes := &fakePanes{alive: true, frame: composingFrame + ": one"}
	svc := composingService(t, panes, &now)

	got, err := svc.Deliver(t.Context(), "agent-a")
	require.NoError(t, err)
	require.Equal(t, DeliveryLater, got)

	now = now.Add(draftStaleAfter)
	panes.frame = composingFrame + ": one and a bit"

	got, err = svc.Deliver(t.Context(), "agent-a")
	require.NoError(t, err)
	assert.Equal(t, DeliveryLater, got, "the text moved, so the clock starts again")
	assert.Empty(t, panes.actions)

	now = now.Add(draftStaleAfter)

	got, err = svc.Deliver(t.Context(), "agent-a")
	require.NoError(t, err)
	assert.Equal(t, DeliverySoon, got)
	assert.Equal(t, []string{"cut", "paste", "yank"}, panes.actions)
}

// TestDeliverForgetsADraftOnceTheBoxIsEmpty pins that text typed after the box was emptied is
// timed from when it appeared rather than from the draft before it.
func TestDeliverForgetsADraftOnceTheBoxIsEmpty(t *testing.T) {
	now := time.Date(2026, time.September, 9, 12, 0, 0, 0, time.UTC)
	panes := &fakePanes{alive: true, frame: composingFrame}
	svc := composingService(t, panes, &now)

	got, err := svc.Deliver(t.Context(), "agent-a")
	require.NoError(t, err)
	require.Equal(t, DeliveryLater, got)

	now = now.Add(draftStaleAfter)
	panes.frame = idleFrame

	got, err = svc.Deliver(t.Context(), "agent-a")
	require.NoError(t, err)
	require.Equal(t, DeliverySoon, got)
	require.Equal(t, []string{"paste"}, panes.actions, "an empty box needs no cut")

	panes.frame = composingFrame
	panes.actions = nil

	got, err = svc.Deliver(t.Context(), "agent-a")
	require.NoError(t, err)
	assert.Equal(t, DeliveryLater, got, "the box was empty since, so this draft is new")
	assert.Empty(t, panes.actions)
}

// TestDeliverSaysOnceThatAMultiLineDraftIsInTheWay pins the one thing weave does about a box it
// will not touch.
func TestDeliverSaysOnceThatAMultiLineDraftIsInTheWay(t *testing.T) {
	now := time.Date(2026, time.September, 9, 12, 0, 0, 0, time.UTC)
	panes := &fakePanes{alive: true, frame: composingFrame + ": first|second"}
	svc := composingService(t, panes, &now)

	ctx, records := loggedContext(t)

	got, err := svc.Deliver(ctx, "agent-a")
	require.NoError(t, err)
	require.Equal(t, DeliveryLater, got)
	require.Empty(t, records(), "a draft that has not stood long enough is nobody's business yet")

	now = now.Add(draftStaleAfter)

	for range 3 {
		got, err = svc.Deliver(ctx, "agent-a")
		require.NoError(t, err)
		require.Equal(t, DeliveryLater, got)
	}

	written := records()
	require.Len(t, written, 1, "three passes over the same draft say it once")
	assert.Equal(t, "WARN", written[0]["severity_text"])
	assert.Empty(t, panes.actions)
}

// TestDeliverForgetsAnAgentNothingWillLookAtAgain pins that the draft clock does not keep an
// entry for an agent no pass will reach.
func TestDeliverForgetsAnAgentNothingWillLookAtAgain(t *testing.T) {
	archivedAt := time.Date(2026, time.September, 9, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name string
		gone func(agents *fakeAgents)
	}{
		{
			name: "an agent no row holds any more",
			gone: func(agents *fakeAgents) { agents.agent = nil },
		},
		{
			name: "an agent archived while its draft stood",
			gone: func(agents *fakeAgents) { agents.agent.ArchivedAt = &archivedAt },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			now := archivedAt
			panes := &fakePanes{alive: true, frame: composingFrame}
			agents := &fakeAgents{claimed: true, agent: &agentstore.Agent{
				ID:    "agent-a",
				Pane:  "%1",
				State: agentstore.StateIdle,
			}}
			svc := NewDeliveryService(NewDeliveryServiceParams{
				MailboxReader: &fakeMailboxReader{pending: 1},
				NudgeClaimer:  agents,
				PaneReader:    panes,
				PaneWriter:    panes,
				Harnesses:     fakeHarnesses{harness: &fakeHarness{}},
				Now:           func() time.Time { return now },
			})

			got, err := svc.Deliver(t.Context(), "agent-a")
			require.NoError(t, err)
			require.Equal(t, DeliveryLater, got)
			require.Len(t, svc.drafts.boxes, 1, "the draft was seen, so it is being timed")

			tt.gone(agents)

			got, err = svc.Deliver(t.Context(), "agent-a")
			require.NoError(t, err)
			assert.Equal(t, DeliveryDone, got)
			assert.Empty(t, svc.drafts.boxes, "nothing will read this box again")
		})
	}
}
