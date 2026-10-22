// Package document stores the markdown documents agents write for people to read.
package document

import (
	"context"
	"errors"
	"fmt"
	"time"

	"komrad/db"
	"komrad/db/models"
	"komrad/internal/fail"
	"komrad/internal/store"
	textsearchstore "komrad/internal/store/textsearch"
	"komrad/lib/xslice"

	"github.com/google/uuid"
)

// ErrUnknownType is a document naming a type that does not exist.
var ErrUnknownType = fail.InvalidArgument("no document type has that name")

// ErrChanged is a rewrite naming a version the document has already moved past.
var ErrChanged = fail.Precondition("the document has been written since that version was read")

// Document is one document, with an empty Body when it came from a listing.
type Document struct {
	ID        uuid.UUID
	AgentID   uuid.UUID
	TaskID    *uuid.UUID
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
		TaskID:    row.TaskID,
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
		TaskID:    row.TaskID,
		Name:      row.Name,
		Type:      row.Type,
		Version:   row.Version,
		CreatedAt: row.CreatedAt,
		UpdatedAt: row.UpdatedAt,
	}
}

type CreateParams struct {
	AgentID uuid.UUID
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
	ID      uuid.UUID
	AgentID uuid.UUID
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

func (s *DocumentStore) nameTaken(ctx context.Context, agentID uuid.UUID, name string) error {
	id, err := s.queries.GetDocumentIDByName(ctx, models.GetDocumentIDByNameParams{AgentID: agentID, Name: name})
	if err != nil {
		return fmt.Errorf("%w: the agent already has a document named %q", store.ErrNameTaken, name)
	}

	return fmt.Errorf("%w: the agent already has a document named %q, id %s", store.ErrNameTaken, name, id)
}

// Get returns one document whole.
func (s *DocumentStore) Get(ctx context.Context, id uuid.UUID) (*Document, error) {
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
	ID        uuid.UUID `json:"id"`
}

// Validate refuses a cursor that names no row.
func (c Cursor) Validate() error {
	if c.ID == uuid.Nil {
		return errors.New("cursor names no row")
	}

	return nil
}

// ListParams narrows a page of documents to one agent, one task and one type when any is set.
type ListParams struct {
	AgentID *uuid.UUID
	TaskID  *uuid.UUID
	Type    *string
	Limit   int32
	Cursor  *Cursor
}

// List returns a page of documents without their bodies, newest write first.
func (s *DocumentStore) List(ctx context.Context, params ListParams) ([]Document, error) {
	query := models.ListDocumentsParams{
		AgentID: params.AgentID,
		TaskID:  params.TaskID,
		Type:    params.Type,
		Lim:     params.Limit,
	}
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
func (s *DocumentStore) ListVersions(ctx context.Context, documentID uuid.UUID) ([]Version, error) {
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
func (s *DocumentStore) GetVersion(ctx context.Context, documentID uuid.UUID, version int32) (*Version, error) {
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
func (s *DocumentStore) Delete(ctx context.Context, id uuid.UUID) error {
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
	ID      uuid.UUID
	AgentID uuid.UUID
	Name    string
	Snippet string
}

// Results holds the documents with every word of a search, best first, and those with each word only inside a longer one.
type Results struct {
	Matches []SearchHit
	Near    []SearchHit
}

// SearchParams is a search of the documents' current bodies, narrowed to one agent and one type
// when either is set.
type SearchParams struct {
	Query   string
	AgentID *uuid.UUID
	Type    *string
	Limit   int32
}

// Search finds the documents whose current body holds the words of a query, and nothing when it
// wants no word.
func (s *DocumentStore) Search(ctx context.Context, params SearchParams) (*Results, error) {
	prepared := textsearchstore.Prepare(params.Query)
	if len(prepared.Terms.Wanted) == 0 {
		return &Results{}, nil
	}

	if params.Limit <= 0 {
		params.Limit = defaultSearchLimit
	}

	rows, err := s.queries.SearchDocuments(ctx, models.SearchDocumentsParams{
		Wanted:   prepared.Wanted,
		Excluded: prepared.Excluded,
		Phrase:   textsearchstore.Phrase(prepared.Terms.Wanted),
		Headline: textsearchstore.Headline,
		Lim:      params.Limit,
		AgentID:  params.AgentID,
		Type:     params.Type,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to search documents for %q: %w", prepared.Query, err)
	}

	results := &Results{Matches: xslice.Map(rows, func(row *models.SearchDocumentsRow) SearchHit {
		return searchHit(row.ID, row.AgentID, row.Name, row.Snippet)
	})}

	if int32(len(results.Matches)) >= params.Limit || !textsearchstore.HasNear(prepared.Terms.Wanted) {
		return results, nil
	}

	near, err := s.queries.SearchDocumentsNear(ctx, models.SearchDocumentsNearParams{
		Terms:    prepared.Terms.Wanted,
		Patterns: textsearchstore.LikePatterns(prepared.Terms.Wanted),
		Wanted:   prepared.Wanted,
		Excluded: prepared.Excluded,
		Lead:     textsearchstore.SnippetLead,
		Width:    textsearchstore.SnippetWidth,
		Lim:      params.Limit - int32(len(results.Matches)),
		AgentID:  params.AgentID,
		Type:     params.Type,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to search documents near %q: %w", prepared.Query, err)
	}

	results.Near = xslice.Map(near, func(row *models.SearchDocumentsNearRow) SearchHit {
		return searchHit(row.ID, row.AgentID, row.Name, textsearchstore.Mark(row.Snippet, prepared.Terms.Wanted))
	})

	return results, nil
}

// searchHit is the hit for one document a search found, with its snippet.
func searchHit(id uuid.UUID, agentID uuid.UUID, name string, snippet string) SearchHit {
	return SearchHit{
		ID:      id,
		AgentID: agentID,
		Name:    name,
		Snippet: snippet,
	}
}
