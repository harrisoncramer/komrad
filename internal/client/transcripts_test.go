package client

import (
	"context"
	"strconv"
	"strings"
	"testing"

	agentv1 "komrad/proto/gen/agent/v1"
	"komrad/proto/gen/agent/v1/agentv1connect"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

// TestGetFullTranscript covers the panel reading a transcript the endpoint hands over a window at
// a time, where stopping at the first window would show a preview missing most of the match and
// giving up at the ceiling would show nothing at all.
func TestGetFullTranscript(t *testing.T) {
	tests := []struct {
		name      string
		windows   []string
		neverEnds bool
		want      string
	}{
		{
			name:    "a transcript that fits in one window",
			windows: []string{"the whole conversation"},
			want:    "the whole conversation",
		},
		{
			name:    "a transcript spread over three",
			windows: []string{"opening ", "question ", "and the answer"},
			want:    "opening question and the answer",
		},
		{
			name:    "an agent with nothing stored yet",
			windows: []string{""},
			want:    "",
		},
		{
			name:      "an endpoint that never stops handing out tokens",
			windows:   []string{"round and "},
			neverEnds: true,
			want:      strings.Repeat("round and ", maxPages),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agents := &fakeTranscriptAgents{windows: tt.windows, neverEnds: tt.neverEnds}

			got, err := GetFullTranscript(t.Context(), agents, "a1")
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)

			if tt.neverEnds {
				assert.Equal(t, maxPages, agents.calls, "it stops paging rather than following tokens forever")
			}
		})
	}
}

// fakeTranscriptAgents hands back one window per call, minting a token for as long as there is
// another window to read.
type fakeTranscriptAgents struct {
	agentv1connect.AgentServiceClient
	windows   []string
	neverEnds bool
	calls     int
}

func (f *fakeTranscriptAgents) GetTranscript(
	_ context.Context,
	req *connect.Request[agentv1.GetTranscriptRequest],
) (*connect.Response[agentv1.GetTranscriptResponse], error) {
	index := 0
	if cursor := req.Msg.GetCursor(); cursor != "" {
		parsed, err := strconv.Atoi(cursor)
		if err != nil {
			return nil, err
		}

		index = parsed
	}

	f.calls++

	response := &agentv1.GetTranscriptResponse{Content: f.windows[index%len(f.windows)]}
	if f.neverEnds || index+1 < len(f.windows) {
		response.NextPageToken = proto.String(strconv.Itoa(index + 1))
	}

	return connect.NewResponse(response), nil
}
