// Package pagination turns a request's page token and limit into the cursor and
// bound a query runs with, and turns the records that come back into the token
// for the next page.
//
// Every listing pages the same way. A resource opts in by implementing
// Cursorable, returning the cursor that locates it among its peers, and the
// handler that lists it holds a Paginator over that pair. Nothing else about the
// resource matters here, so a new list endpoint reuses this rather than growing
// its own token format.
//
// The token is an opaque base64 of the JSON-encoded cursor. Callers hand it back
// verbatim; the shape inside it is the server's business and may change.
package pagination

import (
	"encoding/base64"
	"encoding/json"
	"fmt"

	paginationv1 "claude-agents/proto/gen/pagination/v1"

	"connectrpc.com/connect"
)

// Cursorable is a resource that can say where it sits in a listing. C is the
// cursor type: whatever the query orders by, which is a sort key plus an id for
// anything whose sort key can tie.
type Cursorable[C any] interface {
	Cursor() (C, error)
}

// Paginator carries the decoded request cursor and the clamped limit for one
// listing over resources of type M.
type Paginator[C any, M Cursorable[C]] struct {
	limit  int64
	cursor *C
}

// New builds a Paginator from a request's pagination. A nil pagination is the
// first page at the default size, so a caller that omits the field entirely is
// served rather than refused. A malformed cursor is the caller's mistake and
// comes back as InvalidArgument; a limit past the ceiling is clamped, since a
// caller asking for too much still wants a page.
func New[C any, M Cursorable[C]](p *paginationv1.Pagination, opts ...Option) (*Paginator[C, M], error) {
	cfg := &options{defaultLimit: defaultLimit, maxLimit: maxLimit}
	for _, opt := range opts {
		opt(cfg)
	}

	cursor, err := decode[C](p.GetCursor())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid pagination cursor: %w", err))
	}

	limit := int64(p.GetLimit())
	switch {
	case limit <= 0:
		limit = cfg.defaultLimit
	case limit > cfg.maxLimit:
		limit = cfg.maxLimit
	}

	return &Paginator[C, M]{limit: limit, cursor: cursor}, nil
}

// Limit is how many records the query should fetch.
func (p *Paginator[C, M]) Limit() int64 { return p.limit }

// Cursor is the decoded request cursor, nil on the first page.
func (p *Paginator[C, M]) Cursor() *C { return p.cursor }

// NextToken returns the token for the page after these records, or nil when
// there is none. A page short of the limit is the last one, since the query
// would have filled it had more rows matched. A page exactly at the limit
// returns a token even when it happens to be the last, which costs the caller
// one empty page rather than a count query on every request.
func (p *Paginator[C, M]) NextToken(resources []M) (*string, error) {
	if int64(len(resources)) < p.limit {
		return nil, nil
	}

	cursor, err := resources[len(resources)-1].Cursor()
	if err != nil {
		return nil, fmt.Errorf("failed to read cursor: %w", err)
	}

	raw, err := json.Marshal(cursor)
	if err != nil {
		return nil, fmt.Errorf("failed to encode cursor: %w", err)
	}

	token := base64.URLEncoding.EncodeToString(raw)
	return &token, nil
}

// decode unpacks a base64 JSON token into a cursor, or nil when the token is
// empty.
func decode[C any](token string) (*C, error) {
	if token == "" {
		return nil, nil
	}

	raw, err := base64.URLEncoding.DecodeString(token)
	if err != nil {
		return nil, fmt.Errorf("failed to decode page token: %w", err)
	}

	var cursor C
	if err := json.Unmarshal(raw, &cursor); err != nil {
		return nil, fmt.Errorf("failed to unmarshal page token: %w", err)
	}

	return &cursor, nil
}
