package agent

import (
	"context"
	"unicode/utf8"

	"weave/internal/agents"
	"weave/internal/caller"
	"weave/internal/fail"
	"weave/internal/rpcerr"
	"weave/lib/pagination"
	"weave/lib/xslice"
	"weave/lib/xstr"
	agentv1 "weave/proto/gen/agent/v1"

	transcriptstore "weave/internal/store/transcript"
)

type transcriptReader interface {
	Get(ctx context.Context, agentID string) (string, error)
	Search(ctx context.Context, params transcriptstore.SearchParams) (transcriptstore.Results, error)
}

// SearchTranscripts runs a search over stored transcripts and returns one page of the turns
// that say the words, and one of the turns that only carry the letters.
func (s *AgentService) SearchTranscripts(ctx context.Context, req *agentv1.SearchTranscriptsRequest) (*agentv1.SearchTranscriptsResponse, *fail.Error) {
	paginator, err := pagination.New[transcriptstore.Cursor, searchPage](req.GetPagination())
	if err != nil {
		return nil, rpcerr.From(err, "failed to read the pagination")
	}

	// Scope is worked out per page rather than carried in the token, so a replayed cursor
	// cannot widen a search.
	scope := searchScope(ctx, req.GetAgentId())
	if scope != nil {
		logCrossAgentRead(ctx, *scope, "agent read another agent's transcript")
	}

	results, err := s.transcripts.Search(ctx, transcriptstore.SearchParams{
		Query:   req.GetQuery(),
		Limit:   paginator.Limit(),
		Cursor:  paginator.Cursor(),
		AgentID: scope,
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

// errCursorMidRune is a cursor pointing inside a character, which the transcript being
// rewritten under it leaves behind.
var errCursorMidRune = fail.InvalidArgument(
	"cursor no longer names the start of a character, since the transcript was rewritten; read it again from the first window")

// errWindowTooSmall is a window too small to hold even one character.
var errWindowTooSmall = fail.InvalidArgument("the window is too small to hold the next character")

// maxTranscriptWindow is the most content one GetTranscript call returns, the rest of a
// transcript being reached by following the page token.
const maxTranscriptWindow = 32 * 1024

// GetTranscript returns one window of an agent's stored transcript by id.
func (s *AgentService) GetTranscript(ctx context.Context, req *agentv1.GetTranscriptRequest) (*agentv1.GetTranscriptResponse, *fail.Error) {
	logCrossAgentRead(ctx, req.GetAgentId(), "agent read another agent's transcript")

	cursor, err := pagination.Decode[transcriptstore.ContentCursor](req.GetCursor())
	if err != nil {
		return nil, rpcerr.From(err, "failed to read the cursor")
	}

	// Empty content would read as an agent with nothing stored yet, so a missing one is
	// not_found instead.
	exists, err := s.agents.Exists(ctx, req.GetAgentId())
	if err != nil {
		return nil, rpcerr.From(err, "failed to look up agent")
	}
	if !exists {
		return nil, agents.ErrAgentNotFound.Saying("no agent with id %q", req.GetAgentId())
	}

	// The store assembles the whole conversation and the window is cut from it below, so the cap
	// bounds what the caller reads rather than what the server does, and paging one to its end
	// costs the whole transcript once per window.
	content, err := s.transcripts.Get(ctx, req.GetAgentId())
	if err != nil {
		return nil, rpcerr.From(err, "failed to get transcript")
	}

	window, next, failure := transcriptWindow(content, cursor, req.GetMaxBytes())
	if failure != nil {
		return nil, failure
	}

	return &agentv1.GetTranscriptResponse{Content: window, NextPageToken: next}, nil
}

// transcriptWindow cuts the window the request asked for out of content and mints the token for
// the one after it, cutting between runes so every window is text the caller can decode.
func transcriptWindow(content string, cursor *transcriptstore.ContentCursor, requested int32) (string, *string, *fail.Error) {
	limit := maxTranscriptWindow
	if requested > 0 && int(requested) < limit {
		limit = int(requested)
	}

	var offset int
	if cursor != nil {
		offset = int(cursor.Offset)
	}

	// An offset past the end is the empty last page rather than an error, a transcript still being
	// written growing between one call and the next.
	if offset >= len(content) {
		return "", nil, nil
	}

	if !utf8.RuneStart(content[offset]) {
		return "", nil, errCursorMidRune
	}

	window, truncated := xstr.Head(content[offset:], limit)
	if !truncated {
		return window, nil, nil
	}

	// A limit landing inside the first rune would return nothing and hand back a token that
	// asks the same question again, so the window has to move forward or stop.
	if window == "" {
		return "", nil, errWindowTooSmall.Saying("maxBytes of %d is too small to hold the next character", limit)
	}

	next, err := pagination.Token(transcriptstore.ContentCursor{Offset: int64(offset + len(window))})
	if err != nil {
		return "", nil, rpcerr.From(err, "failed to build the next page token")
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
