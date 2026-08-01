// Package transcript persists the flattened text of each agent and searches it.
//
// A transcript is keyed by the id of the agent it belongs to, so removing an agent
// has to remove its transcript too. The caller composes both inside one transaction.
package transcript

import (
	"context"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"weave/db/models"
	"weave/internal/store"
)

// Transcript is an agent's stored transcript, the flattened text plus how far into
// the source JSONL the sync has consumed.
type Transcript struct {
	Content      string
	SyncedOffset int64
}

// SearchHit is one turn matched by a search. ContentOffset is where that turn begins
// in the full transcript, so the caller can scroll to it, and Rank is the relevance
// the results are ordered by.
type SearchHit struct {
	AgentID       string
	Name          string
	Archived      bool
	Seq           int32
	Role          string
	ContentOffset int64
	Snippet       string
	Rank          float64
}

// SnippetOpen and SnippetClose wrap the matched terms inside a hit's snippet, so
// the caller can find and style the match without re-running the query. They are
// plain text markers chosen not to occur in ordinary transcript prose.
const (
	SnippetOpen  = "\x02"
	SnippetClose = "\x03"
)

// The shape of a search hit's snippet. Lead and width cut the plain window a
// substring-only match falls back to, and headlineOptions shapes the window a word
// match gets instead, in the comma-separated form ts_headline parses. Both carry the
// same markers, so either kind of snippet reaches the caller alike.
const (
	snippetLead  = 60
	snippetWidth = 200

	headlineOptions = "StartSel=" + SnippetOpen + ",StopSel=" + SnippetClose +
		",MaxWords=35,MinWords=15,MaxFragments=1"
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

// Get returns an agent's stored transcript, or a zero Transcript when none has been
// synced yet, so a first sync sees an offset of 0 and indexes the whole file.
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

// UpsertParams is one transcript write.
type UpsertParams struct {
	AgentID string
	Content string
	Offset  int64
}

// Upsert writes an agent's transcript. This is the copy the panel previews, held
// whole because that is how it is read. What search matches is the turns, written
// separately by AppendTurns.
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

// Turn is one turn of a transcript as the syncer hands it over. Offset is where the
// turn begins in the transcript text it was appended to, which is what lets a hit be
// scrolled to without searching for it again.
type Turn struct {
	Role    string
	Content string
	Offset  int64
}

// AppendTurns indexes newly-synced turns for an agent, numbering them after the
// turns already stored. The search index lives on a generated column, so Postgres
// builds it as part of this write and there is nothing separate to reindex.
func (s *Store) AppendTurns(ctx context.Context, agentID string, turns []Turn) error {
	if len(turns) == 0 {
		return nil
	}

	next, err := s.queries.NextTranscriptChunkSeq(ctx, agentID)
	if err != nil {
		return fmt.Errorf("failed to read the next transcript chunk seq for %q: %w", agentID, err)
	}

	rows := make([]models.InsertTranscriptChunksParams, 0, len(turns))
	for i, turn := range turns {
		rows = append(rows, models.InsertTranscriptChunksParams{
			AgentID:       agentID,
			Seq:           next + int32(i),
			Role:          turn.Role,
			Content:       turn.Content,
			ContentOffset: turn.Offset,
		})
	}

	if _, err := s.queries.InsertTranscriptChunks(ctx, rows); err != nil {
		return fmt.Errorf("failed to insert transcript chunks for %q: %w", agentID, err)
	}

	return nil
}

// DeleteTurns drops every indexed turn for an agent. The syncer calls this when a
// log has been rewritten shorter than what was already read, which means the stored
// turns no longer correspond to the file and the whole transcript is read again.
func (s *Store) DeleteTurns(ctx context.Context, agentID string) error {
	if err := s.queries.DeleteAgentTranscriptChunks(ctx, agentID); err != nil {
		return fmt.Errorf("failed to delete transcript chunks for %q: %w", agentID, err)
	}

	return nil
}

// Delete removes the transcript belonging to an agent, both the whole copy and the
// indexed turns. It runs alongside the agent's own delete, in the same transaction.
func (s *Store) Delete(ctx context.Context, agentID string) error {
	if err := s.queries.DeleteAgentTranscript(ctx, agentID); err != nil {
		return fmt.Errorf("failed to delete transcript for %q: %w", agentID, err)
	}
	if err := s.queries.DeleteAgentTranscriptChunks(ctx, agentID); err != nil {
		return fmt.Errorf("failed to delete transcript chunks for %q: %w", agentID, err)
	}

	return nil
}

// Cursor locates a hit within one query's results. Hits sort by relevance, which
// two turns can tie on, so the agent id and the turn's position break the tie. The
// rank means nothing outside the query that produced it, so a cursor is only ever
// replayed against that same query.
type Cursor struct {
	Rank    float64 `json:"r"`
	AgentID string  `json:"i"`
	Seq     int32   `json:"s"`
}

// SearchParams is one page of a transcript search.
type SearchParams struct {
	Query  string
	Limit  int64
	Cursor *Cursor
}

// Search returns one page of the turns matching the query, most relevant first,
// resuming after the cursor when one is given. A blank query returns no hits rather
// than every turn. One agent can appear more than once, since a conversation that
// came back to something matches on each turn that did.
//
// A turn the word query matched arrives with its snippet already marked, since only
// the database knows which lexemes it matched. A turn only the substring fallback
// found ranks zero and arrives as a plain window, so the literal match is marked
// here.
func (s *Store) Search(ctx context.Context, params SearchParams) ([]SearchHit, error) {
	// Postgres refuses a NUL in a text value outright, so a query carrying one would
	// fail the request rather than match nothing.
	query := strings.ReplaceAll(params.Query, "\x00", "")
	if strings.TrimSpace(query) == "" {
		return nil, nil
	}

	limit := params.Limit
	if limit <= 0 {
		limit = defaultSearchLimit
	}

	args := models.SearchTranscriptsParams{
		Query:    query,
		Headline: headlineOptions,
		Pattern:  likePattern(query),
		Lead:     snippetLead,
		Width:    snippetWidth,
		Lim:      int32(limit),
	}
	if params.Cursor != nil {
		args.CursorRank = &params.Cursor.Rank
		args.CursorAgentID = &params.Cursor.AgentID
		args.CursorSeq = &params.Cursor.Seq
	}

	rows, err := s.queries.SearchTranscripts(ctx, args)
	if err != nil {
		return nil, fmt.Errorf("failed to search transcripts for %q: %w", query, err)
	}

	hits := make([]SearchHit, 0, len(rows))
	for _, row := range rows {
		snippet := row.Snippet
		if row.Rank == 0 {
			snippet = markMatches(snippet, query)
		}

		hits = append(hits, SearchHit{
			AgentID:       row.AgentID,
			Name:          row.Name,
			Archived:      row.Archived,
			Seq:           row.Seq,
			Role:          row.Role,
			ContentOffset: row.ContentOffset,
			Snippet:       snippet,
			Rank:          row.Rank,
		})
	}

	return hits, nil
}

// likePattern turns a user query into an ILIKE pattern matching it as a literal
// substring. The two wildcards and the escape character are all LIKE reads
// specially, so a query containing one is escaped rather than allowed to widen its
// own search.
func likePattern(query string) string {
	escaped := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(query)
	return "%" + escaped + "%"
}

// markMatches wraps every case-insensitive occurrence of query inside a snippet with
// the markers. Only a substring-only hit needs this, since a word match arrives
// already marked. A window cut mid-match is returned unchanged rather than marked
// wrongly.
func markMatches(snippet, query string) string {
	if query == "" {
		return snippet
	}

	var (
		out strings.Builder
		at  int
	)
	for at < len(snippet) {
		start, end, ok := foldIndex(snippet[at:], query)
		if !ok {
			break
		}

		out.WriteString(snippet[at : at+start])
		out.WriteString(SnippetOpen)
		out.WriteString(snippet[at+start : at+end])
		out.WriteString(SnippetClose)
		at += end
	}
	out.WriteString(snippet[at:])

	return out.String()
}

// foldIndex reports where the first case-insensitive occurrence of needle begins and
// ends in hay, as byte offsets into hay itself, and whether there was one.
//
// It compares rune by rune rather than searching a lowercased copy of hay, because
// lowercasing does not preserve byte length and the offsets a search of that copy
// yields therefore index a string the caller does not hold. The Kelvin sign is the
// short example: three bytes that lower to a one-byte k, which Postgres folds the same
// way, so it is a match this side really is handed.
func foldIndex(hay, needle string) (int, int, bool) {
	for start := 0; start < len(hay); {
		_, width := utf8.DecodeRuneInString(hay[start:])

		i, j := start, 0
		for j < len(needle) && i < len(hay) {
			h, hw := utf8.DecodeRuneInString(hay[i:])
			n, nw := utf8.DecodeRuneInString(needle[j:])
			if unicode.ToLower(h) != unicode.ToLower(n) {
				break
			}

			i, j = i+hw, j+nw
		}
		if j == len(needle) {
			return start, i, true
		}

		start += width
	}

	return 0, 0, false
}
