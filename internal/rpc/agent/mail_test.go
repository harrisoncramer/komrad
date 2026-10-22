package agent

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"komrad/internal/caller"
	"komrad/internal/logging"
	"komrad/internal/logtest"
	"komrad/internal/mail"
	"komrad/internal/rpc/render"
	"komrad/lib/xptr"
	agentv1 "komrad/proto/gen/agent/v1"
	"komrad/proto/gen/agent/v1/agentv1komrad"
	"komrad/proto/gen/oneofs"
	paginationv1 "komrad/proto/gen/pagination/v1"

	agentstore "komrad/internal/store/agent"
	mailstore "komrad/internal/store/mail"

	"connectrpc.com/connect"
	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var (
	alphaID       = uuid.MustParse("01a0d1d7-0000-7000-8000-00000000000a")
	betaID        = uuid.MustParse("01a0d1d7-0000-7000-8000-00000000000b")
	gammaID       = uuid.MustParse("01a0d1d7-0000-7000-8000-00000000000c")
	earlierMailID = uuid.MustParse("01a0d1d7-0000-7000-8000-0000000000c0")
)

// fakeMailer answers from its fields and records what each call asked of it.
type fakeMailer struct {
	recipient  agentstore.Agent
	resolveErr error
	err        error
	mail       []mailstore.Mail
	found      []mail.FindRecipientParams
	sent       []mail.SendMailParams
	paged      []mail.ListMailParams
	deleted    []uuid.UUID
	dormant    bool
}

// FindRecipient records the address and answers recipient unless resolveErr refuses it.
func (f *fakeMailer) FindRecipient(_ context.Context, params mail.FindRecipientParams) (*agentstore.Agent, error) {
	f.found = append(f.found, params)
	if f.resolveErr != nil {
		return nil, f.resolveErr
	}

	return &f.recipient, nil
}

// SendMail records the send and answers a fixed piece of mail unless err refuses it.
func (f *fakeMailer) SendMail(_ context.Context, params mail.SendMailParams) (*mailstore.Mail, error) {
	f.sent = append(f.sent, params)
	if f.err != nil {
		return nil, f.err
	}

	sent := mailstore.Mail{
		ID:          mailID,
		FromAgentID: xptr.Ptr(params.From),
		FromName:    "alpha",
		ToAgentID:   params.ToAgentID,
		Body:        params.Body,
		ReplyTo:     params.ReplyTo,
		Kind:        mailstore.KindPlain,
		CreatedAt:   time.Date(2026, time.September, 24, 9, 0, 0, 0, time.UTC),
		ExpiresAt:   time.Date(2026, time.September, 24, 12, 0, 0, 0, time.UTC),
	}
	f.mail = append(f.mail, sent)

	return &sent, nil
}

// ListMail records the listing and answers mail unless err refuses it.
func (f *fakeMailer) ListMail(_ context.Context, params mail.ListMailParams) ([]mailstore.Mail, error) {
	f.paged = append(f.paged, params)
	if f.err != nil {
		return nil, f.err
	}

	return f.mail, nil
}

// DeleteMail records the ids and answers err.
func (f *fakeMailer) DeleteMail(_ context.Context, ids []uuid.UUID) error {
	f.deleted = append(f.deleted, ids...)

	return f.err
}

// IsDormant answers dormant whoever the agent is.
func (f *fakeMailer) IsDormant(agentstore.Agent) bool {
	return f.dormant
}

// TestMailRefusalsMapToCodes pins the code each refusal of the mail service reaches the caller as.
func TestMailRefusalsMapToCodes(t *testing.T) {
	send := func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error {
		_, err := svc.SendMail(ctx, connect.NewRequest(&agentv1.SendMailRequest{
			To:   &agentv1.SendMailRequest_ToAgentId{ToAgentId: betaID.String()},
			Body: "hello",
		}))

		return err
	}
	read := func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error {
		_, err := svc.ReadMail(ctx, connect.NewRequest(&agentv1.ReadMailRequest{}))

		return err
	}
	list := func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error {
		_, err := svc.ListMail(ctx, connect.NewRequest(&agentv1.ListMailRequest{}))

		return err
	}

	tests := []struct {
		name      string
		call      func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error
		err       error
		atResolve bool
		asNobody  bool
		wantCode  connect.Code
	}{
		{
			name:      "an address nothing answers to",
			call:      send,
			err:       mail.ErrRecipientNotFound,
			atResolve: true,
			wantCode:  connect.CodeNotFound,
		},
		{
			name:      "a name more than one workflow holds",
			call:      send,
			err:       mail.ErrRecipientAmbiguous,
			atResolve: true,
			wantCode:  connect.CodeInvalidArgument,
		},
		{
			name:     "a sender the panel does not know",
			call:     send,
			err:      mail.ErrSenderUnknown,
			wantCode: connect.CodeInvalidArgument,
		},
		{
			name:     "an agent addressing itself",
			call:     send,
			err:      mail.ErrSelfAddressed,
			wantCode: connect.CodeInvalidArgument,
		},
		{
			name:     "a reply to mail nothing holds",
			call:     send,
			err:      mail.ErrReplyToNotFound,
			wantCode: connect.CodeInvalidArgument,
		},
		{
			name:     "an archived recipient",
			call:     send,
			err:      mail.ErrAgentArchived,
			wantCode: connect.CodeFailedPrecondition,
		},
		{
			name:     "an agent reaching outside its task",
			call:     send,
			err:      mail.ErrOutsideTask,
			wantCode: connect.CodePermissionDenied,
		},
		{
			name:     "a full mailbox",
			call:     send,
			err:      mail.ErrMailboxFull,
			wantCode: connect.CodeResourceExhausted,
		},
		{
			name:     "a pair past the exchange cap",
			call:     send,
			err:      mail.ErrExchangeCapped,
			wantCode: connect.CodeResourceExhausted,
		},
		{
			name:     "reading with no id and no caller identity",
			call:     read,
			asNobody: true,
			wantCode: connect.CodeInvalidArgument,
		},
		{
			name:     "listing with no id and no caller identity",
			call:     list,
			asNobody: true,
			wantCode: connect.CodeInvalidArgument,
		},
		{
			name: "listing with a page token that does not decode",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error {
				_, err := svc.ListMail(ctx, connect.NewRequest(&agentv1.ListMailRequest{
					Pagination: &paginationv1.Pagination{Cursor: xptr.Ptr("not a token")},
				}))

				return err
			},
			wantCode: connect.CodeInvalidArgument,
		},
		{
			name:     "reading the mailbox of an agent that does not exist",
			call:     read,
			err:      mail.ErrAgentNotFound,
			wantCode: connect.CodeNotFound,
		},
		{
			name:     "listing the mailbox of an agent that does not exist",
			call:     list,
			err:      mail.ErrAgentNotFound,
			wantCode: connect.CodeNotFound,
		},
		{
			name: "deleting mail nothing holds",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error {
				_, err := svc.DeleteMail(ctx, connect.NewRequest(&agentv1.DeleteMailRequest{Ids: []string{betaID.String()}}))

				return err
			},
			err:      mail.ErrMailNotFound,
			wantCode: connect.CodeNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mailer := &fakeMailer{recipient: agentstore.Agent{
				ID:   betaID,
				Name: "beta",
			}}
			if tt.atResolve {
				mailer.resolveErr = tt.err
			} else {
				mailer.err = tt.err
			}
			svc := newMailHandler(mailer)

			ctx := t.Context()
			if !tt.asNobody {
				ctx = caller.With(ctx, caller.Caller{AgentID: alphaID})
			}

			err := tt.call(ctx, svc)
			assert.Equal(t, tt.wantCode, connect.CodeOf(err))
			if tt.err != nil {
				require.ErrorIs(t, err, tt.err)
			}
		})
	}
}

// TestSendMail pins what a send passes down and what it renders.
func TestSendMail(t *testing.T) {
	mailer := &fakeMailer{recipient: agentstore.Agent{
		ID:    betaID,
		Name:  "beta",
		Pane:  "%beta",
		State: agentstore.StateIdle,
	}}
	svc := newMailHandler(mailer)

	sent, err := svc.SendMail(caller.With(t.Context(), caller.Caller{AgentID: alphaID}), connect.NewRequest(&agentv1.SendMailRequest{
		To:      &agentv1.SendMailRequest_ToAgentName{ToAgentName: "beta"},
		Body:    "which column holds the refund?",
		ReplyTo: xptr.Ptr(earlierMailID.String()),
	}))
	require.NoError(t, err)

	assert.Equal(t, []mail.FindRecipientParams{{From: alphaID, To: oneofs.To{ToAgentName: xptr.Ptr("beta")}}}, mailer.found)
	assert.Equal(t, []mail.SendMailParams{{
		From:      alphaID,
		ToAgentID: betaID,
		Body:      "which column holds the refund?",
		ReplyTo:   xptr.Ptr(earlierMailID),
	}}, mailer.sent)
	assert.Empty(t, cmp.Diff(&agentv1.SendMailResponse{
		Mail: &agentv1.Mail{
			Id:          mailID.String(),
			FromAgentId: xptr.Ptr(alphaID.String()),
			FromName:    "alpha",
			ToAgentId:   betaID.String(),
			Body:        "which column holds the refund?",
			ReplyTo:     xptr.Ptr(earlierMailID.String()),
			CreatedAt:   timestamppb.New(time.Date(2026, 9, 24, 9, 0, 0, 0, time.UTC)),
			ExpiresAt:   timestamppb.New(time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)),
			Kind:        agentv1.MailKind_MAIL_KIND_PLAIN,
		},
	}, sent.Msg, protocmp.Transform()), "an agent running in its pane draws no warning")
}

// TestSendMailWarnsOfADormantRecipient pins a send to a recipient the mail service reads as dormant saying so.
func TestSendMailWarnsOfADormantRecipient(t *testing.T) {
	tests := []struct {
		name        string
		dormant     bool
		wantWarning bool
	}{
		{
			name:        "a dormant recipient",
			dormant:     true,
			wantWarning: true,
		},
		{
			name: "a recipient something runs behind",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mailer := &fakeMailer{
				recipient: agentstore.Agent{
					ID:   betaID,
					Name: "beta",
				},
				dormant: tt.dormant,
			}
			svc := newMailHandler(mailer)

			sent, err := svc.SendMail(caller.With(t.Context(), caller.Caller{AgentID: alphaID}), connect.NewRequest(&agentv1.SendMailRequest{
				To:   &agentv1.SendMailRequest_ToAgentName{ToAgentName: "beta"},
				Body: "which column holds the refund?",
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

// TestReadMail pins which mailbox a read claims and that the handler logs a read of somebody else's.
func TestReadMail(t *testing.T) {
	tests := []struct {
		name        string
		callerIs    uuid.UUID
		id          *string
		wantClaimed uuid.UUID
		wantLogged  uuid.UUID
	}{
		{
			name:        "an unset id falls back to the caller's own",
			callerIs:    alphaID,
			wantClaimed: alphaID,
		},
		{
			name:        "an agent reading another's mailbox",
			callerIs:    alphaID,
			id:          xptr.Ptr(betaID.String()),
			wantClaimed: betaID,
			wantLogged:  betaID,
		},
		{
			name:        "the panel reading an agent's mailbox",
			id:          xptr.Ptr(betaID.String()),
			wantClaimed: betaID,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reviewer := &fakeReviewer{mail: []mailstore.Mail{{
				ID:        mailID,
				ToAgentID: betaID,
				Body:      "refunded_cents",
			}}}
			svc := agentv1komrad.AgentServiceAdapter{Impl: NewHandler(NewHandlerParams{Review: reviewer})}
			ctx, written := newLoggedCaller(t, tt.callerIs)

			read, err := svc.ReadMail(ctx, connect.NewRequest(&agentv1.ReadMailRequest{Id: tt.id}))
			require.NoError(t, err)
			assert.Equal(t, []uuid.UUID{tt.wantClaimed}, reviewer.read)
			require.Len(t, read.Msg.GetMail(), 1)
			assert.Empty(t, diffMail(reviewer.mail[0], read.Msg.GetMail()[0]))
			assertCrossAgentRead(t, assertCrossAgentReadParams{
				Written: written.String(),
				Body:    "agent read another agent's mail",
				Target:  tt.wantLogged,
			})
		})
	}
}

// TestListMail pins which mailbox and which half of it a listing reads, and the page token it hands back.
func TestListMail(t *testing.T) {
	tests := []struct {
		name       string
		callerIs   uuid.UUID
		req        *agentv1.ListMailRequest
		stored     int
		wantListed mail.ListMailParams
		wantNext   bool
		wantLogged uuid.UUID
	}{
		{
			name:     "an unset direction reads the caller's own mailbox",
			callerIs: alphaID,
			req:      &agentv1.ListMailRequest{},
			stored:   1,
			wantListed: mail.ListMailParams{
				AgentID: alphaID,
				Limit:   20,
			},
		},
		{
			name:     "received reads the mailbox too",
			callerIs: alphaID,
			req: &agentv1.ListMailRequest{
				Direction: agentv1.MailDirection_MAIL_DIRECTION_RECEIVED,
			},
			stored: 1,
			wantListed: mail.ListMailParams{
				AgentID: alphaID,
				Limit:   20,
			},
		},
		{
			name:     "sent reads what another agent wrote, and is written to the log",
			callerIs: alphaID,
			req: &agentv1.ListMailRequest{
				Id:        xptr.Ptr(betaID.String()),
				Direction: agentv1.MailDirection_MAIL_DIRECTION_SENT,
			},
			stored: 1,
			wantListed: mail.ListMailParams{
				AgentID: betaID,
				Sent:    true,
				Limit:   20,
			},
			wantLogged: betaID,
		},
		{
			name:     "a full page hands back a token for the next",
			callerIs: alphaID,
			req: &agentv1.ListMailRequest{
				Pagination: &paginationv1.Pagination{Limit: xptr.Ptr[int32](2)},
			},
			stored: 2,
			wantListed: mail.ListMailParams{
				AgentID: alphaID,
				Limit:   2,
			},
			wantNext: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mailer := &fakeMailer{}
			for range tt.stored {
				mailer.mail = append(mailer.mail, mailstore.Mail{
					ID:        uuid.New(),
					ToAgentID: betaID,
					Body:      "refunded_cents",
				})
			}
			svc := newMailHandler(mailer)
			ctx, written := newLoggedCaller(t, tt.callerIs)

			listed, err := svc.ListMail(ctx, connect.NewRequest(tt.req))
			require.NoError(t, err)
			assert.Equal(t, []mail.ListMailParams{tt.wantListed}, mailer.paged)
			require.Len(t, listed.Msg.GetMail(), tt.stored)
			assert.Empty(t, diffMail(mailer.mail[0], listed.Msg.GetMail()[0]))
			assert.Equal(t, tt.wantNext, listed.Msg.GetNextPageToken() != "")
			assertCrossAgentRead(t, assertCrossAgentReadParams{
				Written: written.String(),
				Body:    "agent listed another agent's mail",
				Target:  tt.wantLogged,
			})
		})
	}
}

// TestDeleteMail pins that a delete hands every id down.
func TestDeleteMail(t *testing.T) {
	mailer := &fakeMailer{}
	svc := newMailHandler(mailer)

	_, err := svc.DeleteMail(t.Context(), connect.NewRequest(&agentv1.DeleteMailRequest{
		Ids: []string{alphaID.String(), betaID.String()},
	}))
	require.NoError(t, err)
	assert.Equal(t, []uuid.UUID{alphaID, betaID}, mailer.deleted)
}

// newMailHandler is the handler over a fake mail service, whose reads of mail refuse as the mailer does.
func newMailHandler(mailer *fakeMailer) agentv1komrad.AgentServiceAdapter {
	return agentv1komrad.AgentServiceAdapter{Impl: NewHandler(NewHandlerParams{Mail: mailer, Review: &fakeReviewer{err: mailer.err}})}
}

// newLoggedCaller is a context calling as the agent named, whose log lines land in the builder handed back.
func newLoggedCaller(t *testing.T, agentID uuid.UUID) (context.Context, *strings.Builder) {
	t.Helper()

	var written strings.Builder
	ctx := logging.WithLogger(t.Context(), logging.New(&written, slog.LevelInfo))
	if agentID != uuid.Nil {
		ctx = caller.With(ctx, caller.Caller{AgentID: agentID})
	}

	return ctx, &written
}

type assertCrossAgentReadParams struct {
	Written string
	Body    string
	// Target is whose mailbox or conversation the call read, empty when the handler should record no read.
	Target uuid.UUID
}

func assertCrossAgentRead(t *testing.T, params assertCrossAgentReadParams) {
	t.Helper()

	if params.Target == uuid.Nil {
		assert.Empty(t, params.Written)

		return
	}

	record := logtest.Record(t, params.Written)
	assert.Equal(t, params.Body, record["body"])
	assert.Equal(t, params.Target.String(), record[logging.TargetAgentID])
}

func diffMail(stored mailstore.Mail, rendered *agentv1.Mail) string {
	return cmp.Diff(render.Mail(stored), rendered, protocmp.Transform())
}
