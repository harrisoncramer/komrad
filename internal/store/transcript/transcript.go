// Package transcript persists the flattened text of each agent and searches it.
//
// A transcript is keyed by the id of the agent it belongs to, so removing an agent
// has to remove its transcript too. That is composed by the caller inside one
// transaction: the session package deletes the agent, this one deletes the text.
package transcript

import (
	"context"
	"fmt"
	"strings"

	"claude-agents/db/models"
	"claude-agents/internal/store"
)

// Transcript is a session's stored transcript: the flattened text and how far
// into the source JSONL the sync has consumed.
type Transcript struct {
	Content      string
	SyncedOffset int64
}

// SearchHit is one transcript matched by a search: the session it belongs to,
// its display name, whether that session is archived, a snippet of the matching
// text with the hit marked, and how many times the query occurs in the
// transcript, which is what the results are ranked by.
type SearchHit struct {
	AgentID string
	Name    string
	Deleted bool
	Snippet string
	Rank    int32
}

// SnippetOpen and SnippetClose wrap the matched terms inside a hit's snippet, so
// the caller can find and style the match without re-running the query. They are
// plain text markers chosen not to occur in ordinary transcript prose.
const (
	SnippetOpen  = "\x02"
	SnippetClose = "\x03"
)

// The shape of a search hit's snippet: how much text to keep before the match so
// it reads in context, and how much of the transcript to return in total.
const (
	snippetLead  = 60
	snippetWidth = 200
)

// defaultSearchLimit bounds a search that asks for no limit of its own, so a
// direct caller never pulls the whole corpus by omission.
const defaultSearchLimit = 50

// Store reads and writes transcripts, and searches across them.
type Store struct {
	queries *models.Queries
}

// New builds a transcript store over db, which is either the pool or a
// transaction.
func New(db store.DBTX) *Store {
	return &Store{queries: models.New(db)}
}

// Get returns a session's stored transcript, or a zero Transcript when none has
// been synced yet. No-rows is the zero case, so a first sync sees an offset of 0
// and indexes the whole file. Every other error is surfaced.
func (s *Store) Get(ctx context.Context, agentID string) (Transcript, error) {
	if !store.IsID(agentID) {
		return Transcript{}, nil
	}

	row, err := s.queries.GetTranscript(ctx, agentID)
	if store.NoRows(err) {
		return Transcript{}, nil
	}
	if err != nil {
		return Transcript{}, fmt.Errorf("failed to get transcript for %q: %w", agentID, err)
	}

	return Transcript{Content: row.Content, SyncedOffset: row.SyncedOffset}, nil
}

// UpsertParams is one transcript write. It carries the agent the text belongs to,
// the flattened text, and how far into the source log it was read from.
type UpsertParams struct {
	AgentID string
	Content string
	Offset  int64
}

// Upsert writes a session's transcript. The search index is a trigram index on
// the column itself, so Postgres keeps it in step with the write and there is
// nothing separate to reindex.
func (s *Store) Upsert(ctx context.Context, params UpsertParams) error {
	if err := s.queries.UpsertTranscript(ctx, models.UpsertTranscriptParams{
		AgentID:      params.AgentID,
		Content:      params.Content,
		SyncedOffset: params.Offset,
	}); err != nil {
		return fmt.Errorf("failed to upsert transcript for %q: %w", params.AgentID, err)
	}

	return nil
}

// Delete removes the transcript belonging to a session. It runs alongside the
// session's own delete, in the same transaction, since text left behind by a
// removed agent stays readable through the SQL tools.
func (s *Store) Delete(ctx context.Context, agentID string) error {
	if err := s.queries.DeleteAgentTranscript(ctx, agentID); err != nil {
		return fmt.Errorf("failed to delete transcript for %q: %w", agentID, err)
	}

	return nil
}

// Cursor locates a hit within one query's results. Hits sort by how many times
// the query occurs, which two transcripts can tie on, so the session id breaks
// the tie. The rank only means anything for the query that produced it, so a
// cursor is only ever replayed against that same query.
type Cursor struct {
	Rank    int32  `json:"r"`
	AgentID string `json:"i"`
}

// SearchParams is one page of a transcript search: the text to match, how many
// hits to return, and where in the previous page to resume.
type SearchParams struct {
	Query  string
	Limit  int64
	Cursor *Cursor
}

// Search returns one page of the transcripts containing the query as a
// substring, the transcripts that mention it most first, resuming after the
// cursor when one is given. A blank query returns no hits rather than every
// transcript. The database returns a plain window of the text around each match
// and the markers are added here, so what wraps a hit is decided in one place
// rather than in SQL.
func (s *Store) Search(ctx context.Context, params SearchParams) ([]SearchHit, error) {
	// Postgres refuses a NUL in a text value outright, so a query carrying one
	// would fail the request rather than simply match nothing. Dropping it makes
	// the search find nothing, which is what a search for an unmatchable byte
	// should do: no transcript can contain one either.
	query := strings.ReplaceAll(params.Query, "\x00", "")
	if strings.TrimSpace(query) == "" {
		return nil, nil
	}

	limit := params.Limit
	if limit <= 0 {
		limit = defaultSearchLimit
	}

	args := models.SearchTranscriptsParams{
		Query:   query,
		Pattern: likePattern(query),
		Lead:    snippetLead,
		Width:   snippetWidth,
		Lim:     int32(limit),
	}
	if params.Cursor != nil {
		args.CursorRank = &params.Cursor.Rank
		args.CursorAgentID = &params.Cursor.AgentID
	}

	rows, err := s.queries.SearchTranscripts(ctx, args)
	if err != nil {
		return nil, fmt.Errorf("failed to search transcripts for %q: %w", query, err)
	}

	hits := make([]SearchHit, 0, len(rows))
	for _, row := range rows {
		hits = append(hits, SearchHit{
			AgentID: row.AgentID,
			Name:    row.Name,
			Deleted: row.IsDeleted,
			Snippet: markMatches(row.Snippet, query),
			Rank:    row.Rank,
		})
	}

	return hits, nil
}

// likePattern turns a user query into an ILIKE pattern matching it as a literal
// substring. The wildcards and the escape character are the three things LIKE
// reads specially, so a query containing one is escaped rather than allowed to
// widen its own search.
func likePattern(query string) string {
	escaped := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(query)
	return "%" + escaped + "%"
}

// markMatches wraps every case-insensitive occurrence of query inside a snippet
// with the markers, which is what lets the caller style the match without
// searching the text again. The window the database returns is centred on a
// match, so there is normally at least one. A window that got cut mid-match is
// returned unchanged rather than marked wrongly.
func markMatches(snippet, query string) string {
	if query == "" {
		return snippet
	}

	var (
		out       strings.Builder
		lowerHay  = strings.ToLower(snippet)
		lowerNeed = strings.ToLower(query)
		at        int
	)
	for {
		i := strings.Index(lowerHay[at:], lowerNeed)
		if i < 0 {
			out.WriteString(snippet[at:])
			return out.String()
		}

		start := at + i
		end := start + len(query)
		out.WriteString(snippet[at:start])
		out.WriteString(SnippetOpen)
		out.WriteString(snippet[start:end])
		out.WriteString(SnippetClose)
		at = end
	}
}
