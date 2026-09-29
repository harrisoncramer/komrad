// Package client holds the calls the panel and the CLI make that are more than one request,
// which is paging a listing to its end so the caller gets every agent, workflow or schedule
// rather than the first page of them.
package client

import (
	"context"
	"fmt"

	"komrad/lib/xptr"
	paginationv1 "komrad/proto/gen/pagination/v1"
)

// pageSize is the server's own cap, so a page comes back whole rather than clamped to a size the
// caller did not choose.
const (
	pageSize = 100
	maxPages = 50
)

type fetchPage[T any] func(ctx context.Context, page *paginationv1.Pagination) ([]T, string, error)

func listAll[T any](ctx context.Context, fetch fetchPage[T]) ([]T, error) {
	var (
		all    []T
		cursor string
	)

	for range maxPages {
		page := &paginationv1.Pagination{Limit: xptr.Ptr[int32](pageSize)}
		if cursor != "" {
			page.Cursor = xptr.Ptr(cursor)
		}

		records, next, err := fetch(ctx, page)
		if err != nil {
			return nil, fmt.Errorf("failed to list: %w", err)
		}

		all = append(all, records...)
		if next == "" {
			return all, nil
		}

		cursor = next
	}

	return nil, fmt.Errorf("failed to list: still paging after %d pages", maxPages)
}
