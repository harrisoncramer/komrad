// Package transcript stores each agent's conversation as searchable text, appended as the sync
// loop reads new turns out of the CLI's own log.
package transcript

import (
	"context"
	"fmt"
	"strings"
	"time"
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
	// SyncedAt moves only when the log had new bytes, so it is when the agent last wrote
	// something rather than when weave last looked.
	SyncedAt time.Time
}

type SearchHit struct {
	AgentID       string
	Name          string
	Archived      bool
	Seq           int32
	Role          string
	ContentOffset int64
	ContentLength int64
	Snippet       string
	Rank          float64
}

const (
	SnippetOpen  = "\x02"
	SnippetClose = "\x03"
)

const (
	snippetLead     = 60
	snippetWidth    = 200
	headlineOptions = "StartSel=" + SnippetOpen + ",StopSel=" + SnippetClose +
		",MaxWords=35,MinWords=15,MaxFragments=1"
)

const defaultSearchLimit = 50

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

	return Transcript{
		Content:      row.Content,
		SyncedOffset: row.SyncedOffset,
		SyncedAt:     row.SyncedAt,
	}, nil
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

// Position locates one hit within its own section.
type Position struct {
	Rank    float64 `json:"r"`
	AgentID string  `json:"i"`
	Seq     int32   `json:"s"`
}

// Validate refuses a position holding anything but an agent id.
func (p Position) Validate() error {
	if !store.IsID(p.AgentID) {
		return fmt.Errorf("cursor agent id %q is not an id", p.AgentID)
	}

	return nil
}

// Cursor walks both sections at once, holding nothing for a section that has run out.
type Cursor struct {
	Matches *Position `json:"m,omitempty"`
	Near    *Position `json:"n,omitempty"`
}

// ContentCursor locates where in one transcript's content the next window starts.
type ContentCursor struct {
	Offset int64 `json:"o"`
}

// Validate refuses an offset this server would never have handed out.
func (c ContentCursor) Validate() error {
	if c.Offset < 0 {
		return fmt.Errorf("cursor offset is negative")
	}

	return nil
}

// Validate refuses a cursor whose sections are not both positions this server could have
// handed out.
func (c Cursor) Validate() error {
	// A cursor locating neither section is one this server never handed out.
	if c.Matches == nil && c.Near == nil {
		return fmt.Errorf("cursor locates neither section")
	}

	for _, position := range []*Position{c.Matches, c.Near} {
		if position == nil {
			continue
		}
		if err := position.Validate(); err != nil {
			return err
		}
	}

	return nil
}

type SearchParams struct {
	Query   string
	Limit   int64
	Cursor  *Cursor
	AgentID *string
}

// Results is one page of each section, the turns that say the words and the turns that only
// carry the letters, kept apart because the two are scored by measures that do not compare.
type Results struct {
	Matches []SearchHit
	Near    []SearchHit

	// Next locates the page after this one, holding a position only for a section that filled
	// this page, and nil once neither has more to give.
	Next *Cursor
}

// Search returns one page of each section, most relevant first within each, where a word
// match arrives already marked by the database, a near match is marked here, and a query with
// no word to look for is answered with nothing.
func (s *Store) Search(ctx context.Context, params SearchParams) (Results, error) {
	query := strings.ReplaceAll(params.Query, "\x00", "")

	terms := splitTerms(query)
	if len(terms.positive) == 0 {
		return Results{}, nil
	}

	if params.AgentID != nil && !store.IsID(*params.AgentID) {
		return Results{}, nil
	}

	limit := params.Limit
	if limit <= 0 {
		limit = defaultSearchLimit
	}

	// A first page has no cursor at all, while a later page holds one only for the sections
	// still running, so an exhausted section is asked for nothing rather than restarted.
	first := params.Cursor == nil
	var matchesAt, nearAt *Position
	if !first {
		matchesAt, nearAt = params.Cursor.Matches, params.Cursor.Near
	}

	var results Results

	if first || matchesAt != nil {
		rows, err := s.queries.SearchTranscripts(ctx, models.SearchTranscriptsParams{
			Terms:         terms.positive,
			Excluded:      terms.negative,
			Headline:      headlineOptions,
			Lim:           int32(limit),
			AgentID:       params.AgentID,
			CursorRank:    rankOf(matchesAt),
			CursorAgentID: agentOf(matchesAt),
			CursorSeq:     seqOf(matchesAt),
		})
		if err != nil {
			return Results{}, fmt.Errorf("failed to search transcripts for %q: %w", query, err)
		}

		results.Matches = xslice.Map(rows, func(row *models.SearchTranscriptsRow) SearchHit {
			return SearchHit{
				AgentID:       row.AgentID,
				Name:          row.Name,
				Archived:      row.Archived,
				Seq:           row.Seq,
				Role:          row.Role,
				ContentOffset: row.ContentOffset,
				ContentLength: int64(row.ContentLength),
				Snippet:       row.Snippet,
				Rank:          row.Rank,
			}
		})
	}

	if first || nearAt != nil {
		rows, err := s.queries.SearchTranscriptsNear(ctx, models.SearchTranscriptsNearParams{
			Terms:         terms.positive,
			Patterns:      likePatterns(terms.positive),
			Excluded:      terms.negative,
			Lead:          snippetLead,
			Width:         snippetWidth,
			Lim:           int32(limit),
			AgentID:       params.AgentID,
			CursorRank:    rankOf(nearAt),
			CursorAgentID: agentOf(nearAt),
			CursorSeq:     seqOf(nearAt),
		})
		if err != nil {
			return Results{}, fmt.Errorf("failed to search transcripts near %q: %w", query, err)
		}

		results.Near = xslice.Map(rows, func(row *models.SearchTranscriptsNearRow) SearchHit {
			return SearchHit{
				AgentID:       row.AgentID,
				Name:          row.Name,
				Archived:      row.Archived,
				Seq:           row.Seq,
				Role:          row.Role,
				ContentOffset: row.ContentOffset,
				ContentLength: int64(row.ContentLength),
				Snippet:       markMatches(row.Snippet, terms.positive),
				Rank:          row.Rank,
			}
		})
	}

	results.Next = nextCursor(limit, results)

	return results, nil
}

// nextCursor keeps a position for each section that filled its page, so a section that has
// run out is not asked for again while the other one is still going.
func nextCursor(limit int64, results Results) *Cursor {
	cursor := Cursor{
		Matches: lastOf(limit, results.Matches),
		Near:    lastOf(limit, results.Near),
	}
	if cursor.Matches == nil && cursor.Near == nil {
		return nil
	}

	return &cursor
}

func lastOf(limit int64, hits []SearchHit) *Position {
	if int64(len(hits)) < limit {
		return nil
	}

	last := hits[len(hits)-1]

	return &Position{
		Rank:    last.Rank,
		AgentID: last.AgentID,
		Seq:     last.Seq,
	}
}

func rankOf(p *Position) *float64 {
	if p == nil {
		return nil
	}

	return &p.Rank
}

func agentOf(p *Position) *string {
	if p == nil {
		return nil
	}

	return &p.AgentID
}

func seqOf(p *Position) *int32 {
	if p == nil {
		return nil
	}

	return &p.Seq
}

type queryTerms struct {
	positive []string
	negative []string
}

// splitTerms reads a query as the words it is looking for and the words it wants left out,
// which the database turns into a tsquery and this side turns into substring patterns, so the
// two agree by construction rather than by both parsing the same syntax the same way.
func splitTerms(query string) queryTerms {
	var (
		terms   queryTerms
		text    strings.Builder
		quoted  bool
		minuses int
	)

	flush := func() {
		term, leading := text.String(), minuses
		text.Reset()
		quoted, minuses = false, 0

		switch {
		case term == "":
		case leading == 1:
			terms.negative = append(terms.negative, term)
		default:
			// Only a single minus excludes, so a flag name stays the word it looks like.
			terms.positive = append(terms.positive, strings.Repeat("-", leading)+term)
		}
	}
	for _, r := range query {
		switch {
		case r == '"':
			quoted = !quoted
		case unicode.IsSpace(r) && !quoted:
			flush()
		case r == '-' && !quoted && text.Len() == 0:
			minuses++
		default:
			text.WriteRune(r)
		}
	}
	flush()

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
