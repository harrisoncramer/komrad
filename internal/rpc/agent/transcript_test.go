package agent

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"komrad/internal/agents"
	"komrad/internal/caller"
	"komrad/internal/logging"
	"komrad/internal/logtest"
	"komrad/internal/rpcerr"
	"komrad/internal/transcripts"
	"komrad/lib/pagination"
	"komrad/lib/xptr"
	"komrad/lib/xslice"
	agentv1 "komrad/proto/gen/agent/v1"
	"komrad/proto/gen/agent/v1/agentv1connect"
	"komrad/proto/gen/agent/v1/agentv1komrad"
	paginationv1 "komrad/proto/gen/pagination/v1"

	transcriptstore "komrad/internal/store/transcript"

	"connectrpc.com/connect"
	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/testing/protocmp"
)

// fakeTranscriptReader answers from its fields and records what each call asked of it.
type fakeTranscriptReader struct {
	content  string
	turns    []transcriptstore.StoredTurn
	results  transcriptstore.Results
	err      error
	searched []transcripts.SearchTranscriptsParams
	listed   []transcripts.ListLastTurnsParams
}

// ReadTranscript answers content unless err refuses it.
func (f *fakeTranscriptReader) ReadTranscript(context.Context, string) (string, error) {
	if f.err != nil {
		return "", f.err
	}

	return f.content, nil
}

// ListLastTurns records the params and answers turns unless err refuses it.
func (f *fakeTranscriptReader) ListLastTurns(_ context.Context, params transcripts.ListLastTurnsParams) ([]transcriptstore.StoredTurn, error) {
	f.listed = append(f.listed, params)
	if f.err != nil {
		return nil, f.err
	}

	return f.turns, nil
}

// SearchTranscripts records the params and answers results unless err refuses it.
func (f *fakeTranscriptReader) SearchTranscripts(_ context.Context, params transcripts.SearchTranscriptsParams) (*transcriptstore.Results, error) {
	f.searched = append(f.searched, params)
	if f.err != nil {
		return nil, f.err
	}

	return &f.results, nil
}

// TestTranscriptWindow covers the cut a page token is minted from, which has to move forward by
// whole characters and stop rather than hand back a token asking the same question again.
func TestTranscriptWindow(t *testing.T) {
	tests := []struct {
		name       string
		content    string
		offset     int64
		requested  int32
		want       string
		wantOffset int64
		wantLast   bool
		wantCode   connect.Code
		wantErr    string
	}{
		{
			name:     "content inside the window is the last page",
			content:  "the whole conversation",
			want:     "the whole conversation",
			wantLast: true,
		},
		{
			name:       "content past the window is cut and carries a token",
			content:    "opening question and then some",
			requested:  7,
			want:       "opening",
			wantOffset: 7,
		},
		{
			name:       "a cut inside a character pulls back to the one before it",
			content:    "日本語",
			requested:  7,
			want:       "日本",
			wantOffset: 6,
		},
		{
			name:     "an offset the caller was handed reads on from there",
			content:  "opening question",
			offset:   8,
			want:     "question",
			wantLast: true,
		},
		{
			name:     "an offset at the end is the empty last page",
			content:  "opening",
			offset:   7,
			want:     "",
			wantLast: true,
		},
		{
			name:     "an offset past the end is the empty last page too",
			content:  "opening",
			offset:   500,
			want:     "",
			wantLast: true,
		},
		{
			name:      "a window too small to hold the next character is refused",
			content:   "日本語",
			requested: 2,
			wantCode:  connect.CodeInvalidArgument,
			wantErr:   "max_bytes of 2 is too small",
		},
		{
			name:     "a cursor landing inside a character says so rather than blaming the window",
			content:  "日本語 and then a long tail of ordinary text that follows on",
			offset:   1,
			wantCode: connect.CodeInvalidArgument,
			wantErr:  "cursor no longer names the start of a character",
		},
		{
			name:     "a window bigger than the cap is capped",
			content:  strings.Repeat("a", maxTranscriptWindow+10),
			want:     strings.Repeat("a", maxTranscriptWindow),
			wantCode: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var cursor *transcriptstore.ContentCursor
			if tt.offset != 0 {
				cursor = &transcriptstore.ContentCursor{Offset: tt.offset}
			}

			window, next, failure := transcriptWindow(tt.content, cursor, tt.requested)
			if tt.wantCode != 0 {
				require.NotNil(t, failure)

				_, served := rpcerr.Answer[agentv1.GetTranscriptResponse](nil, failure)
				assert.Equal(t, tt.wantCode, connect.CodeOf(served))
				assert.Contains(t, failure.Error(), tt.wantErr, "the message has to name the real cause")

				return
			}

			require.Nil(t, failure)
			assert.Equal(t, tt.want, window)

			if tt.wantLast {
				assert.Nil(t, next, "the last page carries no token")
				return
			}

			require.NotNil(t, next)

			if tt.wantOffset != 0 {
				decoded, decodeErr := pagination.Decode[transcriptstore.ContentCursor](*next)
				require.NoError(t, decodeErr)
				require.NotNil(t, decoded)
				assert.Equal(t, tt.wantOffset, decoded.Offset)
			}
		})
	}
}

// TestTranscriptRefusalsMapToCodes pins the code each refusal of a transcript read reaches the caller as.
func TestTranscriptRefusalsMapToCodes(t *testing.T) {
	getTranscript := func(cursor string) func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error {
		return func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error {
			req := &agentv1.GetTranscriptRequest{AgentId: alphaID}
			if cursor != "" {
				req.Cursor = proto.String(cursor)
			}

			_, err := svc.GetTranscript(ctx, connect.NewRequest(req))

			return err
		}
	}
	getLastTurns := func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error {
		_, err := svc.GetLastTurns(ctx, connect.NewRequest(&agentv1.GetLastTurnsRequest{AgentId: alphaID}))

		return err
	}
	search := func(cursor string) func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error {
		return func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error {
			req := &agentv1.SearchTranscriptsRequest{Query: "reconciler"}
			if cursor != "" {
				req.Pagination = &paginationv1.Pagination{Cursor: proto.String(cursor)}
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
			err:      agents.ErrAgentNotFound,
			wantCode: connect.CodeNotFound,
		},
		{
			name:     "a transcript cursor this server never handed out",
			call:     getTranscript("not-a-token"),
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
			err:      agents.ErrAgentNotFound,
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
		callerIs    string
		asksFor     string
		wantAgentID *string
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
			if tt.callerIs != "" {
				ctx = caller.With(ctx, caller.Caller{AgentID: tt.callerIs})
			}

			req := &agentv1.SearchTranscriptsRequest{Query: "reconciler"}
			if tt.asksFor != "" {
				req.AgentId = xptr.Ptr(tt.asksFor)
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
		Pagination: &paginationv1.Pagination{Limit: proto.Int32(1)},
	}))
	require.NoError(t, err)

	want := &agentv1.SearchTranscriptsResponse{
		Matches: []*agentv1.SearchHit{{
			AgentId:       alphaID,
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
			AgentId: betaID,
			Name:    "beta",
			Role:    "assistant",
			Snippet: "reconcilers",
		}},
	}
	assert.Empty(t, cmp.Diff(want, first.Msg, protocmp.Transform(), protocmp.IgnoreFields(&agentv1.SearchTranscriptsResponse{}, "next_page_token")))
	require.NotNil(t, first.Msg.NextPageToken)

	_, err = svc.SearchTranscripts(t.Context(), connect.NewRequest(&agentv1.SearchTranscriptsRequest{
		Query:      "reconciler",
		Pagination: &paginationv1.Pagination{Limit: proto.Int32(1), Cursor: first.Msg.NextPageToken},
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
			AgentId: xptr.Ptr(betaID),
		}))

		return err
	}
	getBetaTranscript := func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error {
		_, err := svc.GetTranscript(ctx, connect.NewRequest(&agentv1.GetTranscriptRequest{AgentId: betaID}))

		return err
	}
	getBetaLastTurns := func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error {
		_, err := svc.GetLastTurns(ctx, connect.NewRequest(&agentv1.GetLastTurnsRequest{AgentId: betaID}))

		return err
	}

	tests := []struct {
		name       string
		callerIs   string
		read       func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error
		wantBody   string
		wantTarget string
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
			if tt.callerIs != "" {
				ctx = caller.With(ctx, caller.Caller{AgentID: tt.callerIs})
			}

			require.NoError(t, tt.read(ctx, svc))

			if tt.wantBody == "" {
				assert.Empty(t, written.String())

				return
			}

			record := logtest.Record(t, written.String())
			assert.Equal(t, tt.wantBody, record["body"])
			assert.Equal(t, tt.wantTarget, record[logging.TargetAgentID])
		})
	}
}

// TestGetTranscriptPagesToTheEnd pins that the windows join back into the transcript that was stored.
func TestGetTranscriptPagesToTheEnd(t *testing.T) {
	whole := strings.Repeat("a", 50) + strings.Repeat("b", 50) + strings.Repeat("c", 50)
	svc := newTranscriptHandler(&fakeTranscriptReader{content: whole})

	var (
		read   strings.Builder
		cursor string
		pages  int
	)

	for {
		req := &agentv1.GetTranscriptRequest{
			AgentId:  alphaID,
			MaxBytes: proto.Int32(40),
		}
		if cursor != "" {
			req.Cursor = proto.String(cursor)
		}

		got, err := svc.GetTranscript(t.Context(), connect.NewRequest(req))
		require.NoError(t, err)

		read.WriteString(got.Msg.GetContent())
		pages++
		require.Less(t, pages, 20, "the windows have to run out")

		cursor = got.Msg.GetNextPageToken()
		if cursor == "" {
			break
		}
	}

	assert.Equal(t, whole, read.String())
	assert.Equal(t, 4, pages)
}

type lastTurn struct {
	role      string
	seq       int32
	content   string
	truncated bool
}

// TestGetLastTurns pins what reaches the reader and that a window keeps the end of each turn.
func TestGetLastTurns(t *testing.T) {
	stored := []transcriptstore.StoredTurn{
		{
			Seq:     1,
			Role:    "assistant",
			Content: "a first reading of the problem",
		},
		{
			Seq:     2,
			Role:    "user",
			Content: "a nudge",
		},
	}

	tests := []struct {
		name      string
		req       *agentv1.GetLastTurnsRequest
		wantTurns int32
		want      []lastTurn
	}{
		{
			name: "no count is handed down as none",
			req:  &agentv1.GetLastTurnsRequest{},
			want: []lastTurn{
				{
					role:    "assistant",
					seq:     1,
					content: "a first reading of the problem",
				},
				{
					role:    "user",
					seq:     2,
					content: "a nudge",
				},
			},
		},
		{
			name: "a window cuts the head of a turn and says so, leaving a shorter one whole",
			req: &agentv1.GetLastTurnsRequest{
				Turns:    proto.Int32(2),
				MaxBytes: proto.Int32(22),
			},
			wantTurns: 2,
			want: []lastTurn{
				{
					role:      "assistant",
					seq:       1,
					content:   "reading of the problem",
					truncated: true,
				},
				{
					role:    "user",
					seq:     2,
					content: "a nudge",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := &fakeTranscriptReader{turns: stored}
			svc := newTranscriptHandler(reader)

			tt.req.AgentId = alphaID
			got, err := svc.GetLastTurns(t.Context(), connect.NewRequest(tt.req))
			require.NoError(t, err)

			read := xslice.Map(got.Msg.GetTurns(), func(turn *agentv1.Turn) lastTurn {
				return lastTurn{
					role:      turn.GetRole(),
					seq:       turn.GetSeq(),
					content:   turn.GetContent(),
					truncated: turn.GetTruncated(),
				}
			})
			assert.Equal(t, tt.want, read)
			assert.Equal(t, []transcripts.ListLastTurnsParams{{
				AgentID: alphaID,
				Turns:   tt.wantTurns,
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
					Pagination: &paginationv1.Pagination{Limit: proto.Int32(5000)},
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
