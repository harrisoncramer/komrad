package agent

import (
	"context"
	"fmt"

	"weave/internal/caller"
	"weave/internal/rpcerr"
	"weave/lib/pagination"
	"weave/lib/xslice"
	"weave/lib/xstr"
	agentv1 "weave/proto/gen/agent/v1"

	transcriptstore "weave/internal/store/transcript"

	"connectrpc.com/connect"
)

type transcriptReader interface {
	Get(ctx context.Context, agentID string) (transcriptstore.Transcript, error)
	Search(ctx context.Context, params transcriptstore.SearchParams) (transcriptstore.Results, error)
}

// SearchTranscripts runs a search over stored transcripts and returns one page of the turns
// that say the words, and one of the turns that only carry the letters.
func (s *Service) SearchTranscripts(ctx context.Context, req *connect.Request[agentv1.SearchTranscriptsRequest]) (*connect.Response[agentv1.SearchTranscriptsResponse], error) {
	paginator, err := pagination.New[transcriptstore.Cursor, searchPage](req.Msg.GetPagination())
	if err != nil {
		return nil, err
	}

	// Scope is worked out per page rather than carried in the token, so a replayed cursor
	// cannot widen a search.
	scope := searchScope(ctx, req.Msg.GetAgentId())
	if scope != nil {
		logCrossAgentRead(ctx, *scope, "agent read another agent's transcript")
	}

	results, err := s.transcripts.Search(ctx, transcriptstore.SearchParams{
		Query:   req.Msg.GetQuery(),
		Limit:   paginator.Limit(),
		Cursor:  paginator.Cursor(),
		AgentID: scope,
	})
	if err != nil {
		return nil, rpcerr.Wrap(err, "failed to search transcripts")
	}

	var next *string
	if results.Next != nil {
		if next, err = pagination.Token(*results.Next); err != nil {
			return nil, rpcerr.Wrap(err, "failed to build the next page token")
		}
	}

	return connect.NewResponse(&agentv1.SearchTranscriptsResponse{
		Matches:       xslice.Map(results.Matches, renderSearchHit),
		Near:          xslice.Map(results.Near, renderSearchHit),
		NextPageToken: next,
	}), nil
}

// maxTranscriptWindow is the most content one GetTranscript call returns. A transcript runs to
// a hundred kilobytes, which is more than a caller reading it through a context window can take
// in one piece, so the rest is fetched by following the page token.
const maxTranscriptWindow = 32 * 1024

// GetTranscript returns one window of an agent's stored transcript by id, so the search preview
// can show a match in its surrounding conversation.
func (s *Service) GetTranscript(ctx context.Context, req *connect.Request[agentv1.GetTranscriptRequest]) (*connect.Response[agentv1.GetTranscriptResponse], error) {
	logCrossAgentRead(ctx, req.Msg.GetAgentId(), "agent read another agent's transcript")

	cursor, err := pagination.Decode[transcriptstore.ContentCursor](req.Msg.GetCursor())
	if err != nil {
		return nil, err
	}

	// Empty content would read as an agent with nothing stored yet, so a missing one is
	// not_found instead.
	exists, err := s.agents.Exists(ctx, req.Msg.GetAgentId())
	if err != nil {
		return nil, rpcerr.Wrap(err, "failed to look up agent")
	}
	if !exists {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("no agent with id %q", req.Msg.GetAgentId()))
	}

	tr, err := s.transcripts.Get(ctx, req.Msg.GetAgentId())
	if err != nil {
		return nil, rpcerr.Wrap(err, "failed to get transcript")
	}

	window, next, err := transcriptWindow(tr.Content, cursor, req.Msg.GetMaxBytes())
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(&agentv1.GetTranscriptResponse{Content: window, NextPageToken: next}), nil
}

// transcriptWindow cuts the window the request asked for out of content and mints the token for
// the one after it. The cut is rune-safe, so the offset a token carries always lands between
// characters and a window is always text the caller can decode.
func transcriptWindow(content string, cursor *transcriptstore.ContentCursor, requested int32) (string, *string, error) {
	limit := maxTranscriptWindow
	if requested > 0 && int(requested) < limit {
		limit = int(requested)
	}

	var offset int
	if cursor != nil {
		offset = int(cursor.Offset)
	}

	// An offset past the end is the empty last page rather than an error, since a transcript
	// the agent is still writing grows between one call and the next.
	if offset >= len(content) {
		return "", nil, nil
	}

	window, truncated := xstr.Head(content[offset:], limit)
	if !truncated {
		return window, nil, nil
	}

	// A limit landing inside the first rune would return nothing and hand back a token that
	// asks the same question again, so the window has to move forward or stop.
	if window == "" {
		return "", nil, connect.NewError(connect.CodeInvalidArgument,
			fmt.Errorf("maxBytes of %d is too small to hold the next character", limit))
	}

	next, err := pagination.Token(transcriptstore.ContentCursor{Offset: int64(offset + len(window))})
	if err != nil {
		return "", nil, rpcerr.Wrap(err, "failed to build the next page token")
	}

	return window, next, nil
}

func searchScope(ctx context.Context, requested string) *string {
	if requested != "" {
		return &requested
	}

	from := caller.From(ctx)
	if from.AgentID == "" {
		return nil
	}

	return &from.AgentID
}
