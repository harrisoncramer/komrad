package mail

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"weave/internal/harness"
	"weave/internal/harness/claude"
	"weave/internal/harness/codex"
	"weave/internal/harness/pane"
	"weave/internal/store"
	agentstore "weave/internal/store/agent"
	mailstore "weave/internal/store/mail"
	"weave/internal/storetest"

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
	seenLongAgo := seenNow.Add(-agentstore.StateSeenBound - time.Minute)

	tests := []struct {
		name      string
		agent     *agentstore.Agent
		pending   int64
		alive     bool
		shell     bool
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
			name: "an agent a person is holding, whose tree is not weave's to type into",
			agent: &agentstore.Agent{
				ID:     "agent-a",
				Pane:   "%1",
				State:  agentstore.StateIdle,
				HeldAt: &archivedAt,
			},
			pending: 1,
			alive:   true,
			want:    DeliveryLater,
			// No claim and no nudge: a held agent is one somebody stopped on purpose.

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
			name: "an agent whose CLI has exited with its pane still stamped as its own",
			agent: &agentstore.Agent{
				ID:    "agent-a",
				Pane:  "%1",
				State: agentstore.StateIdle,
			},
			pending: 1,
			alive:   true,
			shell:   true,
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
				ID:             "agent-a",
				Pane:           "%1",
				State:          agentstore.StateIdle,
				StateChangedAt: idleSince,
				NudgedAt:       &nudgedAfter,
			},
			pending:   1,
			alive:     true,
			want:      DeliveryLater,
			wantClaim: true,
		},
		{
			name: "an agent whose last nudge has stood long enough to have been lost",
			agent: &agentstore.Agent{
				ID:             "agent-a",
				Pane:           "%1",
				State:          agentstore.StateIdle,
				StateChangedAt: idleSince,
				NudgedAt:       &nudgedBefore,
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
				Alive:  tt.alive,
				Shell:  tt.shell,
				InMode: tt.inMode,
				Frame:  frame,
			}
			svc := NewDeliveryService(NewDeliveryServiceParams{
				MailboxReader: &fakeMailboxReader{pending: tt.pending},
				NudgeClaimer:  agents,
				PaneReader:    panes,
				Channel:       pane.NewPane(pane.NewPaneParams{Terminal: panes, Harnesses: fakeHarnesses{harness: &fakeHarness{}}}),
			})

			got, err := svc.Deliver(t.Context(), "agent-a")
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)

			if !tt.wantNudge {
				assert.Empty(t, panes.Actions, "nothing is sent to the agent")
				assert.Equal(t, tt.wantClaim, agents.taken,
					"the right to type is only spent on an agent that could have taken the keys")

				return
			}

			require.Len(t, panes.Pasted, 1, "the line goes as one paste rather than as typed keys")
			assert.Contains(t, panes.Pasted[0], "read_mail")
			assert.Equal(t, []string{"paste"}, panes.Actions,
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
		nudgeSentAt *time.Time
		turnEndedAt *time.Time
		want        string
	}{
		{
			name: "an agent nobody has told before",
			want: "INFO",
		},
		{
			name:        "an agent that ended a turn after the last nudge",
			nudgeSentAt: &nudgedAt,
			turnEndedAt: &after,
			want:        "INFO",
		},
		{
			name:        "an agent that has ended no turn since the last nudge",
			nudgeSentAt: &nudgedAt,
			turnEndedAt: &before,
			want:        "WARN",
		},
		{
			name:        "an agent that has never reported a turn at all",
			nudgeSentAt: &nudgedAt,
			want:        "WARN",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agents := &fakeAgents{claimed: true, agent: &agentstore.Agent{
				ID:          "agent-a",
				Pane:        "%1",
				State:       agentstore.StateIdle,
				NudgeSentAt: tt.nudgeSentAt,
				TurnEndedAt: tt.turnEndedAt,
			}}
			panes := &fakePanes{Alive: true, Frame: idleFrame}

			svc := NewDeliveryService(NewDeliveryServiceParams{
				MailboxReader: &fakeMailboxReader{pending: 1},
				NudgeClaimer:  agents,
				PaneReader:    panes,
				Channel:       pane.NewPane(pane.NewPaneParams{Terminal: panes, Harnesses: fakeHarnesses{harness: &fakeHarness{}}}),
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

// TestDeliverDoesNotWarnAfterAPassThatTypedNothing pins that a claim spent on a pass the
// channel refused leaves the went-nowhere warning unarmed, so the first nudge an agent actually
// gets reads as a first one.
func TestDeliverDoesNotWarnAfterAPassThatTypedNothing(t *testing.T) {
	agents := &fakeAgents{claimed: true, agent: &agentstore.Agent{
		ID:    "agent-a",
		Pane:  "%1",
		State: agentstore.StateIdle,
	}}
	channel := &fakeChannel{RefuseSend: true}

	svc := NewDeliveryService(NewDeliveryServiceParams{
		MailboxReader: &fakeMailboxReader{pending: 1},
		NudgeClaimer:  agents,
		PaneReader:    &fakePanes{Alive: true, Frame: idleFrame},
		Channel:       channel,
	})

	ctx, records := loggedContext(t)

	got, err := svc.Deliver(ctx, "agent-a")
	require.NoError(t, err)
	require.Equal(t, DeliveryLater, got)
	require.Empty(t, channel.Sent, "the channel refused the line, so nothing was typed")

	channel.RefuseSend = false

	got, err = svc.Deliver(ctx, "agent-a")
	require.NoError(t, err)
	require.Equal(t, DeliverySoon, got)
	require.Len(t, channel.Sent, 1)

	written := records()
	require.Len(t, written, 1)
	assert.Equal(t, "INFO", written[0]["severity_text"],
		"the agent has been told once, so nothing went nowhere")
}

// TestDeliverKeepsALandedNudgeWhenTheBookkeepingFails pins that a nudge already typed is not
// reported as a failed delivery because the row it is recorded on refused the write.
func TestDeliverKeepsALandedNudgeWhenTheBookkeepingFails(t *testing.T) {
	agents := &fakeAgents{claimed: true, sentErr: errors.New("no database"), agent: &agentstore.Agent{
		ID:    "agent-a",
		Pane:  "%1",
		State: agentstore.StateIdle,
	}}
	channel := &fakeChannel{}

	svc := NewDeliveryService(NewDeliveryServiceParams{
		MailboxReader: &fakeMailboxReader{pending: 1},
		NudgeClaimer:  agents,
		PaneReader:    &fakePanes{Alive: true, Frame: idleFrame},
		Channel:       channel,
	})

	ctx, records := loggedContext(t)

	got, err := svc.Deliver(ctx, "agent-a")
	require.NoError(t, err, "the rest of the pass goes on")
	assert.Equal(t, DeliverySoon, got)
	assert.Len(t, channel.Sent, 1)

	written := records()
	require.Len(t, written, 2)
	assert.Equal(t, "ERROR", written[1]["severity_text"])
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
	panes := &fakePanes{Alive: true, Frame: idleFrame}

	svc := NewDeliveryService(NewDeliveryServiceParams{
		MailboxReader: mailbox,
		NudgeClaimer:  agents,
		PaneReader:    panes,
		Channel:       pane.NewPane(pane.NewPaneParams{Terminal: panes, Harnesses: fakeHarnesses{harness: &fakeHarness{}}}),
	})

	got, err := svc.Deliver(t.Context(), "agent-a")
	require.NoError(t, err)
	assert.Equal(t, DeliveryDone, got)
	assert.True(t, mailbox.readExpired, "an empty mailbox is checked for mail that ran out of time")
	assert.Empty(t, panes.Pasted)
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
	sentErr error
}

func (f *fakeAgents) GetByID(_ context.Context, id string) (*agentstore.Agent, error) {
	if f.agent == nil {
		return nil, fmt.Errorf("%w: agent %q", store.ErrNotFound, id)
	}

	// The store hands back a row read before the claim stamps it, so a caller never sees its own
	// claim.
	agent := *f.agent

	return &agent, nil
}

func (f *fakeAgents) ClaimNudge(context.Context, string, time.Duration) (bool, error) {
	f.taken = true
	if f.claimed {
		claimedAt := time.Now()
		f.agent.NudgedAt = &claimedAt
	}

	return f.claimed, nil
}

func (f *fakeAgents) SetNudgeSent(_ context.Context, _ string) error {
	if f.sentErr != nil {
		return f.sentErr
	}

	sentAt := time.Now()
	f.agent.NudgeSentAt = &sentAt

	return nil
}

// fakePanes stands in for tmux, showing one frame until a cut of its input box has landed and
// the frame after it once it has.
type fakePanes struct {
	Alive    bool
	Shell    bool
	InMode   bool
	ClientAt bool
	Frame    string

	// AfterCut is the frame the pane shows once its line has been cut, an empty box unless a
	// test says otherwise.
	AfterCut string

	// StaleReads is how many reads after the cut still show the frame from before it, which is
	// a CLI that has yet to repaint.
	StaleReads int

	// PasteErr and YankErr are what tmux refuses with.
	PasteErr error
	YankErr  error

	// Actions is what was done to the pane, in the order it was done.
	Actions  []string
	Pasted   []string
	captures int
	cutAt    int
	cut      bool
}

func (f *fakePanes) IsAgentRunning(agentstore.Agent) bool { return f.Alive }

func (f *fakePanes) SendKeys(string, ...string) error { return nil }

func (f *fakePanes) IsPaneInMode(string) bool { return f.InMode }

func (f *fakePanes) IsPaneRunningCLI(string) bool { return !f.Shell }

func (f *fakePanes) CapturePane(string) string {
	f.captures++

	if f.cut && f.captures > f.cutAt+f.StaleReads {
		if f.AfterCut != "" {
			return f.AfterCut
		}

		return idleFrame
	}

	return f.Frame
}

func (f *fakePanes) IsClientAtPane(string, time.Duration) bool { return f.ClientAt }

func (f *fakePanes) PasteAndSubmit(_, text string) error {
	if f.PasteErr != nil {
		f.Actions = append(f.Actions, "paste-failed")

		return f.PasteErr
	}

	f.Actions = append(f.Actions, "paste")
	f.Pasted = append(f.Pasted, text)

	return nil
}

func (f *fakePanes) CutLine(string) error {
	f.Actions = append(f.Actions, "cut")
	f.cut = true
	f.cutAt = f.captures

	return nil
}

func (f *fakePanes) YankLine(string) error {
	if f.YankErr != nil {
		f.Actions = append(f.Actions, "yank-failed")

		return f.YankErr
	}

	f.Actions = append(f.Actions, "yank")

	return nil
}

// TestDeliverAllVisitsEveryMailboxHoldingMail pins that a sweep reaches every mailbox with
// something waiting in it.
func TestDeliverAllVisitsEveryMailboxHoldingMail(t *testing.T) {
	agents := &fakeAgents{}
	panes := &fakePanes{Frame: idleFrame}
	mailbox := &fakeMailboxReader{mailboxes: []string{"agent-a", "agent-b"}}

	svc := NewDeliveryService(NewDeliveryServiceParams{
		MailboxReader: mailbox,
		NudgeClaimer:  agents,
		PaneReader:    panes,
		Channel:       pane.NewPane(pane.NewPaneParams{Terminal: panes, Harnesses: fakeHarnesses{harness: &fakeHarness{}}}),
	})

	require.NoError(t, svc.DeliverAll(t.Context()))
	assert.Empty(t, panes.Pasted, "mailboxes belonging to no agent are left alone")
}

// TestDeliverAllDoesNothingWhenNoMailIsWaiting pins that an empty sweep touches no pane.
func TestDeliverAllDoesNothingWhenNoMailIsWaiting(t *testing.T) {
	agents := &fakeAgents{}
	panes := &fakePanes{}

	svc := NewDeliveryService(NewDeliveryServiceParams{
		MailboxReader: &fakeMailboxReader{},
		NudgeClaimer:  agents,
		PaneReader:    panes,
		Channel:       pane.NewPane(pane.NewPaneParams{Terminal: panes, Harnesses: fakeHarnesses{harness: &fakeHarness{}}}),
	})

	require.NoError(t, svc.DeliverAll(t.Context()))
	assert.False(t, agents.taken)
}

// TestDeliverRetriesANudgeTheAgentNeverActedOn pins that unread mail puts the nudge back.
func TestDeliverRetriesANudgeTheAgentNeverActedOn(t *testing.T) {
	st := storetest.Open(t)
	ctx := t.Context()
	agent := seedIdler(t, st, "idler")

	_, err := st.Mail.Send(ctx, mailstore.SendParams{
		FromName:  "somebody",
		ToAgentID: agent.ID,
		Body:      "please read this",
		Deadline:  time.Hour,
	})
	require.NoError(t, err)

	panes := &fakePanes{Alive: true, Frame: idleFrame}
	svc := NewDeliveryService(NewDeliveryServiceParams{
		MailboxReader: st.Mail,
		NudgeClaimer:  st.Agents,
		PaneReader:    panes,
		Channel:       pane.NewPane(pane.NewPaneParams{Terminal: panes, Harnesses: fakeHarnesses{harness: &fakeHarness{}}}),
	})

	got, err := svc.Deliver(ctx, agent.ID)
	require.NoError(t, err)
	assert.Equal(t, DeliverySoon, got)
	require.Len(t, panes.Pasted, 1, "an idle agent holding mail is told about it")

	for range 30 {
		got, err = svc.Deliver(ctx, agent.ID)
		require.NoError(t, err)
		assert.Equal(t, DeliveryLater, got)
	}
	assert.Len(t, panes.Pasted, 1, "and not told again once a pass")

	backdateNudge(t, st, agent.ID, nudgeRetryAfter+time.Minute)

	got, err = svc.Deliver(ctx, agent.ID)
	require.NoError(t, err)
	assert.Equal(t, DeliverySoon, got)
	assert.Len(t, panes.Pasted, 2, "a nudge that did not get the mail read is sent again")

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
	st := storetest.Open(t)
	ctx := t.Context()
	agent := seedIdler(t, st, "sleeper")

	_, err := st.Mail.Send(ctx, mailstore.SendParams{
		FromName:  "somebody",
		ToAgentID: agent.ID,
		Body:      "please read this",
		Deadline:  time.Hour,
	})
	require.NoError(t, err)

	panes := &fakePanes{Alive: true, Frame: idleFrame}
	svc := NewDeliveryService(NewDeliveryServiceParams{
		MailboxReader: st.Mail,
		NudgeClaimer:  st.Agents,
		PaneReader:    panes,
		Channel:       pane.NewPane(pane.NewPaneParams{Terminal: panes, Harnesses: fakeHarnesses{harness: &fakeHarness{}}}),
	})

	got, err := svc.Deliver(ctx, agent.ID)
	require.NoError(t, err)
	assert.Equal(t, DeliverySoon, got)
	require.Len(t, panes.Pasted, 1)

	_, archiveErr := st.Agents.Archive(ctx, []string{agent.ID})

	require.NoError(t, archiveErr)

	got, err = svc.Deliver(ctx, agent.ID)
	require.NoError(t, err)
	assert.Equal(t, DeliveryDone, got, "an archived agent is not typed at")
	assert.Len(t, panes.Pasted, 1)

	require.NoError(t, st.Agents.Restore(ctx, []string{agent.ID}))
	backdateNudge(t, st, agent.ID, nudgeRetryAfter+time.Minute)

	got, err = svc.Deliver(ctx, agent.ID)
	require.NoError(t, err)
	assert.Equal(t, DeliverySoon, got)
	assert.Len(t, panes.Pasted, 2, "an agent back from the archive is told about the mail it never read")
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
		{name: "codex", harness: codex.NewHarness(codex.NewHarnessParams{Home: t.TempDir()})},
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
			panes := &fakePanes{Alive: true, Frame: shellEcho}
			svc := NewDeliveryService(NewDeliveryServiceParams{
				MailboxReader: &fakeMailboxReader{pending: 1},
				NudgeClaimer:  agents,
				PaneReader:    panes,
				Channel:       pane.NewPane(pane.NewPaneParams{Terminal: panes, Harnesses: fixedHarnesses{harness: tt.harness}}),
			})

			got, err := svc.Deliver(t.Context(), "agent-a")
			require.NoError(t, err)
			assert.Equal(t, DeliveryLater, got)
			assert.Empty(t, panes.Actions, "nothing is sent to a pane that is not listening")
		})
	}
}

// TestDeliverTellsTheChannelToForgetAnAgentNothingWillLookAtAgain pins that a channel holding
// something about an agent is told when no pass will reach that agent again.
func TestDeliverTellsTheChannelToForgetAnAgentNothingWillLookAtAgain(t *testing.T) {
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
			name: "an agent archived while its mail waited",
			gone: func(agents *fakeAgents) { agents.agent.ArchivedAt = &archivedAt },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			panes := &fakePanes{Alive: true, Frame: composingFrame}
			agents := &fakeAgents{claimed: true, agent: &agentstore.Agent{
				ID:    "agent-a",
				Pane:  "%1",
				State: agentstore.StateIdle,
			}}
			channel := &fakeChannel{}
			svc := NewDeliveryService(NewDeliveryServiceParams{
				MailboxReader: &fakeMailboxReader{pending: 1},
				NudgeClaimer:  agents,
				PaneReader:    panes,
				Channel:       channel,
			})

			got, err := svc.Deliver(t.Context(), "agent-a")
			require.NoError(t, err)
			require.Equal(t, DeliverySoon, got)
			require.Empty(t, channel.Forgotten)

			tt.gone(agents)

			got, err = svc.Deliver(t.Context(), "agent-a")
			require.NoError(t, err)
			assert.Equal(t, DeliveryDone, got)
			assert.Equal(t, []string{"agent-a"}, channel.Forgotten, "nothing will reach this agent again")
		})
	}
}

// TestDeliverSpendsTheClaimOnAChannelThatTakesTheLineAndThenRefuses pins the two ways a send
// can fail after the claim was taken.
func TestDeliverSpendsTheClaimOnAChannelThatTakesTheLineAndThenRefuses(t *testing.T) {
	tests := []struct {
		name    string
		channel *fakeChannel
		wantErr bool
	}{
		{
			name:    "a channel that says yes and then declines",
			channel: &fakeChannel{RefuseSend: true},
		},
		{
			name:    "a channel whose send fails outright",
			channel: &fakeChannel{SendErr: errors.New("tmux said no")},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agents := &fakeAgents{claimed: true, agent: &agentstore.Agent{
				ID:    "agent-a",
				Pane:  "%1",
				State: agentstore.StateIdle,
			}}
			svc := NewDeliveryService(NewDeliveryServiceParams{
				MailboxReader: &fakeMailboxReader{pending: 1},
				NudgeClaimer:  agents,
				PaneReader:    &fakePanes{Alive: true, Frame: idleFrame},
				Channel:       tt.channel,
			})

			got, err := svc.Deliver(t.Context(), "agent-a")
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			assert.Equal(t, DeliveryLater, got)
			assert.True(t, agents.taken, "the claim is spent on a send that reached the agent")
			assert.Empty(t, tt.channel.Sent)
		})
	}
}

// TestDeliverLeavesTheClaimOnAChannelThatWasNeverReady pins that an agent weave never spoke to
// is left for the next pass rather than held off for the retry window.
func TestDeliverLeavesTheClaimOnAChannelThatWasNeverReady(t *testing.T) {
	agents := &fakeAgents{claimed: true, agent: &agentstore.Agent{
		ID:    "agent-a",
		Pane:  "%1",
		State: agentstore.StateIdle,
	}}
	channel := &fakeChannel{RefuseReady: true}
	svc := NewDeliveryService(NewDeliveryServiceParams{
		MailboxReader: &fakeMailboxReader{pending: 1},
		NudgeClaimer:  agents,
		PaneReader:    &fakePanes{Alive: true, Frame: idleFrame},
		Channel:       channel,
	})

	got, err := svc.Deliver(t.Context(), "agent-a")
	require.NoError(t, err)
	assert.Equal(t, DeliveryLater, got)
	assert.False(t, agents.taken, "nothing reached the agent, so the right to type is not spent")
	assert.Empty(t, channel.Sent)
}

// fakeChannel stands in for whatever reaches an agent, taking every line it is handed.
type fakeChannel struct {
	RefuseReady bool
	RefuseSend  bool
	SendErr     error
	Sent        []string
	Forgotten   []string
	Interrupts  int
}

func (f *fakeChannel) Ready(context.Context, harness.Target) (bool, error) {
	return !f.RefuseReady, nil
}

func (f *fakeChannel) Send(_ context.Context, _ harness.Target, text string) (bool, error) {
	if f.SendErr != nil {
		return false, f.SendErr
	}
	if f.RefuseSend {
		return false, nil
	}
	f.Sent = append(f.Sent, text)

	return true, nil
}

func (f *fakeChannel) Interrupt(context.Context, harness.Target) error {
	f.Interrupts++

	return nil
}

func (f *fakeChannel) Forget(agentID string) { f.Forgotten = append(f.Forgotten, agentID) }
