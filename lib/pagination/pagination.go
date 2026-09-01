// Package pagination turns a request's page token and limit into the cursor and bound a
// query runs with, and turns the records that come back into the token for the next page.
//
// A resource opts in by implementing Cursorable, returning the cursor that locates it among
// its peers, and the handler that lists it holds a Paginator over that pair. The token is an
// opaque base64 of the JSON-encoded cursor, so callers hand it back verbatim and the shape
// inside it stays the server's business. A cursor type says through Validate what it will
// take back, so a token holding something the query cannot use is refused here rather than by
// the database.
package pagination

import (
	"encoding/base64"
	"encoding/json"
	"fmt"

	paginationv1 "weave/proto/gen/pagination/v1"

	"connectrpc.com/connect"
)

// Cursor is a decoded cursor that can say whether it holds a position this server could
// have handed out.
type Cursor interface {
	Validate() error
}

// Cursorable is a resource that can say where it sits in a listing, where C is the sort key
// the query orders by plus an id to break ties.
type Cursorable[C Cursor] interface {
	Cursor() (C, error)
}

// Paginator carries the decoded request cursor and the clamped limit for one
// listing over resources of type M.
type Paginator[C Cursor, M Cursorable[C]] struct {
	limit  int64
	cursor *C
}

// The limits a Paginator applies, so a caller that asks for nothing gets a readable page
// and one that asks for too much is capped.
const (
	defaultLimit int64 = 50
	maxLimit     int64 = 500
)

// New builds a Paginator from a request's pagination, which may be nil for the first page
// at the default size.
func New[C Cursor, M Cursorable[C]](p *paginationv1.Pagination) (*Paginator[C, M], error) {
	cursor, err := decode[C](p.GetCursor())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid pagination cursor: %w", err))
	}

	limit := int64(p.GetLimit())
	switch {
	case limit <= 0:
		limit = defaultLimit
	case limit > maxLimit:
		limit = maxLimit
	}

	return &Paginator[C, M]{limit: limit, cursor: cursor}, nil
}

// Limit is how many records the query should fetch.
func (p *Paginator[C, M]) Limit() int64 { return p.limit }

// Cursor is the decoded request cursor, nil on the first page.
func (p *Paginator[C, M]) Cursor() *C { return p.cursor }

// NextToken returns the token for the page after these records, or nil when there is none.
func (p *Paginator[C, M]) NextToken(resources []M) (*string, error) {
	// A page short of the limit is the last one, so a page landing exactly on the limit costs
	// the caller one empty page rather than a count query on every request.
	if int64(len(resources)) < p.limit {
		return nil, nil
	}

	cursor, err := resources[len(resources)-1].Cursor()
	if err != nil {
		return nil, fmt.Errorf("failed to read cursor: %w", err)
	}

	return Token(cursor)
}

// Token encodes a cursor a handler has built itself, which a listing of more than one
// section needs, since its next page is not located by the last record alone.
func Token[C Cursor](cursor C) (*string, error) {
	raw, err := json.Marshal(cursor)
	if err != nil {
		return nil, fmt.Errorf("failed to encode cursor: %w", err)
	}

	token := base64.URLEncoding.EncodeToString(raw)

	return &token, nil
}

// decode unpacks a base64 JSON token into a cursor, or nil when the token is empty.
func decode[C Cursor](token string) (*C, error) {
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

	if err := cursor.Validate(); err != nil {
		return nil, err
	}

	return &cursor, nil
}
