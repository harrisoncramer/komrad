package agent

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"komrad/internal/agenterr"
	"komrad/internal/caller"
	"komrad/internal/fail"
	"komrad/internal/logging"
	"komrad/internal/logtest"
	"komrad/internal/transcripts"
	"komrad/lib/pagination"
	"komrad/lib/xptr"
	agentv1 "komrad/proto/gen/agent/v1"
	"komrad/proto/gen/agent/v1/agentv1connect"
	"komrad/proto/gen/agent/v1/agentv1komrad"
	paginationv1 "komrad/proto/gen/pagination/v1"

	transcriptstore "komrad/internal/store/transcript"

	"connectrpc.com/connect"
	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/testing/protocmp"
)

// fakeTranscriptReader answers from its fields and records what each call asked of it.
type fakeTranscriptReader struct {
	window    transcripts.Window
	turns     []transcripts.Turn
	forwarded transcripts.Forwarded
	results   transcriptstore.Results
	err       error
	read      []transcripts.ReadTranscriptWindowParams
	searched  []transcripts.SearchTranscriptsParams
	listed    []transcripts.ListLastTurnsParams
	forwards  []transcripts.ForwardLastTurnsParams
}

// ReadTranscriptWindow records the params and answers window unless err refuses it.
func (f *fakeTranscriptReader) ReadTranscriptWindow(_ context.Context, params transcripts.ReadTranscriptWindowParams) (*transcripts.Window, error) {
	f.read = append(f.read, params)
	if f.err != nil {
		return nil, f.err
	}

	return &f.window, nil
}

// ListLastTurns records the params and answers turns unless err refuses it.
func (f *fakeTranscriptReader) ListLastTurns(_ context.Context, params transcripts.ListLastTurnsParams) ([]transcripts.Turn, error) {
	f.listed = append(f.listed, params)
	if f.err != nil {
		return nil, f.err
	}

	return f.turns, nil
}

// ForwardLastTurns records the params and answers forwarded unless err refuses it.
func (f *fakeTranscriptReader) ForwardLastTurns(_ context.Context, params transcripts.ForwardLastTurnsParams) (*transcripts.Forwarded, error) {
	f.forwards = append(f.forwards, params)
	if f.err != nil {
		return nil, f.err
	}

	return &f.forwarded, nil
}

// SearchTranscripts records the params and answers results unless err refuses it.
func (f *fakeTranscriptReader) SearchTranscripts(_ context.Context, params transcripts.SearchTranscriptsParams) (*transcriptstore.Results, error) {
	f.searched = append(f.searched, params)
	if f.err != nil {
		return nil, f.err
	}

	return &f.results, nil
}

// TestGetTranscript pins what reaches the reader and that a window's cursor becomes the page token.
func TestGetTranscript(t *testing.T) {
	token, err := pagination.Token(transcriptstore.ContentCursor{Offset: 8})
	require.NoError(t, err)

	tests := []struct {
		name       string
		req        *agentv1.GetTranscriptRequest
		window     transcripts.Window
		wantCursor *transcriptstore.ContentCursor
		wantMax    int32
		wantToken  *string
	}{
		{
			name:   "the last window carries no token",
			req:    &agentv1.GetTranscriptRequest{},
			window: transcripts.Window{Content: "the whole conversation"},
		},
		{
			name: "a cursor and a window are handed down and the next cursor comes back as a token",
			req: &agentv1.GetTranscriptRequest{
				Cursor:   token,
				MaxBytes: xptr.Ptr[int32](7),
			},
			window: transcripts.Window{
				Content: "opening",
				Next:    &transcriptstore.ContentCursor{Offset: 8},
			},
			wantCursor: &transcriptstore.ContentCursor{Offset: 8},
			wantMax:    7,
			wantToken:  token,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := &fakeTranscriptReader{window: tt.window}
			svc := newTranscriptHandler(reader)

			tt.req.AgentId = alphaID.String()
			got, err := svc.GetTranscript(t.Context(), connect.NewRequest(tt.req))
			require.NoError(t, err)

			assert.Equal(t, tt.window.Content, got.Msg.GetContent())
			assert.Equal(t, tt.wantToken, got.Msg.NextPageToken)
			assert.Equal(t, []transcripts.ReadTranscriptWindowParams{{
				AgentID:  alphaID,
				Cursor:   tt.wantCursor,
				MaxBytes: tt.wantMax,
			}}, reader.read)
		})
	}
}

// TestTranscriptRefusalsMapToCodes pins the code each refusal of a transcript read reaches the caller as.
func TestTranscriptRefusalsMapToCodes(t *testing.T) {
	getTranscript := func(cursor string) func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error {
		return func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error {
			req := &agentv1.GetTranscriptRequest{AgentId: alphaID.String()}
			if cursor != "" {
				req.Cursor = xptr.Ptr(cursor)
			}

			_, err := svc.GetTranscript(ctx, connect.NewRequest(req))

			return err
		}
	}
	getLastTurns := func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error {
		_, err := svc.GetLastTurns(ctx, connect.NewRequest(&agentv1.GetLastTurnsRequest{AgentId: alphaID.String()}))

		return err
	}
	search := func(cursor string) func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error {
		return func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error {
			req := &agentv1.SearchTranscriptsRequest{Query: "reconciler"}
			if cursor != "" {
				req.Pagination = &paginationv1.Pagination{Cursor: xptr.Ptr(cursor)}
			}

			_, err := svc.SearchTranscripts(ctx, connect.NewRequest(req))

			return err
		}
	}

	tests := []struct {
		name     string
		call     func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error
		err      error
		wantCode connect.Code
	}{
		{
			name:     "the transcript of an agent nobody holds",
			call:     getTranscript(""),
			err:      agenterr.ErrAgentNotFound,
			wantCode: connect.CodeNotFound,
		},
		{
			name:     "a transcript cursor this server never handed out",
			call:     getTranscript("not-a-token"),
			wantCode: connect.CodeInvalidArgument,
		},
		{
			name:     "a window the reader refuses",
			call:     getTranscript(""),
			err:      fail.InvalidArgument("max_bytes of 2 is too small"),
			wantCode: connect.CodeInvalidArgument,
		},
		{
			name:     "a transcript the store cannot read",
			call:     getTranscript(""),
			err:      errors.New("connection refused"),
			wantCode: connect.CodeInternal,
		},
		{
			name:     "the last turns of an agent nobody holds",
			call:     getLastTurns,
			err:      agenterr.ErrAgentNotFound,
			wantCode: connect.CodeNotFound,
		},
		{
			name:     "last turns the store cannot read",
			call:     getLastTurns,
			err:      errors.New("connection refused"),
			wantCode: connect.CodeInternal,
		},
		{
			name:     "a search token holding an id no row can have",
			call:     search("eyJtIjp7InIiOjAuNSwiaSI6Im5vdC1hbi1pZCIsInMiOjF9fQ=="),
			wantCode: connect.CodeInvalidArgument,
		},
		{
			name:     "a search the store cannot run",
			call:     search(""),
			err:      errors.New("connection refused"),
			wantCode: connect.CodeInternal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := newTranscriptHandler(&fakeTranscriptReader{err: tt.err})

			err := tt.call(t.Context(), svc)

			assert.Equal(t, tt.wantCode, connect.CodeOf(err))
		})
	}
}

// TestSearchTranscriptsScope pins that a search passes on the agent it names and the agent asking.
func TestSearchTranscriptsScope(t *testing.T) {
	tests := []struct {
		name        string
		callerIs    uuid.UUID
		asksFor     uuid.UUID
		wantAgentID *uuid.UUID
	}{
		{
			name:     "an agent that names nobody",
			callerIs: alphaID,
		},
		{
			name:        "an agent that names another",
			callerIs:    alphaID,
			asksFor:     betaID,
			wantAgentID: xptr.Ptr(betaID),
		},
		{
			name: "the panel naming nobody",
		},
		{
			name:        "the panel naming one",
			asksFor:     betaID,
			wantAgentID: xptr.Ptr(betaID),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := &fakeTranscriptReader{}
			svc := newTranscriptHandler(reader)

			ctx := t.Context()
			if tt.callerIs != uuid.Nil {
				ctx = caller.With(ctx, caller.Caller{AgentID: tt.callerIs})
			}

			req := &agentv1.SearchTranscriptsRequest{Query: "reconciler"}
			if tt.asksFor != uuid.Nil {
				req.AgentId = xptr.Ptr(tt.asksFor.String())
			}

			_, err := svc.SearchTranscripts(ctx, connect.NewRequest(req))
			require.NoError(t, err)

			require.Len(t, reader.searched, 1)
			assert.Equal(t, tt.wantAgentID, reader.searched[0].AgentID)
			assert.Equal(t, tt.callerIs, reader.searched[0].Caller)
		})
	}
}

// TestSearchTranscripts pins how each hit renders and that the store's next cursor becomes the page token.
func TestSearchTranscripts(t *testing.T) {
	next := transcriptstore.Cursor{Sort: transcriptstore.SortBest, Matches: &transcriptstore.Position{
		Key:     0.5,
		AgentID: alphaID,
		Seq:     1,
	}}
	reader := &fakeTranscriptReader{results: transcriptstore.Results{
		Matches: []transcriptstore.SearchHit{{
			AgentID:       alphaID,
			Name:          "alpha",
			Archived:      true,
			Seq:           1,
			Role:          "user",
			ContentOffset: 18,
			ContentLength: 34,
			Snippet:       "the \x02reconciler\x03 derives",
			Rank:          0.5,
		}},
		Near: []transcriptstore.SearchHit{{
			AgentID: betaID,
			Name:    "beta",
			Role:    "assistant",
			Snippet: "reconcilers",
		}},
		Next: &next,
	}}
	svc := newTranscriptHandler(reader)

	first, err := svc.SearchTranscripts(t.Context(), connect.NewRequest(&agentv1.SearchTranscriptsRequest{
		Query:      "reconciler",
		Pagination: &paginationv1.Pagination{Limit: xptr.Ptr[int32](1)},
	}))
	require.NoError(t, err)

	want := &agentv1.SearchTranscriptsResponse{
		Matches: []*agentv1.SearchHit{{
			AgentId:       alphaID.String(),
			Name:          "alpha",
			Archived:      true,
			Seq:           1,
			Role:          "user",
			ContentOffset: 18,
			ContentLength: 34,
			Snippet:       "the \x02reconciler\x03 derives",
			Score:         0.5,
		}},
		Near: []*agentv1.SearchHit{{
			AgentId: betaID.String(),
			Name:    "beta",
			Role:    "assistant",
			Snippet: "reconcilers",
		}},
	}
	assert.Empty(t, cmp.Diff(want, first.Msg, protocmp.Transform(), protocmp.IgnoreFields(&agentv1.SearchTranscriptsResponse{}, "next_page_token")))
	require.NotNil(t, first.Msg.NextPageToken)

	_, err = svc.SearchTranscripts(t.Context(), connect.NewRequest(&agentv1.SearchTranscriptsRequest{
		Query:      "reconciler",
		Pagination: &paginationv1.Pagination{Limit: xptr.Ptr[int32](1), Cursor: first.Msg.NextPageToken},
	}))
	require.NoError(t, err)

	firstPage := transcripts.SearchTranscriptsParams{
		Query: "reconciler",
		Limit: 1,
	}
	secondPage := transcripts.SearchTranscriptsParams{
		Query:  "reconciler",
		Limit:  1,
		Cursor: &next,
	}
	assert.Equal(t, []transcripts.SearchTranscriptsParams{firstPage, secondPage}, reader.searched)
}

// TestCrossAgentReadIsRecorded pins the line a read of somebody else's conversation leaves, and the silence a read of your own does.
func TestCrossAgentReadIsRecorded(t *testing.T) {
	searchBeta := func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error {
		_, err := svc.SearchTranscripts(ctx, connect.NewRequest(&agentv1.SearchTranscriptsRequest{
			Query:   "reconciler",
			AgentId: xptr.Ptr(betaID.String()),
		}))

		return err
	}
	getBetaTranscript := func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error {
		_, err := svc.GetTranscript(ctx, connect.NewRequest(&agentv1.GetTranscriptRequest{AgentId: betaID.String()}))

		return err
	}
	getBetaLastTurns := func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error {
		_, err := svc.GetLastTurns(ctx, connect.NewRequest(&agentv1.GetLastTurnsRequest{AgentId: betaID.String()}))

		return err
	}

	tests := []struct {
		name       string
		callerIs   uuid.UUID
		read       func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error
		wantBody   string
		wantTarget uuid.UUID
	}{
		{
			name:     "an agent searching itself",
			callerIs: betaID,
			read:     searchBeta,
		},
		{
			name: "the panel reading anyone",
			read: getBetaTranscript,
		},
		{
			name:       "an agent searching another agent",
			callerIs:   alphaID,
			read:       searchBeta,
			wantBody:   "agent read another agent's transcript",
			wantTarget: betaID,
		},
		{
			name:       "an agent fetching another agent's whole conversation",
			callerIs:   alphaID,
			read:       getBetaTranscript,
			wantBody:   "agent read another agent's transcript",
			wantTarget: betaID,
		},
		{
			name:       "an agent fetching another agent's last turns",
			callerIs:   alphaID,
			read:       getBetaLastTurns,
			wantBody:   "agent read another agent's transcript",
			wantTarget: betaID,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := newTranscriptHandler(&fakeTranscriptReader{})

			var written strings.Builder
			ctx := logging.WithLogger(t.Context(), logging.New(&written, slog.LevelInfo))
			if tt.callerIs != uuid.Nil {
				ctx = caller.With(ctx, caller.Caller{AgentID: tt.callerIs})
			}

			require.NoError(t, tt.read(ctx, svc))

			if tt.wantBody == "" {
				assert.Empty(t, written.String())

				return
			}

			record := logtest.Record(t, written.String())
			assert.Equal(t, tt.wantBody, record["body"])
			assert.Equal(t, tt.wantTarget.String(), record[logging.TargetAgentID])
		})
	}
}

// TestGetLastTurns pins what reaches the reader and how each turn renders.
func TestGetLastTurns(t *testing.T) {
	listed := []transcripts.Turn{
		{
			Role:      "assistant",
			Seq:       1,
			Content:   "reading of the problem",
			Truncated: true,
		},
		{
			Role:    "user",
			Seq:     2,
			Content: "a nudge",
		},
	}

	tests := []struct {
		name      string
		req       *agentv1.GetLastTurnsRequest
		wantTurns int32
		wantMax   int32
	}{
		{
			name: "no count is handed down as none",
			req:  &agentv1.GetLastTurnsRequest{},
		},
		{
			name: "a count and a window are handed down",
			req: &agentv1.GetLastTurnsRequest{
				Turns:    xptr.Ptr[int32](2),
				MaxBytes: xptr.Ptr[int32](22),
			},
			wantTurns: 2,
			wantMax:   22,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := &fakeTranscriptReader{turns: listed}
			svc := newTranscriptHandler(reader)

			tt.req.AgentId = alphaID.String()
			got, err := svc.GetLastTurns(t.Context(), connect.NewRequest(tt.req))
			require.NoError(t, err)

			assert.Empty(t, cmp.Diff([]*agentv1.Turn{
				{
					Role:      "assistant",
					Seq:       1,
					Content:   "reading of the problem",
					Truncated: true,
				},
				{
					Role:    "user",
					Seq:     2,
					Content: "a nudge",
				},
			}, got.Msg.GetTurns(), protocmp.Transform()))
			assert.Equal(t, []transcripts.ListLastTurnsParams{{
				AgentID:  alphaID,
				Turns:    tt.wantTurns,
				MaxBytes: tt.wantMax,
			}}, reader.listed)
		})
	}
}

// newTranscriptHandler is the handler over a fake of the transcripts alone.
func newTranscriptHandler(reader *fakeTranscriptReader) agentv1komrad.AgentServiceAdapter {
	return agentv1komrad.AgentServiceAdapter{Impl: NewHandler(NewHandlerParams{Transcripts: reader})}
}

// TestTranscriptRequestValidation covers the proto constraints on a transcript read.
func TestTranscriptRequestValidation(t *testing.T) {
	tests := []struct {
		name    string
		call    func(t *testing.T, c agentv1connect.AgentServiceClient) error
		wantErr bool
	}{
		{
			name: "an empty search query is refused",
			call: func(t *testing.T, c agentv1connect.AgentServiceClient) error {
				_, err := c.SearchTranscripts(t.Context(), connect.NewRequest(&agentv1.SearchTranscriptsRequest{Query: ""}))

				return err
			},
			wantErr: true,
		},
		{
			name: "a query of two characters is accepted",
			call: func(t *testing.T, c agentv1connect.AgentServiceClient) error {
				_, err := c.SearchTranscripts(t.Context(), connect.NewRequest(&agentv1.SearchTranscriptsRequest{Query: "ab"}))

				return err
			},
			wantErr: false,
		},
		{
			name: "an oversized limit is clamped, not refused",
			call: func(t *testing.T, c agentv1connect.AgentServiceClient) error {
				_, err := c.SearchTranscripts(t.Context(), connect.NewRequest(&agentv1.SearchTranscriptsRequest{
					Query:      "mcp",
					Pagination: &paginationv1.Pagination{Limit: xptr.Ptr[int32](5000)},
				}))

				return err
			},
		},
		{
			name: "an unset limit still means the server default",
			call: func(t *testing.T, c agentv1connect.AgentServiceClient) error {
				_, err := c.SearchTranscripts(t.Context(), connect.NewRequest(&agentv1.SearchTranscriptsRequest{Query: "mcp"}))

				return err
			},
		},
		{
			name: "a transcript read with no agent id is refused",
			call: func(t *testing.T, c agentv1connect.AgentServiceClient) error {
				_, err := c.GetTranscript(t.Context(), connect.NewRequest(&agentv1.GetTranscriptRequest{AgentId: ""}))

				return err
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, _ := validatedClient(t)

			err := tt.call(t, client)
			if tt.wantErr {
				assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
				return
			}

			require.NoError(t, err)
		})
	}
}
