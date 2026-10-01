package transcripts

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"komrad/internal/agenterr"
	"komrad/internal/logging"
	agentstore "komrad/internal/store/agent"
	transcriptstore "komrad/internal/store/transcript"
	"komrad/lib/xstr"
)

const assistantRole = "assistant"

type NewTranscriptReaderParams struct {
	Agents      *agentstore.AgentStore
	Transcripts *transcriptstore.TranscriptStore
	Clock       func() time.Time
}

// TranscriptReader reads what agents wrote.
type TranscriptReader struct {
	agents      *agentstore.AgentStore
	transcripts *transcriptstore.TranscriptStore
	now         func() time.Time
}

// NewTranscriptReader builds the reader over the agent and transcript stores.
func NewTranscriptReader(params NewTranscriptReaderParams) *TranscriptReader {
	now := params.Clock
	if now == nil {
		now = time.Now
	}

	return &TranscriptReader{
		agents:      params.Agents,
		transcripts: params.Transcripts,
		now:         now,
	}
}

// SearchTranscriptsParams is a search's text and page, with the agent it names and the agent asking.
type SearchTranscriptsParams struct {
	Query   string
	Limit   int64
	Cursor  *transcriptstore.Cursor
	AgentID *string
	Caller  string
}

// SearchTranscripts runs a search over the stored transcripts narrowed by the qualifiers the query holds.
func (r *TranscriptReader) SearchTranscripts(ctx context.Context, params SearchTranscriptsParams) (*transcriptstore.Results, error) {
	query, err := ParseQuery(params.Query, r.now())
	if err != nil {
		return nil, err
	}

	scope := params.AgentID
	if scope == nil && params.Caller != "" {
		if query.Widens() {
			logging.From(ctx).LogAttrs(ctx, slog.LevelInfo, "agent searched every agent's transcript")
		} else {
			scope = &params.Caller
		}
	}

	results, err := r.transcripts.Search(ctx, transcriptstore.SearchParams{
		Query:   query.Words,
		Limit:   params.Limit,
		Cursor:  params.Cursor,
		AgentID: scope,
		Filter:  query.Filter,
		Sort:    query.Sort,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to search transcripts: %w", err)
	}

	return &results, nil
}

type ReadTranscriptWindowParams struct {
	AgentID  string
	Cursor   *transcriptstore.ContentCursor
	MaxBytes int32
}

// ReadTranscriptWindow returns one window of an agent's stored transcript, cut between runes.
func (r *TranscriptReader) ReadTranscriptWindow(ctx context.Context, params ReadTranscriptWindowParams) (*Window, error) {
	if err := r.requireAgent(ctx, params.AgentID); err != nil {
		return nil, err
	}

	content, err := r.transcripts.Get(ctx, params.AgentID)
	if err != nil {
		return nil, fmt.Errorf("failed to get the transcript of agent %q: %w", params.AgentID, err)
	}

	return cutWindow(content, params.Cursor, params.MaxBytes)
}

type ListLastTurnsParams struct {
	AgentID  string
	Turns    int32
	MaxBytes int32
}

// ListLastTurns returns the newest turns an agent wrote and the prompts between them, oldest first, one turn when none is asked for, each cut from the head to fit the window.
func (r *TranscriptReader) ListLastTurns(ctx context.Context, params ListLastTurnsParams) ([]Turn, error) {
	if err := r.requireAgent(ctx, params.AgentID); err != nil {
		return nil, err
	}

	count := params.Turns
	if count <= 0 {
		count = 1
	}

	stored, err := r.transcripts.ListLastTurns(ctx, transcriptstore.ListLastTurnsParams{
		AgentID: params.AgentID,
		Role:    assistantRole,
		Limit:   count,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list the last turns of agent %q: %w", params.AgentID, err)
	}

	limit := clampWindow(params.MaxBytes)

	turns := make([]Turn, 0, len(stored))
	for _, turn := range stored {
		content, truncated := xstr.Tail(turn.Content, limit)
		turns = append(turns, Turn{
			Role:      turn.Role,
			Seq:       turn.Seq,
			Content:   content,
			Truncated: truncated,
		})
	}

	return turns, nil
}

func (r *TranscriptReader) requireAgent(ctx context.Context, agentID string) error {
	exists, err := r.agents.Exists(ctx, agentID)
	if err != nil {
		return fmt.Errorf("failed to look up agent %q: %w", agentID, err)
	}
	if !exists {
		return agenterr.ErrAgentNotFound.Saying("no agent with id %q", agentID)
	}

	return nil
}
