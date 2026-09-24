package agent

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"weave/internal/agents"
	"weave/internal/caller"
	"weave/internal/logging"
	"weave/internal/logtest"
	"weave/internal/rpcerr"
	"weave/lib/pagination"
	"weave/lib/xptr"
	"weave/lib/xslice"
	agentv1 "weave/proto/gen/agent/v1"
	"weave/proto/gen/agent/v1/agentv1weave"
	paginationv1 "weave/proto/gen/pagination/v1"

	agentstore "weave/internal/store/agent"
	transcriptstore "weave/internal/store/transcript"

	"connectrpc.com/connect"
	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/testing/protocmp"
)

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
			wantErr:   "maxBytes of 2 is too small",
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

// fakeAgentReader answers whether an agent exists from its field.
type fakeAgentReader struct {
	missing bool
}

// GetByID answers an agent with the id asked for unless it is missing.
func (f fakeAgentReader) GetByID(_ context.Context, id string) (*agentstore.Agent, error) {
	if f.missing {
		return nil, agents.ErrAgentNotFound
	}

	return &agentstore.Agent{ID: id}, nil
}

// Exists answers true unless the agent is missing.
func (f fakeAgentReader) Exists(context.Context, string) (bool, error) {
	return !f.missing, nil
}

// fakeTranscriptReader answers from its fields and records what each call asked of it.
type fakeTranscriptReader struct {
	content  string
	turns    []transcriptstore.StoredTurn
	results  transcriptstore.Results
	err      error
	searched []transcriptstore.SearchParams
	listed   []transcriptstore.ListLastTurnsParams
}

// Get answers content unless err refuses it.
func (f *fakeTranscriptReader) Get(context.Context, string) (string, error) {
	return f.content, f.err
}

// ListLastTurns records the params and answers turns unless err refuses it.
func (f *fakeTranscriptReader) ListLastTurns(_ context.Context, params transcriptstore.ListLastTurnsParams) ([]transcriptstore.StoredTurn, error) {
	f.listed = append(f.listed, params)
	if f.err != nil {
		return nil, f.err
	}

	return f.turns, nil
}

// Search records the params and answers results unless err refuses it.
func (f *fakeTranscriptReader) Search(_ context.Context, params transcriptstore.SearchParams) (transcriptstore.Results, error) {
	f.searched = append(f.searched, params)

	return f.results, f.err
}

// TestTranscriptRefusalsMapToCodes pins the code each refusal of a transcript read reaches the caller as.
func TestTranscriptRefusalsMapToCodes(t *testing.T) {
	getTranscript := func(cursor string) func(ctx context.Context, svc agentv1weave.AgentServiceAdapter) error {
		return func(ctx context.Context, svc agentv1weave.AgentServiceAdapter) error {
			req := &agentv1.GetTranscriptRequest{AgentId: alphaID}
			if cursor != "" {
				req.Cursor = proto.String(cursor)
			}

			_, err := svc.GetTranscript(ctx, connect.NewRequest(req))

			return err
		}
	}
	getLastTurns := func(ctx context.Context, svc agentv1weave.AgentServiceAdapter) error {
		_, err := svc.GetLastTurns(ctx, connect.NewRequest(&agentv1.GetLastTurnsRequest{AgentId: alphaID}))

		return err
	}
	search := func(cursor string) func(ctx context.Context, svc agentv1weave.AgentServiceAdapter) error {
		return func(ctx context.Context, svc agentv1weave.AgentServiceAdapter) error {
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
		call     func(ctx context.Context, svc agentv1weave.AgentServiceAdapter) error
		missing  bool
		err      error
		wantCode connect.Code
	}{
		{
			name:     "the transcript of an agent nobody holds",
			call:     getTranscript(""),
			missing:  true,
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
			missing:  true,
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
			svc := newTranscriptHandler(fakeAgentReader{missing: tt.missing}, &fakeTranscriptReader{err: tt.err})

			err := tt.call(t.Context(), svc)

			assert.Equal(t, tt.wantCode, connect.CodeOf(err))
		})
	}
}

// TestSearchTranscriptsScope pins which agent a search reaches, worked out from the caller as well as the request.
func TestSearchTranscriptsScope(t *testing.T) {
	tests := []struct {
		name        string
		callerIs    string
		asksFor     string
		cursor      *string
		wantAgentID *string
	}{
		{
			name:        "an agent that names nobody reads its own conversation",
			callerIs:    alphaID,
			wantAgentID: xptr.Ptr(alphaID),
		},
		{
			name:        "an agent that names another reads that one",
			callerIs:    alphaID,
			asksFor:     betaID,
			wantAgentID: xptr.Ptr(betaID),
		},
		{
			name:        "a replayed token does not widen an agent's own search",
			callerIs:    alphaID,
			cursor:      proto.String("eyJtIjp7InIiOjAuNSwiaSI6IjAxYTBkMWQ3LTAwMDAtNzAwMC04MDAwLTAwMDAwMDAwMDAwYiIsInMiOjF9fQ=="),
			wantAgentID: xptr.Ptr(alphaID),
		},
		{
			name: "the panel reads every conversation",
		},
		{
			name:        "the panel can still narrow to one",
			asksFor:     betaID,
			wantAgentID: xptr.Ptr(betaID),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			transcripts := &fakeTranscriptReader{}
			svc := newTranscriptHandler(fakeAgentReader{}, transcripts)

			ctx := t.Context()
			if tt.callerIs != "" {
				ctx = caller.With(ctx, caller.Caller{AgentID: tt.callerIs})
			}

			req := &agentv1.SearchTranscriptsRequest{
				Query:      "reconciler",
				Pagination: &paginationv1.Pagination{Cursor: tt.cursor},
			}
			if tt.asksFor != "" {
				req.AgentId = xptr.Ptr(tt.asksFor)
			}

			_, err := svc.SearchTranscripts(ctx, connect.NewRequest(req))
			require.NoError(t, err)

			require.Len(t, transcripts.searched, 1)
			assert.Equal(t, tt.wantAgentID, transcripts.searched[0].AgentID)
		})
	}
}

// TestSearchTranscripts pins how each hit renders and that the store's next cursor becomes the page token.
func TestSearchTranscripts(t *testing.T) {
	next := transcriptstore.Cursor{Matches: &transcriptstore.Position{
		Rank:    0.5,
		AgentID: alphaID,
		Seq:     1,
	}}
	transcripts := &fakeTranscriptReader{results: transcriptstore.Results{
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
	svc := newTranscriptHandler(fakeAgentReader{}, transcripts)

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

	firstPage := transcriptstore.SearchParams{
		Query: "reconciler",
		Limit: 1,
	}
	secondPage := transcriptstore.SearchParams{
		Query:  "reconciler",
		Limit:  1,
		Cursor: &next,
	}
	assert.Equal(t, []transcriptstore.SearchParams{firstPage, secondPage}, transcripts.searched)
}

// TestCrossAgentReadIsRecorded pins the line a read of somebody else's conversation leaves, and the silence a read of your own does.
func TestCrossAgentReadIsRecorded(t *testing.T) {
	searchBeta := func(ctx context.Context, svc agentv1weave.AgentServiceAdapter) error {
		_, err := svc.SearchTranscripts(ctx, connect.NewRequest(&agentv1.SearchTranscriptsRequest{
			Query:   "reconciler",
			AgentId: xptr.Ptr(betaID),
		}))

		return err
	}
	getBetaTranscript := func(ctx context.Context, svc agentv1weave.AgentServiceAdapter) error {
		_, err := svc.GetTranscript(ctx, connect.NewRequest(&agentv1.GetTranscriptRequest{AgentId: betaID}))

		return err
	}
	getBetaLastTurns := func(ctx context.Context, svc agentv1weave.AgentServiceAdapter) error {
		_, err := svc.GetLastTurns(ctx, connect.NewRequest(&agentv1.GetLastTurnsRequest{AgentId: betaID}))

		return err
	}

	tests := []struct {
		name       string
		callerIs   string
		read       func(ctx context.Context, svc agentv1weave.AgentServiceAdapter) error
		wantRecord bool
	}{
		{
			name:       "an agent searching another agent",
			callerIs:   alphaID,
			read:       searchBeta,
			wantRecord: true,
		},
		{
			name:       "an agent fetching another agent's whole conversation",
			callerIs:   alphaID,
			read:       getBetaTranscript,
			wantRecord: true,
		},
		{
			name:       "an agent fetching another agent's last turns",
			callerIs:   alphaID,
			read:       getBetaLastTurns,
			wantRecord: true,
		},
		{
			name:     "an agent searching itself",
			callerIs: betaID,
			read:     searchBeta,
		},
		{
			name: "the panel reading anyone",
			read: getBetaTranscript,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := newTranscriptHandler(fakeAgentReader{}, &fakeTranscriptReader{})

			var written strings.Builder
			ctx := logging.WithLogger(t.Context(), logging.New(&written, slog.LevelInfo))
			if tt.callerIs != "" {
				ctx = caller.With(ctx, caller.Caller{AgentID: tt.callerIs})
			}

			require.NoError(t, tt.read(ctx, svc))

			if !tt.wantRecord {
				assert.Empty(t, written.String())

				return
			}

			record := logtest.Record(t, written.String())
			assert.Equal(t, "agent read another agent's transcript", record["body"])
			assert.Equal(t, betaID, record[logging.TargetAgentID])
		})
	}
}

// TestGetTranscriptPagesToTheEnd pins that the windows join back into the transcript that was stored.
func TestGetTranscriptPagesToTheEnd(t *testing.T) {
	whole := strings.Repeat("a", 50) + strings.Repeat("b", 50) + strings.Repeat("c", 50)
	svc := newTranscriptHandler(fakeAgentReader{}, &fakeTranscriptReader{content: whole})

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

// TestGetLastTurns pins what reaches the store and that a window keeps the end of each turn.
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
		wantLimit int32
		want      []lastTurn
	}{
		{
			name:      "the default is the last thing the agent said",
			req:       &agentv1.GetLastTurnsRequest{},
			wantLimit: 1,
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
			wantLimit: 2,
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
			transcripts := &fakeTranscriptReader{turns: stored}
			svc := newTranscriptHandler(fakeAgentReader{}, transcripts)

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
			assert.Equal(t, []transcriptstore.ListLastTurnsParams{{
				AgentID: alphaID,
				Role:    "assistant",
				Limit:   tt.wantLimit,
			}}, transcripts.listed)
		})
	}
}

// newTranscriptHandler is the handler over fakes of the agents and the transcripts alone.
func newTranscriptHandler(agentReader fakeAgentReader, transcripts *fakeTranscriptReader) agentv1weave.AgentServiceAdapter {
	return agentv1weave.AgentServiceAdapter{Impl: NewHandler(NewHandlerParams{
		Agents:      agentReader,
		Transcripts: transcripts,
	})}
}
