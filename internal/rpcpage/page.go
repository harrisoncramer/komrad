// Package rpcpage runs one page of a keyset listing for a handler.
package rpcpage

import (
	"komrad/internal/fail"
	"komrad/internal/rpcerr"
	"komrad/lib/pagination"
	paginationv1 "komrad/proto/gen/pagination/v1"
)

// page is one page of a listing and the token for the page after it.
type page[M any] struct {
	Resources []M
	Next      *string
}

// List reads the page a request's pagination names and mints the token for the next one.
func List[C pagination.Cursor, M pagination.Cursorable[C]](request *paginationv1.Pagination, read func(limit int64, cursor *C) ([]M, *fail.Error)) (*page[M], *fail.Error) {
	paginator, err := pagination.New[C, M](request)
	if err != nil {
		return nil, rpcerr.From(err, "failed to read the pagination")
	}

	resources, failure := read(paginator.Limit(), paginator.Cursor())
	if failure != nil {
		return nil, failure
	}

	var next *string
	if paginator.HasNext(resources) {
		if next, err = paginator.NextToken(resources); err != nil {
			return nil, rpcerr.From(err, "failed to build the next page token")
		}
	}

	return &page[M]{Resources: resources, Next: next}, nil
}
