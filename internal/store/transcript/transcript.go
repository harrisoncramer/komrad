// Package transcript stores each agent's conversation as searchable text, appended as the sync
// loop reads new turns out of the CLI's own log.
package transcript

import (
	"context"
	"fmt"
	"strings"
	"time"

	"komrad/db"
	"komrad/db/models"
	"komrad/internal/fail"
	"komrad/internal/store"
	textsearchstore "komrad/internal/store/textsearch"
	"komrad/lib/xslice"
)

// session is one conversation an agent has held.
type session struct {
	ID           string
	SyncedOffset int64
	OpenedAt     time.Time
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
	SpokenAt      *time.Time
	Key           float64
}

const defaultSearchLimit = 50

const unknownTimeKey = -1e18

// errSortChanged is a cursor passed back under a sort other than the one it was handed out for.
var errSortChanged = fail.InvalidArgument("the cursor was handed out under another sort; search again from the first page")

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

type storedTurn struct {
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
func (s *TranscriptStore) ListLastTurns(ctx context.Context, params ListLastTurnsParams) ([]storedTurn, error) {
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

	return xslice.Map(rows, func(row *models.ListLastTranscriptTurnsRow) storedTurn {
		return storedTurn{
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
func (s *TranscriptStore) OpenSession(ctx context.Context, params OpenSessionParams) (session, error) {
	id, err := store.NewID()
	if err != nil {
		return session{}, err
	}

	row, err := s.queries.OpenTranscriptSession(ctx, models.OpenTranscriptSessionParams{
		ID:        id,
		AgentID:   params.AgentID,
		SessionID: params.SessionID,
	})
	if err != nil {
		return session{}, fmt.Errorf("failed to open a transcript session for %q: %w", params.AgentID, err)
	}

	return session{
		ID:           row.ID,
		SyncedOffset: row.SyncedOffset,
		OpenedAt:     row.OpenedAt,
	}, nil
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

// MarkDrained stamps a session as read to the end as of readAt.
func (s *TranscriptStore) MarkDrained(ctx context.Context, sessionID string, readAt time.Time) error {
	if err := s.queries.MarkTranscriptSessionDrained(ctx, models.MarkTranscriptSessionDrainedParams{
		ID:       sessionID,
		SyncedAt: readAt,
	}); err != nil {
		return fmt.Errorf("failed to mark transcript session %q drained: %w", sessionID, err)
	}

	return nil
}

type Turn struct {
	Role     string
	Content  string
	Offset   int64
	SpokenAt time.Time
}

// turnSeparator is the blank line the assembled conversation puts between two turns.
const turnSeparator = "\n\n"

type AppendTurnsParams struct {
	AgentID   string
	SessionID string
	Turns     []Turn
}

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
		chunks.SpokenAts = append(chunks.SpokenAts, turn.SpokenAt)
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

// sortOrder is the order a search hands its hits back in.
type sortOrder string

const (
	SortBest   sortOrder = "best"
	sortNewest sortOrder = "new"
)

// Validate refuses a sort this server does not know.
func (s sortOrder) Validate() error {
	switch s {
	case SortBest, sortNewest:
		return nil
	}

	return fmt.Errorf("sort %q is not one of best or new", string(s))
}

// Position locates one hit within its own section.
type Position struct {
	Key     float64 `json:"k"`
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
	Sort    sortOrder `json:"o"`
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
	if err := c.Sort.Validate(); err != nil {
		return err
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

// Filter narrows a search to the turns and agents it names.
type Filter struct {
	Speaker       *string
	AgentName     *string
	WorkflowName  *string
	Kinds         []string
	ExcludedKinds []string
	Since         *time.Time
	Until         *time.Time
}

// IsZero reports whether the filter narrows nothing.
func (f Filter) IsZero() bool {
	return f.Speaker == nil && f.AgentName == nil && f.WorkflowName == nil &&
		len(f.Kinds) == 0 && len(f.ExcludedKinds) == 0 &&
		f.Since == nil && f.Until == nil
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

type SearchParams struct {
	Query   string
	Limit   int64
	Cursor  *Cursor
	AgentID *string
	Filter  Filter
}

// Search returns one page of each section, newest first when it has no words and best first otherwise.
func (s *TranscriptStore) Search(ctx context.Context, params SearchParams) (Results, error) {
	query := strings.ReplaceAll(params.Query, "\x00", "")

	terms := textsearchstore.Split(query)
	browse := len(terms.Wanted) == 0
	if browse && params.Filter.IsZero() {
		return Results{}, nil
	}

	sort := SortBest
	if browse {
		sort = sortNewest
	}
	if params.Cursor != nil && params.Cursor.Sort != sort {
		return Results{}, errSortChanged.Saying("the cursor was handed out for sort %q, not %q; search again from the first page", params.Cursor.Sort, sort)
	}

	wanted, excluded := textsearchstore.Quote(terms.Wanted, ""), textsearchstore.Quote(terms.Excluded, "-")

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
	filter := params.Filter

	matchesRan := first || matchesAt != nil
	if matchesRan {
		rows, err := s.queries.SearchTranscripts(ctx, models.SearchTranscriptsParams{
			Wanted:        wanted,
			Excluded:      excluded,
			Phrase:        textsearchstore.Phrase(terms.Wanted),
			Headline:      textsearchstore.Headline,
			Width:         textsearchstore.Width,
			Sort:          string(sort),
			UnknownKey:    unknownTimeKey,
			Browse:        browse,
			Lim:           int32(limit),
			AgentID:       params.AgentID,
			Speaker:       filter.Speaker,
			AgentName:     toContainsPattern(filter.AgentName),
			WorkflowName:  toContainsPattern(filter.WorkflowName),
			Kinds:         orEmpty(filter.Kinds),
			ExcludedKinds: orEmpty(filter.ExcludedKinds),
			Since:         filter.Since,
			Until:         filter.Until,
			CursorKey:     keyOf(matchesAt),
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
				Rank:          toRank(sort, row.SortKey),
				SpokenAt:      row.SpokenAt,
				Key:           row.SortKey,
			}
		})
	}

	nearStarts := matchesRan && !browse && int64(len(results.Matches)) < limit && textsearchstore.HasNear(terms.Wanted)
	if nearAt != nil || nearStarts {
		rows, err := s.queries.SearchTranscriptsNear(ctx, models.SearchTranscriptsNearParams{
			Terms:         terms.Wanted,
			Patterns:      textsearchstore.LikePatterns(terms.Wanted),
			Wanted:        wanted,
			Excluded:      excluded,
			Lead:          textsearchstore.Lead,
			Width:         textsearchstore.Width,
			Sort:          string(sort),
			UnknownKey:    unknownTimeKey,
			Lim:           int32(limit),
			AgentID:       params.AgentID,
			Speaker:       filter.Speaker,
			AgentName:     toContainsPattern(filter.AgentName),
			WorkflowName:  toContainsPattern(filter.WorkflowName),
			Kinds:         orEmpty(filter.Kinds),
			ExcludedKinds: orEmpty(filter.ExcludedKinds),
			Since:         filter.Since,
			Until:         filter.Until,
			CursorKey:     keyOf(nearAt),
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
				Snippet:       textsearchstore.Mark(row.Snippet, terms.Wanted),
				Rank:          toRank(sort, row.SortKey),
				SpokenAt:      row.SpokenAt,
				Key:           row.SortKey,
			}
		})
	}

	results.Next = nextCursor(limit, sort, results)

	return results, nil
}

// nextCursor keeps a position for each section that filled its page.
func nextCursor(limit int64, sort sortOrder, results Results) *Cursor {
	cursor := Cursor{
		Sort:    sort,
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
		Key:     last.Key,
		AgentID: last.AgentID,
		Seq:     last.Seq,
	}
}

func keyOf(p *Position) *float64 {
	if p == nil {
		return nil
	}

	return &p.Key
}

func toRank(sort sortOrder, key float64) float64 {
	if sort != SortBest {
		return 0
	}

	return key
}

func toContainsPattern(text *string) *string {
	if text == nil {
		return nil
	}

	pattern := textsearchstore.LikePattern(*text)

	return &pattern
}

func orEmpty(values []string) []string {
	if values == nil {
		return []string{}
	}

	return values
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
