package mark

import (
	"context"

	"weave/internal/watch"
	watchv1 "weave/proto/gen/watch/v1"
)

// WatchedMarks declares the marks to the watch.
var WatchedMarks = watch.Resource{
	Enum:      watchv1.Resource_RESOURCE_MARK,
	Table:     "marks",
	KeyColumn: "id",
}

// ListWatched returns whichever of keys are still marks, or every mark when keys is nil.
func (s *MarkStore) ListWatched(ctx context.Context, keys []string) ([]Mark, error) {
	if keys == nil {
		return s.List(ctx)
	}

	return s.ListByKey(ctx, keys)
}
