package agents

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
				Channel:       pane.NewPane(pane.NewPaneParams{Terminal: panes, Harnesses: fakeHarnesses{harness: &fakeHarness{}}}),
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
		Channel:       pane.NewPane(pane.NewPaneParams{Terminal: panes, Harnesses: fakeHarnesses{harness: &fakeHarness{}}}),
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

	// pasteErr and yankErr are what tmux refuses with, which is a query timing out under load
	// or a pane that has just gone.
	pasteErr error
	yankErr  error

	// actions is what was done to the pane, in the order it was done.
	actions  []string
	pasted   []string
	captures int
	cutAt    int
	cut      bool
}

func (f *fakePanes) IsAgentRunning(agentstore.Agent) bool { return f.alive }

func (f *fakePanes) SendKeys(string, ...string) error { return nil }

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
	if f.pasteErr != nil {
		f.actions = append(f.actions, "paste-failed")

		return f.pasteErr
	}

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
	if f.yankErr != nil {
		f.actions = append(f.actions, "yank-failed")

		return f.yankErr
	}

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
		Channel:       pane.NewPane(pane.NewPaneParams{Terminal: panes, Harnesses: fakeHarnesses{harness: &fakeHarness{}}}),
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
		Channel:       pane.NewPane(pane.NewPaneParams{Terminal: panes, Harnesses: fakeHarnesses{harness: &fakeHarness{}}}),
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
		Panes:    tmux,
		Observer: pane.NewPane(pane.NewPaneParams{Terminal: tmux, Harnesses: fakeHarnesses{harness: &fakeHarness{}, only: []string{testHarnessID}}}),
		States:   st.Agents,
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
		Channel:       pane.NewPane(pane.NewPaneParams{Terminal: panes, Harnesses: fakeHarnesses{harness: &fakeHarness{}, only: []string{testHarnessID}}}),
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
		Channel:       pane.NewPane(pane.NewPaneParams{Terminal: panes, Harnesses: fakeHarnesses{harness: &fakeHarness{}}}),
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
		Channel:       pane.NewPane(pane.NewPaneParams{Terminal: panes, Harnesses: fakeHarnesses{harness: &fakeHarness{}}}),
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
				Channel:       pane.NewPane(pane.NewPaneParams{Terminal: panes, Harnesses: fixedHarnesses{harness: tt.harness}}),
			})

			got, err := svc.Deliver(t.Context(), "agent-a")
			require.NoError(t, err)
			assert.Equal(t, DeliveryLater, got)
			assert.Empty(t, panes.actions, "nothing is sent to a pane that is not listening")
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
			panes := &fakePanes{alive: true, frame: composingFrame}
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
			require.Empty(t, channel.forgotten)

			tt.gone(agents)

			got, err = svc.Deliver(t.Context(), "agent-a")
			require.NoError(t, err)
			assert.Equal(t, DeliveryDone, got)
			assert.Equal(t, []string{"agent-a"}, channel.forgotten, "nothing will reach this agent again")
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
			channel: &fakeChannel{refuseSend: true},
		},
		{
			name:    "a channel whose send fails outright",
			channel: &fakeChannel{sendErr: errors.New("tmux said no")},
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
				PaneReader:    &fakePanes{alive: true, frame: idleFrame},
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
			assert.Empty(t, tt.channel.sent)
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
	channel := &fakeChannel{refuseReady: true}
	svc := NewDeliveryService(NewDeliveryServiceParams{
		MailboxReader: &fakeMailboxReader{pending: 1},
		NudgeClaimer:  agents,
		PaneReader:    &fakePanes{alive: true, frame: idleFrame},
		Channel:       channel,
	})

	got, err := svc.Deliver(t.Context(), "agent-a")
	require.NoError(t, err)
	assert.Equal(t, DeliveryLater, got)
	assert.False(t, agents.taken, "nothing reached the agent, so the right to type is not spent")
	assert.Empty(t, channel.sent)
}

// fakeChannel stands in for whatever reaches an agent, taking every line it is handed.
type fakeChannel struct {
	refuseReady bool
	refuseSend  bool
	sendErr     error
	sent        []string
	forgotten   []string
}

func (f *fakeChannel) Ready(context.Context, harness.Target) (bool, error) {
	return !f.refuseReady, nil
}

func (f *fakeChannel) Send(_ context.Context, _ harness.Target, text string) (bool, error) {
	if f.sendErr != nil {
		return false, f.sendErr
	}
	if f.refuseSend {
		return false, nil
	}
	f.sent = append(f.sent, text)

	return true, nil
}

func (f *fakeChannel) Interrupt(context.Context, harness.Target) error { return nil }

func (f *fakeChannel) Forget(agentID string) { f.forgotten = append(f.forgotten, agentID) }
