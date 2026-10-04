package agent

import (
	"context"

	"komrad/internal/caller"
	"komrad/internal/fail"
	"komrad/internal/rpc/render"
	"komrad/internal/rpcerr"
	"komrad/internal/transcripts"
	"komrad/lib/pagination"
	"komrad/lib/xslice"
	agentv1 "komrad/proto/gen/agent/v1"

	transcriptstore "komrad/internal/store/transcript"
)

type transcriptReader interface {
	ReadTranscriptWindow(ctx context.Context, params transcripts.ReadTranscriptWindowParams) (*transcripts.Window, error)
	ListLastTurns(ctx context.Context, params transcripts.ListLastTurnsParams) ([]transcripts.Turn, error)
	ComposeForward(ctx context.Context, params transcripts.ComposeForwardParams) (*transcripts.Forward, error)
	SearchTranscripts(ctx context.Context, params transcripts.SearchTranscriptsParams) (*transcriptstore.Results, error)
}

// SearchTranscripts runs a search over stored transcripts and returns one page of the turns
// that say the words, and one of the turns that only carry the letters.
func (s *Handler) SearchTranscripts(ctx context.Context, req *agentv1.SearchTranscriptsRequest) (*agentv1.SearchTranscriptsResponse, *fail.Error) {
	paginator, err := pagination.New[transcriptstore.Cursor, searchPage](req.GetPagination())
	if err != nil {
		return nil, rpcerr.From(err, "failed to read the pagination")
	}

	var scope *string
	if requested := req.GetAgentId(); requested != "" {
		scope = &requested
		logCrossAgentRead(ctx, requested, "agent read another agent's transcript")
	}

	results, err := s.transcripts.SearchTranscripts(ctx, transcripts.SearchTranscriptsParams{
		Query:   req.GetQuery(),
		Limit:   paginator.Limit(),
		Cursor:  paginator.Cursor(),
		AgentID: scope,
		Caller:  caller.From(ctx).AgentID,
	})
	if err != nil {
		return nil, rpcerr.From(err, "failed to search transcripts")
	}

	var next *string
	if results.Next != nil {
		if next, err = pagination.Token(*results.Next); err != nil {
			return nil, rpcerr.From(err, "failed to build the next page token")
		}
	}

	return &agentv1.SearchTranscriptsResponse{
		Matches:       xslice.Map(results.Matches, renderSearchHit),
		Near:          xslice.Map(results.Near, renderSearchHit),
		NextPageToken: next,
	}, nil
}

// GetTranscript returns one window of an agent's stored transcript by id.
func (s *Handler) GetTranscript(ctx context.Context, req *agentv1.GetTranscriptRequest) (*agentv1.GetTranscriptResponse, *fail.Error) {
	logCrossAgentRead(ctx, req.GetAgentId(), "agent read another agent's transcript")

	var cursor *transcriptstore.ContentCursor
	if token := req.GetCursor(); token != "" {
		decoded, err := pagination.Decode[transcriptstore.ContentCursor](token)
		if err != nil {
			return nil, rpcerr.From(err, "failed to read the cursor")
		}

		cursor = decoded
	}

	window, err := s.transcripts.ReadTranscriptWindow(ctx, transcripts.ReadTranscriptWindowParams{
		AgentID:  req.GetAgentId(),
		Cursor:   cursor,
		MaxBytes: req.GetMaxBytes(),
	})
	if err != nil {
		return nil, rpcerr.From(err, "failed to get transcript")
	}

	var next *string
	if window.Next != nil {
		if next, err = pagination.Token(*window.Next); err != nil {
			return nil, rpcerr.From(err, "failed to build the next page token")
		}
	}

	return &agentv1.GetTranscriptResponse{Content: window.Content, NextPageToken: next}, nil
}

// GetLastTurns returns the newest turns an agent wrote and the prompts between them, oldest
// first, each cut from the head to fit the window asked for.
func (s *Handler) GetLastTurns(ctx context.Context, req *agentv1.GetLastTurnsRequest) (*agentv1.GetLastTurnsResponse, *fail.Error) {
	logCrossAgentRead(ctx, req.GetAgentId(), "agent read another agent's transcript")

	turns, err := s.transcripts.ListLastTurns(ctx, transcripts.ListLastTurnsParams{
		AgentID:  req.GetAgentId(),
		Turns:    req.GetTurns(),
		MaxBytes: req.GetMaxBytes(),
	})
	if err != nil {
		return nil, rpcerr.From(err, "failed to list the last turns")
	}

	return &agentv1.GetLastTurnsResponse{Turns: xslice.Map(turns, render.Turn)}, nil
}
