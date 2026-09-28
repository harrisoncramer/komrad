package agent

import (
	"context"
	"unicode/utf8"

	"komrad/internal/caller"
	"komrad/internal/fail"
	"komrad/internal/rpcerr"
	"komrad/internal/transcripts"
	"komrad/lib/pagination"
	"komrad/lib/xslice"
	"komrad/lib/xstr"
	agentv1 "komrad/proto/gen/agent/v1"

	transcriptstore "komrad/internal/store/transcript"
)

type transcriptReader interface {
	ReadTranscript(ctx context.Context, agentID string) (string, error)
	ListLastTurns(ctx context.Context, params transcripts.ListLastTurnsParams) ([]transcriptstore.StoredTurn, error)
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

// errCursorMidRune is a cursor pointing inside a character.
var errCursorMidRune = fail.InvalidArgument(
	"cursor no longer names the start of a character, since the transcript was rewritten; read it again from the first window")

// errWindowTooSmall is a window too small to hold even one character.
var errWindowTooSmall = fail.InvalidArgument("the window is too small to hold the next character")

// maxTranscriptWindow is the most content one call returns.
const maxTranscriptWindow = 32 * 1024

// clampWindow is the window a request asked for, bounded above by maxTranscriptWindow.
func clampWindow(requested int32) int {
	if requested > 0 && int(requested) < maxTranscriptWindow {
		return int(requested)
	}

	return maxTranscriptWindow
}

// GetTranscript returns one window of an agent's stored transcript by id.
func (s *Handler) GetTranscript(ctx context.Context, req *agentv1.GetTranscriptRequest) (*agentv1.GetTranscriptResponse, *fail.Error) {
	logCrossAgentRead(ctx, req.GetAgentId(), "agent read another agent's transcript")

	cursor, err := pagination.Decode[transcriptstore.ContentCursor](req.GetCursor())
	if err != nil {
		return nil, rpcerr.From(err, "failed to read the cursor")
	}

	content, err := s.transcripts.ReadTranscript(ctx, req.GetAgentId())
	if err != nil {
		return nil, rpcerr.From(err, "failed to get transcript")
	}

	window, next, failure := transcriptWindow(content, cursor, req.GetMaxBytes())
	if failure != nil {
		return nil, failure
	}

	return &agentv1.GetTranscriptResponse{Content: window, NextPageToken: next}, nil
}

// transcriptWindow cuts the window the request asked for out of content, between runes, and
// mints the token for the one after it.
func transcriptWindow(content string, cursor *transcriptstore.ContentCursor, requested int32) (string, *string, *fail.Error) {
	limit := clampWindow(requested)

	var offset int
	if cursor != nil {
		offset = int(cursor.Offset)
	}

	// An offset past the end is the empty last page of a transcript still being written.
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

	// A limit landing inside the first rune would hand back a token asking the same question again.
	if window == "" {
		return "", nil, errWindowTooSmall.Saying("max_bytes of %d is too small to hold the next character", limit)
	}

	next, err := pagination.Token(transcriptstore.ContentCursor{Offset: int64(offset + len(window))})
	if err != nil {
		return "", nil, rpcerr.From(err, "failed to build the next page token")
	}

	return window, next, nil
}

// GetLastTurns returns the newest turns an agent wrote and the prompts between them, oldest
// first, each cut from the head to fit the window asked for.
func (s *Handler) GetLastTurns(ctx context.Context, req *agentv1.GetLastTurnsRequest) (*agentv1.GetLastTurnsResponse, *fail.Error) {
	logCrossAgentRead(ctx, req.GetAgentId(), "agent read another agent's transcript")

	turns, failure := s.listLastTurns(ctx, listLastTurnsParams{
		AgentID:  req.GetAgentId(),
		Turns:    req.GetTurns(),
		MaxBytes: req.GetMaxBytes(),
	})
	if failure != nil {
		return nil, failure
	}

	return &agentv1.GetLastTurnsResponse{Turns: turns}, nil
}

type listLastTurnsParams struct {
	AgentID  string
	Turns    int32
	MaxBytes int32
}

// listLastTurns reads an agent's newest turns and the prompts between them, oldest first, each
// cut from the head to fit the window.
func (s *Handler) listLastTurns(ctx context.Context, params listLastTurnsParams) ([]*agentv1.Turn, *fail.Error) {
	newest, err := s.transcripts.ListLastTurns(ctx, transcripts.ListLastTurnsParams{
		AgentID: params.AgentID,
		Turns:   params.Turns,
	})
	if err != nil {
		return nil, rpcerr.From(err, "failed to list the last turns")
	}

	limit := clampWindow(params.MaxBytes)

	turns := make([]*agentv1.Turn, 0, len(newest))
	for _, turn := range newest {
		content, truncated := xstr.Tail(turn.Content, limit)
		turns = append(turns, &agentv1.Turn{
			Role:      turn.Role,
			Seq:       turn.Seq,
			Content:   content,
			Truncated: truncated,
		})
	}

	return turns, nil
}
