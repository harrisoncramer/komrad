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

// GetDocumentType returns the type a name names, refusing an unknown one with the names that exist.
func (s *DocumentService) GetDocumentType(ctx context.Context, name string) (*documenttypestore.Type, error) {
	got, err := s.types.GetByName(ctx, name)
	if !errors.Is(err, store.ErrNotFound) {
		return got, err
	}

	types, err := s.types.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("%w: %q", documentstore.ErrUnknownType, name)
	}

	names := xslice.Map(types, func(t documenttypestore.Type) string { return t.Name })

	return nil, fmt.Errorf("%w: %q, and the types are %s", documentstore.ErrUnknownType, name, strings.Join(names, ", "))
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
