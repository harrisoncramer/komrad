// Package documenttype stores the sorts of document a person defines and the context agents read for each.
package documenttype

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

var (
	// ErrChanged is a rewrite naming a version the type has already moved past.
	ErrChanged = fail.Precondition("the document type has been written since that version was read")
	// ErrInUse is a delete of a type that documents are still written as.
	ErrInUse = fail.Precondition("documents are still written as that type")
)

// Type is one sort of document and the context an agent reads before writing one.
type Type struct {
	ID        string
	Name      string
	Context   string
	Version   int32
	CreatedAt time.Time
	UpdatedAt time.Time
}

type DocumentTypeStore struct {
	queries *models.Queries
}

// NewDocumentTypeStore builds a document type store over a pool or transaction.
func NewDocumentTypeStore(db store.DBTX) *DocumentTypeStore {
	return &DocumentTypeStore{queries: models.New(db)}
}

func fromTypeRow(row *models.DocumentType) Type {
	return Type{
		ID:        row.ID,
		Name:      row.Name,
		Context:   row.Context,
		Version:   row.Version,
		CreatedAt: row.CreatedAt,
		UpdatedAt: row.UpdatedAt,
	}
}

// List returns every document type, by name.
func (s *DocumentTypeStore) List(ctx context.Context) ([]Type, error) {
	rows, err := s.queries.ListDocumentTypes(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list document types: %w", err)
	}

	return xslice.Map(rows, fromTypeRow), nil
}

// Get returns one document type by id.
func (s *DocumentTypeStore) Get(ctx context.Context, id string) (*Type, error) {
	if !store.IsID(id) {
		return nil, fmt.Errorf("%w: document type %q", store.ErrNotFound, id)
	}

	row, err := s.queries.GetDocumentType(ctx, id)
	if db.IsNoRows(err) {
		return nil, fmt.Errorf("%w: document type %q", store.ErrNotFound, id)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get document type %q: %w", id, err)
	}

	got := fromTypeRow(row)

	return &got, nil
}

// GetByName returns one document type by its name.
func (s *DocumentTypeStore) GetByName(ctx context.Context, name string) (*Type, error) {
	row, err := s.queries.GetDocumentTypeByName(ctx, name)
	if db.IsNoRows(err) {
		return nil, fmt.Errorf("%w: document type %q", store.ErrNotFound, name)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get document type %q: %w", name, err)
	}

	got := fromTypeRow(row)

	return &got, nil
}

// Create writes a new document type, refusing a name already used.
func (s *DocumentTypeStore) Create(ctx context.Context, name, typeContext string) (*Type, error) {
	row, err := s.queries.InsertDocumentType(ctx, models.InsertDocumentTypeParams{Name: name, Context: typeContext})
	if db.IsUniqueViolation(err) {
		return nil, fmt.Errorf("%w: a document type is already named %q", store.ErrNameTaken, name)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to create document type %q: %w", name, err)
	}

	created := fromTypeRow((*models.DocumentType)(row))

	return &created, nil
}

type UpdateParams struct {
	ID      string
	Name    string
	Context string
	// Version is the version the rewrite replaces.
	Version int32
}

// Update renames or rewrites a document type, refusing a version it has moved past.
func (s *DocumentTypeStore) Update(ctx context.Context, params UpdateParams) (*Type, error) {
	if !store.IsID(params.ID) {
		return nil, fmt.Errorf("%w: document type %q", store.ErrNotFound, params.ID)
	}

	row, err := s.queries.UpdateDocumentType(ctx, models.UpdateDocumentTypeParams{
		ID:          params.ID,
		Name:        params.Name,
		Context:     params.Context,
		ReadVersion: params.Version,
	})
	if db.IsNoRows(err) {
		held, err := s.Get(ctx, params.ID)
		if err != nil {
			return nil, err
		}

		return nil, fmt.Errorf("%w: version %d was read, and it is at version %d, last written %s",
			ErrChanged, params.Version, held.Version, held.UpdatedAt.Format(time.RFC3339))
	}
	if db.IsUniqueViolation(err) {
		return nil, fmt.Errorf("%w: a document type is already named %q", store.ErrNameTaken, params.Name)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to update document type %q: %w", params.ID, err)
	}

	updated := fromTypeRow((*models.DocumentType)(row))

	return &updated, nil
}

// Delete removes a document type, refusing one that documents are still written as.
func (s *DocumentTypeStore) Delete(ctx context.Context, id string) error {
	held, err := s.Get(ctx, id)
	if err != nil {
		return err
	}

	deleted, err := s.queries.DeleteDocumentType(ctx, id)
	if db.IsForeignKeyViolation(err, "documents_type_fkey") {
		count, err := s.queries.CountDocumentsOfType(ctx, held.Name)
		if err != nil {
			return fmt.Errorf("%w: %q", ErrInUse, held.Name)
		}

		return fmt.Errorf("%w: %d documents are %q", ErrInUse, count, held.Name)
	}
	if err != nil {
		return fmt.Errorf("failed to delete document type %q: %w", id, err)
	}
	if deleted == 0 {
		return fmt.Errorf("%w: document type %q", store.ErrNotFound, id)
	}

	return nil
}

// Version is one write of a document type as it was written, with an empty Context when it came
// from a listing.
type Version struct {
	Version   int32
	Name      string
	Context   string
	CreatedAt time.Time
}

// ListVersions returns every write of a document type, the newest first.
func (s *DocumentTypeStore) ListVersions(ctx context.Context, documentTypeID string) ([]Version, error) {
	if !store.IsID(documentTypeID) {
		return nil, nil
	}

	rows, err := s.queries.ListDocumentTypeVersions(ctx, documentTypeID)
	if err != nil {
		return nil, fmt.Errorf("failed to list the versions of document type %q: %w", documentTypeID, err)
	}

	return xslice.Map(rows, func(row *models.ListDocumentTypeVersionsRow) Version {
		return Version{
			Version:   row.Version,
			Name:      row.Name,
			CreatedAt: row.CreatedAt,
		}
	}), nil
}

// GetVersion returns one write of a document type whole.
func (s *DocumentTypeStore) GetVersion(ctx context.Context, documentTypeID string, version int32) (*Version, error) {
	if !store.IsID(documentTypeID) {
		return nil, fmt.Errorf("%w: document type %q", store.ErrNotFound, documentTypeID)
	}

	row, err := s.queries.GetDocumentTypeVersion(ctx, models.GetDocumentTypeVersionParams{DocumentTypeID: documentTypeID, Version: version})
	if db.IsNoRows(err) {
		return nil, fmt.Errorf("%w: version %d of document type %q", store.ErrNotFound, version, documentTypeID)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get version %d of document type %q: %w", version, documentTypeID, err)
	}

	return &Version{
		Version:   row.Version,
		Name:      row.Name,
		Context:   row.Context,
		CreatedAt: row.CreatedAt,
	}, nil
}
