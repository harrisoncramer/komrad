// Package document stores the markdown documents agents write for people to read.
package document

import (
	"context"
	"fmt"
	"time"

	"komrad/db"
	"komrad/db/models"
	"komrad/internal/fail"
	"komrad/internal/store"
	"komrad/lib/xslice"
)

// Kind is what sort of document it is.
type Kind string

const (
	KindRFC       Kind = Kind(models.DocumentKindRfc)
	KindReport    Kind = Kind(models.DocumentKindReport)
	KindExplainer Kind = Kind(models.DocumentKindExplainer)
	KindNotes     Kind = Kind(models.DocumentKindNotes)
)

// ErrChanged is a rewrite naming a version the document has already moved past.
var ErrChanged = fail.Precondition("the document has been written since that version was read")

// Document is one document, with an empty Body when it came from a listing.
type Document struct {
	ID        string
	AgentID   string
	Name      string
	Kind      Kind
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
		Kind:      Kind(row.Kind),
		Body:      row.Body,
		Version:   row.Version,
		CreatedAt: row.CreatedAt,
		UpdatedAt: row.UpdatedAt,
	}
}

func fromListRow(row *models.ListAgentDocumentsRow) Document {
	return Document{
		ID:        row.ID,
		AgentID:   row.AgentID,
		Name:      row.Name,
		Kind:      Kind(row.Kind),
		Version:   row.Version,
		CreatedAt: row.CreatedAt,
		UpdatedAt: row.UpdatedAt,
	}
}

type CreateParams struct {
	AgentID string
	Name    string
	Kind    Kind
	Body    string
}

// Create writes a new document, refusing a name the agent already uses.
func (s *DocumentStore) Create(ctx context.Context, params CreateParams) (*Document, error) {
	row, err := s.queries.InsertDocument(ctx, models.InsertDocumentParams{
		AgentID: params.AgentID,
		Name:    params.Name,
		Kind:    models.DocumentKind(params.Kind),
		Body:    params.Body,
	})
	if db.IsUniqueViolation(err) {
		return nil, s.nameTaken(ctx, params.AgentID, params.Name)
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
	Kind    Kind
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
		Kind:        models.DocumentKind(params.Kind),
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

// List returns an agent's documents without their bodies, newest write first.
func (s *DocumentStore) List(ctx context.Context, agentID string, kind *Kind) ([]Document, error) {
	if !store.IsID(agentID) {
		return nil, nil
	}

	var filter models.NullDocumentKind
	if kind != nil {
		filter = models.NullDocumentKind{DocumentKind: models.DocumentKind(*kind), Valid: true}
	}

	rows, err := s.queries.ListAgentDocuments(ctx, models.ListAgentDocumentsParams{AgentID: agentID, Kind: filter})
	if err != nil {
		return nil, fmt.Errorf("failed to list the documents of agent %q: %w", agentID, err)
	}

	return xslice.Map(rows, fromListRow), nil
}

// Version is one write of a document as it was written, with an empty Body when it came from a
// listing.
type Version struct {
	Version   int32
	Name      string
	Kind      Kind
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
			Kind:      Kind(row.Kind),
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
		Kind:      Kind(row.Kind),
		Body:      row.Body,
		ByPerson:  row.ByPerson,
		CreatedAt: row.CreatedAt,
	}, nil
}
