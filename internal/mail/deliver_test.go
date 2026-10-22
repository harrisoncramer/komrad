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

	"komrad/internal/harnesstest"
	"komrad/internal/logging"
	agentstore "komrad/internal/store/agent"
	mailstore "komrad/internal/store/mail"
	"komrad/internal/storetest"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const nudgeLine = "komrad: 1 piece(s) of unread mail at 15:04:05, call read_mail"

const movingDir = "/moving"

// TestDeliver pins which agents are told about their mail, and what a send that reached nothing
// or failed leaves behind.
func TestDeliver(t *testing.T) {
	tests := []struct {
		name      string
		state     agentstore.State
		cwd       string
		noMail    bool
		refuse    bool
		sendErr   error
		arrange   func(t *testing.T, st *storetest.Stores, id string)
		want      Delivery
		wantErr   bool
		wantClaim bool
		wantNudge bool
	}{
		{
			name: "an agent archived since the mail was sent",
			arrange: func(t *testing.T, st *storetest.Stores, id uuid.UUID) {
				_, err := st.Agents.Archive(t.Context(), []uuid.UUID{id})
				require.NoError(t, err)
			},
			want: DeliveryDone,
		},
		{
			name:   "an empty mailbox",
			noMail: true,
			want:   DeliveryDone,
		},
		{
			name:  "an agent sitting on a permission prompt",
			state: agentstore.StateWaiting,
			want:  DeliveryLater,
		},
		{
			name: "an agent whose checkout komrad is moving",
			cwd:  movingDir,
			want: DeliveryLater,
		},
		{
			name: "an agent already told about the mail it holds",
			arrange: func(t *testing.T, st *storetest.Stores, id string) {
				claimed, err := st.Mail.ClaimNudge(t.Context(), id)
				require.NoError(t, err)
				require.True(t, claimed)
			},
			want:      DeliveryLater,
			wantClaim: true,
		},
		{
			name:   "an agent whose CLI is not listening gives the claim back",
			refuse: true,
			want:   DeliveryLater,
		},
		{
			name:    "a send that failed before reaching the CLI gives the claim back",
			refuse:  true,
			sendErr: errors.New("initialize timed out"),
			want:    DeliveryLater,
			wantErr: true,
		},
		{
			name:      "a send that failed after the line went out keeps the claim",
			sendErr:   errors.New("connection reset"),
			want:      DeliveryLater,
			wantErr:   true,
			wantClaim: true,
		},
		{
			name:      "an agent mid-turn",
			state:     agentstore.StateThinking,
			want:      DeliverySoon,
			wantClaim: true,
			wantNudge: true,
		},
		{
			name:      "an idle agent never told",
			want:      DeliverySoon,
			wantClaim: true,
			wantNudge: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := storetest.Open(t)
			agent := seedAgent(t, st, tt.cwd)
			if tt.state != "" {
				require.NoError(t, st.Agents.SetStateByID(t.Context(), agent.ID, tt.state))
			}
			if !tt.noMail {
				sendTo(t, st, agent.ID)
			}
			if tt.arrange != nil {
				tt.arrange(t, st, agent.ID)
			}

			channel := &harnesstest.Channel{RefuseSend: tt.refuse, SendErr: tt.sendErr}
			got, err := newDeliveryService(st, channel).Deliver(t.Context(), agent.ID)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, tt.want, got)
			if tt.wantNudge {
				require.Len(t, channel.Sent, 1)
				assert.Equal(t, nudgeLine, channel.Sent[0])
			} else {
				assert.Empty(t, channel.Sent)
			}

			assert.Equal(t, tt.wantClaim, holdsClaim(t, st, agent.ID))
		})
	}
}

// TestDeliverReportsExpiredMail pins that komrad reports mail nobody read before its deadline
// rather than dropping it in silence.
func TestDeliverReportsExpiredMail(t *testing.T) {
	st := storetest.Open(t)
	agent := seedAgent(t, st, "")
	expired, err := st.Mail.Send(t.Context(), mailstore.SendParams{
		FromName:  "somebody",
		ToAgentID: agent.ID,
		Body:      "too late",
		Deadline:  time.Nanosecond,
	})
	require.NoError(t, err)

	channel := &harnesstest.Channel{}
	ctx, records := newLoggedContext(t)

	got, err := newDeliveryService(st, channel).Deliver(ctx, agent.ID)
	require.NoError(t, err)
	assert.Equal(t, DeliveryDone, got)
	assert.Empty(t, channel.Sent)

	written := records()
	require.Len(t, written, 1)
	assert.Equal(t, "WARN", written[0]["severity_text"])
	assert.Equal(t, expired.ID.String(), written[0][logging.MailID])
}

// TestDeliverAll pins that a sweep tells every agent holding mail and no other.
func TestDeliverAll(t *testing.T) {
	st := storetest.Open(t)
	seedAgent(t, st, "")
	for range 3 {
		sendTo(t, st, seedAgent(t, st, "").ID)
	}

	channel := &harnesstest.Channel{}
	require.NoError(t, newDeliveryService(st, channel).DeliverAll(t.Context()))
	assert.Len(t, channel.Sent, 3)
}

type movingTrees struct{}

func (movingTrees) IsMoving(dir string) bool {
	return dir == movingDir
}

func newDeliveryService(st *storetest.Stores, channel *harnesstest.Channel) *DeliveryService {
	return NewDeliveryService(NewDeliveryServiceParams{
		Mail:      st.Mail,
		Agents:    st.Agents,
		Harnesses: harnesstest.Registry{Harness: &harnesstest.Fake{Delivery: channel}},
		Trees:     movingTrees{},
		Now: func() time.Time {
			return time.Date(2026, time.October, 22, 15, 4, 5, 0, time.Local)
		},
	})
}

func seedAgent(t *testing.T, st *storetest.Stores, cwd string) agentstore.Agent {
	t.Helper()

	name := uuid.NewString()

	return *st.SeedAgent(t, agentstore.Agent{
		Name:    name,
		Pane:    "%" + name,
		Cwd:     cwd,
		State:   agentstore.StateIdle,
		Harness: harnesstest.FakeID,
	})
}

func sendTo(t *testing.T, st *storetest.Stores, agentID uuid.UUID) {
	t.Helper()

	_, err := st.Mail.Send(t.Context(), mailstore.SendParams{
		FromName:  "somebody",
		ToAgentID: agentID,
		Body:      "please read this",
		Deadline:  time.Hour,
	})
	require.NoError(t, err)
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

func holdsClaim(t *testing.T, st *storetest.Stores, agentID string) bool {
	t.Helper()

	var held bool
	err := st.Pool.GetRawPool().QueryRow(t.Context(),
		"SELECT EXISTS (SELECT 1 FROM mail WHERE to_agent_id = $1 AND nudged_at IS NOT NULL)", agentID).Scan(&held)
	require.NoError(t, err)

	return held
}
