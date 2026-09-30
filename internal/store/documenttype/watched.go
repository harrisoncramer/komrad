package documenttype

import (
	"context"
	"fmt"

	"komrad/internal/watch"
	"komrad/lib/xslice"
	watchv1 "komrad/proto/gen/watch/v1"
)

// WatchedDocumentTypes declares the document types to the watch.
var WatchedDocumentTypes = watch.Resource{
	Enum:      watchv1.Resource_RESOURCE_DOCUMENT_TYPE,
	Table:     "document_types",
	KeyColumn: "id",
}

// ListWatched returns the document types among keys, or every one when keys is nil, by name.
func (s *DocumentTypeStore) ListWatched(ctx context.Context, keys []string) ([]Type, error) {
	rows, err := s.queries.ListWatchedDocumentTypes(ctx, keys)
	if err != nil {
		return nil, fmt.Errorf("failed to list the watched document types: %w", err)
	}

	return xslice.Map(rows, fromTypeRow), nil
}
