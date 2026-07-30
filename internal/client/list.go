package client

import (
	"context"
	"fmt"

	paginationv1 "claude-agents/proto/gen/pagination/v1"

	"google.golang.org/protobuf/proto"
)

const (
	pageSize = 200
	maxPages = 50
)

// fetchPage reads one page and reports the records on it alongside the token for
// the next, empty once the listing is exhausted. Each listing has its own request
// and response messages, so this is the only shape they have in common.
type fetchPage[T any] func(ctx context.Context, page *paginationv1.Pagination) ([]T, string, error)

// listAll follows a listing's page tokens to the end and returns everything they covered. Running out of pages is a failure rather than the end of the listing
func listAll[T any](ctx context.Context, what string, fetch fetchPage[T]) ([]T, error) {
	var (
		all    []T
		cursor string
	)

	for range maxPages {
		page := &paginationv1.Pagination{Limit: proto.Int32(pageSize)}
		if cursor != "" {
			page.Cursor = proto.String(cursor)
		}

		records, next, err := fetch(ctx, page)
		if err != nil {
			return nil, fmt.Errorf("failed to list %s: %w", what, err)
		}

		all = append(all, records...)
		if next == "" {
			return all, nil
		}

		cursor = next
	}

	return nil, fmt.Errorf("failed to list %s: still paging after %d pages", what, maxPages)
}
