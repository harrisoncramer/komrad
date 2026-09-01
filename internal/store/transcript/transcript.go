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
	"weave/lib/xslice"
)

type Transcript struct {
	Content      string
	SyncedOffset int64
}

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

const (
	SnippetOpen  = "\x02"
	SnippetClose = "\x03"
)

const (
	snippetLead  = 60
	snippetWidth = 200

	headlineOptions = "StartSel=" + SnippetOpen + ",StopSel=" + SnippetClose +
		",MaxWords=35,MinWords=15,MaxFragments=1"
)

const defaultSearchLimit = 50

// substringRankWeight scores a substring hit as a fraction of one mention of the word, which
// is the 0.1 ts_rank_cd gives a single occurrence, so the two kinds of hit sort among each
// other rather than every substring hit landing behind every word match.
const substringRankWeight = 0.1

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

type UpsertParams struct {
	AgentID string
	Content string
	Offset  int64
}

// Upsert writes an agent's transcript, the whole copy the panel previews, while the turns
// search matches are written by AppendTurns.
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

type Turn struct {
	Role    string
	Content string
	Offset  int64
}

// AppendTurns indexes newly-synced turns for an agent, numbering them after those already
// stored, with Postgres building the search index as part of the write.
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

// DeleteTurns drops every indexed turn for an agent, which the syncer calls once a log has
// been rewritten shorter than what was already read.
func (s *Store) DeleteTurns(ctx context.Context, agentID string) error {
	if err := s.queries.DeleteAgentTranscriptChunks(ctx, agentID); err != nil {
		return fmt.Errorf("failed to delete transcript chunks for %q: %w", agentID, err)
	}

	return nil
}

// Delete removes the transcripts belonging to agents, both the whole copies and the indexed
// turns, in the same transaction as the agents' own delete.
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

type Cursor struct {
	Rank    float64 `json:"r"`
	AgentID string  `json:"i"`
	Seq     int32   `json:"s"`
}

// Validate refuses a cursor holding anything but an agent id, since a rank or a sequence out
// of place only narrows the page.
func (c Cursor) Validate() error {
	if !store.IsID(c.AgentID) {
		return fmt.Errorf("cursor agent id %q is not an id", c.AgentID)
	}

	return nil
}

type SearchParams struct {
	Query   string
	Limit   int64
	Cursor  *Cursor
	AgentID *string
}

// Search returns one page of the turns matching the query, most relevant first, where a word
// match arrives already marked by the database, a substring match is marked here, and a query
// asking only for words to leave out is answered with nothing rather than with every turn it
// technically matches.
func (s *Store) Search(ctx context.Context, params SearchParams) ([]SearchHit, error) {
	query := strings.ReplaceAll(params.Query, "\x00", "")

	terms := splitTerms(query)
	if len(terms.positive) == 0 {
		return nil, nil
	}

	if params.AgentID != nil && !store.IsID(*params.AgentID) {
		return nil, nil
	}

	limit := params.Limit
	if limit <= 0 {
		limit = defaultSearchLimit
	}

	args := models.SearchTranscriptsParams{
		Query:           query,
		Terms:           terms.positive,
		SubstringWeight: substringRankWeight,
		Patterns:        likePatterns(terms.positive),
		Excluded:        likePatterns(terms.negative),
		Headline:        headlineOptions,
		Lead:            snippetLead,
		Width:           snippetWidth,
		Lim:             int32(limit),
		AgentID:         params.AgentID,
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

	hits := xslice.Map(rows, func(row *models.SearchTranscriptsRow) SearchHit {
		snippet := row.Snippet
		if !row.WordMatch {
			snippet = markMatches(snippet, terms.positive)
		}

		return SearchHit{
			AgentID:       row.AgentID,
			Name:          row.Name,
			Archived:      row.Archived,
			Seq:           row.Seq,
			Role:          row.Role,
			ContentOffset: row.ContentOffset,
			Snippet:       snippet,
			Rank:          row.Rank,
		}
	})

	return hits, nil
}

type queryTerms struct {
	positive []string
	negative []string
}

// splitTerms reads the words a query is asking for out of its websearch syntax, since the
// substring fallback matches text as it was typed and would otherwise look for the operators
// themselves.
func splitTerms(query string) queryTerms {
	var (
		terms  queryTerms
		tokens []string
		token  strings.Builder
		quoted bool
	)

	flush := func() {
		if token.Len() > 0 {
			tokens = append(tokens, token.String())
			token.Reset()
		}
	}
	for _, r := range query {
		switch {
		case r == '"':
			quoted = !quoted
		case unicode.IsSpace(r) && !quoted:
			flush()
		default:
			token.WriteRune(r)
		}
	}
	flush()

	for _, tok := range tokens {
		if strings.EqualFold(tok, "or") {
			continue
		}

		term, negated := strings.CutPrefix(tok, "-")
		if term == "" {
			continue
		}
		if negated {
			terms.negative = append(terms.negative, term)
			continue
		}

		terms.positive = append(terms.positive, term)
	}

	return terms
}

func likePatterns(terms []string) []string {
	patterns := make([]string, 0, len(terms))
	for _, term := range terms {
		patterns = append(patterns, likePattern(term))
	}

	return patterns
}

func likePattern(query string) string {
	escaped := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(query)
	return "%" + escaped + "%"
}

func markMatches(snippet string, terms []string) string {
	var (
		out strings.Builder
		at  int
	)
	for at < len(snippet) {
		start, end, ok := firstMatch(snippet[at:], terms)
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

// firstMatch returns the earliest occurrence of any term, preferring the longest where two
// start at the same place, so overlapping terms are marked once rather than nested.
func firstMatch(hay string, terms []string) (int, int, bool) {
	found := false

	var bestStart, bestEnd int
	for _, term := range terms {
		if term == "" {
			continue
		}

		start, end, ok := foldIndex(hay, term)
		if !ok {
			continue
		}
		if !found || start < bestStart || (start == bestStart && end > bestEnd) {
			found, bestStart, bestEnd = true, start, end
		}
	}

	return bestStart, bestEnd, found
}

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
