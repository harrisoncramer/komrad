// Package transcript stores each agent's conversation as searchable text, appended as the sync
// loop reads new turns out of the CLI's own log.
package transcript

import (
	"context"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"komrad/db"
	"komrad/db/models"
	"komrad/internal/store"
	"komrad/lib/xslice"
)

// Session is one conversation an agent has held.
type Session struct {
	ID           string
	SyncedOffset int64
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

const minNearTermRunes = 3

type TranscriptStore struct {
	queries *models.Queries
}

// NewTranscriptStore builds a transcript store over db, either the pool or a transaction.
func NewTranscriptStore(db store.DBTX) *TranscriptStore {
	return &TranscriptStore{queries: models.New(db)}
}

// Get assembles an agent's whole conversation from its indexed turns.
func (s *TranscriptStore) Get(ctx context.Context, agentID string) (string, error) {
	if !store.IsID(agentID) {
		return "", nil
	}

	content, err := s.queries.GetTranscript(ctx, agentID)
	if db.IsNoRows(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("failed to get transcript for %q: %w", agentID, err)
	}

	return content, nil
}

type StoredTurn struct {
	Seq     int32
	Role    string
	Content string
}

type ListLastTurnsParams struct {
	AgentID string
	Role    string
	Limit   int32
}

// ListLastTurns returns the newest turns spoken in one role and every turn between them, oldest
// first.
func (s *TranscriptStore) ListLastTurns(ctx context.Context, params ListLastTurnsParams) ([]StoredTurn, error) {
	if !store.IsID(params.AgentID) {
		return nil, nil
	}

	rows, err := s.queries.ListLastTranscriptTurns(ctx, models.ListLastTranscriptTurnsParams{
		AgentID: params.AgentID,
		Role:    params.Role,
		Lim:     params.Limit,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list the last turns of %q: %w", params.AgentID, err)
	}

	return xslice.Map(rows, func(row *models.ListLastTranscriptTurnsRow) StoredTurn {
		return StoredTurn{
			Seq:     row.Seq,
			Role:    row.Role,
			Content: row.Content,
		}
	}), nil
}

type OpenSessionParams struct {
	AgentID   string
	SessionID string
}

// OpenSession returns the session an agent's current conversation is copied under.
func (s *TranscriptStore) OpenSession(ctx context.Context, params OpenSessionParams) (Session, error) {
	id, err := store.NewID()
	if err != nil {
		return Session{}, err
	}

	row, err := s.queries.OpenTranscriptSession(ctx, models.OpenTranscriptSessionParams{
		ID:        id,
		AgentID:   params.AgentID,
		SessionID: params.SessionID,
	})
	if err != nil {
		return Session{}, fmt.Errorf("failed to open a transcript session for %q: %w", params.AgentID, err)
	}

	return Session{ID: row.ID, SyncedOffset: row.SyncedOffset}, nil
}

// Advance records how far a session's log has been read.
func (s *TranscriptStore) Advance(ctx context.Context, sessionID string, offset int64) error {
	if err := s.queries.AdvanceTranscriptSession(ctx, models.AdvanceTranscriptSessionParams{
		ID:           sessionID,
		SyncedOffset: offset,
	}); err != nil {
		return fmt.Errorf("failed to advance transcript session %q: %w", sessionID, err)
	}

	return nil
}

type Turn struct {
	Role    string
	Content string
	Offset  int64
}

type AppendTurnsParams struct {
	AgentID   string
	SessionID string
	Turns     []Turn
}

// turnSeparator is the blank line the assembled conversation puts between two turns.
const turnSeparator = "\n\n"

// AppendTurns indexes newly-synced turns for an agent, numbering them after those already
// stored.
func (s *TranscriptStore) AppendTurns(ctx context.Context, params AppendTurnsParams) error {
	if len(params.Turns) == 0 {
		return nil
	}

	next, err := s.queries.NextTranscriptChunk(ctx, params.AgentID)
	if err != nil {
		return fmt.Errorf("failed to read the next transcript chunk for %q: %w", params.AgentID, err)
	}

	chunks := models.InsertTranscriptChunksParams{AgentID: params.AgentID, SessionID: params.SessionID}
	for i, turn := range placeTurns(next.ContentLength, next.NextSeq == 0, params.Turns) {
		chunks.Seqs = append(chunks.Seqs, next.NextSeq+int32(i))
		chunks.Roles = append(chunks.Roles, turn.Role)
		chunks.Contents = append(chunks.Contents, turn.Content)
		chunks.ContentOffsets = append(chunks.ContentOffsets, turn.Offset)
	}

	if err := s.queries.InsertTranscriptChunks(ctx, chunks); err != nil {
		return fmt.Errorf("failed to insert transcript chunks for %q: %w", params.AgentID, err)
	}

	return nil
}

// placeTurns numbers each turn by where it starts in the assembled conversation.
func placeTurns(length int64, first bool, turns []Turn) []Turn {
	placed := make([]Turn, 0, len(turns))
	for i, turn := range turns {
		if !first || i > 0 {
			length += int64(len(turnSeparator))
		}

		turn.Offset = length
		placed = append(placed, turn)
		length += int64(len(turn.Content))
	}

	return placed
}

// DeleteSessionTurns drops the turns indexed under one session.
func (s *TranscriptStore) DeleteSessionTurns(ctx context.Context, sessionID string) error {
	if err := s.queries.DeleteTranscriptSessionChunks(ctx, sessionID); err != nil {
		return fmt.Errorf("failed to delete transcript chunks for session %q: %w", sessionID, err)
	}

	return nil
}

// Delete removes the conversations belonging to agents.
func (s *TranscriptStore) Delete(ctx context.Context, agentIDs []string) error {
	if len(agentIDs) == 0 {
		return nil
	}

	if err := s.queries.DeleteAgentsTranscriptSessions(ctx, agentIDs); err != nil {
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
// carry the letters.
type Results struct {
	Matches []SearchHit
	Near    []SearchHit

	// Next locates the page after this one, holding a position only for a section that filled
	// this page, and nil once neither has any more.
	Next *Cursor
}

// Search returns one page of each section, most relevant first within each, answering a query
// with no word to look for with nothing.
func (s *TranscriptStore) Search(ctx context.Context, params SearchParams) (Results, error) {
	query := strings.ReplaceAll(params.Query, "\x00", "")

	terms := splitTerms(query)
	if len(terms.wanted) == 0 {
		return Results{}, nil
	}

	wanted, excluded := quoteTerms(terms.wanted, ""), quoteTerms(terms.excluded, "-")

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

	matchesRan := first || matchesAt != nil
	if matchesRan {
		rows, err := s.queries.SearchTranscripts(ctx, models.SearchTranscriptsParams{
			Wanted:        wanted,
			Excluded:      excluded,
			Phrase:        phrasePattern(terms.wanted),
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

	nearStarts := matchesRan && int64(len(results.Matches)) < limit && hasNearTerms(terms.wanted)
	if nearAt != nil || nearStarts {
		rows, err := s.queries.SearchTranscriptsNear(ctx, models.SearchTranscriptsNearParams{
			Terms:         terms.wanted,
			Patterns:      likePatterns(terms.wanted),
			Wanted:        wanted,
			Excluded:      excluded,
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
				Snippet:       markMatches(row.Snippet, terms.wanted),
				Rank:          row.Rank,
			}
		})
	}

	results.Next = nextCursor(limit, results)

	return results, nil
}

// nextCursor keeps a position for each section that filled its page.
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
	wanted   []string
	excluded []string
}

// splitTerms reads a query as the words it is looking for, quoted or not, and the words it
// wants left out.
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
			terms.excluded = append(terms.excluded, term)
		default:
			// Only a single minus excludes, so a flag name stays the word it looks like.
			terms.wanted = append(terms.wanted, strings.Repeat("-", leading)+term)
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

func quoteTerms(terms []string, prefix string) string {
	quoted := xslice.Map(terms, func(term string) string {
		return prefix + `"` + term + `"`
	})

	return strings.Join(quoted, " ")
}

func hasNearTerms(terms []string) bool {
	for _, term := range terms {
		if utf8.RuneCountInString(term) < minNearTermRunes {
			return false
		}
	}

	return true
}

func phrasePattern(terms []string) *string {
	var words []string
	for _, term := range terms {
		words = append(words, strings.FieldsFunc(term, func(r rune) bool {
			return !unicode.IsLetter(r) && !unicode.IsDigit(r)
		})...)
	}
	if len(words) == 0 {
		return nil
	}

	pattern := `\m` + strings.Join(words, `[^[:alnum:]]+`) + `\M`

	return &pattern
}

func likePatterns(terms []string) []string {
	patterns := xslice.Map(terms, likePattern)

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
// start at the same place.
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
