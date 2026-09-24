package client

import (
	"context"
	"fmt"
	"strings"

	agentv1 "komrade/proto/gen/agent/v1"
	"komrade/proto/gen/agent/v1/agentv1connect"

	"connectrpc.com/connect"
)

// GetFullTranscript returns one agent's transcript, following the page tokens until the content
// is exhausted or the page ceiling is reached.
func GetFullTranscript(ctx context.Context, agents agentv1connect.AgentServiceClient, agentID string) (string, error) {
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
