package agent

import (
	"testing"
	"time"

	"komrad/internal/agenterr"
	"komrad/internal/caller"
	"komrad/internal/fail"
	"komrad/internal/mail"
	"komrad/internal/transcripts"
	"komrad/lib/xptr"
	agentv1 "komrad/proto/gen/agent/v1"
	"komrad/proto/gen/agent/v1/agentv1komrad"

	agentstore "komrad/internal/store/agent"
	mailstore "komrad/internal/store/mail"

	"buf.build/gen/go/bufbuild/protovalidate/protocolbuffers/go/buf/validate"
	"connectrpc.com/connect"
	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/testing/protocmp"
)

// TestForwardLastTurnsRefusalsMapToCodes pins the code each refusal of the transcript service reaches the caller as.
func TestForwardLastTurnsRefusalsMapToCodes(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		wantCode connect.Code
	}{
		{
			name:     "a source no agent holds",
			err:      agenterr.ErrAgentNotFound,
			wantCode: connect.CodeNotFound,
		},
		{
			name:     "a source with nothing synced yet has nothing to forward",
			err:      fail.Precondition("nothing to forward"),
			wantCode: connect.CodeFailedPrecondition,
		},
		{
			name:     "a body past what mail holds is refused rather than cut",
			err:      fail.InvalidArgument("too long"),
			wantCode: connect.CodeInvalidArgument,
		},
		{
			name:     "a recipient no agent holds",
			err:      mail.ErrRecipientNotFound,
			wantCode: connect.CodeNotFound,
		},
		{
			name:     "a send the mail service refuses",
			err:      mail.ErrExchangeCapped,
			wantCode: connect.CodeResourceExhausted,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := newForwardHandler(&fakeTranscriptReader{err: tt.err})

			_, err := svc.ForwardLastTurns(t.Context(), connect.NewRequest(&agentv1.ForwardLastTurnsRequest{
				FromAgentId: alphaID.String(),
				ToAgentId:   betaID.String(),
			}))

			assert.Equal(t, tt.wantCode, connect.CodeOf(err))
		})
	}
}

// TestForwardLastTurns covers what a forward hands the transcript service and what it renders.
func TestForwardLastTurns(t *testing.T) {
	tests := []struct {
		name        string
		callerIs    uuid.UUID
		dormant     bool
		wantFrom    uuid.UUID
		wantWarning bool
	}{
		{
			name:        "a forward to a dormant recipient",
			callerIs:    gammaID,
			dormant:     true,
			wantFrom:    gammaID,
			wantWarning: true,
		},
		{
			name: "the panel forwarding as nobody",
		},
		{
			name:     "an agent forwarding to a recipient something runs behind",
			callerIs: gammaID,
			wantFrom: gammaID,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := &fakeTranscriptReader{forwarded: transcripts.Forwarded{
				Mail: mailstore.Mail{
					ID:        mailID,
					ToAgentID: betaID,
					CreatedAt: time.Date(2026, time.September, 24, 9, 0, 0, 0, time.UTC),
				},
				Recipient: agentstore.Agent{
					ID:   betaID,
					Name: "beta",
				},
				Dormant:    tt.dormant,
				Turns:      3,
				Characters: 20,
			}}
			svc := newForwardHandler(reader)

			ctx := t.Context()
			if tt.callerIs != uuid.Nil {
				ctx = caller.With(ctx, caller.Caller{AgentID: tt.callerIs})
			}

			got, err := svc.ForwardLastTurns(ctx, connect.NewRequest(&agentv1.ForwardLastTurnsRequest{
				FromAgentId: alphaID.String(),
				ToAgentId:   betaID.String(),
				Turns:       xptr.Ptr[int32](2),
				MaxBytes:    xptr.Ptr[int32](22),
				Note:        xptr.Ptr("alpha settled the cursor question; build on it"),
			}))
			require.NoError(t, err)

			assert.Equal(t, []transcripts.ForwardLastTurnsParams{{
				From:      tt.wantFrom,
				SourceID:  alphaID,
				ToAgentID: betaID,
				Turns:     2,
				MaxBytes:  22,
				Note:      "alpha settled the cursor question; build on it",
			}}, reader.forwards)

			if tt.wantWarning {
				assert.Contains(t, got.Msg.GetWarning(), "beta")
				assert.Contains(t, got.Msg.GetWarning(), "dormant")

				return
			}

			assert.Empty(t, cmp.Diff(&agentv1.ForwardLastTurnsResponse{
				MailId:     mailID.String(),
				ToAgentId:  betaID.String(),
				CreatedAt:  "2026-09-24T09:00:00Z",
				Turns:      3,
				Characters: 20,
			}, got.Msg, protocmp.Transform()), "the response never carries the text")
		})
	}
}

// TestForwardLastTurnsRecordsTheRead covers a forward leaving the line a read of somebody else's conversation leaves.
func TestForwardLastTurnsRecordsTheRead(t *testing.T) {
	svc := newForwardHandler(&fakeTranscriptReader{})
	ctx, written := newLoggedCaller(t, gammaID)

	_, err := svc.ForwardLastTurns(ctx, connect.NewRequest(&agentv1.ForwardLastTurnsRequest{
		FromAgentId: alphaID.String(),
		ToAgentId:   betaID.String(),
	}))
	require.NoError(t, err)
	assertCrossAgentRead(t, assertCrossAgentReadParams{
		Written: written.String(),
		Body:    "agent read another agent's transcript",
		Target:  alphaID,
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

// newForwardHandler is the handler over a fake of the transcript service.
func newForwardHandler(reader *fakeTranscriptReader) agentv1komrad.AgentServiceAdapter {
	return agentv1komrad.AgentServiceAdapter{Impl: NewHandler(NewHandlerParams{Transcripts: reader})}
}

var (
	mailID = uuid.New()
)
