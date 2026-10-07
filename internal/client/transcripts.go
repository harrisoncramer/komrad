package client

import (
	"context"
	"fmt"
	"strings"

	agentv1 "komrad/proto/gen/agent/v1"

	"connectrpc.com/connect"
)

<<<<<<< HEAD
=======
// maxTranscriptPages is the most windows one transcript is read in, about 32MB at the server's
// window, past which it is reported too long rather than shown cut short.
const maxTranscriptPages = 1024

>>>>>>> 61775cf936138e0857861ae4d254f464ce42b490
// TranscriptGetter is the part of the agent service a transcript is read through.
type TranscriptGetter interface {
	GetTranscript(ctx context.Context, req *connect.Request[agentv1.GetTranscriptRequest]) (*connect.Response[agentv1.GetTranscriptResponse], error)
}

// GetFullTranscript returns one agent's transcript, following the page tokens until the content
<<<<<<< HEAD
// is exhausted or the page ceiling is reached.
=======
// is exhausted, and fails rather than return part of it once the page ceiling is reached.
>>>>>>> 61775cf936138e0857861ae4d254f464ce42b490
func GetFullTranscript(ctx context.Context, agents TranscriptGetter, agentID string) (string, error) {
	var (
		content strings.Builder
		cursor  string
	)

<<<<<<< HEAD
	for range maxPages {
=======
	for range maxTranscriptPages {
>>>>>>> 61775cf936138e0857861ae4d254f464ce42b490
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

<<<<<<< HEAD
	return content.String(), nil
=======
	return "", fmt.Errorf("failed to get transcript: still paging after %d pages", maxTranscriptPages)
>>>>>>> 61775cf936138e0857861ae4d254f464ce42b490
}
