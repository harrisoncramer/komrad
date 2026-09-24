package mail

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"komrade/db/testmodels"
	"komrade/internal/agents"
	"komrade/internal/harness"
	"komrade/internal/harness/claude"
	"komrade/internal/harness/codex"
	"komrade/internal/harness/pane"
	"komrade/internal/harnesstest"
	"komrade/internal/logging"
	agentstore "komrade/internal/store/agent"
	mailstore "komrade/internal/store/mail"
	"komrade/internal/storetest"
	"komrade/internal/tmuxtest"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const nudgeLine = "komrade: 1 piece(s) of unread mail, call read_mail"

// nudgeSentFailer is the real agent store refusing to record that a nudge was sent.
type nudgeSentFailer struct {
	*agentstore.AgentStore
}

// SetNudgeSent refuses the write.
func (nudgeSentFailer) SetNudgeSent(context.Context, string) error {
	return errors.New("no database")
}

// TestDeliver pins which agents mail is handed to and which are passed over, covering the pane, the state, and how recently it was last told.
func TestDeliver(t *testing.T) {
	tests := []struct {
		name      string
		state     agentstore.State
		noMail    bool
		paneGone  bool
		shell     bool
		inMode    bool
		frame     string
		arrange   func(t *testing.T, st *storetest.Stores, id string)
		want      Delivery
		wantClaim bool
		wantNudge bool
	}{
		{
			name: "an agent archived since the mail was sent",
			arrange: func(t *testing.T, st *storetest.Stores, id string) {
				_, err := st.Agents.Archive(t.Context(), []string{id})
				require.NoError(t, err)
			},
			want: DeliveryDone,
		},
		{
			name: "an agent a person is holding, whose tree is not komrade's to type into",
			arrange: func(t *testing.T, st *storetest.Stores, id string) {
				_, err := st.Agents.Hold(t.Context(), agentstore.HoldParams{
					ID: id,
					At: time.Now(),
				})
				require.NoError(t, err)
			},
			want: DeliveryLater,
		},
		{
			name:   "an empty mailbox",
			noMail: true,
			want:   DeliveryDone,
		},
		{
			name:  "an agent mid-turn, which its own stop hook will catch",
			state: agentstore.StateThinking,
			want:  DeliverySoon,
		},
		{
			name:  "an agent sitting on a permission prompt is never typed at",
			state: agentstore.StateWaiting,
			want:  DeliveryLater,
		},
		{
			name:    "a prompt nothing has confirmed is still left alone, since keys typed at one answer it",
			state:   agentstore.StateWaiting,
			arrange: backdateStateSeen,
			want:    DeliveryLater,
		},
		{
			name:     "an agent whose pane is gone",
			paneGone: true,
			want:     DeliveryLater,
		},
		{
			name:  "an agent whose CLI has exited with its pane still stamped as its own",
			shell: true,
			want:  DeliveryLater,
		},
		{
			name:   "an agent whose pane the user is scrolled back in",
			inMode: true,
			want:   DeliveryLater,
		},
		{
			name:  "an agent whose pane has opened but whose CLI has not finished coming up",
			frame: harnesstest.FrameUnrecognized,
			want:  DeliveryLater,
		},
		{
			name:  "an agent whose prompt somebody is halfway through typing into",
			frame: harnesstest.FrameComposing,
			want:  DeliveryLater,
		},
		{
			name: "an agent told about the mail recently enough that the line may yet be answered",
			arrange: func(t *testing.T, st *storetest.Stores, id string) {
				claimed, err := st.Agents.ClaimNudge(t.Context(), id, nudgeRetryAfter)
				require.NoError(t, err)
				require.True(t, claimed)
			},
			want:      DeliveryLater,
			wantClaim: true,
		},
		{
			name:      "an idle agent never told before",
			want:      DeliverySoon,
			wantClaim: true,
			wantNudge: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := storetest.Open(t)
			terminal := tmuxtest.NewFake()
			agent := seedListeningAgent(t, st, terminal)
			if tt.state != "" {
				require.NoError(t, st.Agents.SetStateByID(t.Context(), agent.ID, tt.state))
			}
			if !tt.noMail {
				sendTo(t, st, agent.ID)
			}
			if tt.paneGone {
				delete(terminal.Owners, agent.Pane)
			}
			if tt.shell {
				terminal.Commands[agent.Pane] = "zsh"
			}
			if tt.frame != "" {
				terminal.Captures[agent.Pane] = tt.frame
			}
			terminal.InMode[agent.Pane] = tt.inMode
			if tt.arrange != nil {
				tt.arrange(t, st, agent.ID)
			}

			svc := newDeliveryService(st, terminal, newPaneChannel(terminal))

			got, err := svc.Deliver(t.Context(), agent.ID)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)

			stored, err := st.Agents.GetByID(t.Context(), agent.ID)
			require.NoError(t, err)
			assert.Equal(t, tt.wantClaim, stored.NudgedAt != nil,
				"the right to type is only spent on an agent that could have taken the keys")

			if !tt.wantNudge {
				assert.Empty(t, terminal.Sent[agent.Pane], "nothing is sent to the agent")

				return
			}

			assert.Equal(t, [][]string{{nudgeLine}}, terminal.Sent[agent.Pane],
				"the line goes as one paste and nothing else is typed")
		})
	}
}

// TestDeliverNudgesAnAgentMidTurnThatNoPassHasConfirmed pins that a thinking row nothing has seen lately is typed at rather than trusted.
func TestDeliverNudgesAnAgentMidTurnThatNoPassHasConfirmed(t *testing.T) {
	st := storetest.Open(t)
	terminal := tmuxtest.NewFake()
	agent := seedListeningAgent(t, st, terminal)
	require.NoError(t, st.Agents.SetStateByID(t.Context(), agent.ID, agentstore.StateThinking))
	backdateStateSeen(t, st, agent.ID)
	sendTo(t, st, agent.ID)

	svc := newDeliveryService(st, terminal, newPaneChannel(terminal))

	got, err := svc.Deliver(t.Context(), agent.ID)
	require.NoError(t, err)
	assert.Equal(t, DeliverySoon, got)
	assert.Equal(t, [][]string{{nudgeLine}}, terminal.Sent[agent.Pane])
}

// TestDeliverSaysWhenANudgeWentNowhere pins which second nudge is worth a warning.
func TestDeliverSaysWhenANudgeWentNowhere(t *testing.T) {
	tests := []struct {
		name    string
		arrange func(t *testing.T, st *storetest.Stores, id string)
		want    string
	}{
		{
			name: "an agent that has ended no turn since the last nudge",
			arrange: func(t *testing.T, st *storetest.Stores, id string) {
				require.NoError(t, st.Agents.SetTurnEnded(t.Context(), id))
				require.NoError(t, st.Agents.SetNudgeSent(t.Context(), id))
			},
			want: "WARN",
		},
		{
			name: "an agent that has never reported a turn at all",
			arrange: func(t *testing.T, st *storetest.Stores, id string) {
				require.NoError(t, st.Agents.SetNudgeSent(t.Context(), id))
			},
			want: "WARN",
		},
		{
			name: "an agent that ended a turn after the last nudge",
			arrange: func(t *testing.T, st *storetest.Stores, id string) {
				require.NoError(t, st.Agents.SetNudgeSent(t.Context(), id))
				require.NoError(t, st.Agents.SetTurnEnded(t.Context(), id))
			},
			want: "INFO",
		},
		{
			name:    "an agent nobody has told before",
			arrange: func(*testing.T, *storetest.Stores, string) {},
			want:    "INFO",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := storetest.Open(t)
			terminal := tmuxtest.NewFake()
			agent := seedListeningAgent(t, st, terminal)
			sendTo(t, st, agent.ID)
			tt.arrange(t, st, agent.ID)

			svc := newDeliveryService(st, terminal, newPaneChannel(terminal))
			ctx, records := newLoggedContext(t)

			got, err := svc.Deliver(ctx, agent.ID)
			require.NoError(t, err)
			require.Equal(t, DeliverySoon, got)

			written := records()
			require.Len(t, written, 1)
			assert.Equal(t, tt.want, written[0]["severity_text"])
		})
	}
}

// TestDeliverDoesNotWarnAfterAPassThatTypedNothing pins that a claim spent on a refused send leaves the went-nowhere warning unarmed.
func TestDeliverDoesNotWarnAfterAPassThatTypedNothing(t *testing.T) {
	st := storetest.Open(t)
	terminal := tmuxtest.NewFake()
	agent := seedListeningAgent(t, st, terminal)
	sendTo(t, st, agent.ID)

	channel := &harnesstest.Channel{RefuseSend: true}
	svc := newDeliveryService(st, terminal, channel)
	ctx, records := newLoggedContext(t)

	got, err := svc.Deliver(ctx, agent.ID)
	require.NoError(t, err)
	require.Equal(t, DeliveryLater, got)
	require.Empty(t, channel.Sent, "the channel refused the line, so nothing was typed")

	channel.RefuseSend = false
	backdateNudge(t, st, agent.ID)

	got, err = svc.Deliver(ctx, agent.ID)
	require.NoError(t, err)
	require.Equal(t, DeliverySoon, got)
	require.Len(t, channel.Sent, 1)

	written := records()
	require.Len(t, written, 1)
	assert.Equal(t, "INFO", written[0]["severity_text"], "the agent has been told once, so nothing went nowhere")
}

// TestDeliverKeepsALandedNudgeWhenTheBookkeepingFails pins that a nudge already typed is not reported as a failed delivery.
func TestDeliverKeepsALandedNudgeWhenTheBookkeepingFails(t *testing.T) {
	st := storetest.Open(t)
	terminal := tmuxtest.NewFake()
	agent := seedListeningAgent(t, st, terminal)
	sendTo(t, st, agent.ID)

	channel := &harnesstest.Channel{}
	svc := NewDeliveryService(NewDeliveryServiceParams{
		MailboxReader: st.Mail,
		NudgeClaimer:  nudgeSentFailer{AgentStore: st.Agents},
		PaneReader:    agents.NewPaneLiveness(terminal),
		Channel:       channel,
	})
	ctx, records := newLoggedContext(t)

	got, err := svc.Deliver(ctx, agent.ID)
	require.NoError(t, err, "the rest of the pass goes on")
	assert.Equal(t, DeliverySoon, got)
	assert.Len(t, channel.Sent, 1)

	written := records()
	require.Len(t, written, 2)
	assert.Equal(t, "ERROR", written[1]["severity_text"])
}

// TestDeliverReportsExpiredMail pins that mail nobody read before its deadline is reported rather than dropped in silence.
func TestDeliverReportsExpiredMail(t *testing.T) {
	st := storetest.Open(t)
	terminal := tmuxtest.NewFake()
	agent := seedListeningAgent(t, st, terminal)
	expired, err := st.Mail.Send(t.Context(), mailstore.SendParams{
		FromName:  "somebody",
		ToAgentID: agent.ID,
		Body:      "too late",
		Deadline:  time.Nanosecond,
	})
	require.NoError(t, err)

	svc := newDeliveryService(st, terminal, newPaneChannel(terminal))
	ctx, records := newLoggedContext(t)

	got, err := svc.Deliver(ctx, agent.ID)
	require.NoError(t, err)
	assert.Equal(t, DeliveryDone, got)
	assert.Empty(t, terminal.Sent[agent.Pane])

	written := records()
	require.Len(t, written, 1)
	assert.Equal(t, "WARN", written[0]["severity_text"])
	assert.Equal(t, expired.ID, written[0][logging.MailID])
}

// TestDeliverAll pins that a sweep tells every agent holding mail and leaves the rest alone.
func TestDeliverAll(t *testing.T) {
	st := storetest.Open(t)
	terminal := tmuxtest.NewFake()
	first := seedListeningAgent(t, st, terminal)
	second := seedListeningAgent(t, st, terminal)
	empty := seedListeningAgent(t, st, terminal)
	sendTo(t, st, first.ID)
	sendTo(t, st, second.ID)

	svc := newDeliveryService(st, terminal, newPaneChannel(terminal))

	require.NoError(t, svc.DeliverAll(t.Context()))
	assert.Equal(t, [][]string{{nudgeLine}}, terminal.Sent[first.Pane])
	assert.Equal(t, [][]string{{nudgeLine}}, terminal.Sent[second.Pane])
	assert.Empty(t, terminal.Sent[empty.Pane])
}

// TestDeliverRetriesANudgeTheAgentNeverActedOn pins that unread mail puts the nudge back once the retry window has passed.
func TestDeliverRetriesANudgeTheAgentNeverActedOn(t *testing.T) {
	st := storetest.Open(t)
	ctx := t.Context()
	terminal := tmuxtest.NewFake()
	agent := seedListeningAgent(t, st, terminal)
	sendTo(t, st, agent.ID)

	svc := newDeliveryService(st, terminal, newPaneChannel(terminal))

	got, err := svc.Deliver(ctx, agent.ID)
	require.NoError(t, err)
	assert.Equal(t, DeliverySoon, got)
	require.Len(t, terminal.Sent[agent.Pane], 1, "an idle agent holding mail is told about it")

	for range 30 {
		got, err = svc.Deliver(ctx, agent.ID)
		require.NoError(t, err)
		assert.Equal(t, DeliveryLater, got)
	}
	assert.Len(t, terminal.Sent[agent.Pane], 1, "and not told again once a pass")

	backdateNudge(t, st, agent.ID)

	got, err = svc.Deliver(ctx, agent.ID)
	require.NoError(t, err)
	assert.Equal(t, DeliverySoon, got)
	assert.Len(t, terminal.Sent[agent.Pane], 2, "a nudge that did not get the mail read is sent again")

	stranded, err := st.Agents.GetByID(ctx, agent.ID)
	require.NoError(t, err)
	assert.Equal(t, agentstore.StateIdle, stranded.State, "nothing about the agent moved to earn the retry")

	pending, err := st.Mail.CountPending(ctx, agent.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(1), pending, "and the mail is still waiting")
}

// TestDeliverRetriesANudgeAcrossAnArchive pins the same retry across an archive, which leaves the agent's spent nudge as it was.
func TestDeliverRetriesANudgeAcrossAnArchive(t *testing.T) {
	st := storetest.Open(t)
	ctx := t.Context()
	terminal := tmuxtest.NewFake()
	agent := seedListeningAgent(t, st, terminal)
	sendTo(t, st, agent.ID)

	svc := newDeliveryService(st, terminal, newPaneChannel(terminal))

	got, err := svc.Deliver(ctx, agent.ID)
	require.NoError(t, err)
	assert.Equal(t, DeliverySoon, got)
	require.Len(t, terminal.Sent[agent.Pane], 1)

	_, err = st.Agents.Archive(ctx, []string{agent.ID})
	require.NoError(t, err)

	got, err = svc.Deliver(ctx, agent.ID)
	require.NoError(t, err)
	assert.Equal(t, DeliveryDone, got, "an archived agent is not typed at")
	assert.Len(t, terminal.Sent[agent.Pane], 1)

	require.NoError(t, st.Agents.Restore(ctx, []string{agent.ID}))

	restored, err := st.Agents.GetByID(ctx, agent.ID)
	require.NoError(t, err)
	restored.Pane = agent.Pane
	require.NoError(t, st.Agents.Update(ctx, *restored), "resumed into the pane it had")

	got, err = svc.Deliver(ctx, agent.ID)
	require.NoError(t, err)
	assert.Equal(t, DeliveryLater, got, "the nudge spent before the archive still holds")
	assert.Len(t, terminal.Sent[agent.Pane], 1)

	backdateNudge(t, st, agent.ID)

	got, err = svc.Deliver(ctx, agent.ID)
	require.NoError(t, err)
	assert.Equal(t, DeliverySoon, got)
	assert.Len(t, terminal.Sent[agent.Pane], 2, "an agent back from the archive is told about the mail it never read")
}

// TestDeliverDefersToARealHarnessStillComingUp pins that each registered harness reads a pane still echoing its launch as not listening.
func TestDeliverDefersToARealHarnessStillComingUp(t *testing.T) {
	shellEcho := "harry@mac ~/komrade % KOMRADE_AGENT_ID=01a0 codex resume 019abc"

	tests := []struct {
		name    string
		harness harness.Harness
	}{
		{
			name:    "claude",
			harness: claude.NewHarness(claude.NewHarnessParams{}),
		},
		{
			name:    "codex",
			harness: codex.NewHarness(codex.NewHarnessParams{Home: t.TempDir()}),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := storetest.Open(t)
			terminal := tmuxtest.NewFake()
			agent := seedListeningAgent(t, st, terminal)
			require.NoError(t, st.Agents.SetHarness(t.Context(), agentstore.SetHarnessParams{
				ID:      agent.ID,
				Harness: tt.harness.ID(),
			}))
			terminal.Captures[agent.Pane] = shellEcho
			sendTo(t, st, agent.ID)

			registry, err := harness.NewRegistry(tt.harness)
			require.NoError(t, err)

			svc := newDeliveryService(st, terminal, pane.NewPane(pane.NewPaneParams{
				Terminal:  terminal,
				Harnesses: registry,
			}))

			got, err := svc.Deliver(t.Context(), agent.ID)
			require.NoError(t, err)
			assert.Equal(t, DeliveryLater, got)
			assert.Empty(t, terminal.Sent[agent.Pane], "nothing is sent to a pane that is not listening")
		})
	}
}

// TestDeliverTellsTheChannelToForgetAnAgentNothingWillLookAtAgain pins that the channel is told when no pass will reach an agent again.
func TestDeliverTellsTheChannelToForgetAnAgentNothingWillLookAtAgain(t *testing.T) {
	tests := []struct {
		name string
		gone func(t *testing.T, st *storetest.Stores, id string)
	}{
		{
			name: "an agent no row holds any more",
			gone: func(t *testing.T, st *storetest.Stores, id string) {
				_, err := st.Agents.Delete(t.Context(), []string{id})
				require.NoError(t, err)
			},
		},
		{
			name: "an agent archived while its mail waited",
			gone: func(t *testing.T, st *storetest.Stores, id string) {
				_, err := st.Agents.Archive(t.Context(), []string{id})
				require.NoError(t, err)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := storetest.Open(t)
			terminal := tmuxtest.NewFake()
			agent := seedListeningAgent(t, st, terminal)
			sendTo(t, st, agent.ID)

			channel := &harnesstest.Channel{}
			svc := newDeliveryService(st, terminal, channel)

			got, err := svc.Deliver(t.Context(), agent.ID)
			require.NoError(t, err)
			require.Equal(t, DeliverySoon, got)
			require.Empty(t, channel.Forgotten)

			tt.gone(t, st, agent.ID)

			got, err = svc.Deliver(t.Context(), agent.ID)
			require.NoError(t, err)
			assert.Equal(t, DeliveryDone, got)
			assert.Equal(t, []string{agent.ID}, channel.Forgotten, "nothing will reach this agent again")
		})
	}
}

// TestDeliverSpendsTheClaimOnAChannelThatTakesTheLineAndThenRefuses pins the two ways a send can fail after the claim was taken.
func TestDeliverSpendsTheClaimOnAChannelThatTakesTheLineAndThenRefuses(t *testing.T) {
	tests := []struct {
		name    string
		channel *harnesstest.Channel
		wantErr bool
	}{
		{
			name:    "a channel whose send fails outright",
			channel: &harnesstest.Channel{SendErr: errors.New("tmux said no")},
			wantErr: true,
		},
		{
			name:    "a channel that says yes and then declines",
			channel: &harnesstest.Channel{RefuseSend: true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := storetest.Open(t)
			terminal := tmuxtest.NewFake()
			agent := seedListeningAgent(t, st, terminal)
			sendTo(t, st, agent.ID)

			svc := newDeliveryService(st, terminal, tt.channel)

			got, err := svc.Deliver(t.Context(), agent.ID)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			assert.Equal(t, DeliveryLater, got)
			assert.Empty(t, tt.channel.Sent)

			stored, err := st.Agents.GetByID(t.Context(), agent.ID)
			require.NoError(t, err)
			assert.NotNil(t, stored.NudgedAt, "the claim is spent on a send that reached the agent")
		})
	}
}

// TestDeliverLeavesTheClaimOnAChannelThatWasNeverReady pins that an agent komrade never spoke to is left for the next pass.
func TestDeliverLeavesTheClaimOnAChannelThatWasNeverReady(t *testing.T) {
	st := storetest.Open(t)
	terminal := tmuxtest.NewFake()
	agent := seedListeningAgent(t, st, terminal)
	sendTo(t, st, agent.ID)

	channel := &harnesstest.Channel{RefuseReady: true}
	svc := newDeliveryService(st, terminal, channel)

	got, err := svc.Deliver(t.Context(), agent.ID)
	require.NoError(t, err)
	assert.Equal(t, DeliveryLater, got)
	assert.Empty(t, channel.Sent)

	stored, err := st.Agents.GetByID(t.Context(), agent.ID)
	require.NoError(t, err)
	assert.Nil(t, stored.NudgedAt, "nothing reached the agent, so the right to type is not spent")
}

func newDeliveryService(st *storetest.Stores, terminal *tmuxtest.Fake, channel harness.Channel) *DeliveryService {
	return NewDeliveryService(NewDeliveryServiceParams{
		MailboxReader: st.Mail,
		NudgeClaimer:  st.Agents,
		PaneReader:    agents.NewPaneLiveness(terminal),
		Channel:       channel,
	})
}

func newPaneChannel(terminal *tmuxtest.Fake) *pane.Pane {
	return pane.NewPane(pane.NewPaneParams{
		Terminal:  terminal,
		Harnesses: harnesstest.Registry{Harness: &harnesstest.Fake{NoSession: true}},
	})
}

// seedListeningAgent records an idle agent whose pane is its own and shows an empty prompt.
func seedListeningAgent(t *testing.T, st *storetest.Stores, terminal *tmuxtest.Fake) agentstore.Agent {
	t.Helper()

	name := uuid.NewString()
	agent := st.SeedAgent(t, agentstore.Agent{
		Name:    name,
		Pane:    "%" + name,
		State:   agentstore.StateIdle,
		Harness: harnesstest.FakeID,
	})

	terminal.Owners[agent.Pane] = agent.ID
	terminal.Commands[agent.Pane] = harnesstest.FakeID
	terminal.Captures[agent.Pane] = harnesstest.FrameIdle

	return *agent
}

func sendTo(t *testing.T, st *storetest.Stores, agentID string) {
	t.Helper()

	_, err := st.Mail.Send(t.Context(), mailstore.SendParams{
		FromName:  "somebody",
		ToAgentID: agentID,
		Body:      "please read this",
		Deadline:  time.Hour,
	})
	require.NoError(t, err)
}

func backdateNudge(t *testing.T, st *storetest.Stores, id string) {
	t.Helper()

	queries := testmodels.New(st.Pool.GetDB())
	require.NoError(t, queries.BackdateAgentNudge(t.Context(), testmodels.BackdateAgentNudgeParams{
		AgeMicros: (nudgeRetryAfter + time.Minute).Microseconds(),
		ID:        id,
	}))
}

func backdateStateSeen(t *testing.T, st *storetest.Stores, id string) {
	t.Helper()

	queries := testmodels.New(st.Pool.GetDB())
	require.NoError(t, queries.BackdateAgentStateSeen(t.Context(), testmodels.BackdateAgentStateSeenParams{
		AgeMicros: (agentstore.StateSeenBound + time.Minute).Microseconds(),
		ID:        id,
	}))
}

// newLoggedContext is a context whose log lines a test can read back as records.
func newLoggedContext(t *testing.T) (context.Context, func() []map[string]any) {
	t.Helper()

	var written bytes.Buffer

	ctx := logging.WithLogger(t.Context(), logging.New(&written, slog.LevelDebug))

	return ctx, func() []map[string]any {
		records := make([]map[string]any, 0, 2)
		for _, line := range strings.Split(strings.TrimSpace(written.String()), "\n") {
			if line == "" {
				continue
			}

			var record map[string]any
			require.NoError(t, json.Unmarshal([]byte(line), &record))
			records = append(records, record)
		}

		return records
	}
}
