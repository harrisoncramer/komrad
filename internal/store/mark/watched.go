package mark

import (
	"context"
	"fmt"

	"komrade/internal/watch"
	"komrade/lib/xslice"
	watchv1 "komrade/proto/gen/watch/v1"
)

// WatchedMarks declares the marks to the watch.
var WatchedMarks = watch.Resource{
	Enum:      watchv1.Resource_RESOURCE_MARK,
	Table:     "marks",
	KeyColumn: "id",
}

// ListWatched returns whichever of keys are still marks, or every mark when keys is nil.
func (s *MarkStore) ListWatched(ctx context.Context, keys []string) ([]Mark, error) {
	rows, err := s.queries.ListWatchedMarks(ctx, keys)
	if err != nil {
		return nil, fmt.Errorf("failed to list watched marks: %w", err)
	}

	return xslice.Map(rows, fromRow), nil
}
