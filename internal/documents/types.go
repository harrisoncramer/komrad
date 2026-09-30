package documents

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"komrad/internal/store"
	documentstore "komrad/internal/store/document"
	documenttypestore "komrad/internal/store/documenttype"
	"komrad/lib/xslice"
)

// ListDocumentTypes returns every document type with its context, by name.
func (s *DocumentService) ListDocumentTypes(ctx context.Context) ([]documenttypestore.Type, error) {
	return s.types.List(ctx)
}

// GetDocumentType returns the type a name names, or as an earlier write left it when version names
// one, refusing an unknown name with the names that exist.
func (s *DocumentService) GetDocumentType(ctx context.Context, name string, version *int32) (*documenttypestore.Type, error) {
	got, err := s.types.GetByName(ctx, name)
	if errors.Is(err, store.ErrNotFound) {
		return nil, s.unknownType(ctx, name)
	}
	if err != nil || version == nil {
		return got, err
	}

	written, err := s.types.GetVersion(ctx, got.ID, *version)
	if err != nil {
		return nil, err
	}

	got.Name, got.Context = written.Name, written.Context
	got.Version, got.UpdatedAt = written.Version, written.CreatedAt

	return got, nil
}

func (s *DocumentService) unknownType(ctx context.Context, name string) error {
	types, err := s.types.List(ctx)
	if err != nil {
		return fmt.Errorf("%w: %q", documentstore.ErrUnknownType, name)
	}

	names := xslice.Map(types, func(t documenttypestore.Type) string { return t.Name })

	return fmt.Errorf("%w: %q, and the types are %s", documentstore.ErrUnknownType, name, strings.Join(names, ", "))
}

type WriteDocumentTypeParams struct {
	// ID is the type to rewrite, or nil to create one.
	ID *string
	// Version is the version a rewrite replaces, required with ID.
	Version *int32
	Name    string
	Context string
}

// WriteDocumentType creates a document type, or renames or rewrites the one ID names.
func (s *DocumentService) WriteDocumentType(ctx context.Context, params WriteDocumentTypeParams) (*documenttypestore.Type, error) {
	if params.ID == nil {
		return s.types.Create(ctx, params.Name, params.Context)
	}
	if params.Version == nil {
		return nil, ErrNoTypeVersion
	}

	return s.types.Update(ctx, documenttypestore.UpdateParams{
		ID:      *params.ID,
		Name:    params.Name,
		Context: params.Context,
		Version: *params.Version,
	})
}

// DeleteDocumentType removes a document type no document is written as.
func (s *DocumentService) DeleteDocumentType(ctx context.Context, id string) error {
	return s.types.Delete(ctx, id)
}

// ListDocumentTypeVersions returns every write of a document type, the newest first.
func (s *DocumentService) ListDocumentTypeVersions(ctx context.Context, id string) ([]documenttypestore.Version, error) {
	if _, err := s.types.Get(ctx, id); err != nil {
		return nil, err
	}

	return s.types.ListVersions(ctx, id)
}
