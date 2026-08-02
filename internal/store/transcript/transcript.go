// Package transcript persists the flattened text of each agent and searches it. A
// transcript is keyed by its agent's id, so removing an agent has to remove its transcript
// too, which the caller composes inside one transaction.
package transcript

import (
	"context"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"weave/db"
	"weave/db/models"
	"weave/internal/store"
)

// Transcript is an agent's stored transcript, the flattened text plus how far into
// the source JSONL the sync has consumed.
type Transcript struct {
	Content      string
	SyncedOffset int64
}

// SearchHit is one turn matched by a search. ContentOffset is where that turn begins in
// the full transcript, so the caller can scroll to it, and Rank is what results order by.
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

// SnippetOpen and SnippetClose wrap the matched terms inside a hit's snippet, so the
// caller can style the match without re-running the query. They are chosen not to occur in
// ordinary transcript prose.
const (
	SnippetOpen  = "\x02"
	SnippetClose = "\x03"
)

// The shape of a search hit's snippet. Lead and width cut the plain window a
// substring-only match falls back to, and headlineOptions shapes the window a word match
// gets, in the form ts_headline parses. Both carry the same markers.
const (
	snippetLead  = 60
	snippetWidth = 200

	headlineOptions = "StartSel=" + SnippetOpen + ",StopSel=" + SnippetClose +
		",MaxWords=35,MinWords=15,MaxFragments=1"
)

// defaultSearchLimit bounds a search that asks for no limit of its own, so a caller never
// pulls the whole corpus by omission.
const defaultSearchLimit = 50

// Store reads and writes transcripts, and searches across them.
type Store struct {
	queries *models.Queries
}

// New builds a transcript store over db, either the pool or a transaction.
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
	if db.IsNoRows(err) {
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

// Upsert writes an agent's transcript, the whole copy the panel previews. What search
// matches is the turns, written separately by AppendTurns.
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

// Turn is one turn of a transcript as the syncer hands it over. Offset is where the turn
// begins in the transcript it was appended to, which is what a hit scrolls to.
type Turn struct {
	Role    string
	Content string
	Offset  int64
}

// AppendTurns indexes newly-synced turns for an agent, numbering them after those already
// stored. The search index lives on a generated column, so Postgres builds it as part of
// this write and there is nothing separate to reindex.
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

// DeleteTurns drops every indexed turn for an agent. The syncer calls this when a log has
// been rewritten shorter than what was read, so the stored turns no longer correspond to
// the file and the whole transcript is read again.
func (s *Store) DeleteTurns(ctx context.Context, agentID string) error {
	if err := s.queries.DeleteAgentTranscriptChunks(ctx, agentID); err != nil {
		return fmt.Errorf("failed to delete transcript chunks for %q: %w", agentID, err)
	}

	return nil
}

// Delete removes the transcripts belonging to agents, both the whole copies and the
// indexed turns. It runs alongside the agents' own delete, in the same transaction.
func (s *Store) Delete(ctx context.Context, agentIDs []string) error {
	if len(agentIDs) == 0 {
		return nil
	}

	if err := s.queries.DeleteAgentTranscripts(ctx, agentIDs); err != nil {
		return fmt.Errorf("failed to delete the transcripts of %d agents: %w", len(agentIDs), err)
	}
	if err := s.queries.DeleteAgentsTranscriptChunks(ctx, agentIDs); err != nil {
		return fmt.Errorf("failed to delete the transcript chunks of %d agents: %w", len(agentIDs), err)
	}

	return nil
}

// Cursor locates a hit within one query's results. Hits sort by relevance, which two turns
// can tie on, so the agent id and the turn's position break the tie. The rank means
// nothing outside the query that produced it, so a cursor is only replayed against it.
type Cursor struct {
	Rank    float64 `json:"r"`
	AgentID string  `json:"i"`
	Seq     int32   `json:"s"`
}

// SearchParams is one page of a transcript search, where AgentID narrows it to one agent's
// conversation and nil searches every one of them.
type SearchParams struct {
	Query   string
	Limit   int64
	Cursor  *Cursor
	AgentID *string
}

// Search returns one page of the turns matching the query, most relevant first, resuming
// after the cursor when one is given. A blank query returns no hits, and one agent can
// appear more than once, since a conversation matches on each turn that did.
//
// A turn the word query matched arrives with its snippet already marked, since only the
// database knows which lexemes it matched. A turn only the substring fallback found ranks
// zero and arrives plain, so its literal match is marked here.
func (s *Store) Search(ctx context.Context, params SearchParams) ([]SearchHit, error) {
	// Postgres refuses a NUL in a text value outright, so a query carrying one would fail
	// the request rather than match nothing.
	query := strings.ReplaceAll(params.Query, "\x00", "")
	if strings.TrimSpace(query) == "" {
		return nil, nil
	}

	// A string that is not an id matches nothing rather than failing on the uuid the
	// column would compare it against.
	if params.AgentID != nil && !store.IsID(*params.AgentID) {
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
		AgentID:  params.AgentID,
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

// likePattern turns a user query into an ILIKE pattern matching it as a literal substring,
// escaping the wildcards so a query carrying one cannot widen its own search.
func likePattern(query string) string {
	escaped := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(query)
	return "%" + escaped + "%"
}

// markMatches wraps every case-insensitive occurrence of query inside a snippet with the
// markers. Only a substring-only hit needs this, since a word match arrives already
// marked.
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

// foldIndex reports where the first case-insensitive occurrence of needle begins and ends
// in hay, as byte offsets into hay itself, and whether there was one.
//
// It compares rune by rune rather than searching a lowercased copy, since lowercasing does
// not preserve byte length and the offsets would index a string the caller does not hold.
// The Kelvin sign is the short example: three bytes that lower to a one-byte k, which
// Postgres folds the same way, so it is a match this side really is handed.
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
