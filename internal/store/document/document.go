// Package document stores the markdown documents agents write for people to read.
package document

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

// ErrUnknownType is a document naming a type that does not exist.
var ErrUnknownType = fail.InvalidArgument("no document type has that name")

// ErrChanged is a rewrite naming a version the document has already moved past.
var ErrChanged = fail.Precondition("the document has been written since that version was read")

// Document is one document, with an empty Body when it came from a listing.
type Document struct {
	ID        string
	AgentID   string
	Name      string
	Type      string
	Body      string
	Version   int32
	CreatedAt time.Time
	UpdatedAt time.Time
}

type DocumentStore struct {
	queries *models.Queries
}

// NewDocumentStore builds a document store over a pool or transaction.
func NewDocumentStore(db store.DBTX) *DocumentStore {
	return &DocumentStore{queries: models.New(db)}
}

func fromRow(row *models.Document) Document {
	return Document{
		ID:        row.ID,
		AgentID:   row.AgentID,
		Name:      row.Name,
		Type:      row.Type,
		Body:      row.Body,
		Version:   row.Version,
		CreatedAt: row.CreatedAt,
		UpdatedAt: row.UpdatedAt,
	}
}

func fromListRow(row *models.ListDocumentsRow) Document {
	return Document{
		ID:        row.ID,
		AgentID:   row.AgentID,
		Name:      row.Name,
		Type:      row.Type,
		Version:   row.Version,
		CreatedAt: row.CreatedAt,
		UpdatedAt: row.UpdatedAt,
	}
}

type CreateParams struct {
	AgentID string
	Name    string
	Type    string
	Body    string
	// ByPerson is whether a person wrote it rather than the agent that owns it.
	ByPerson bool
}

// Create writes a new document, refusing a name the agent already uses.
func (s *DocumentStore) Create(ctx context.Context, params CreateParams) (*Document, error) {
	row, err := s.queries.InsertDocument(ctx, models.InsertDocumentParams{
		AgentID:  params.AgentID,
		Name:     params.Name,
		Type:     params.Type,
		Body:     params.Body,
		ByPerson: params.ByPerson,
	})
	if db.IsUniqueViolation(err) {
		return nil, s.nameTaken(ctx, params.AgentID, params.Name)
	}
	if unknownType(err) {
		return nil, fmt.Errorf("%w: %q", ErrUnknownType, params.Type)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to create document %q: %w", params.Name, err)
	}

	created := fromRow((*models.Document)(row))

	return &created, nil
}

type UpdateParams struct {
	ID      string
	AgentID string
	Name    string
	Type    string
	Body    string
	// Version is the version the rewrite replaces.
	Version int32
	// ByPerson is whether a person wrote it rather than the agent that owns it.
	ByPerson bool
}

// Update rewrites one of the agent's own documents, refusing a version it has moved past.
func (s *DocumentStore) Update(ctx context.Context, params UpdateParams) (*Document, error) {
	if !store.IsID(params.ID) {
		return nil, fmt.Errorf("%w: document %q", store.ErrNotFound, params.ID)
	}

	row, err := s.queries.UpdateDocument(ctx, models.UpdateDocumentParams{
		ID:          params.ID,
		AgentID:     params.AgentID,
		Name:        params.Name,
		Type:        params.Type,
		Body:        params.Body,
		ReadVersion: params.Version,
		ByPerson:    params.ByPerson,
	})
	if db.IsNoRows(err) {
		return nil, s.missedUpdate(ctx, params)
	}
	if db.IsUniqueViolation(err) {
		return nil, s.nameTaken(ctx, params.AgentID, params.Name)
	}
	if unknownType(err) {
		return nil, fmt.Errorf("%w: %q", ErrUnknownType, params.Type)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to update document %q: %w", params.ID, err)
	}

	updated := fromRow((*models.Document)(row))

	return &updated, nil
}

// missedUpdate is why a rewrite matched no row: a version already moved past, or no such document.
func (s *DocumentStore) missedUpdate(ctx context.Context, params UpdateParams) error {
	held, err := s.Get(ctx, params.ID)
	if err != nil {
		return err
	}
	if held.AgentID != params.AgentID {
		return fmt.Errorf("%w: document %q of agent %q", store.ErrNotFound, params.ID, params.AgentID)
	}

	return fmt.Errorf("%w: version %d was read, and it is at version %d, last written %s",
		ErrChanged, params.Version, held.Version, held.UpdatedAt.Format(time.RFC3339))
}

func unknownType(err error) bool {
	return db.IsForeignKeyViolation(err, "documents_type_fkey")
}

func (s *DocumentStore) nameTaken(ctx context.Context, agentID, name string) error {
	id, err := s.queries.GetDocumentIDByName(ctx, models.GetDocumentIDByNameParams{AgentID: agentID, Name: name})
	if err != nil {
		return fmt.Errorf("%w: the agent already has a document named %q", store.ErrNameTaken, name)
	}

	return fmt.Errorf("%w: the agent already has a document named %q, id %s", store.ErrNameTaken, name, id)
}

// Get returns one document whole.
func (s *DocumentStore) Get(ctx context.Context, id string) (*Document, error) {
	if !store.IsID(id) {
		return nil, fmt.Errorf("%w: document %q", store.ErrNotFound, id)
	}

	row, err := s.queries.GetDocument(ctx, id)
	if db.IsNoRows(err) {
		return nil, fmt.Errorf("%w: document %q", store.ErrNotFound, id)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get document %q: %w", id, err)
	}

	got := fromRow(row)

	return &got, nil
}

// Cursor is where a page of documents left off: when the last was written and its id.
type Cursor struct {
	UpdatedAt time.Time `json:"updated_at"`
	ID        string    `json:"id"`
}

// Validate refuses a cursor that names no row.
func (c Cursor) Validate() error {
	if !store.IsID(c.ID) {
		return fmt.Errorf("cursor id %q is not an id", c.ID)
	}

	return nil
}

// ListParams narrows a page of documents to one agent and one type when either is set.
type ListParams struct {
	AgentID *string
	Type    *string
	Limit   int32
	Cursor  *Cursor
}

// List returns a page of documents without their bodies, newest write first.
func (s *DocumentStore) List(ctx context.Context, params ListParams) ([]Document, error) {
	if params.AgentID != nil && !store.IsID(*params.AgentID) {
		return nil, nil
	}

	query := models.ListDocumentsParams{AgentID: params.AgentID, Type: params.Type, Lim: params.Limit}
	if params.Cursor != nil {
		query.CursorID = &params.Cursor.ID
		query.CursorUpdatedAt = params.Cursor.UpdatedAt
	}

	rows, err := s.queries.ListDocuments(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to list documents: %w", err)
	}

	return xslice.Map(rows, fromListRow), nil
}

// Version is one write of a document as it was written, with an empty Body when it came from a
// listing.
type Version struct {
	Version   int32
	Name      string
	Type      string
	Body      string
	ByPerson  bool
	CreatedAt time.Time
}

// ListVersions returns every write of a document, the newest first.
func (s *DocumentStore) ListVersions(ctx context.Context, documentID string) ([]Version, error) {
	if !store.IsID(documentID) {
		return nil, nil
	}

	rows, err := s.queries.ListDocumentVersions(ctx, documentID)
	if err != nil {
		return nil, fmt.Errorf("failed to list the versions of document %q: %w", documentID, err)
	}

	return xslice.Map(rows, func(row *models.ListDocumentVersionsRow) Version {
		return Version{
			Version:   row.Version,
			Name:      row.Name,
			Type:      row.Type,
			ByPerson:  row.ByPerson,
			CreatedAt: row.CreatedAt,
		}
	}), nil
}

// GetVersion returns one write of a document whole.
func (s *DocumentStore) GetVersion(ctx context.Context, documentID string, version int32) (*Version, error) {
	if !store.IsID(documentID) {
		return nil, fmt.Errorf("%w: document %q", store.ErrNotFound, documentID)
	}

	row, err := s.queries.GetDocumentVersion(ctx, models.GetDocumentVersionParams{DocumentID: documentID, Version: version})
	if db.IsNoRows(err) {
		return nil, fmt.Errorf("%w: version %d of document %q", store.ErrNotFound, version, documentID)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get version %d of document %q: %w", version, documentID, err)
	}

	return &Version{
		Version:   row.Version,
		Name:      row.Name,
		Type:      row.Type,
		Body:      row.Body,
		ByPerson:  row.ByPerson,
		CreatedAt: row.CreatedAt,
	}, nil
}

// Delete removes a document and every write of it.
func (s *DocumentStore) Delete(ctx context.Context, id string) error {
	if !store.IsID(id) {
		return fmt.Errorf("%w: document %q", store.ErrNotFound, id)
	}

	deleted, err := s.queries.DeleteDocument(ctx, id)
	if err != nil {
		return fmt.Errorf("failed to delete document %q: %w", id, err)
	}
	if deleted == 0 {
		return fmt.Errorf("%w: document %q", store.ErrNotFound, id)
	}

	return nil
}

// defaultSearchLimit is how many documents a search returns when it names no limit.
const defaultSearchLimit = 50

// SearchHit is one document whose body a search found, with the passage that matched.
type SearchHit struct {
	ID      string
	AgentID string
	Name    string
	Snippet string
	Rank    float64
}

// Results is what a search found: the documents holding every word, best first, then those
// holding each word only as part of a longer one.
type Results struct {
	Matches []SearchHit
	Near    []SearchHit
}

// SearchParams is a search of the documents' current bodies, narrowed to one agent and one type
// when either is set.
type SearchParams struct {
	Query   string
	AgentID *string
	Type    *string
	Limit   int32
}

// Search finds the documents whose current body holds the words of a query, and nothing when it
// wants no word.
func (s *DocumentStore) Search(ctx context.Context, params SearchParams) (*Results, error) {
	query := strings.ReplaceAll(params.Query, "\x00", "")

	terms := textsearchstore.Split(query)
	if len(terms.Wanted) == 0 {
		return &Results{}, nil
	}
	if params.AgentID != nil && !store.IsID(*params.AgentID) {
		return &Results{}, nil
	}

	if params.Limit <= 0 {
		params.Limit = defaultSearchLimit
	}

	wanted, excluded := textsearchstore.Quote(terms.Wanted, ""), textsearchstore.Quote(terms.Excluded, "-")

	rows, err := s.queries.SearchDocuments(ctx, models.SearchDocumentsParams{
		Wanted:   wanted,
		Excluded: excluded,
		Phrase:   textsearchstore.Phrase(terms.Wanted),
		Headline: textsearchstore.Headline,
		Lim:      params.Limit,
		AgentID:  params.AgentID,
		Type:     params.Type,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to search documents for %q: %w", query, err)
	}

	results := &Results{Matches: xslice.Map(rows, func(row *models.SearchDocumentsRow) SearchHit {
		return SearchHit{ID: row.ID, AgentID: row.AgentID, Name: row.Name, Snippet: row.Snippet, Rank: row.Rank}
	})}

	if int32(len(results.Matches)) >= params.Limit || !textsearchstore.HasNear(terms.Wanted) {
		return results, nil
	}

	near, err := s.queries.SearchDocumentsNear(ctx, models.SearchDocumentsNearParams{
		Terms:    terms.Wanted,
		Patterns: textsearchstore.LikePatterns(terms.Wanted),
		Wanted:   wanted,
		Excluded: excluded,
		Lead:     textsearchstore.Lead,
		Width:    textsearchstore.Width,
		Lim:      params.Limit - int32(len(results.Matches)),
		AgentID:  params.AgentID,
		Type:     params.Type,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to search documents near %q: %w", query, err)
	}

	results.Near = xslice.Map(near, func(row *models.SearchDocumentsNearRow) SearchHit {
		return SearchHit{ID: row.ID, AgentID: row.AgentID, Name: row.Name, Snippet: textsearchstore.Mark(row.Snippet, terms.Wanted), Rank: row.Rank}
	})

	return results, nil
}
