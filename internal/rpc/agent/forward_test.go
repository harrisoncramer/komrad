package agent

import (
	"testing"

	"komrad/internal/agents"
	"komrad/internal/caller"
	"komrad/internal/fail"
	"komrad/internal/mail"
	"komrad/internal/tmuxtest"
	"komrad/internal/transcripts"
	"komrad/lib/dates"
	"komrad/lib/xptr"
	agentv1 "komrad/proto/gen/agent/v1"
	"komrad/proto/gen/agent/v1/agentv1komrad"

	agentstore "komrad/internal/store/agent"

	"buf.build/gen/go/bufbuild/protovalidate/protocolbuffers/go/buf/validate"
	"connectrpc.com/connect"
	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/testing/protocmp"
)

// TestForwardLastTurns covers what a forward hands the reader and the mail it sends.
func TestForwardLastTurns(t *testing.T) {
	composed := transcripts.Forward{
		Body:       "Forwarded from alpha",
		Turns:      3,
		Characters: 20,
	}

	tests := []struct {
		name       string
		from       string
		to         string
		callerIs   string
		req        *agentv1.ForwardLastTurnsRequest
		composeErr error
		sendErr    error
		wantCode   connect.Code
		wantParams transcripts.ComposeForwardParams
	}{
		{
			name:     "a source no agent holds",
			from:     uuid.NewString(),
			callerIs: "gamma",
			req:      &agentv1.ForwardLastTurnsRequest{},
			wantCode: connect.CodeNotFound,
		},
		{
			name:       "a source with nothing synced yet has nothing to forward",
			callerIs:   "gamma",
			req:        &agentv1.ForwardLastTurnsRequest{},
			composeErr: fail.Precondition("nothing to forward"),
			wantCode:   connect.CodeFailedPrecondition,
		},
		{
			name:       "a body past what mail holds is refused rather than cut",
			callerIs:   "gamma",
			req:        &agentv1.ForwardLastTurnsRequest{},
			composeErr: fail.InvalidArgument("too long"),
			wantCode:   connect.CodeInvalidArgument,
		},
		{
			name:     "a recipient no agent holds",
			to:       uuid.NewString(),
			callerIs: "gamma",
			req:      &agentv1.ForwardLastTurnsRequest{},
			wantCode: connect.CodeNotFound,
		},
		{
			name:     "a send the mail service refuses",
			callerIs: "gamma",
			req:      &agentv1.ForwardLastTurnsRequest{},
			sendErr:  mail.ErrExchangeCapped,
			wantCode: connect.CodeResourceExhausted,
		},
		{
			name: "the panel forwarding as nobody",
			req:  &agentv1.ForwardLastTurnsRequest{},
		},
		{
			name:     "the count, window and note are handed down and the composed body is sent",
			callerIs: "gamma",
			req: &agentv1.ForwardLastTurnsRequest{
				Turns:    xptr.Ptr[int32](2),
				MaxBytes: xptr.Ptr[int32](22),
				Note:     xptr.Ptr("alpha settled the cursor question; build on it"),
			},
			wantParams: transcripts.ComposeForwardParams{
				Turns:    2,
				MaxBytes: 22,
				Note:     "alpha settled the cursor question; build on it",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rows := &fakeRowReader{}
			reader := &fakeTranscriptReader{
				forward: composed,
				err:     tt.composeErr,
			}
			mailer := &fakeMailer{err: tt.sendErr}
			svc, terminal := newForwardHandler(rows, reader, mailer)

			alpha := rows.seed(agentstore.Agent{Name: "alpha", State: agentstore.StateIdle})
			beta := rows.seed(agentstore.Agent{
				Name:  "beta",
				Pane:  "%beta",
				State: agentstore.StateIdle,
			})
			gamma := rows.seed(agentstore.Agent{Name: "gamma", State: agentstore.StateIdle})
			terminal.Owners[beta.Pane] = beta.ID

			tt.req.FromAgentId = tt.from
			if tt.req.FromAgentId == "" {
				tt.req.FromAgentId = alpha.ID
			}

			tt.req.ToAgentId = tt.to
			if tt.req.ToAgentId == "" {
				tt.req.ToAgentId = beta.ID
			}

			ctx := t.Context()
			from := ""
			if tt.callerIs == "gamma" {
				ctx = caller.With(ctx, caller.Caller{AgentID: gamma.ID})
				from = gamma.ID
			}

			got, err := svc.ForwardLastTurns(ctx, connect.NewRequest(tt.req))
			if tt.wantCode != 0 {
				assert.Equal(t, tt.wantCode, connect.CodeOf(err))
				assert.Empty(t, mailer.mail, "a refused forward sends nothing")

				return
			}

			require.NoError(t, err)
			tt.wantParams.Source = alpha
			assert.Equal(t, []transcripts.ComposeForwardParams{tt.wantParams}, reader.composed)
			require.Equal(t, []mail.SendMailParams{{
				From:      from,
				ToAgentID: beta.ID,
				Body:      composed.Body,
			}}, mailer.sent)
			require.Len(t, mailer.mail, 1)
			assert.Empty(t, cmp.Diff(&agentv1.ForwardLastTurnsResponse{
				MailId:     mailer.mail[0].ID,
				ToAgentId:  beta.ID,
				CreatedAt:  dates.Stamp(mailer.mail[0].CreatedAt),
				Turns:      int32(composed.Turns),
				Characters: int32(composed.Characters),
			}, got.Msg, protocmp.Transform()), "the response never carries the text")
		})
	}
}

// TestForwardLastTurnsWarnsOfADormantRecipient covers a forward to an agent nothing runs behind saying so.
func TestForwardLastTurnsWarnsOfADormantRecipient(t *testing.T) {
	tests := []struct {
		name        string
		running     bool
		wantWarning bool
	}{
		{
			name:        "an idle agent whose pane is gone",
			wantWarning: true,
		},
		{
			name:    "an idle agent in its pane",
			running: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rows := &fakeRowReader{}
			svc, terminal := newForwardHandler(rows, &fakeTranscriptReader{}, &fakeMailer{})

			alpha := rows.seed(agentstore.Agent{Name: "alpha", State: agentstore.StateIdle})
			beta := rows.seed(agentstore.Agent{
				Name:  "beta",
				Pane:  "%beta",
				State: agentstore.StateIdle,
			})
			if tt.running {
				terminal.Owners[beta.Pane] = beta.ID
			}

			sent, err := svc.ForwardLastTurns(t.Context(), connect.NewRequest(&agentv1.ForwardLastTurnsRequest{
				FromAgentId: alpha.ID,
				ToAgentId:   beta.ID,
			}))
			require.NoError(t, err)

			if !tt.wantWarning {
				assert.Nil(t, sent.Msg.Warning)

				return
			}

			assert.Contains(t, sent.Msg.GetWarning(), "beta")
			assert.Contains(t, sent.Msg.GetWarning(), "dormant")
		})
	}
}

// TestForwardLastTurnsRecordsTheRead covers a forward leaving the line a read of somebody else's conversation leaves.
func TestForwardLastTurnsRecordsTheRead(t *testing.T) {
	rows := &fakeRowReader{}
	svc, _ := newForwardHandler(rows, &fakeTranscriptReader{}, &fakeMailer{})

	alpha := rows.seed(agentstore.Agent{Name: "alpha", State: agentstore.StateIdle})
	beta := rows.seed(agentstore.Agent{Name: "beta", State: agentstore.StateIdle})
	gamma := rows.seed(agentstore.Agent{Name: "gamma", State: agentstore.StateIdle})

	ctx, written := newLoggedCaller(t, gamma.ID)

	_, err := svc.ForwardLastTurns(ctx, connect.NewRequest(&agentv1.ForwardLastTurnsRequest{
		FromAgentId: alpha.ID,
		ToAgentId:   beta.ID,
	}))
	require.NoError(t, err)
	assertCrossAgentRead(t, assertCrossAgentReadParams{
		Written: written.String(),
		Body:    "agent read another agent's transcript",
		Target:  alpha.ID,
	})
}

// TestMaxMailBodyIsTheWireCap pins the cap a forward is held to against the one send_mail's request declares.
func TestMaxMailBodyIsTheWireCap(t *testing.T) {
	field := (&agentv1.SendMailRequest{}).ProtoReflect().Descriptor().Fields().ByName("body")
	require.NotNil(t, field)

	rules, ok := proto.GetExtension(field.Options(), validate.E_Field).(*validate.FieldRules)
	require.True(t, ok)

	assert.EqualValues(t, mail.MaxMailBody, rules.GetString().GetMaxLen())
}

// newForwardHandler is the handler over fakes of the rows, the transcripts and the mail service, reading liveness off a fake tmux.
func newForwardHandler(rows *fakeRowReader, reader *fakeTranscriptReader, mailer *fakeMailer) (agentv1komrad.AgentServiceAdapter, *tmuxtest.Fake) {
	terminal := tmuxtest.NewFake()

	return agentv1komrad.AgentServiceAdapter{Impl: NewHandler(NewHandlerParams{
		Agents:      rows,
		Transcripts: reader,
		Mail:        mailer,
		Liveness:    agents.NewPaneLiveness(terminal),
	})}, terminal
}
