package client

import (
	"context"
	"fmt"
	"strings"

	agentv1 "komrad/proto/gen/agent/v1"

	"connectrpc.com/connect"
)

// TranscriptGetter is the part of the agent service a transcript is read through.
type TranscriptGetter interface {
	GetTranscript(ctx context.Context, req *connect.Request[agentv1.GetTranscriptRequest]) (*connect.Response[agentv1.GetTranscriptResponse], error)
}

// GetFullTranscript returns one agent's transcript, following the page tokens until the content
// is exhausted or the page ceiling is reached.
func GetFullTranscript(ctx context.Context, agents TranscriptGetter, agentID string) (string, error) {
	var (
		content strings.Builder
		cursor  string
	)

	for range maxPages {
		req := &agentv1.GetTranscriptRequest{AgentId: agentID}
		if cursor != "" {
			req.Cursor = &cursor
		}

		resp, err := agents.GetTranscript(ctx, connect.NewRequest(req))
		if err != nil {
			return "", fmt.Errorf("failed to get transcript: %w", err)
		}

		content.WriteString(resp.Msg.GetContent())

		if resp.Msg.GetNextPageToken() == "" {
			return content.String(), nil
		}

		cursor = resp.Msg.GetNextPageToken()
	}

	return content.String(), nil
}
