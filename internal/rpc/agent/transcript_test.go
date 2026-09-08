package agent

import (
	"strings"
	"testing"

	"weave/lib/pagination"
	agentv1 "weave/proto/gen/agent/v1"

	transcriptstore "weave/internal/store/transcript"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	agentstore "weave/internal/store/agent"
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

			window, next, err := transcriptWindow(tt.content, cursor, tt.requested)
			if tt.wantCode != 0 {
				assert.Equal(t, tt.wantCode, connect.CodeOf(err))
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr, "the message has to name the real cause")

				return
			}

			require.NoError(t, err)
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

// TestGetTranscriptPagesToTheEnd covers the windows joining back into the transcript that was
// stored.
func TestGetTranscriptPagesToTheEnd(t *testing.T) {
	svc, st := service(t)
	agent := st.SeedAgent(t, agentstore.Agent{Name: "alpha", State: agentstore.StateThinking})
	turns := []string{strings.Repeat("a", 50), strings.Repeat("b", 50), strings.Repeat("c", 50)}
	whole := seedTranscript(t, st, agent.ID, turns)

	var (
		read   strings.Builder
		cursor string
		pages  int
	)

	for {
		req := &agentv1.GetTranscriptRequest{AgentId: agent.ID, MaxBytes: proto.Int32(40)}
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
	assert.Greater(t, pages, 1, "a transcript longer than the window takes more than one call")
}

// TestGetTranscriptRefusesACursorItNeverHandedOut covers a token from somewhere else, which is a
// caller's mistake rather than a missing transcript.
func TestGetTranscriptRefusesACursorItNeverHandedOut(t *testing.T) {
	svc, st := service(t)
	agent := st.SeedAgent(t, agentstore.Agent{Name: "alpha", State: agentstore.StateThinking})

	_, err := svc.GetTranscript(t.Context(), connect.NewRequest(&agentv1.GetTranscriptRequest{
		AgentId: agent.ID,
		Cursor:  proto.String("not-a-token"),
	}))

	assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
}
